package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// 票据（ticket）是把「登录凭据」从 URL 里赶出去之后，用来替代 ?token= 的
// 自包含、按用途限定、带过期时间的一次性凭据。
//
// 背景：有一批资源是浏览器**无法附带 Authorization 头**去请求的——
// <img src>、<a href>、new EventSource(...)。过去这些地址只能靠
// ?token=<登录凭据> 通行，后果是：
//
//  1. 登录凭据进入浏览器历史、Referer 头、反向代理访问日志；
//  2. 同一串 ?token= 能打开全部受保护接口，权限远大于「看一眼这张图」。
//
// 现在改为：客户端先用带 Authorization 头的 fetch 调 /api/fs/sign 换取票据，
// 再把票据放进 URL。票据只对「某个路径 + 某种动作」有效，且有硬性过期时间，
// 泄漏面从「整个 API」收敛为「一个文件、一段时间」。
//
// 与 /api/fs/serve 同理，票据写在**路径**里而不是 query：iframe 内的相对
// 子资源引用按 RFC 3986 §5.3 只做路径合并，query 会被整段丢弃。
const fsTicketPrefix = "/api/fs/ticket"

// 票据作用域。每个作用域只解锁一类动作，彼此不可互相替代——尤其要防止一张
// read 票据被塞进 serve 的路径位，把「读一个文件」偷换成「托管整个目录」。
const (
	fsScopeServe    = "serve"    // 静态网页预览（托管整个目录）
	fsScopeRead     = "read"     // 单文件内联读取（<img src>、新标签页打开）
	fsScopeDownload = "download" // 单文件附件下载（<a href>）
	fsScopeZip      = "zip"      // 文件/目录打包下载
	fsScopeUploads  = "uploads"  // 会话上传的附件
	fsScopeEvents   = "events"   // 全局 SSE 事件流（EventSource 发不出请求头）
)

const (
	// 一次性动作票据：客户端在真正发起请求前才去换，所以可以给得比较短。
	fsTicketActionTTL = 1 * time.Hour
	// 长驻票据：serve 要被 iframe 内的相对子资源反复继承；events 要能扛过
	// EventSource 的自动重连（重连复用同一个 URL，票据中途过期会让重连直接
	// 403 而不是恢复）。两者按天给。
	fsTicketStreamTTL = 24 * time.Hour
)

var errFSTicket = errors.New("invalid or expired ticket")

// fsTicket 是票据的语义载荷。
//
// Path 在签发时就是**服务端解析后的绝对路径**：调用方在签发前已经用
// resolveFSPath 做过会话边界校验，所以消费端只需验签名，不必再查会话。
// 这是有意为之——把 session_id 交给客户端再回传，等于凭空多出一个可篡改的
// 输入；让客户端只能回传「我们已经认可的结论」，攻击面更小。
type fsTicket struct {
	Scope string
	Path  string // read/download/zip/serve：绝对路径；uploads：上传根内相对路径；events：空
	Opts  string // zip 专用：含 "h" 表示归档包含隐藏文件
	Exp   time.Time
}

// fsTicketKey 由 authToken 派生出唯一的票据签名密钥。
//
// 单独派生而不用 authToken 本身，是为了让票据与登录凭据在密码学上分离：
// 拿到票据无法反推 authToken，因而也无法冒充登录。authToken 为空时拒绝签发，
// 否则密钥会退化成一个公开常量，任何人都能伪造票据。
func (s *Server) fsTicketKey() ([]byte, error) {
	if strings.TrimSpace(s.authToken) == "" {
		return nil, errors.New("ticket signing unavailable: server auth token not configured")
	}
	sum := sha256.Sum256([]byte("go-magic/fs-ticket/v1\x00" + s.authToken))
	return sum[:], nil
}

// signFSTicket 签发自包含凭据：
//
//	base64url(v1 \x00 scope \x00 path \x00 opts \x00 exp) + "." + base64url(hmac[:16])
//
// 载荷内嵌全部语义字段（含路径），因此校验时不需要任何额外的 query 参数——
// 这正是「相对引用/子资源」场景能把它一路带过去的原因。
func (s *Server) signFSTicket(t fsTicket) (string, error) {
	key, err := s.fsTicketKey()
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString([]byte(strings.Join([]string{
		"v1",
		t.Scope,
		t.Path,
		t.Opts,
		strconv.FormatInt(t.Exp.Unix(), 10),
	}, "\x00")))

	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)[:16]), nil
}

// parseFSTicket 校验凭据并还原载荷。
//
// 缺段、签名不符、版本不认识、已过期一律返回 errFSTicket，且不区分原因：
// 把校验失败的具体理由反馈出去，只会变成攻击者的探测信号。
func (s *Server) parseFSTicket(sig string, now time.Time) (fsTicket, error) {
	var zero fsTicket

	payload, sigPart, ok := strings.Cut(sig, ".")
	if !ok || payload == "" || sigPart == "" {
		return zero, errFSTicket
	}

	key, err := s.fsTicketKey()
	if err != nil {
		return zero, err
	}
	want := hmac.New(sha256.New, key)
	want.Write([]byte(payload))
	got, err := base64.RawURLEncoding.DecodeString(sigPart)
	if err != nil || !hmac.Equal(want.Sum(nil)[:16], got) {
		return zero, errFSTicket
	}

	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return zero, errFSTicket
	}
	parts := strings.Split(string(raw), "\x00")
	if len(parts) != 5 || parts[0] != "v1" {
		return zero, errFSTicket
	}
	expUnix, err := strconv.ParseInt(parts[4], 10, 64)
	if err != nil || now.Unix() > expUnix {
		return zero, errFSTicket
	}

	return fsTicket{
		Scope: parts[1],
		Path:  parts[2],
		Opts:  parts[3],
		Exp:   time.Unix(expUnix, 0),
	}, nil
}

// signTicketURL 把票据拼成消费地址。
func (s *Server) signTicketURL(t fsTicket) (string, error) {
	sig, err := s.signFSTicket(t)
	if err != nil {
		return "", err
	}
	return fsTicketPrefix + "/" + sig, nil
}

// handleFSTicket 是票据唯一的消费入口（GET/HEAD /api/fs/ticket/<sig>）。
//
// 与 /api/fs/serve 一样刻意不套 requireAuth：它服务的正是「带不上认证头」的
// 请求。<sig> 自身既是凭据也是行为描述，所以这里不存在任何可被客户端篡改的
// query 参数——路径、动作、有效期全部取自签名载荷。
func (s *Server) handleFSTicket(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sig := strings.TrimPrefix(r.URL.Path, fsTicketPrefix+"/")
	// 票据必须恰好占一个路径段。多出来的段一律拒绝，避免 /t/<sig>/../ 之类的
	// 路径混淆把请求带到别的分支上。
	if sig == "" || strings.Contains(sig, "/") {
		http.NotFound(w, r)
		return
	}

	tk, err := s.parseFSTicket(sig, time.Now())
	if err != nil {
		http.Error(w, "invalid or expired ticket", http.StatusForbidden)
		return
	}

	switch tk.Scope {
	case fsScopeRead:
		s.serveTicketFile(w, r, tk.Path, false)
	case fsScopeDownload:
		s.serveTicketFile(w, r, tk.Path, true)
	case fsScopeZip:
		s.serveTicketZip(w, r, tk.Path, strings.Contains(tk.Opts, "h"))
	case fsScopeUploads:
		abs, err := s.resolveUploadTicketPath(tk.Path)
		if err != nil {
			// 载荷里的路径越界：与签名错误同样处理，不泄漏哪一步失败。
			http.Error(w, "invalid or expired ticket", http.StatusForbidden)
			return
		}
		// 上传内容是**不可信用户内容**，与 /api/uploads/ 保持同一套加固：
		// sandbox CSP 阻止内联 SVG/HTML 在本源执行脚本，nosniff + attachment
		// 让响应语义恒为「下载」。
		w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Disposition", "attachment")
		s.serveTicketFile(w, r, abs, false)
	case fsScopeServe:
		// 预览票据只能走 /api/fs/serve（那里才做 subpath 解析）。
		http.Error(w, "ticket scope not served by this endpoint", http.StatusForbidden)
	case fsScopeEvents:
		// 事件票据只用于 /api/events。
		http.Error(w, "ticket scope not served by this endpoint", http.StatusForbidden)
	default:
		http.Error(w, "unknown ticket scope", http.StatusForbidden)
	}
}

// serveTicketFile 输出单个文件。attachment 为 true 时按下载语义应答。
//
// 用 http.ServeContent 而非 ServeFile：ServeFile 保留了目录处理与对
// index.html 的 301 跳转，那些逻辑必须排除（票据只授权单个文件），
// 而 ServeContent 同时保留 Range 支持——视频/大文件拖拽进度条依赖它。
func (s *Server) serveTicketFile(w http.ResponseWriter, r *http.Request, abs string, attachment bool) {
	if abs == "" {
		http.Error(w, "invalid or expired ticket", http.StatusForbidden)
		return
	}
	info, err := os.Stat(abs)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if info.IsDir() {
		http.Error(w, "path is a directory", http.StatusBadRequest)
		return
	}

	f, err := os.Open(abs)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()

	if attachment {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", "attachment; filename=\""+filepath.Base(abs)+"\"")
	}
	// 一律 nosniff：即便票据只授权一个文件，也不让浏览器把无扩展名的内容
	// 猜成 text/html 后在本源下当文档渲染。
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, filepath.Base(abs), info.ModTime(), f)
}

// serveTicketZip 按票据打包下载。
func (s *Server) serveTicketZip(w http.ResponseWriter, r *http.Request, abs string, showHidden bool) {
	if abs == "" {
		http.Error(w, "invalid or expired ticket", http.StatusForbidden)
		return
	}
	info, err := os.Stat(abs)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.writeFSZipArchive(w, abs, info, showHidden); err != nil {
		fmt.Fprintf(os.Stderr, "zip (ticket) error: %v\n", err)
	}
}

// normalizeUploadTicketPath 把「/api/uploads/<rel>」或裸相对路径统一成上传根内
// 的相对路径。
//
// 越界输入一律显式拒绝，而不是靠 path.Clean 把它夹回根上：夹回虽然同样安全，
// 却会静默地把请求指向另一个文件。这与 safeJoinFSServe 的取舍保持一致——越界
// 就该报错，不该变成「换个文件给你」，否则调用方会把 404 误读成「文件不存在」。
func normalizeUploadTicketPath(raw string) (string, error) {
	rel := strings.TrimSpace(raw)
	if rel == "" || strings.ContainsRune(rel, 0) {
		return "", errFSTicket
	}

	// 允许两种写法：完整的站内引用（/api/uploads/<rel>）或裸相对路径。
	rel = strings.TrimPrefix(rel, "/api/uploads/")
	rel = strings.TrimPrefix(rel, "api/uploads/")

	// 剥掉已知前缀后仍带前导斜杠，说明是别的绝对路径（如 /etc/passwd）。
	// 直接拒绝，而不是顺手把它当相对路径来解释——那正是路径穿越的温床。
	if rel == "" || strings.HasPrefix(rel, "/") || filepath.IsAbs(rel) {
		return "", errFSTicket
	}

	for _, seg := range strings.Split(rel, "/") {
		if seg == ".." {
			return "", errFSTicket
		}
	}
	return rel, nil
}

// resolveUploadTicketPath 把上传相对路径落回上传根目录之内。
//
// 签发时已经清洗过一次，这里仍然独立再校验一次：载荷里的路径虽然受签名保护，
// 但它终究是一段路径。把「已经被校验过」当作不再校验的理由，正是路径穿越
// 漏洞最常见的成因。符号链接也一并解析，避免根内的软链指向根外。
func (s *Server) resolveUploadTicketPath(rel string) (string, error) {
	clean, err := normalizeUploadTicketPath(rel)
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(clean) {
		return "", errFSTicket
	}

	root, err := filepath.Abs(s.uploadsRoot())
	if err != nil {
		return "", err
	}
	abs := filepath.Join(root, filepath.FromSlash(path.Clean("/"+clean)))
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	if !fsServeWithin(root, abs) {
		return "", errFSTicket
	}
	return abs, nil
}

// eventsRequestAuthorized 校验 SSE 订阅权限：scope=events 的票据，或常规请求头凭据。
//
// 这条流是「浏览器发不出请求头」的典型受害者——EventSource 无法自定义
// Authorization，所以票据必须支持从查询参数进来。除此之外不接受任何形式，
// 尤其是 ?token=：那正是这次改造要关掉的后门。
func (s *Server) eventsRequestAuthorized(r *http.Request) bool {
	sig := strings.TrimSpace(r.URL.Query().Get("sig"))
	if sig == "" {
		return s.authorized(r)
	}
	// 带了票据就必须是有效票据。不在这里回落到 header 判定：否则「票据错了」
	// 会退化成「没带票据」，两种状态混作一种，等于给了探测者一个免费的消歧信号。
	tk, err := s.parseFSTicket(sig, time.Now())
	return err == nil && tk.Scope == fsScopeEvents
}
