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

	// QQ
	registry.Register(PlatformInfo{
		ID:          "qq",
		Name:        "QQ",
		Description: "QQ 频道机器人官方 API",
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

	// WhatsApp (personal)
	registry.Register(PlatformInfo{
		ID:          "whatsapp",
		Name:        "WhatsApp",
		Description: "WhatsApp personal account gateway (whatsmeow)",
		Version:     "1.0.0",
		Author:      "go-magic",
		Tags:        []string{"whatsapp", "whatsmeow", "personal"},
	}, func(ctx context.Context, config map[string]interface{}) (PlatformHandler, error) {
		dataDir, _ := config["data_dir"].(string)
		return NewWhatsAppGateway(dataDir), nil
	})

	// WhatsApp Business
	registry.Register(PlatformInfo{
		ID:          "whatsapp_business",
		Name:        "WhatsApp Business",
		Description: "WhatsApp Business API (webhook-based)",
		Version:     "1.0.0",
		Author:      "go-magic",
		Tags:        []string{"whatsapp", "business", "whatsapp_business"},
	}, func(ctx context.Context, config map[string]interface{}) (PlatformHandler, error) {
		phoneNumberID := getConfigString(config, "phone_number_id", "app_id")
		accessToken := getConfigString(config, "access_token", "token")
		appSecret, _ := config["app_secret"].(string)
		verifyToken, _ := config["verify_token"].(string)
		return NewWhatsAppBusinessGateway(phoneNumberID, accessToken, appSecret, verifyToken), nil
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
		if appID == "" || appSecret == "" {
			return nil, fmt.Errorf("feishu: app_id and app_secret are required")
		}
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
		autoLogin, _ := config["auto_login"].(bool)
		if token == "" {
			return nil, fmt.Errorf("wechat_ilink: token is required")
		}
		cfg := WeChatILinkConfig{
			Token:     token,
			BaseURL:   baseURL,
			DataDir:   dataDir,
			AutoLogin: autoLogin,
		}
		return NewWeChatILinkGateway(cfg), nil
	})

	// WeCom (WeChat Work)
	registry.Register(PlatformInfo{
		ID:          "wecom",
		Name:        "WeCom",
		Description: "WeChat Work (WeCom) Bot API",
		Version:     "1.0.0",
		Author:      "go-magic",
		Tags:        []string{"wecom", "wechat_work"},
	}, func(ctx context.Context, config map[string]interface{}) (PlatformHandler, error) {
		corpID, _ := config["corp_id"].(string)
		agentID, _ := config["agent_id"].(string)
		secret, _ := config["secret"].(string)
		if corpID == "" || secret == "" {
			return nil, fmt.Errorf("wecom: corp_id and secret are required")
		}
		return NewWeComAppGateway(corpID, agentID, secret), nil
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
		if appKey == "" {
			return nil, fmt.Errorf("dingtalk: app_key is required")
		}
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
}
