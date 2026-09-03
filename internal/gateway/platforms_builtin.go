package gateway

import (
	"context"
	"fmt"
)

func init() {
	RegisterBuiltinPlatforms()
}

// RegisterBuiltinPlatforms registers all built-in platforms with the registry
func RegisterBuiltinPlatforms() {
	registry := GetRegistry()

	// QQ（仅官方机器人 API；个人 QQ 扫码 NapCat/LLOneBot OneBot 模式已于 2026-09 移除）
	registry.Register(PlatformInfo{
		ID:          "qq",
		Name:        "QQ",
		Description: "QQ 官方机器人 API（应用接入）",
		Version:     "1.0.0",
		Author:      "go-magic",
		Tags:        []string{"qq", "tencent", "official"},
	}, func(ctx context.Context, config map[string]interface{}) (PlatformHandler, error) {
		appID := getConfigString(config, "app_id", "number")
		appSecret := getConfigString(config, "app_secret", "password")
		sandbox, _ := config["sandbox"].(bool)
		intent, _ := config["intent"].(int)
		return NewQQGateway(appID, appSecret, sandbox, intent), nil
	})

	// Telegram
	registry.Register(PlatformInfo{
		ID:          "telegram",
		Name:        "Telegram",
		Description: "Telegram Bot API",
		Version:     "1.0.0",
		Author:      "go-magic",
		Tags:        []string{"telegram", "bot"},
	}, func(ctx context.Context, config map[string]interface{}) (PlatformHandler, error) {
		token, _ := config["token"].(string)
		if token == "" {
			return nil, fmt.Errorf("telegram: token is required")
		}
		return NewTelegramHandler(token, nil)
	})

	// Discord
	registry.Register(PlatformInfo{
		ID:          "discord",
		Name:        "Discord",
		Description: "Discord Bot API",
		Version:     "1.0.0",
		Author:      "go-magic",
		Tags:        []string{"discord", "bot"},
	}, func(ctx context.Context, config map[string]interface{}) (PlatformHandler, error) {
		token, _ := config["token"].(string)
		if token == "" {
			return nil, fmt.Errorf("discord: token is required")
		}
		return NewDiscordGateway(token)
	})

	// Slack
	registry.Register(PlatformInfo{
		ID:          "slack",
		Name:        "Slack",
		Description: "Slack Bot API",
		Version:     "1.0.0",
		Author:      "go-magic",
		Tags:        []string{"slack", "bot"},
	}, func(ctx context.Context, config map[string]interface{}) (PlatformHandler, error) {
		botToken := getConfigString(config, "bot_token", "token")
		signingSecret := getConfigString(config, "signing_secret", "app_secret")
		if botToken == "" {
			return nil, fmt.Errorf("slack: bot_token is required")
		}
		return NewSlackGateway(botToken, signingSecret), nil
	})

	// Feishu / Lark
	registry.Register(PlatformInfo{
		ID:          "feishu",
		Name:        "Feishu",
		Description: "Feishu (Lark) Bot API",
		Version:     "1.0.0",
		Author:      "go-magic",
		Tags:        []string{"feishu", "lark", "bytedance"},
	}, func(ctx context.Context, config map[string]interface{}) (PlatformHandler, error) {
		appID, _ := config["app_id"].(string)
		appSecret, _ := config["app_secret"].(string)
		return NewFeishuGateway(appID, appSecret), nil
	})

	// WeChat iLink
	registry.Register(PlatformInfo{
		ID:          "wechat_ilink",
		Name:        "WeChat iLink",
		Description: "WeChat personal account via iLink Bot API",
		Version:     "1.0.0",
		Author:      "go-magic",
		Tags:        []string{"wechat", "ilink", "personal"},
	}, func(ctx context.Context, config map[string]interface{}) (PlatformHandler, error) {
		token, _ := config["token"].(string)
		baseURL, _ := config["base_url"].(string)
		dataDir, _ := config["data_dir"].(string)
		if token == "" {
			return nil, fmt.Errorf("wechat_ilink: token is required")
		}
		cfg := WeChatILinkConfig{
			Token:   token,
			BaseURL: baseURL,
			DataDir: dataDir,
		}
		return NewWeChatILinkGateway(cfg), nil
	})

	// WeCom (WeChat Work) —— 仅保留官方智能机器人（扫码创建）
	// 自建应用 (app, corp_id/agent_id/secret API 回调) 模式已于 2026-09 移除。
	registry.Register(PlatformInfo{
		ID:          "wecom",
		Name:        "WeCom",
		Description: "WeCom official AI Bot (官方智能机器人扫码): bot_id/secret, WebSocket 免公网",
		Version:     "1.2.0",
		Author:      "go-magic",
		Tags:        []string{"wecom", "wechat_work", "aibot"},
	}, func(ctx context.Context, config map[string]interface{}) (PlatformHandler, error) {
		mode := getConfigString(config, "mode", "")
		if mode == "app" || (mode == "" && config["corp_id"] != nil) {
			// 历史自建应用配置：明确报错引导迁移，避免静默失效
			return nil, fmt.Errorf("wecom: self-built app mode (corp_id/agent_id) was removed (2026-09); only official AI Bot is supported — delete corp_id/agent_id and set bot_id/secret, or re-scan to create in Web UI")
		}
		botID := getConfigString(config, "bot_id", "")
		secret := getConfigString(config, "secret", "bot_secret")
		if mode == "aibot" && botID == "" {
			return nil, fmt.Errorf("wecom: bot_id is required")
		}
		// bot_id 为空（平台未启用/未配置）时返回空实例，onConnect 仅告警跳过
		return NewWeComAIBotGateway(botID, secret, getConfigString(config, "ws_url", "")), nil
	})

	// DingTalk
	registry.Register(PlatformInfo{
		ID:          "dingtalk",
		Name:        "DingTalk",
		Description: "DingTalk Bot API",
		Version:     "1.0.0",
		Author:      "go-magic",
		Tags:        []string{"dingtalk", "alibaba"},
	}, func(ctx context.Context, config map[string]interface{}) (PlatformHandler, error) {
		appKey, _ := config["app_key"].(string)
		appSecret, _ := config["app_secret"].(string)
		agentID, _ := config["agent_id"].(string)
		gw := NewDingTalkGateway(appKey, appSecret)
		if agentID != "" {
			gw.SetAgentID(agentID)
		}
		return gw, nil
	})

	// LINE
	registry.Register(PlatformInfo{
		ID:          "line",
		Name:        "LINE",
		Description: "LINE Bot API",
		Version:     "1.0.0",
		Author:      "go-magic",
		Tags:        []string{"line", "bot"},
	}, func(ctx context.Context, config map[string]interface{}) (PlatformHandler, error) {
		channelSecret := getConfigString(config, "channel_secret", "app_secret")
		channelToken := getConfigString(config, "channel_token", "token")
		if channelSecret == "" || channelToken == "" {
			return nil, fmt.Errorf("line: channel_secret and channel_token are required")
		}
		return NewLineGateway(channelSecret, channelToken), nil
	})

	// Matrix
	registry.Register(PlatformInfo{
		ID:          "matrix",
		Name:        "Matrix",
		Description: "Matrix protocol client",
		Version:     "1.0.0",
		Author:      "go-magic",
		Tags:        []string{"matrix", "federated"},
	}, func(ctx context.Context, config map[string]interface{}) (PlatformHandler, error) {
		homeserver := getConfigString(config, "homeserver", "api_url")
		userID := getConfigString(config, "user_id", "app_id")
		token, _ := config["token"].(string)
		password := getConfigString(config, "password", "app_secret")
		mode, _ := config["mode"].(string)

		if mode == "password" && password != "" {
			return NewMatrixGatewayWithLogin(homeserver, userID, password, "")
		}
		if homeserver == "" || userID == "" || token == "" {
			return nil, fmt.Errorf("matrix: homeserver, user_id and token are required")
		}
		return NewMatrixGateway(homeserver, userID, token), nil
	})

	// Microsoft Teams (Bot Framework) —— 2026-09 海外新增
	registry.Register(PlatformInfo{
		ID:          "teams",
		Name:        "Microsoft Teams",
		Description: "Microsoft Teams Bot Framework (app_id/app_secret, webhook 回调)",
		Version:     "1.0.0",
		Author:      "go-magic",
		Tags:        []string{"teams", "microsoft", "bot-framework"},
	}, func(ctx context.Context, config map[string]interface{}) (PlatformHandler, error) {
		appID := getConfigString(config, "app_id", "")
		appSecret := getConfigString(config, "app_secret", "password")
		// 未配置时返回空实例，onConnect 会 markDisconnected 如实上报；配置错误由 onConnect 返回错误
		return NewTeamsGateway(appID, appSecret), nil
	})

	// Google Chat —— 2026-09 海外新增
	registry.Register(PlatformInfo{
		ID:          "googlechat",
		Name:        "Google Chat",
		Description: "Google Chat incoming webhook + Events API (webhook_url/events_token)",
		Version:     "1.0.0",
		Author:      "go-magic",
		Tags:        []string{"google", "gchat", "chat"},
	}, func(ctx context.Context, config map[string]interface{}) (PlatformHandler, error) {
		webhookURL := getConfigString(config, "webhook_url", "")
		eventsToken := getConfigString(config, "events_token", "token")
		return NewGoogleChatGateway(webhookURL, eventsToken), nil
	})

	// Email —— 2026-09 海外新增（IMAP 收 + SMTP 发）
	registry.Register(PlatformInfo{
		ID:          "email",
		Name:        "Email",
		Description: "Email gateway (IMAP receive + SMTP send)",
		Version:     "1.0.0",
		Author:      "go-magic",
		Tags:        []string{"email", "imap", "smtp"},
	}, func(ctx context.Context, config map[string]interface{}) (PlatformHandler, error) {
		return NewEmailGateway(parseEmailConfig(config)), nil
	})

	// SMS —— 2026-09 海外新增（Twilio）
	registry.Register(PlatformInfo{
		ID:          "sms",
		Name:        "SMS",
		Description: "SMS gateway via Twilio (account_sid/auth_token/from)",
		Version:     "1.0.0",
		Author:      "go-magic",
		Tags:        []string{"sms", "twilio", "phone"},
	}, func(ctx context.Context, config map[string]interface{}) (PlatformHandler, error) {
		accountSID := getConfigString(config, "account_sid", "sid")
		authToken := getConfigString(config, "auth_token", "token", "password")
		fromNumber := getConfigString(config, "from", "from_number")
		return NewSmsGateway(accountSID, authToken, fromNumber), nil
	})
}
