package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// fakeWeComAIBotServer 是企业微信官方长连接端点的内存替身：收到 aibot_subscribe
// 认证帧后先回订阅响应（errcode），再推送私聊/群聊文本回调，并记录客户端发出的
// 所有回复帧。subscribeErrCode/subscribeErrMsg 非零时模拟服务端拒绝订阅。
type fakeWeComAIBotServer struct {
	t         *testing.T
	srv       *httptest.Server
	url       string
	subscribe chan wecomAIBotFrame // 收到的认证帧
	outbound  chan wecomAIBotFrame // 客户端发出的 respond/send 帧

	subscribeErrCode int
	subscribeErrMsg  string
}

func newFakeWeComAIBotServer(t *testing.T) *fakeWeComAIBotServer {
	t.Helper()
	f := &fakeWeComAIBotServer{
		t:         t,
		subscribe: make(chan wecomAIBotFrame, 4),
		outbound:  make(chan wecomAIBotFrame, 32),
	}

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("upgrade failed: %v", err)
			return
		}
		defer conn.Close()

		// 单 goroutine 服务整条连接：对每帧回 ack（真实企微对 respond/send/ping 均有
		// {headers:{req_id},errcode,errmsg} 响应、无 cmd）；订阅成功后推送事件。
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var fr wecomAIBotFrame
			if err := json.Unmarshal(data, &fr); err != nil {
				continue
			}
			errcode := 0
			errmsg := "ok"
			if fr.Cmd == "aibot_subscribe" {
				select {
				case f.subscribe <- fr:
				default:
				}
				if f.subscribeErrCode != 0 { // 订阅被拒场景：注入错误码
					errcode = f.subscribeErrCode
					errmsg = f.subscribeErrMsg
				}
			}
			ackJSON := fmt.Sprintf(`{"headers":{"req_id":%q},"errcode":%d,"errmsg":%q}`,
				fr.Headers.ReqID, errcode, errmsg)
			if err := conn.WriteMessage(websocket.TextMessage, []byte(ackJSON)); err != nil {
				return
			}
			switch {
			case fr.Cmd == "aibot_subscribe" && errcode == 0:
				if err := conn.WriteMessage(websocket.TextMessage, []byte(f.privateEvent("req-priv-1", "你好"))); err != nil {
					return
				}
				time.Sleep(30 * time.Millisecond)
				if err := conn.WriteMessage(websocket.TextMessage, []byte(f.groupEvent("req-grp-2", "大家好"))); err != nil {
					return
				}
			case fr.Cmd == "aibot_subscribe" && errcode != 0:
				// 订阅被拒场景：服务端保持连接，等待网关自行断开
			default:
				// respond/send/ping：记录帧供断言
				select {
				case f.outbound <- fr:
				default:
				}
			}
		}
	})

	f.srv = httptest.NewServer(mux)
	f.url = "ws" + strings.TrimPrefix(f.srv.URL, "http")
	t.Cleanup(f.srv.Close)
	return f
}

// wecomAIBotTestBody 覆盖测试关心的 body 字段。
type wecomAIBotTestBody struct {
	BotID    string `json:"bot_id"`
	Secret   string `json:"secret"`
	UserID   string `json:"userid"`
	ChatID   string `json:"chatid"`
	ChatType int    `json:"chat_type"`
	MsgType  string `json:"msgtype"`
	Text     struct {
		Content string `json:"content"`
	} `json:"text"`
	Stream struct {
		ID      string `json:"id"`
		Finish  bool   `json:"finish"`
		Content string `json:"content"`
	} `json:"stream"`
	Markdown struct {
		Content string `json:"content"`
	} `json:"markdown"`
}

func decodeBody(t *testing.T, fr wecomAIBotFrame) wecomAIBotTestBody {
	t.Helper()
	var b wecomAIBotTestBody
	if err := json.Unmarshal(fr.Body, &b); err != nil {
		t.Fatalf("bad body %s: %v", string(fr.Body), err)
	}
	return b
}

func mustFrame(t *testing.T, ch <-chan wecomAIBotFrame) wecomAIBotFrame {
	t.Helper()
	select {
	case fr := <-ch:
		return fr
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for wecom frame")
	}
	return wecomAIBotFrame{}
}

func (f *fakeWeComAIBotServer) privateEvent(reqID, text string) string {
	return `{
		"cmd":"aibot_msg_callback",
		"headers":{"req_id":"` + reqID + `"},
		"body":{
			"msgid":"msg-priv-1","aibotid":"bot_x","chattype":"single",
			"from":{"userid":"zhangsan","corpid":"corp1"},
			"msgtype":"text","text":{"content":` + mustJSONString(text) + `}
		}
	}`
}

func (f *fakeWeComAIBotServer) groupEvent(reqID, text string) string {
	return `{
		"cmd":"aibot_msg_callback",
		"headers":{"req_id":"` + reqID + `"},
		"body":{
			"msgid":"msg-grp-1","aibotid":"bot_x","chatid":"chat-grp-1",
			"chattype":"group","from":{"userid":"wangwu","corpid":"corp1"},
			"msgtype":"text","text":{"content":` + mustJSONString(text) + `}
		}
	}`
}

func waitWeComConnected(t *testing.T, g *WeComAIBotGateway) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if g.IsConnected() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("wecom aibot gateway did not connect within 5s")
}

// TestWeComAIBotFullFlow 覆盖：认证帧、私聊/群聊回调解析、流式占位(finish=false)
// + 同 stream.id 全量替换(finish=true)的被动回复、以及 aibot_send_msg 主动推送。
func TestWeComAIBotFullFlow(t *testing.T) {
	f := newFakeWeComAIBotServer(t)
	g := NewWeComAIBotGateway("bot_x", "sec_y", f.url)
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		cancel()
		_ = g.Disconnect()
	}()

	if err := g.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	waitWeComConnected(t, g)

	// 1) 认证帧：aibot_subscribe + bot_id/secret
	sub := mustFrame(t, f.subscribe)
	if sub.Cmd != "aibot_subscribe" {
		t.Fatalf("subscribe cmd = %q", sub.Cmd)
	}
	sb := decodeBody(t, sub)
	if sb.BotID != "bot_x" || sb.Secret != "sec_y" {
		t.Fatalf("subscribe body = %+v, want bot_id=bot_x secret=sec_y", sb)
	}

	// 2) 回调到达即发流式占位（msgtype=stream, finish=false, 透传回调 req_id，
	//    不带 chatid/userid —— respond_msg 靠 req_id 关联会话）。fake 先推私聊后推群聊。
	privClaim := mustFrame(t, f.outbound)
	if privClaim.Cmd != "aibot_respond_msg" {
		t.Fatalf("private claim cmd = %q, want aibot_respond_msg", privClaim.Cmd)
	}
	if privClaim.Headers.ReqID != "req-priv-1" {
		t.Fatalf("private claim req_id = %q, want req-priv-1", privClaim.Headers.ReqID)
	}
	pb := decodeBody(t, privClaim)
	if pb.MsgType != "stream" || pb.Stream.Finish || pb.Stream.ID == "" {
		t.Fatalf("private claim body = %+v, want msgtype=stream finish=false with stream.id", pb)
	}
	if pb.Stream.Content != wecomAIBotClaimText {
		t.Fatalf("private claim content = %q, want %q", pb.Stream.Content, wecomAIBotClaimText)
	}
	if pb.ChatID != "" || pb.UserID != "" {
		t.Fatalf("respond_msg body must not carry chatid/userid: %+v", pb)
	}

	grpClaim := mustFrame(t, f.outbound)
	if grpClaim.Cmd != "aibot_respond_msg" || grpClaim.Headers.ReqID != "req-grp-2" {
		t.Fatalf("group claim = cmd %q req %q, want aibot_respond_msg req-grp-2",
			grpClaim.Cmd, grpClaim.Headers.ReqID)
	}
	gb := decodeBody(t, grpClaim)
	if gb.MsgType != "stream" || gb.Stream.Finish || gb.Stream.ID == "" {
		t.Fatalf("group claim body = %+v, want msgtype=stream finish=false", gb)
	}

	// 3) 回调进入统一消息流
	m1 := recvMsg(t, g.Receive(), 5*time.Second)
	if m1.IsGroup || m1.ChannelID != "zhangsan" || m1.UserID != "zhangsan" {
		t.Fatalf("unexpected private msg: %+v", m1)
	}
	if m1.Content != "你好" {
		t.Fatalf("private content = %q, want 你好", m1.Content)
	}
	m2 := recvMsg(t, g.Receive(), 5*time.Second)
	if !m2.IsGroup || m2.ChannelID != "chat-grp-1" || m2.UserID != "wangwu" {
		t.Fatalf("unexpected group msg: %+v", m2)
	}
	if m2.Content != "大家好" {
		t.Fatalf("group content = %q, want 大家好", m2.Content)
	}

	// 4) 群回复 -> 同一 stream.id 的 finish=true 全量替换（非 text！text 会被 40008 拒收）
	if err := g.Send(context.Background(), Response{ChannelID: "chat-grp-1", Content: "群回复"}); err != nil {
		t.Fatalf("group send failed: %v", err)
	}
	fr := mustFrame(t, f.outbound)
	if fr.Cmd != "aibot_respond_msg" {
		t.Fatalf("group reply cmd = %q, want aibot_respond_msg", fr.Cmd)
	}
	if fr.Headers.ReqID != "req-grp-2" {
		t.Fatalf("group reply req_id = %q, want req-grp-2", fr.Headers.ReqID)
	}
	gr := decodeBody(t, fr)
	if gr.MsgType != "stream" || !gr.Stream.Finish {
		t.Fatalf("group reply body = %+v, want msgtype=stream finish=true", gr)
	}
	if gr.Stream.ID != gb.Stream.ID {
		t.Fatalf("group reply stream.id = %q, want claim's %q", gr.Stream.ID, gb.Stream.ID)
	}
	if gr.Stream.Content != "群回复" {
		t.Fatalf("group reply content = %q, want 群回复", gr.Stream.Content)
	}
	if gr.ChatID != "" || gr.UserID != "" {
		t.Fatalf("respond_msg body must not carry chatid/userid: %+v", gr)
	}

	// 5) 私聊回复 -> 同一 stream.id 的 finish=true 替换
	if err := g.Send(context.Background(), Response{ChannelID: "zhangsan", Content: "私聊回"}); err != nil {
		t.Fatalf("private send failed: %v", err)
	}
	fr = mustFrame(t, f.outbound)
	if fr.Cmd != "aibot_respond_msg" || fr.Headers.ReqID != "req-priv-1" {
		t.Fatalf("private reply = cmd %q req %s, want aibot_respond_msg req-priv-1", fr.Cmd, fr.Headers.ReqID)
	}
	pr := decodeBody(t, fr)
	if pr.MsgType != "stream" || !pr.Stream.Finish || pr.Stream.ID != pb.Stream.ID {
		t.Fatalf("private reply body = %+v, want finish=true with claim stream.id %q", pr, pb.Stream.ID)
	}
	if pr.Stream.Content != "私聊回" {
		t.Fatalf("private reply content = %q, want 私聊回", pr.Stream.Content)
	}

	// 6) 主动推送（无在途回调的会话）-> aibot_send_msg + markdown（不支持 text）
	if err := g.Send(context.Background(), Response{ChannelID: "lisi", Content: "主动推"}); err != nil {
		t.Fatalf("active send failed: %v", err)
	}
	fr = mustFrame(t, f.outbound)
	if fr.Cmd != "aibot_send_msg" {
		t.Fatalf("active cmd = %q, want aibot_send_msg", fr.Cmd)
	}
	ab := decodeBody(t, fr)
	if ab.MsgType != "markdown" || ab.Markdown.Content != "主动推" {
		t.Fatalf("active body = %+v, want msgtype=markdown content=主动推", ab)
	}
	if ab.ChatID != "lisi" || ab.ChatType != 1 {
		t.Fatalf("active private body = %+v, want chatid=lisi chat_type=1", ab)
	}
	if ab.UserID != "" {
		t.Fatalf("active private body must not carry userid: %+v", ab)
	}
	if ab.Text.Content != "" {
		t.Fatalf("active push must not use msgtype=text: %+v", ab)
	}

	// 7) 群发主动推送（占位已消费）-> send_msg + chatid + chat_type=2
	if err := g.Send(context.Background(), Response{ChannelID: "chat-grp-1", Content: "再推一次"}); err != nil {
		t.Fatalf("group active send failed: %v", err)
	}
	fr = mustFrame(t, f.outbound)
	if fr.Cmd != "aibot_send_msg" {
		t.Fatalf("group active cmd = %q, want aibot_send_msg", fr.Cmd)
	}
	gab := decodeBody(t, fr)
	if gab.ChatID != "chat-grp-1" || gab.ChatType != 2 {
		t.Fatalf("group active body = %+v, want chatid=chat-grp-1 chat_type=2", gab)
	}
	if gab.MsgType != "markdown" || gab.Markdown.Content != "再推一次" {
		t.Fatalf("group active body = %+v, want markdown 再推一次", gab)
	}
}

// TestWeComAIBotSubscribeRejected 验证订阅被服务端拒绝(errcode!=0)时网关不得保持 connected。
// 回归点：旧实现写完 subscribe 帧即乐观置 connected、不校验响应 errcode，
// 导致凭证错误/未开长连接 API 时"看似已连接却永远收不到消息"的静默失败。
func TestWeComAIBotSubscribeRejected(t *testing.T) {
	f := newFakeWeComAIBotServer(t)
	f.subscribeErrCode = 1001
	f.subscribeErrMsg = "invalid secret"

	g := NewWeComAIBotGateway("bot_x", "wrong-secret", f.url)
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		cancel()
		_ = g.Disconnect()
	}()

	if err := g.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	// BasePlatform.Connect 会先乐观置 connected，run() 收到 errcode!=0 后应立即收回。
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !g.IsConnected() {
			return // 订阅被拒后正确回到未连接态
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("gateway stayed connected after subscribe rejection (errcode=%d errmsg=%q)",
		f.subscribeErrCode, f.subscribeErrMsg)
}

// TestWeComAIBotLongReply 验证超长回复在官方 content 上限内走"单条流式消息全量替换"，
// 不再按 1900 字分片成多条（stream.content 全量语义 + 20480 字节上限已覆盖常规长回复）。
func TestWeComAIBotLongReply(t *testing.T) {
	f := newFakeWeComAIBotServer(t)
	g := NewWeComAIBotGateway("bot_x", "sec_y", f.url)
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		cancel()
		_ = g.Disconnect()
	}()
	if err := g.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	waitWeComConnected(t, g)

	// 服务器推送的私聊/群聊回调各触发一条占位帧；随后消息进入 Receive。
	privClaim := mustFrame(t, f.outbound) // req-priv-1
	grpClaim := mustFrame(t, f.outbound)  // req-grp-2
	if privClaim.Headers.ReqID != "req-priv-1" || grpClaim.Headers.ReqID != "req-grp-2" {
		t.Fatalf("unexpected claim order: priv=%q grp=%q", privClaim.Headers.ReqID, grpClaim.Headers.ReqID)
	}
	claimStreamID := decodeBody(t, grpClaim).Stream.ID
	recvMsg(t, g.Receive(), 5*time.Second)
	recvMsg(t, g.Receive(), 5*time.Second)

	long := strings.Repeat("字", 4000) // 12000 字节 < 20480 上限
	if err := g.Send(context.Background(), Response{ChannelID: "chat-grp-1", Content: long}); err != nil {
		t.Fatalf("long send failed: %v", err)
	}
	fr := mustFrame(t, f.outbound)
	if fr.Cmd != "aibot_respond_msg" || fr.Headers.ReqID != "req-grp-2" {
		t.Fatalf("long reply = cmd %q req %q, want aibot_respond_msg req-grp-2", fr.Cmd, fr.Headers.ReqID)
	}
	b := decodeBody(t, fr)
	if b.MsgType != "stream" || !b.Stream.Finish {
		t.Fatalf("long reply body = %+v, want msgtype=stream finish=true", b)
	}
	if b.Stream.ID != claimStreamID {
		t.Fatalf("long reply stream.id = %q, want claim's %q", b.Stream.ID, claimStreamID)
	}
	if b.Stream.Content != long {
		t.Fatalf("long reply content mismatch: got %d runes want %d", len([]rune(b.Stream.Content)), len([]rune(long)))
	}

	// 单条流式消息承载全量后不得再有分片帧
	select {
	case extra := <-f.outbound:
		t.Fatalf("unexpected extra frame after single-stream long reply: cmd=%s body=%s", extra.Cmd, extra.Body)
	case <-time.After(300 * time.Millisecond):
	}
}

// TestTrimWeComContent 验证超过 20480 字节的回复被安全截断到 rune 边界内。
func TestTrimWeComContent(t *testing.T) {
	short := "你好，Magic"
	if got := trimWeComContent(short); got != short {
		t.Fatalf("short content should pass through unchanged, got %q", got)
	}

	cjk := strings.Repeat("字", 10000) // 30000 字节 > 上限
	got := trimWeComContent(cjk)
	if len(got) > wecomAIBotContentMaxBytes {
		t.Fatalf("cjk truncation exceeded limit: %d bytes", len(got))
	}
	if len(got) < wecomAIBotContentMaxBytes-3 {
		t.Fatalf("cjk truncation too aggressive: %d bytes (want near %d)", len(got), wecomAIBotContentMaxBytes)
	}

	ascii := strings.Repeat("a", 30000)
	got2 := trimWeComContent(ascii)
	if len(got2) != wecomAIBotContentMaxBytes {
		t.Fatalf("ascii truncation = %d bytes, want %d", len(got2), wecomAIBotContentMaxBytes)
	}
}

// mustJSONString 把字符串编码为 JSON 字符串字面量（用于拼接测试 payload）。
func mustJSONString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// recvMsg 等待接收一条网关消息，超时则测试失败。
func recvMsg(t *testing.T, ch <-chan Message, timeout time.Duration) Message {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case m := <-ch:
			return m
		case <-deadline:
			t.Fatalf("timed out waiting for message")
		}
	}
}
