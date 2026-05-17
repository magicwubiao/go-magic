package tool

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// ============================================================================
// SMS 配置结构
// ============================================================================

// SMSConfig 短信服务配置
type SMSConfig struct {
	Provider string `json:"provider" yaml:"provider"` // 提供商: twilio, aliyun, tencent

	// Twilio 配置
	Twilio TwilioConfig `json:"twilio" yaml:"twilio"`

	// 阿里云短信配置
	Aliyun AliyunSMSConfig `json:"aliyun" yaml:"aliyun"`

	// 腾讯云短信配置
	Tencent TencentSMSConfig `json:"tencent" yaml:"tencent"`
}

// TwilioConfig Twilio 配置
type TwilioConfig struct {
	AccountSID string `json:"account_sid" yaml:"account_sid"` // Account SID
	AuthToken  string `json:"auth_token" yaml:"auth_token"`   // Auth Token
	FromNumber string `json:"from_number" yaml:"from_number"` // 发件人号码
}

// AliyunSMSConfig 阿里云短信配置
type AliyunSMSConfig struct {
	AccessKeyID     string `json:"access_key_id" yaml:"access_key_id"`         // AccessKey ID
	AccessKeySecret string `json:"access_key_secret" yaml:"access_key_secret"` // AccessKey Secret
	SignName        string `json:"sign_name" yaml:"sign_name"`                 // 短信签名
	Endpoint        string `json:"endpoint" yaml:"endpoint"`                   // API 端点
}

// TencentSMSConfig 腾讯云短信配置
type TencentSMSConfig struct {
	SecretID  string `json:"secret_id" yaml:"secret_id"`   // Secret ID
	SecretKey string `json:"secret_key" yaml:"secret_key"` // Secret Key
	SDKAppID  string `json:"sdk_app_id" yaml:"sdk_app_id"` // SDK App ID
	SignName  string `json:"sign_name" yaml:"sign_name"`   // 短信签名
	Endpoint  string `json:"endpoint" yaml:"endpoint"`     // API 端点
}

// SMSMessage 短信消息
type SMSMessage struct {
	To           string            `json:"to"`            // 接收号码
	TemplateCode string            `json:"template_code"` // 模板代码
	TemplateData map[string]string `json:"template_data"` // 模板参数
	Content      string            `json:"content"`       // 直接发送内容（Twilio 使用）
}

// ============================================================================
// SMS 工具接口和实现
// ============================================================================

// SMSProvider 短信提供商接口
type SMSProvider interface {
	Send(ctx context.Context, msg *SMSMessage) error
	Name() string
}

// SMSTool 短信发送工具
type SMSTool struct {
	config   *SMSConfig
	provider SMSProvider
}

// NewSMSTool 创建短信发送工具
func NewSMSTool(config *SMSConfig) *SMSTool {
	tool := &SMSTool{config: config}

	if config != nil {
		switch config.Provider {
		case "twilio":
			tool.provider = NewTwilioProvider(&config.Twilio)
		case "aliyun":
			tool.provider = NewAliyunProvider(&config.Aliyun)
		case "tencent":
			tool.provider = NewTencentProvider(&config.Tencent)
		}
	}

	return tool
}

// Name 返回工具名称
func (t *SMSTool) Name() string {
	return "send_sms"
}

// Description 返回工具描述
func (t *SMSTool) Description() string {
	return "Send SMS via Twilio, Aliyun, or Tencent Cloud SMS service"
}

// Schema 返回工具参数 Schema
func (t *SMSTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"to": map[string]interface{}{
				"type":        "string",
				"description": "Recipient phone number (E.164 format recommended, e.g., +8613800138000)",
			},
			"content": map[string]interface{}{
				"type":        "string",
				"description": "Message content (for Twilio) or template code (for Aliyun/Tencent)",
			},
			"template_code": map[string]interface{}{
				"type":        "string",
				"description": "Template code for Aliyun/Tencent SMS",
			},
			"template_data": map[string]interface{}{
				"type":        "object",
				"description": "Template parameters as key-value pairs (for Aliyun/Tencent)",
			},
		},
		"required": []string{"to", "content"},
	}
}

// Execute 执行短信发送
func (t *SMSTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	if t.config == nil {
		return nil, fmt.Errorf("SMS configuration not set")
	}

	if t.provider == nil {
		return nil, fmt.Errorf("SMS provider not configured")
	}

	// 解析参数
	to, ok := params["to"].(string)
	if !ok || to == "" {
		return nil, fmt.Errorf("recipient phone number is required")
	}

	content, ok := params["content"].(string)
	if !ok || content == "" {
		return nil, fmt.Errorf("content is required")
	}

	// 解析模板数据
	templateData := make(map[string]string)
	if td, ok := params["template_data"].(map[string]interface{}); ok {
		for k, v := range td {
			if s, ok := v.(string); ok {
				templateData[k] = s
			} else {
				templateData[k] = fmt.Sprintf("%v", v)
			}
		}
	}

	// 获取模板代码
	templateCode := content
	if tc, ok := params["template_code"].(string); ok && tc != "" {
		templateCode = tc
	}

	msg := &SMSMessage{
		To:           to,
		Content:      content,
		TemplateCode: templateCode,
		TemplateData: templateData,
	}

	// 发送短信
	if err := t.provider.Send(ctx, msg); err != nil {
		return nil, fmt.Errorf("failed to send SMS: %w", err)
	}

	return map[string]interface{}{
		"success":  true,
		"to":       to,
		"provider": t.provider.Name(),
	}, nil
}

// ============================================================================
// Twilio 提供商实现
// ============================================================================

// TwilioProvider Twilio 短信提供商
type TwilioProvider struct {
	config *TwilioConfig
	client *http.Client
}

// NewTwilioProvider 创建 Twilio 提供商
func NewTwilioProvider(config *TwilioConfig) *TwilioProvider {
	return &TwilioProvider{
		config: config,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// Name 返回提供商名称
func (p *TwilioProvider) Name() string {
	return "twilio"
}

// Send 发送短信
func (p *TwilioProvider) Send(ctx context.Context, msg *SMSMessage) error {
	if p.config.AccountSID == "" || p.config.AuthToken == "" {
		return fmt.Errorf("Twilio credentials not configured")
	}

	// 构建请求 URL
	apiURL := fmt.Sprintf("https://api.twilio.com/2010-04-01/Accounts/%s/Messages.json", p.config.AccountSID)

	// 构建请求数据
	data := url.Values{}
	data.Set("To", msg.To)
	data.Set("From", p.config.FromNumber)
	data.Set("Body", msg.Content)

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// 设置认证和头
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(p.config.AccountSID, p.config.AuthToken)

	// 发送请求
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("Twilio API error (status %d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// ============================================================================
// 阿里云短信提供商实现
// ============================================================================

// AliyunProvider 阿里云短信提供商
type AliyunProvider struct {
	config *AliyunSMSConfig
	client *http.Client
}

// NewAliyunProvider 创建阿里云短信提供商
func NewAliyunProvider(config *AliyunSMSConfig) *AliyunProvider {
	endpoint := config.Endpoint
	if endpoint == "" {
		endpoint = "https://dysmsapi.aliyuncs.com"
	}

	return &AliyunProvider{
		config: config,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// Name 返回提供商名称
func (p *AliyunProvider) Name() string {
	return "aliyun"
}

// Send 发送短信
func (p *AliyunProvider) Send(ctx context.Context, msg *SMSMessage) error {
	if p.config.AccessKeyID == "" || p.config.AccessKeySecret == "" {
		return fmt.Errorf("Aliyun credentials not configured")
	}

	// 构建模板参数 JSON
	templateParam, err := json.Marshal(msg.TemplateData)
	if err != nil {
		return fmt.Errorf("failed to marshal template data: %w", err)
	}

	// 构建请求参数
	params := map[string]string{
		"Action":        "SendSms",
		"Version":       "2017-05-25",
		"RegionId":      "cn-hangzhou",
		"PhoneNumbers":  msg.To,
		"SignName":      p.config.SignName,
		"TemplateCode":  msg.TemplateCode,
		"TemplateParam": string(templateParam),
	}

	// 添加公共参数
	params["Format"] = "JSON"
	params["AccessKeyId"] = p.config.AccessKeyID
	params["SignatureMethod"] = "HMAC-SHA1"
	params["Timestamp"] = time.Now().UTC().Format("2006-01-02T15:04:05Z")
	params["SignatureVersion"] = "1.0"
	params["SignatureNonce"] = generateNonce()

	// 计算签名
	signature := p.sign(params)
	params["Signature"] = signature

	// 构建请求 URL
	endpoint := p.config.Endpoint
	if endpoint == "" {
		endpoint = "https://dysmsapi.aliyuncs.com"
	}

	reqURL := endpoint + "/?" + buildQueryString(params)

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// 发送请求
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	// 解析响应
	var result struct {
		Code      string `json:"Code"`
		Message   string `json:"Message"`
		RequestID string `json:"RequestId"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if result.Code != "OK" {
		return fmt.Errorf("Aliyun SMS error: %s - %s", result.Code, result.Message)
	}

	return nil
}

// sign 计算阿里云签名
func (p *AliyunProvider) sign(params map[string]string) string {
	// 按参数名排序
	var keys []string
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// 构建规范化查询字符串
	var canonicalQueryString []string
	for _, k := range keys {
		canonicalQueryString = append(canonicalQueryString,
			percentEncode(k)+"="+percentEncode(params[k]))
	}

	// 构建待签名字符串
	stringToSign := "GET&" + percentEncode("/") + "&" + percentEncode(strings.Join(canonicalQueryString, "&"))

	// 计算签名
	key := p.config.AccessKeySecret + "&"
	h := hmac.New(sha1.New, []byte(key))
	h.Write([]byte(stringToSign))
	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))

	return signature
}

// ============================================================================
// 腾讯云短信提供商实现
// ============================================================================

// TencentProvider 腾讯云短信提供商
type TencentProvider struct {
	config *TencentSMSConfig
	client *http.Client
}

// NewTencentProvider 创建腾讯云短信提供商
func NewTencentProvider(config *TencentSMSConfig) *TencentProvider {
	return &TencentProvider{
		config: config,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// Name 返回提供商名称
func (p *TencentProvider) Name() string {
	return "tencent"
}

// Send 发送短信
func (p *TencentProvider) Send(ctx context.Context, msg *SMSMessage) error {
	if p.config.SecretID == "" || p.config.SecretKey == "" {
		return fmt.Errorf("Tencent Cloud credentials not configured")
	}

	// 构建模板参数数组
	var templateParams []string
	if len(msg.TemplateData) > 0 {
		// 按 key 排序以保证顺序一致
		var keys []string
		for k := range msg.TemplateData {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			templateParams = append(templateParams, msg.TemplateData[k])
		}
	}

	// 构建请求体
	requestBody := map[string]interface{}{
		"PhoneNumberSet":   []string{msg.To},
		"SmsSdkAppId":      p.config.SDKAppID,
		"SignName":         p.config.SignName,
		"TemplateId":       msg.TemplateCode,
		"TemplateParamSet": templateParams,
	}

	bodyJSON, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %w", err)
	}

	// 构建请求
	endpoint := p.config.Endpoint
	if endpoint == "" {
		endpoint = "sms.tencentcloudapi.com"
	}

	service := "sms"
	version := "2021-01-11"
	action := "SendSms"
	region := "ap-guangzhou"
	timestamp := time.Now().Unix()

	// 构建规范请求
	httpRequestMethod := "POST"
	canonicalURI := "/"
	canonicalQueryString := ""
	canonicalHeaders := fmt.Sprintf("content-type:application/json\nhost:%s\n", endpoint)
	signedHeaders := "content-type;host"
	payloadHash := sha256Hex(string(bodyJSON))

	canonicalRequest := fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n%s",
		httpRequestMethod, canonicalURI, canonicalQueryString,
		canonicalHeaders, signedHeaders, payloadHash)

	// 构建待签名字符串
	date := time.Unix(timestamp, 0).UTC().Format("2006-01-02")
	credentialScope := fmt.Sprintf("%s/%s/tc3_request", date, service)
	hashedCanonicalRequest := sha256Hex(canonicalRequest)
	stringToSign := fmt.Sprintf("TC3-HMAC-SHA256\n%d\n%s\n%s",
		timestamp, credentialScope, hashedCanonicalRequest)

	// 计算签名
	secretDate := hmacSHA256(date, []byte("TC3"+p.config.SecretKey))
	secretService := hmacSHA256(service, secretDate)
	secretSigning := hmacSHA256("tc3_request", secretService)
	signature := hex.EncodeToString(hmacSHA256(stringToSign, secretSigning))

	// 构建 Authorization
	authorization := fmt.Sprintf("TC3-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		p.config.SecretID, credentialScope, signedHeaders, signature)

	// 发送请求
	reqURL := fmt.Sprintf("https://%s", endpoint)
	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, bytes.NewReader(bodyJSON))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Host", endpoint)
	req.Header.Set("X-TC-Action", action)
	req.Header.Set("X-TC-Version", version)
	req.Header.Set("X-TC-Timestamp", fmt.Sprintf("%d", timestamp))
	req.Header.Set("X-TC-Region", region)
	req.Header.Set("Authorization", authorization)

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	// 解析响应
	var result struct {
		Response struct {
			SendStatusSet []struct {
				Code    string `json:"Code"`
				Message string `json:"Message"`
			} `json:"SendStatusSet"`
			Error struct {
				Code    string `json:"Code"`
				Message string `json:"Message"`
			} `json:"Error"`
		} `json:"Response"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if result.Response.Error.Code != "" {
		return fmt.Errorf("Tencent SMS error: %s - %s", result.Response.Error.Code, result.Response.Error.Message)
	}

	if len(result.Response.SendStatusSet) > 0 {
		status := result.Response.SendStatusSet[0]
		if status.Code != "Ok" {
			return fmt.Errorf("Tencent SMS error: %s - %s", status.Code, status.Message)
		}
	}

	return nil
}

// ============================================================================
// 辅助函数
// ============================================================================

// percentEncode URL 编码（RFC 3986）
func percentEncode(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

// buildQueryString 构建查询字符串
func buildQueryString(params map[string]string) string {
	var parts []string
	for k, v := range params {
		parts = append(parts, percentEncode(k)+"="+percentEncode(v))
	}
	return strings.Join(parts, "&")
}

// generateNonce 生成随机 nonce
func generateNonce() string {
	return fmt.Sprintf("%d%d", time.Now().UnixNano(), randomInt(1000, 9999))
}

// randomInt 生成随机整数
func randomInt(min, max int) int {
	return min + int(time.Now().UnixNano())%(max-min)
}

// sha256Hex 计算 SHA256 哈希并返回十六进制字符串
func sha256Hex(s string) string {
	h := sha256.New()
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}

// hmacSHA256 计算 HMAC-SHA256
func hmacSHA256(data string, key []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}
