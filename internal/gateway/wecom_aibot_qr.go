package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/magicwubiao/go-magic/pkg/config"
	"github.com/magicwubiao/go-magic/pkg/log"
)

// WeCom AI Bot 扫码创建（官方「一键创建智能机器人」）。
//
// 流程：
//  1. GET https://work.weixin.qq.com/ai/qc/generate
//     -> data = {scode, auth_url}；auth_url 即二维码内容，用户用企业微信 App
//     扫码后在 App 内确认；
//  2. GET https://work.weixin.qq.com/ai/qc/query_result?scode=...
//     -> data.status 由 init/wait -> scaned -> success；success 时
//     data.bot_info={botid, secret}
//  3. 把 botid/secret 写入 config.json（gateway.platforms.wecom:
//     mode="aibot"）。确认后 server 端通过 SetWeComConfirmedHook 自动重启
//     gateway 使新凭据生效（gateway 只在启动时读取一次凭据），无需手动重启。
//
// 端点只接受 GET（实测 2026-09-02：POST 或带 ?source/plat 参数均返回 404「请求的
// 网页不存在」或空响应），无需登录 cookie 与请求体；URL 实现成包级变量以便单测替换。

var (
	wecomAIQRGenerateURL = "https://work.weixin.qq.com/ai/qc/generate"
	wecomAIQRPollURL     = "https://work.weixin.qq.com/ai/qc/query_result"
	wecomAIQRHTTPTimeout = 15 * time.Second
	wecomAIQRPollTick    = 3 * time.Second // 扫码轮询间隔
	// wecomAIQRUserAgent 伪装浏览器 UA，规避企微风控对脚本 UA 的空响应/拦截。
	wecomAIQRUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
)

// wecomQRGenerateResp 是 generate 接口的响应（字段嵌在 data 下）。
type wecomQRGenerateResp struct {
	Data struct {
		Scode   string `json:"scode"`
		AuthURL string `json:"auth_url"`
	} `json:"data"`
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

// wecomQRPollResp 是 query_result 接口的响应。
type wecomQRPollResp struct {
	Data struct {
		Status  string `json:"status"` // wait / scaned / success / expired / fail
		BotInfo struct {
			BotID  string `json:"botid"`
			Secret string `json:"secret"`
		} `json:"bot_info"`
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	} `json:"data"`
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

// GenerateWeComAIBotQR 请求一个新的扫码会话，返回二维码内容(auth_url)与 scode。
// scode 用于后续轮询 query_result。
func GenerateWeComAIBotQR(ctx context.Context) (authURL, scode string, err error) {
	client := &http.Client{Timeout: wecomAIQRHTTPTimeout}

	// 官方端点只接受裸 GET：POST 或携带 source/plat 等 query 参数会返回
	// 404「请求的网页不存在」或空响应，因此这里不设置 body 与查询串。
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, wecomAIQRGenerateURL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", wecomAIQRUserAgent)

	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("wecom generate status=%d body=%s", resp.StatusCode, truncateForLog(raw))
	}

	var out wecomQRGenerateResp
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", "", fmt.Errorf("wecom generate parse: %w body=%s", err, truncateForLog(raw))
	}
	if out.Data.AuthURL == "" || out.Data.Scode == "" {
		return "", "", fmt.Errorf("wecom generate missing auth_url/scode: errcode=%d errmsg=%s body=%s",
			out.ErrCode, out.ErrMsg, truncateForLog(raw))
	}
	return out.Data.AuthURL, out.Data.Scode, nil
}

// PollWeComAIBotQR 轮询一次扫码结果。
// 返回状态：wait/scaned/expired/success；success 时附带 botID/secret。
func PollWeComAIBotQR(ctx context.Context, scode string) (status, botID, secret string, err error) {
	client := &http.Client{Timeout: wecomAIQRHTTPTimeout}

	u := wecomAIQRPollURL + "?scode=" + url.QueryEscape(scode)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", "", "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", "", "", fmt.Errorf("wecom poll status=%d body=%s", resp.StatusCode, truncateForLog(raw))
	}

	var out wecomQRPollResp
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", "", "", fmt.Errorf("wecom poll parse: %w body=%s", err, truncateForLog(raw))
	}
	if out.ErrCode != 0 || out.Data.ErrCode != 0 {
		msg := out.ErrMsg
		if msg == "" {
			msg = out.Data.ErrMsg
		}
		return "", "", "", fmt.Errorf("wecom poll errcode=%d errmsg=%s", out.ErrCode, msg)
	}

	switch out.Data.Status {
	case "success":
		if out.Data.BotInfo.BotID == "" || out.Data.BotInfo.Secret == "" {
			return "", "", "", fmt.Errorf("wecom success but missing bot_info")
		}
		return "success", out.Data.BotInfo.BotID, out.Data.BotInfo.Secret, nil
	case "expired":
		return "expired", "", "", nil
	case "wait", "scaned":
		return out.Data.Status, "", "", nil
	default:
		log.Debugf("[WeCom/AIBot] Unknown poll status: %s", out.Data.Status)
		return "wait", "", "", nil
	}
}

// pollWeComAIBotStatus 被 QRCodeManager 的轮询器调用：成功时把凭据写入
// config.json 并返回 confirmed。
func pollWeComAIBotStatus(ctx context.Context, session *QRCodeSession) (string, error) {
	scode, _ := session.Metadata["scode"].(string)
	if scode == "" {
		// 兼容旧会话：无 scode 无法轮询
		return session.Status, nil
	}
	status, botID, secret, err := PollWeComAIBotQR(ctx, scode)
	if err != nil {
		return "", err
	}
	switch status {
	case "success":
		if err := saveWeComBotCredentials(botID, secret); err != nil {
			log.Errorf("[WeCom/AIBot] Failed to save credentials: %v", err)
			return "error", err
		}
		log.Infof("[WeCom/AIBot] QR login confirmed, saved bot_id=%s", botID)
		return "confirmed", nil
	case "scaned":
		return "scanning", nil
	case "expired":
		return "expired", nil
	default:
		return "pending", nil
	}
}

// saveWeComBotCredentials 把 AI Bot 凭据写入 config.json 的
// gateway.platforms.wecom（mode=aibot, bot_id, secret, enabled）。
// 同时清理自建应用遗留字段（corp_id/agent_id，该模式已移除）。
func saveWeComBotCredentials(botID, secret string) error {
	if botID == "" || secret == "" {
		return fmt.Errorf("empty bot credentials")
	}

	configPath := filepath.Join(config.GetMagicHome(), "config.json")
	var cfg map[string]interface{}

	if data, err := os.ReadFile(configPath); err == nil {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return fmt.Errorf("parse config: %w", err)
		}
	}
	if cfg == nil {
		cfg = map[string]interface{}{}
	}

	gatewaySection := ensureMapQR(cfg, "gateway")
	platforms := ensureMapQR(gatewaySection, "platforms")
	wecomSection := ensureMapQR(platforms, "wecom")
	wecomSection["enabled"] = true
	wecomSection["mode"] = "aibot"
	wecomSection["bot_id"] = botID
	wecomSection["secret"] = secret
	// 自建应用模式已移除：扫码成功后抹掉遗留的 corp_id/agent_id，避免混淆
	delete(wecomSection, "corp_id")
	delete(wecomSection, "agent_id")
	delete(wecomSection, "aes_key")

	newData, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(configPath, newData, 0644); err != nil {
		return err
	}
	log.Infof("[WeCom/AIBot] Saved credentials to %s", configPath)
	return nil
}

func truncateForLog(b []byte) string {
	s := string(b)
	if len(s) > 300 {
		return s[:300] + "..."
	}
	return s
}
