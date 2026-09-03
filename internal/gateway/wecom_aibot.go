package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/gorilla/websocket"
	"github.com/magicwubiao/go-magic/pkg/log"
)

// WeCom AI Bot（企业微信「智能机器人」）接入 —— 官方扫码通道。
//
// 企业微信官方在 Web UI / 后台支持扫码「一键创建智能机器人」，创建后得到
// bot_id + secret（无 corp_id/agent_id，无需公网回调 URL）。本网关作为正向
// WebSocket 客户端连接官方长连接端点收发消息：
//
//	{
//	  "enabled": true,
//	  "mode": "aibot",
//	  "bot_id": "bot_xxxx",   // 扫码创建或后台「工作台-智能机器人」获取
//	  "secret": "sec_xxxx"
//	}
//
// 扫码创建流程（generate/query_result）见 wecom_aibot_qr.go；本文件只负责
// WebSocket 运行态。协议帧（字段以官方 aibot-node-sdk 为准）：
//
//	认证:   {cmd:"aibot_subscribe", headers:{req_id:""}, body:{bot_id, secret}}
//	响应:   {headers:{req_id:<透传>}, errcode:0, errmsg:"ok"}   ← 无 cmd 字段，须校验 errcode
//	收消息: {cmd:"aibot_msg_callback", headers:{req_id}, body:{msgid,chattype,from,msgtype,text,...}}
//	回复:   {cmd:"aibot_respond_msg",  headers:{req_id:<透传回调 req_id>}, body:{msgtype:"stream",stream:{id,finish,content}}}
//	        被动回复 msgtype 仅支持 stream/template_card/markdown/file/image/voice/video，
//	        **不支持 text**（用 text 会收到 errcode=40008 invalid message type）。
//	        消息回调后 24h 内可回复；本实现采用两段式流式回复（官方推荐）：
//	        回调到达即用生成的 stream.id 发 finish=false 占位（"正在思考"），完整回复
//	        就绪后用同一 req_id+stream.id 发 finish=true 全量替换；content 为全量内容，
//	        最长 20480 字节（UTF-8），支持 Markdown，10 分钟内须 finish=true。
//	主动发: {cmd:"aibot_send_msg",     headers:{req_id:<新>},  body:{chatid,chat_type,msgtype:"markdown",markdown:{content}}}
//	        msgtype 仅支持 markdown/template_card 等，同样不支持 text；前置条件：用户
//	        需先在本会话给机器人发过消息。频率限制 30 条/分钟、1000 条/小时。
//	心跳:   {cmd:"ping", headers:{req_id:""}}   ← 不带 body

const (
	wecomAIBotDefaultWSURL     = "wss://openws.work.weixin.qq.com"
	wecomAIBotReconnectMin     = 3 * time.Second
	wecomAIBotReconnectMax     = 30 * time.Second
	wecomAIBotHeartbeat        = 30 * time.Second
	wecomAIBotSubscribeTimeout = 10 * time.Second // 等待订阅响应(errcode)的最长时间
	wecomAIBotAckTimeout       = 5 * time.Second  // 等待发送帧服务端确认(errcode=0)的最长时间
	wecomAIBotContentMaxBytes  = 20000            // stream/markdown content 官方上限 20480 字节，留余量
	wecomAIBotClaimText        = "正在思考，请稍候…"      // 占位帧文案（随后被最终回复原位替换）
)

// WeComAIBotGateway 是企业微信官方智能机器人的网关实现（平台名 "wecom"）。
// 企业微信仅保留这一种接入方式：官方扫码创建机器人后得 bot_id/secret，
// 本网关作为正向 WebSocket 客户端连接官方长连接端点。
type WeComAIBotGateway struct {
	*BasePlatform

	botID string
	wsURL string

	secret string

	wsConn *websocket.Conn
	wsMu   sync.Mutex

	reqCounter atomic.Int64

	// channelID -> 在途被动回复（占位 finish=false 已发出，等 onSend 发 finish=true 替换）。
	// 回调到达即写一条；finish 发出后删除；断线时整体清空。占位发送时连接尚未就绪
	// （订阅确认与早期回调竞态）则保留记录，由 attach() 在置 connected 前统一补发。
	pendingStreams   map[string]wecomPendingStream
	pendingStreamsMu sync.RWMutex

	// channelID -> "private" | "group"，用于主动推送时判 chat_type
	msgSource   map[string]string
	msgSourceMu sync.RWMutex

	// req_id -> ack 通知通道（sendFrameSync 等待服务端确认用；订阅等待走 subAckCh）。
	ackWaiters map[string]chan wecomAIBotAck

	// 订阅响应等待状态：每次连接尝试期间注册，收到匹配 req_id 的响应帧后投递。
	ackMu    sync.Mutex
	subAckCh chan wecomAIBotAck
	subReqID string
}

// wecomAIBotFrame 是 WS 上下行的统一帧。
type wecomAIBotFrame struct {
	Cmd     string            `json:"cmd"` // aibot_subscribe / aibot_msg_callback / aibot_event_callback / aibot_respond_msg / aibot_send_msg / ping / ack
	Headers wecomAIBotHeaders `json:"headers"`
	Body    json.RawMessage   `json:"body,omitempty"` // 心跳(ping)与各类响应帧无 body，不应携带该字段
}

type wecomAIBotHeaders struct {
	ReqID string `json:"req_id"`
}

// wecomPendingStream 记录已向某会话发出流式占位的被动链路。
type wecomPendingStream struct {
	ReqID    string // 透传回调的 req_id —— finish 帧必须复用
	StreamID string // 占位时生成的 stream.id —— finish 帧必须复用
	// ClaimSent 表示 finish=false 占位帧是否已实际发出。订阅确认与早期消息回调
	// 存在竞态：回调可能先于 attach() 到达（此时连接尚未挂到网关上），占位发送
	// 会被推迟，由 attach() 统一补发；该标记防止补发与直发重复。
	ClaimSent bool
}

// wecomAIBotAck 是服务端对订阅/心跳/回复/推送请求的响应帧。
// 注意：官方响应格式不带 cmd 字段，只有 {headers:{req_id}, errcode, errmsg}，
// 因此不能用 wecomAIBotFrame 直接解析；errcode=0 才表示成功。
type wecomAIBotAck struct {
	Headers wecomAIBotHeaders `json:"headers"`
	ErrCode int               `json:"errcode"`
	ErrMsg  string            `json:"errmsg"`
}

// wecomAIBotStreamReply 是 aibot_respond_msg 流式消息的 body（msgtype=stream）。
type wecomAIBotStreamReply struct {
	MsgType string           `json:"msgtype"`
	Stream  wecomAIBotStream `json:"stream"`
}

type wecomAIBotStream struct {
	ID      string `json:"id"`
	Finish  bool   `json:"finish"`
	Content string `json:"content"`
}

// wecomAIBotMarkdownPush 是 aibot_send_msg 主动推送 markdown 的 body。
type wecomAIBotMarkdownPush struct {
	MsgType  string `json:"msgtype"`
	Markdown struct {
		Content string `json:"content"`
	} `json:"markdown"`
	ChatID   string `json:"chatid"`
	ChatType int    `json:"chat_type"`
}

// wecomAIBotMessageBody 覆盖 aibot_msg_callback 的消息体。
type wecomAIBotMessageBody struct {
	MsgID       string `json:"msgid"`
	AibotID     string `json:"aibotid"`
	ChatID      string `json:"chatid"`   // 群聊为群 ID；单聊可能为空
	ChatType    string `json:"chattype"` // single | group
	CreateTime  int64  `json:"create_time"`
	ResponseURL string `json:"response_url"`
	From        struct {
		UserID string `json:"userid"`
		CorpID string `json:"corpid"`
	} `json:"from"`
	MsgType string `json:"msgtype"` // text / image / mixed / voice / file / video
	Text    struct {
		Content string `json:"content"`
	} `json:"text"`
}

// NewWeComAIBotGateway 创建 AI Bot 网关。wsURL 为空使用官方默认端点。
func NewWeComAIBotGateway(botID, secret, wsURL string) *WeComAIBotGateway {
	config := map[string]interface{}{
		"dm_policy":    "open",
		"group_policy": "open",
		"max_retries":  -1,
	}

	g := &WeComAIBotGateway{
		botID:          strings.TrimSpace(botID),
		secret:         strings.TrimSpace(secret),
		wsURL:          strings.TrimSpace(wsURL),
		pendingStreams: make(map[string]wecomPendingStream),
		msgSource:      make(map[string]string),
		ackWaiters:     make(map[string]chan wecomAIBotAck),
	}
	if g.wsURL == "" {
		g.wsURL = wecomAIBotDefaultWSURL
	}

	g.BasePlatform = NewBasePlatform("wecom", config)
	g.BasePlatform.onConnect = g.onConnect
	g.BasePlatform.onDisconnect = g.onDisconnect
	g.BasePlatform.onSend = g.onSend

	return g
}

// onConnect 启动后台连接循环（非阻塞）：端点不可达时自动退避重试。
func (g *WeComAIBotGateway) onConnect(ctx context.Context) error {
	if g.botID == "" || g.secret == "" {
		// Registered for API access / QR pairing but not configured yet: report
		// the truthful state instead of letting Connect() claim a fake
		// "connected" (the run loop never starts without credentials).
		g.markDisconnected(fmt.Errorf("wecom not configured: bot_id/secret missing (set gateway.platforms.wecom to enable)"))
		return nil
	}
	log.Infof("[WeCom/AIBot] Starting connection loop for %s", g.wsURL)
	go g.run(ctx)
	return nil
}

func (g *WeComAIBotGateway) onDisconnect() error {
	g.wsMu.Lock()
	if g.wsConn != nil {
		_ = g.wsConn.Close()
		g.wsConn = nil
	}
	g.wsMu.Unlock()
	g.resetConversationState()
	log.Info("[WeCom/AIBot] Gateway disconnected")
	return nil
}

// run 是连接主循环：拨号 -> 认证 -> 心跳 -> 收发，断线后退避重连。
func (g *WeComAIBotGateway) run(ctx context.Context) {
	delay := wecomAIBotReconnectMin
	for {
		if ctx.Err() != nil {
			return
		}

		// Connect() 不再自行标记 connected；这里以拨号结果为准：连接建立前先
		// 置 connecting，只有订阅确认(errcode==0)后才 markConnected。
		g.setConnected(false)
		GetStatusManager().SetState(g.Name(), StateConnecting)

		conn, err := g.dial(ctx)
		if err != nil {
			log.Warnf("[WeCom/AIBot] Cannot reach %s: %v (retry in %v)", g.wsURL, err, delay)
			GetStatusManager().SetError(g.Name(), err)
			GetStatusManager().SetState(g.Name(), StateDisconnected)
			if !sleepCtx(ctx, delay) {
				return
			}
			delay *= 2
			if delay > wecomAIBotReconnectMax {
				delay = wecomAIBotReconnectMax
			}
			continue
		}
		delay = wecomAIBotReconnectMin

		// 先启动读取循环（读取订阅响应与后续消息回调；其返回即连接断开）。
		readErr := make(chan error, 1)
		go func() { readErr <- g.readLoop(ctx, conn) }()

		// 认证帧。注册订阅等待后发送；必须等到服务端 errcode==0 的确认，
		// 否则不置 connected —— 防止"看似已连接但订阅被拒、永远收不到消息"的静默失败。
		reqID := g.nextReqID()
		g.setSubscribeWait(reqID)
		if err := g.write(conn, wecomAIBotFrame{
			Cmd:     "aibot_subscribe",
			Headers: wecomAIBotHeaders{ReqID: reqID},
			Body:    mustJSONRaw(map[string]string{"bot_id": g.botID, "secret": g.secret}),
		}); err != nil {
			log.Warnf("[WeCom/AIBot] Subscribe write failed: %v (retry in %v)", err, delay)
			g.clearSubscribeWait()
			_ = conn.Close()
			if !sleepCtx(ctx, delay) {
				return
			}
			continue
		}

		select {
		case ack := <-g.takeSubscribeAck():
			if ack.ErrCode != 0 {
				log.Warnf("[WeCom/AIBot] Subscribe rejected: errcode=%d errmsg=%q — check bot_id/secret and that the bot's API mode is 'long-connection' (retry in %v)",
					ack.ErrCode, ack.ErrMsg, delay)
				GetStatusManager().SetError(g.Name(), fmt.Errorf("wecom subscribe rejected: errcode=%d errmsg=%s", ack.ErrCode, ack.ErrMsg))
				_ = conn.Close()
				if !sleepCtx(ctx, delay) {
					return
				}
				continue
			}
			log.Infof("[WeCom/AIBot] Subscribe confirmed (errcode=0) as bot %s", g.botID)
		case err := <-readErr:
			log.Warnf("[WeCom/AIBot] Connection closed while subscribing: %v (retry in %v)", err, delay)
			_ = conn.Close()
			if !sleepCtx(ctx, delay) {
				return
			}
			continue
		case <-time.After(wecomAIBotSubscribeTimeout):
			log.Warnf("[WeCom/AIBot] No subscribe ack within %v (retry in %v)", wecomAIBotSubscribeTimeout, delay)
			_ = conn.Close()
			if !sleepCtx(ctx, delay) {
				return
			}
			continue
		case <-ctx.Done():
			_ = conn.Close()
			return
		}
		g.clearSubscribeWait()

		g.attach(conn)
		// Subscribe confirmed (errcode==0) — only now is the link real.
		g.markConnected()
		log.Infof("[WeCom/AIBot] Connected & subscribed as bot %s", g.botID)

		// 心跳
		hbCtx, hbCancel := context.WithCancel(ctx)
		go g.heartbeatLoop(hbCtx, conn)

		err = <-readErr
		hbCancel()
		g.detach(conn)
		g.resetConversationState() // 旧连接上的 req_id/stream 已失效，清理在途占位等会话态

		if ctx.Err() != nil {
			return
		}
		log.Warnf("[WeCom/AIBot] Connection lost: %v (reconnecting in %v)", err, delay)
		if !sleepCtx(ctx, delay) {
			return
		}
	}
}

func (g *WeComAIBotGateway) dial(ctx context.Context) (*websocket.Conn, error) {
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.DialContext(ctx, g.wsURL, http.Header{})
	if err != nil {
		return nil, err
	}
	return conn, nil
}

// setSubscribeWait 注册当前连接尝试等待的订阅 req_id 与接收通道。
func (g *WeComAIBotGateway) setSubscribeWait(reqID string) {
	g.ackMu.Lock()
	defer g.ackMu.Unlock()
	g.subAckCh = make(chan wecomAIBotAck, 1)
	g.subReqID = reqID
}

// takeSubscribeAck 返回订阅响应通道（调用方 select 消费）。
func (g *WeComAIBotGateway) takeSubscribeAck() <-chan wecomAIBotAck {
	g.ackMu.Lock()
	defer g.ackMu.Unlock()
	return g.subAckCh
}

// clearSubscribeWait 结束订阅等待（成功或连接关闭后调用）。
func (g *WeComAIBotGateway) clearSubscribeWait() {
	g.ackMu.Lock()
	defer g.ackMu.Unlock()
	g.subAckCh = nil
	g.subReqID = ""
}

// handleAck 处理服务端响应帧：优先投递给该 req_id 的 ack 等待者（sendFrameSync），
// 否则若与当前等待的订阅 req_id 匹配则投递订阅通道；都不匹配仅记录非零 errcode。
func (g *WeComAIBotGateway) handleAck(ack wecomAIBotAck) {
	if ack.ErrCode == 0 {
		log.Debugf("[WeCom/AIBot] ack ok (req_id=%s)", ack.Headers.ReqID)
	} else {
		log.Warnf("[WeCom/AIBot] server error: errcode=%d errmsg=%q (req_id=%s)",
			ack.ErrCode, ack.ErrMsg, ack.Headers.ReqID)
	}

	// 1) 通用等待者：回复/推送帧的同步确认（sendFrameSync 注册后在此收尾）。
	if reqID := ack.Headers.ReqID; reqID != "" {
		g.ackMu.Lock()
		wch := g.ackWaiters[reqID]
		delete(g.ackWaiters, reqID)
		g.ackMu.Unlock()
		if wch != nil {
			select {
			case wch <- ack:
			default:
			}
			return
		}
	}

	// 2) 订阅等待（subAckCh）
	g.ackMu.Lock()
	ch := g.subAckCh
	want := g.subReqID
	g.ackMu.Unlock()
	if ch == nil {
		return
	}
	if want != "" && ack.Headers.ReqID != "" && ack.Headers.ReqID != want {
		return
	}
	select {
	case ch <- ack:
	default:
	}
}

// resetConversationState 连接断开后清理全部会话态：在途占位（req_id/stream.id 随旧连接
// 失效）、已学习的会话类型、未消费的 ack 等待者。防止重连后误用旧链路。
func (g *WeComAIBotGateway) resetConversationState() {
	g.pendingStreamsMu.Lock()
	g.pendingStreams = make(map[string]wecomPendingStream)
	g.pendingStreamsMu.Unlock()

	g.msgSourceMu.Lock()
	g.msgSource = make(map[string]string)
	g.msgSourceMu.Unlock()

	g.ackMu.Lock()
	g.ackWaiters = make(map[string]chan wecomAIBotAck)
	g.ackMu.Unlock()
}

// heartbeatLoop 每 30s 发送一次 ping 帧保持连接。
func (g *WeComAIBotGateway) heartbeatLoop(ctx context.Context, conn *websocket.Conn) {
	ticker := time.NewTicker(wecomAIBotHeartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := g.write(conn, wecomAIBotFrame{
				Cmd:     "ping",
				Headers: wecomAIBotHeaders{ReqID: g.nextReqID()},
			}); err != nil {
				log.Debugf("[WeCom/AIBot] Heartbeat write failed: %v", err)
				return
			}
		}
	}
}

func (g *WeComAIBotGateway) attach(conn *websocket.Conn) {
	g.wsMu.Lock()
	g.wsConn = conn
	// 补发未发出的占位帧：订阅确认与早期消息回调竞态时，回调先于 attach 到达，
	// 当时占位发送被推迟（连接尚未挂到网关）。必须在置 connected 之前补发完成 ——
	// 保证占位帧先于任何 finish 帧到达服务端，且 onSend 消费 pending 前占位已就位。
	g.pendingStreamsMu.Lock()
	for id, p := range g.pendingStreams {
		if p.ClaimSent {
			continue
		}
		if err := g.write(conn, wecomClaimFrame(p)); err != nil {
			log.Warnf("[WeCom/AIBot] deferred claim flush failed for %s: %v — dropping placeholder, reply will fall back to active push", id, err)
			delete(g.pendingStreams, id)
			continue
		}
		p.ClaimSent = true
		g.pendingStreams[id] = p
	}
	g.pendingStreamsMu.Unlock()
	g.wsMu.Unlock()
	g.setConnected(true)
}

func (g *WeComAIBotGateway) detach(conn *websocket.Conn) {
	g.wsMu.Lock()
	if g.wsConn == conn {
		g.wsConn = nil
	}
	g.wsMu.Unlock()
	_ = conn.Close()
	g.setConnected(false)
}

// write 通过指定连接发送一帧。
func (g *WeComAIBotGateway) write(conn *websocket.Conn, frame wecomAIBotFrame) error {
	data, err := json.Marshal(frame)
	if err != nil {
		return fmt.Errorf("marshal frame: %w", err)
	}
	return conn.WriteMessage(websocket.TextMessage, data)
}

// readLoop 持续读取下行帧：消息回调进入统一 Message 流，其余忽略。
func (g *WeComAIBotGateway) readLoop(ctx context.Context, conn *websocket.Conn) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		_, data, err := conn.ReadMessage()
		if err != nil {
			return err
		}

		var frame wecomAIBotFrame
		if err := json.Unmarshal(data, &frame); err != nil {
			log.Debugf("[WeCom/AIBot] Failed to parse frame: %v", err)
			continue
		}

		if frame.Cmd == "" {
			// 服务端对订阅/心跳请求的响应帧不带 cmd（仅 headers+errcode+errmsg）。
			var ack wecomAIBotAck
			if err := json.Unmarshal(data, &ack); err == nil && (ack.ErrCode != 0 || ack.Headers.ReqID != "" || ack.ErrMsg != "") {
				g.handleAck(ack)
			} else {
				log.Debugf("[WeCom/AIBot] Unrecognized frame: %s", clipRunes(string(data), 200))
			}
			continue
		}

		switch frame.Cmd {
		case "aibot_msg_callback":
			g.handleMessageEvent(frame)
		case "aibot_event_callback":
			g.handleEventCallback(frame)
		case "ack", "pong", "ping":
			// 模板卡片确认等：最小闭环暂不处理
		default:
			log.Debugf("[WeCom/AIBot] Unknown cmd: %s", frame.Cmd)
		}
	}
}

// handleEventCallback 记录事件回调（enter_chat / 模板卡片交互等），用于确认通道活跃。
func (g *WeComAIBotGateway) handleEventCallback(frame wecomAIBotFrame) {
	var ev struct {
		Event string `json:"event"`
		Type  string `json:"type"`
	}
	_ = json.Unmarshal(frame.Body, &ev)
	name := ev.Event
	if name == "" {
		name = ev.Type
	}
	log.Infof("[WeCom/AIBot] event callback: %s", name)
}

// handleMessageEvent 解析 aibot_msg_callback 并转发为统一 Message。
func (g *WeComAIBotGateway) handleMessageEvent(frame wecomAIBotFrame) {
	var body wecomAIBotMessageBody
	if err := json.Unmarshal(frame.Body, &body); err != nil {
		log.Debugf("[WeCom/AIBot] Failed to parse message body: %v", err)
		return
	}
	// 最小闭环：只处理文本消息
	if body.MsgType != "text" {
		log.Debugf("[WeCom/AIBot] Dropping non-text message (msgtype=%s)", body.MsgType)
		return
	}
	content := strings.TrimSpace(body.Text.Content)
	if content == "" {
		return
	}

	userID := strings.TrimSpace(body.From.UserID)
	if userID == "" {
		userID = body.ChatID // 兜底
	}
	if userID == "" {
		log.Debugf("[WeCom/AIBot] Message without sender, dropping")
		return
	}

	msg := Message{
		ID:        body.MsgID,
		Platform:  "wecom",
		UserID:    userID,
		Content:   content,
		Timestamp: time.Now(),
	}

	channelID := userID
	switch body.ChatType {
	case "group":
		if body.ChatID == "" {
			log.Debugf("[WeCom/AIBot] Group message without chatid, dropping")
			return
		}
		if !g.ShouldProcessChannel(body.ChatID) {
			return
		}
		msg.ChannelID = body.ChatID
		msg.IsGroup = true
		// AI Bot 回调只在召唤 bot 时下发（单聊直发 / 群聊 @），帧本身不带 @ 标记，
		// 因此群消息一律按 mentioned 处理，具体放行与否交给上层 group_policy/allowlist。
		msg.IsMentioned = true
		g.cacheSource(body.ChatID, "group")
		channelID = body.ChatID
	default: // single 及其它
		msg.ChannelID = userID
		msg.IsMentioned = true
		g.cacheSource(userID, "private")
	}
	msg.Metadata = map[string]interface{}{
		"type":         body.ChatType,
		"msgtype":      body.MsgType,
		"chatid":       body.ChatID,
		"aibot_id":     body.AibotID,
		"response_url": body.ResponseURL,
		"req_id":       frame.Headers.ReqID,
	}

	// 到达日志（内容截断）：用于确认回调确实下发到网关。
	log.Infof("[WeCom/AIBot] msg callback: chattype=%s msgtype=%s from=%s content=%q",
		body.ChatType, body.MsgType, userID, clipRunes(content, 80))

	// 流式占位：消息能通过访问控制（EmitMessage 同款判定）就先发 finish=false 占位，
	// 让用户立即看到"正在思考"；最终回复（onSend）用同一 req_id+stream.id 原位替换。
	// 必须在 EmitMessage 之前完成 —— 被访问控制拒绝的消息不应留下悬挂占位。
	if g.ShouldAcceptMessage(msg) {
		g.claimPassiveReply(channelID, frame.Headers.ReqID)
	}

	g.EmitMessage(msg)
}

// clipRunes 按 rune 截断字符串用于日志展示。
func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// onSend 把回复发回对应会话。
//
// 优先走"被动回复 + 流式替换"（handleMessageEvent 收到回调时已发 finish=false 占位，
// 此处用同一 req_id+stream.id 发 finish=true 全量替换 —— 回复帧 msgtype=stream，
// 不使用 text：被动回复 msgtype 仅支持 stream/template_card/markdown/file/image/
// voice/video，用 text 会被企微以 errcode=40008 invalid message type 拒收）。
// finish 帧被服务端拒绝/超时，或本就没有在途回调（主动场景）时，降级为
// aibot_send_msg + markdown 主动推送（需用户先在本会话给机器人发过消息）。
func (g *WeComAIBotGateway) onSend(ctx context.Context, resp Response) error {
	if !g.IsConnected() {
		return fmt.Errorf("WeCom AI Bot gateway not connected")
	}
	if resp.ChannelID == "" {
		return fmt.Errorf("channel ID is required")
	}
	channelID := resp.ChannelID
	content := trimWeComContent(strings.TrimSpace(resp.Content))
	if content == "" {
		// 空回复不回帧；占位若已发出会由企微在 10 分钟后自动结束。
		log.Debugf("[WeCom/AIBot] empty reply for %s, skipping send", channelID)
		return nil
	}

	// 1) 被动链路：占位已在回调时发出，用同一 req_id+stream.id 发 finish=true 全量替换。
	if p, ok := g.takePendingStream(channelID); ok {
		frame := wecomAIBotFrame{
			Cmd:     "aibot_respond_msg",
			Headers: wecomAIBotHeaders{ReqID: p.ReqID},
			Body: mustJSONRaw(wecomAIBotStreamReply{
				MsgType: "stream",
				Stream: wecomAIBotStream{
					ID:      p.StreamID,
					Finish:  true,
					Content: content,
				},
			}),
		}
		if err := g.sendFrameSync(frame); err == nil {
			return nil
		} else {
			log.Warnf("[WeCom/AIBot] stream finish rejected (req_id=%s stream=%s): %v — falling back to active push",
				p.ReqID, p.StreamID, err)
		}
	}

	// 2) 主动推送（无在途回调 / finish 被拒）：aibot_send_msg + markdown。
	return g.sendMarkdownPush(channelID, g.detectSource(channelID), content)
}

// claimPassiveReply 为回调消息建立被动回复链路：分配 stream.id、记录 pending，
// 并发出 finish=false 占位帧（用户在等待 LLM 期间立即看到"正在思考"）。
// 连接尚未就绪（订阅确认与早期回调竞态窗口）时只登记不发送，由 attach() 在置
// connected 前统一补发；连接就绪时直接发送并标记 ClaimSent。
// 注意：不要在发送失败时清空 pending —— 除 attach 补发失败外，onSend 仍可复用
// 同一 req_id/stream.id 发 finish=true 全量替换（无需占位先行的场景官方同样支持）。
func (g *WeComAIBotGateway) claimPassiveReply(channelID, reqID string) {
	if channelID == "" || reqID == "" {
		return
	}
	p := wecomPendingStream{ReqID: reqID, StreamID: g.nextStreamID()}
	g.cachePendingStream(channelID, p)
	if !g.trySendClaim(channelID, p) {
		log.Debugf("[WeCom/AIBot] claim for %s deferred until connection attach", channelID)
	}
}

// wecomClaimFrame 构造 finish=false 的占位帧。
func wecomClaimFrame(p wecomPendingStream) wecomAIBotFrame {
	return wecomAIBotFrame{
		Cmd:     "aibot_respond_msg",
		Headers: wecomAIBotHeaders{ReqID: p.ReqID},
		Body: mustJSONRaw(wecomAIBotStreamReply{
			MsgType: "stream",
			Stream: wecomAIBotStream{
				ID:      p.StreamID,
				Finish:  false,
				Content: wecomAIBotClaimText,
			},
		}),
	}
}

// trySendClaim 尝试立即发送占位帧。返回 false 表示连接未就绪或写入失败 ——
// pending 记录保留，由 attach() 补发或 onSend 直接走 finish 全量替换。
// 持有 wsMu 串行化写入（与 attach 补发互斥），成功后在同一窗口内标记 ClaimSent，
// 防止补发与直发重复。
func (g *WeComAIBotGateway) trySendClaim(channelID string, p wecomPendingStream) bool {
	g.wsMu.Lock()
	defer g.wsMu.Unlock()
	if g.wsConn == nil {
		return false
	}
	if err := g.write(g.wsConn, wecomClaimFrame(p)); err != nil {
		log.Warnf("[WeCom/AIBot] claim placeholder send failed for %s (req_id=%s stream=%s): %v",
			channelID, p.ReqID, p.StreamID, err)
		return false
	}
	g.pendingStreamsMu.Lock()
	if cur, ok := g.pendingStreams[channelID]; ok && cur.StreamID == p.StreamID {
		cur.ClaimSent = true
		g.pendingStreams[channelID] = cur
	}
	g.pendingStreamsMu.Unlock()
	return true
}

// sendFrame 通过当前连接发送一帧（不等待服务端确认）。
func (g *WeComAIBotGateway) sendFrame(frame wecomAIBotFrame) error {
	g.wsMu.Lock()
	defer g.wsMu.Unlock()
	if g.wsConn == nil {
		return fmt.Errorf("not connected")
	}
	return g.write(g.wsConn, frame)
}

// sendFrameSync 发送一帧并等待服务端 ack（errcode==0）后才返回；超时或 errcode!=0
// 返回错误。调用方据此降级（如 finish 被拒时改走主动推送）。
// 注意：禁止在 readLoop goroutine 内调用（ack 由 readLoop 读取，会互相等待）。
func (g *WeComAIBotGateway) sendFrameSync(frame wecomAIBotFrame) error {
	reqID := frame.Headers.ReqID
	if reqID == "" {
		return g.sendFrame(frame)
	}
	ch := make(chan wecomAIBotAck, 1)
	g.ackMu.Lock()
	g.ackWaiters[reqID] = ch
	g.ackMu.Unlock()

	if err := g.sendFrame(frame); err != nil {
		g.ackMu.Lock()
		delete(g.ackWaiters, reqID)
		g.ackMu.Unlock()
		return err
	}

	select {
	case ack := <-ch:
		if ack.ErrCode != 0 {
			return fmt.Errorf("server error: errcode=%d errmsg=%q", ack.ErrCode, ack.ErrMsg)
		}
		return nil
	case <-time.After(wecomAIBotAckTimeout):
		g.ackMu.Lock()
		delete(g.ackWaiters, reqID)
		g.ackMu.Unlock()
		return fmt.Errorf("no server ack within %v", wecomAIBotAckTimeout)
	}
}

// sendMarkdownPush 通过 aibot_send_msg 向指定会话主动推送 markdown 消息。
// 前置条件：用户需先在本会话给机器人发过消息；chat_type 1=单聊 2=群聊。
func (g *WeComAIBotGateway) sendMarkdownPush(channelID, source, content string) error {
	body := wecomAIBotMarkdownPush{MsgType: "markdown", ChatID: channelID}
	body.Markdown.Content = content
	if source == "group" {
		body.ChatType = 2
	} else {
		body.ChatType = 1
	}
	return g.sendFrameSync(wecomAIBotFrame{
		Cmd:     "aibot_send_msg",
		Headers: wecomAIBotHeaders{ReqID: g.nextReqID()},
		Body:    mustJSONRaw(body),
	})
}

// SendText 主动向指定会话推送文本（channelID 为 userid 或 chatid，自动判型）。
// 走 markdown 消息（aibot_send_msg 不支持 text）。
func (g *WeComAIBotGateway) SendText(channelID, text string) error {
	if channelID == "" {
		return fmt.Errorf("channel ID is required")
	}
	content := trimWeComContent(text)
	if content == "" {
		return nil
	}
	return g.sendMarkdownPush(channelID, g.detectSource(channelID), content)
}

// trimWeComContent 把内容限制在官方 content 上限（20480 字节）内，按 rune 边界截断。
// stream 与 markdown 的 content 均为此上限；单条回复超过该长度的情况极其罕见。
func trimWeComContent(s string) string {
	if len(s) <= wecomAIBotContentMaxBytes {
		return s
	}
	var b []byte
	for _, r := range s {
		if len(b)+utf8.RuneLen(r) > wecomAIBotContentMaxBytes {
			break
		}
		b = utf8.AppendRune(b, r)
	}
	log.Warnf("[WeCom/AIBot] reply too long (%d bytes > %d limit), truncating at rune boundary",
		len(s), wecomAIBotContentMaxBytes)
	return string(b)
}

func (g *WeComAIBotGateway) cacheSource(channelID, msgType string) {
	g.msgSourceMu.Lock()
	g.msgSource[channelID] = msgType
	g.msgSourceMu.Unlock()
}

func (g *WeComAIBotGateway) detectSource(channelID string) string {
	g.msgSourceMu.RLock()
	t, ok := g.msgSource[channelID]
	g.msgSourceMu.RUnlock()
	if ok {
		return t
	}
	return "private"
}

func (g *WeComAIBotGateway) cachePendingStream(channelID string, p wecomPendingStream) {
	g.pendingStreamsMu.Lock()
	defer g.pendingStreamsMu.Unlock()
	if g.pendingStreams == nil {
		g.pendingStreams = make(map[string]wecomPendingStream)
	}
	g.pendingStreams[channelID] = p
}

// takePendingStream 取走并删除某会话的在途占位记录（finish 发完即一次性消费）。
func (g *WeComAIBotGateway) takePendingStream(channelID string) (wecomPendingStream, bool) {
	g.pendingStreamsMu.Lock()
	defer g.pendingStreamsMu.Unlock()
	p, ok := g.pendingStreams[channelID]
	if ok {
		delete(g.pendingStreams, channelID)
	}
	return p, ok
}

func (g *WeComAIBotGateway) nextReqID() string {
	return "gm" + strconv.FormatInt(g.reqCounter.Add(1), 10)
}

// nextStreamID 生成流式消息 stream.id（每消息唯一，客户端自定义即可）。
func (g *WeComAIBotGateway) nextStreamID() string {
	return "gms" + strconv.FormatInt(g.reqCounter.Add(1), 10)
}

// HandleSlashCommand 处理网关斜杠命令。
func (g *WeComAIBotGateway) HandleSlashCommand(cmd string, msg Message) (Response, error) {
	switch cmd {
	case "help":
		return Response{
			Content: "🤖 Magic Bot - WeCom (AI Bot)\n\n" +
				"📋 Commands:\n" +
				"/help - Show this help\n" +
				"/ping - Check bot status\n" +
				"/status - Connection status",
		}, nil
	case "ping":
		return Response{Content: "Pong! 🏓"}, nil
	case "status":
		if g.IsConnected() {
			return Response{Content: "✅ Bot is connected and ready!"}, nil
		}
		return Response{Content: "❌ Bot is not connected"}, nil
	default:
		return Response{}, fmt.Errorf("unknown command: %s", cmd)
	}
}

// CheckHealth 返回健康状态。
func (g *WeComAIBotGateway) CheckHealth() *HealthStatus {
	status := g.BasePlatform.CheckHealth()
	status.Platform = "wecom"
	status.Platforms = make(map[string]PlatformStatus)

	platformStatus := PlatformStatus{Name: "wecom", Status: "connected"}
	if g.botID == "" || g.secret == "" {
		platformStatus.Status = "not_configured"
		platformStatus.Error = "bot_id/secret not configured"
		status.Status = "not_configured"
		status.Platforms["wecom"] = platformStatus
		return status
	}
	if !g.IsConnected() {
		platformStatus.Status = "disconnected"
		if status.Error != "" {
			platformStatus.Error = status.Error
		} else {
			platformStatus.Error = "Not connected to WeCom AI Bot endpoint"
		}
		status.Status = "error"
		status.Platforms["wecom"] = platformStatus
		return status
	}

	if status.Details == nil {
		status.Details = make(map[string]interface{})
	}
	status.Details["mode"] = "aibot"
	status.Details["bot_id"] = g.botID
	status.Details["ws_url"] = g.wsURL
	status.Platforms["wecom"] = platformStatus
	return status
}

// mustJSONRaw 把任意值编码为 RawMessage；失败返回空对象，不 panic。
func mustJSONRaw(v interface{}) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return b
}

// sleepCtx 睡眠 d 时长；ctx 提前取消时返回 false（调用方应退出循环）。
func sleepCtx(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}
