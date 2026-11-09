package server

// 手机扫码接入：桌面端在侧边栏底部展示"手机扫码访问"入口，弹出面板渲染
// 局域网地址二维码，手机扫码即可在同一局域网的浏览器里打开本面板。
//
// 可选开启"扫码免密登录"（对应面板上的开关）：开启后桌面端生成一次性配对码
// 并编码进二维码，手机打开链接后自动兑换正式会话，无需手动输密码。
// 安全约束：
//   - 配对码只在内存里保存，不落盘；进程重启即全部失效；
//   - 单次有效（兑换即删除）、5 分钟过期；
//   - 必须先在已登录的面板上显式打开开关，开关默认关闭；
//   - 兑换接口走登录限流器（loginRateLimiter）防在线爆破。

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"
)

// pairTokenTTL 是配对码的有效期。扫码 → 浏览器打开 → 自动兑换通常几秒内
// 完成，5 分钟已足够宽裕；过期后面板上点"刷新"重新生成即可。
const pairTokenTTL = 5 * time.Minute

// lanIPv4 返回本机第一个非回环 IPv4 地址（用于拼局域网访问 URL）。
// 找不到（纯 IPv6 环境或无网卡）返回空串，前端据此提示"未检测到局域网地址"。
func lanIPv4() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok || ipNet.IP.IsLoopback() {
			continue
		}
		if ip4 := ipNet.IP.To4(); ip4 != nil {
			return ip4.String()
		}
	}
	return ""
}

// accessBaseURL 返回局域网访问根地址（不含路径）。端口取 Start 注入的值，
// 尚未启动时（部分测试场景）退回 0，前端会展示无端口地址并在扫码前看到提示。
func (s *Server) accessBaseURL() string {
	s.pairMu.Lock()
	port := s.accessPort
	s.pairMu.Unlock()
	ip := lanIPv4()
	if ip == "" {
		return ""
	}
	if port > 0 {
		return fmt.Sprintf("http://%s:%d", ip, port)
	}
	return fmt.Sprintf("http://%s", ip)
}

// pairingEnabledLocked 读取开关状态。调用方必须已持有 pairMu。
func (s *Server) pairingEnabledLocked() bool {
	return s.pairingEnabled
}

// handleAccessInfo 返回局域网访问信息与扫码免密开关状态（需登录）。
func (s *Server) handleAccessInfo(w http.ResponseWriter, r *http.Request) {
	s.pairMu.Lock()
	enabled := s.pairingEnabledLocked()
	s.pairMu.Unlock()

	jsonResponse(w, map[string]interface{}{
		"url":             s.accessBaseURL(),
		"has_lan":         lanIPv4() != "",
		"pairing_enabled": enabled,
	})
}

// handleAccessPairing 设置"扫码免密登录"开关（需登录）。
func (s *Server) handleAccessPairing(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Enabled == nil {
		jsonError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	s.pairMu.Lock()
	s.pairingEnabled = *req.Enabled
	// 关开关时顺手清掉所有未兑换的配对码，避免"看起来关了、旧码还能用"。
	if !s.pairingEnabled {
		s.pairTokens = nil
	}
	s.pairMu.Unlock()

	jsonResponse(w, map[string]bool{"ok": true})
}

// handleAccessPair 生成一个一次性配对码，并把完整登录 URL 一并返回（需登录）。
func (s *Server) handleAccessPair(w http.ResponseWriter, r *http.Request) {
	s.pairMu.Lock()
	if !s.pairingEnabledLocked() {
		s.pairMu.Unlock()
		jsonError(w, http.StatusBadRequest, "pairing is disabled")
		return
	}
	// 懒清理：每次签发前把过期码扫掉，避免长期运行下的 map 膨胀。
	now := time.Now()
	for tok, exp := range s.pairTokens {
		if now.After(exp) {
			delete(s.pairTokens, tok)
		}
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		s.pairMu.Unlock()
		jsonError(w, http.StatusInternalServerError, "failed to generate pair token")
		return
	}
	tok := hex.EncodeToString(buf)
	if s.pairTokens == nil {
		s.pairTokens = make(map[string]time.Time)
	}
	s.pairTokens[tok] = now.Add(pairTokenTTL)
	s.pairMu.Unlock()

	base := s.accessBaseURL()
	if base == "" {
		jsonError(w, http.StatusServiceUnavailable, "no LAN address available")
		return
	}
	jsonResponse(w, map[string]interface{}{
		"ok":         true,
		"url":        base + "/#/login?pair=" + tok,
		"expires_in": int(pairTokenTTL.Seconds()),
	})
}

// handleAccessPairRedeem 用配对码兑换正式会话（免登录通道，刻意不套 requireAuth）。
// 失败统一回 400 而不是 401：前端 request 层把 401 当"会话失效"做全局登出跳转，
// 配对码错误不该触发那套语义。
func (s *Server) handleAccessPairRedeem(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !s.loginLimiter.allow(ip) {
		jsonError(w, http.StatusTooManyRequests, "too many attempts, please try again later")
		return
	}

	s.authMu.RLock()
	configured := s.authToken != ""
	s.authMu.RUnlock()
	if !configured {
		jsonError(w, http.StatusNotFound, "auth not configured")
		return
	}

	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Token == "" {
		jsonError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	s.pairMu.Lock()
	exp, ok := s.pairTokens[req.Token]
	if ok {
		// 单次有效：无论兑换成败都先删掉，防重放。
		delete(s.pairTokens, req.Token)
	}
	enabled := s.pairingEnabledLocked()
	s.pairMu.Unlock()

	if !enabled || !ok || time.Now().After(exp) {
		jsonError(w, http.StatusBadRequest, "invalid or expired pair token")
		return
	}

	sessionToken := s.sessions.create(false)
	if sessionToken == "" {
		jsonError(w, http.StatusInternalServerError, "failed to create session")
		return
	}
	s.loginLimiter.reset(ip)
	jsonResponse(w, map[string]interface{}{
		"ok":    true,
		"token": sessionToken,
	})
}
