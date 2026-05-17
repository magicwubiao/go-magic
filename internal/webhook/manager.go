package webhook

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Event 事件
type Event struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`
	Source    string                 `json:"source"`
	Timestamp time.Time              `json:"timestamp"`
	Data      map[string]interface{} `json:"data"`
	Retries   int                    `json:"retries"`
}

// Webhook Webhook 配置
type Webhook struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	URL       string            `json:"url"`
	Secret    string            `json:"secret,omitempty"`
	Events    []string          `json:"events"` // 监听的事件类型
	Active    bool              `json:"active"`
	Headers   map[string]string `json:"headers,omitempty"`
	Timeout   int               `json:"timeout"` // 秒
	RetryPolicy RetryPolicy     `json:"retry_policy"`
	CreatedAt time.Time         `json:"created_at"`
	LastTriggered time.Time     `json:"last_triggered,omitempty"`
}

// RetryPolicy retry policy
type RetryPolicy struct {
	MaxRetries          int     `json:"max_retries"`
	RetryDelay          int     `json:"retry_delay_seconds"`
	BackoffMultiplier   float64 `json:"backoff_multiplier"`
}

// DeliveryResult 投递结果
type DeliveryResult struct {
	WebhookID string    `json:"webhook_id"`
	EventID   string    `json:"event_id"`
	Timestamp time.Time `json:"timestamp"`
	Success   bool      `json:"success"`
	StatusCode int      `json:"status_code,omitempty"`
	Response  string    `json:"response,omitempty"`
	Error     string    `json:"error,omitempty"`
	Duration  time.Duration `json:"duration"`
}

// Manager Webhook 管理器
type Manager struct {
	mu       sync.RWMutex
	dataDir  string
	webhooks map[string]*Webhook
	events   chan *Event
	client   *http.Client
}

// NewManager 创建新的 Webhook 管理器
func NewManager(dataDir string) (*Manager, error) {
	m := &Manager{
		dataDir:  dataDir,
		webhooks: make(map[string]*Webhook),
		events:   make(chan *Event, 100),
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}

	// 创建目录
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, err
	}

	// 加载配置
	if err := m.load(); err != nil {
		// 忽略加载错误
	}

	// 启动事件处理器
	go m.processEvents()

	return m, nil
}

// processEvents 处理事件队列
func (m *Manager) processEvents() {
	for event := range m.events {
		m.deliverEvent(event)
	}
}

// Create 创建 Webhook
func (m *Manager) Create(name, url, secret string, events []string) (*Webhook, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	wh := &Webhook{
		ID:        generateID(),
		Name:      name,
		URL:       url,
		Secret:    secret,
		Events:    events,
		Active:    true,
		Headers:   make(map[string]string),
		Timeout:   30,
		RetryPolicy: RetryPolicy{
			MaxRetries:     3,
			RetryDelay:     5,
			BackoffMultiplier: 2.0,
		},
		CreatedAt: time.Now(),
	}

	m.webhooks[wh.ID] = wh
	if err := m.save(); err != nil {
		delete(m.webhooks, wh.ID)
		return nil, err
	}

	return wh, nil
}

// Update 更新 Webhook
func (m *Manager) Update(id string, updates map[string]interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	wh, ok := m.webhooks[id]
	if !ok {
		return fmt.Errorf("webhook '%s' not found", id)
	}

	// 应用更新
	if v, ok := updates["name"].(string); ok {
		wh.Name = v
	}
	if v, ok := updates["url"].(string); ok {
		wh.URL = v
	}
	if v, ok := updates["secret"].(string); ok {
		wh.Secret = v
	}
	if v, ok := updates["events"].([]string); ok {
		wh.Events = v
	}
	if v, ok := updates["active"].(bool); ok {
		wh.Active = v
	}
	if v, ok := updates["timeout"].(int); ok {
		wh.Timeout = v
	}

	if err := m.save(); err != nil {
		return err
	}

	return nil
}

// Delete 删除 Webhook
func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.webhooks[id]; !ok {
		return fmt.Errorf("webhook '%s' not found", id)
	}

	delete(m.webhooks, id)
	return m.save()
}

// Get 获取 Webhook
func (m *Manager) Get(id string) *Webhook {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.webhooks[id]
}

// List 列出所有 Webhook
func (m *Manager) List() []*Webhook {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*Webhook, 0, len(m.webhooks))
	for _, wh := range m.webhooks {
		result = append(result, wh)
	}
	return result
}

// Enable 启用 Webhook
func (m *Manager) Enable(id string) error {
	return m.Update(id, map[string]interface{}{"active": true})
}

// Disable 禁用 Webhook
func (m *Manager) Disable(id string) error {
	return m.Update(id, map[string]interface{}{"active": false})
}

// Emit 触发事件
func (m *Manager) Emit(eventType, source string, data map[string]interface{}) {
	event := &Event{
		ID:        generateID(),
		Type:      eventType,
		Source:    source,
		Timestamp: time.Now(),
		Data:      data,
	}

	// 非阻塞发送
	select {
	case m.events <- event:
	default:
		// 队列满，丢弃事件
	}
}

// EmitSync 同步触发事件（等待所有投递完成）
func (m *Manager) EmitSync(eventType, source string, data map[string]interface{}) []*DeliveryResult {
	m.mu.RLock()
	defer m.mu.RUnlock()

	event := &Event{
		ID:        generateID(),
		Type:      eventType,
		Source:    source,
		Timestamp: time.Now(),
		Data:      data,
	}

	var results []*DeliveryResult
	for _, wh := range m.webhooks {
		if !wh.Active {
			continue
		}
		if !m.shouldDeliver(wh, event) {
			continue
		}
		results = append(results, m.deliverToWebhook(wh, event))
	}

	return results
}

// shouldDeliver 检查是否应该投递
func (m *Manager) shouldDeliver(wh *Webhook, event *Event) bool {
	if len(wh.Events) == 0 {
		return true // 监听所有事件
	}

	for _, e := range wh.Events {
		if e == event.Type || e == "*" {
			return true
		}
	}
	return false
}

// deliverEvent 投递事件
func (m *Manager) deliverEvent(event *Event) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, wh := range m.webhooks {
		if !wh.Active {
			continue
		}
		if !m.shouldDeliver(wh, event) {
			continue
		}

		// 异步投递
		go m.deliverToWebhook(wh, event)
	}
}

// deliverToWebhook 投递到指定 Webhook
func (m *Manager) deliverToWebhook(wh *Webhook, event *Event) *DeliveryResult {
	result := &DeliveryResult{
		WebhookID: wh.ID,
		EventID:   event.ID,
		Timestamp: time.Now(),
	}

	start := time.Now()
	defer func() {
		result.Duration = time.Since(start)
	}()

	// 构建请求体
	payload := map[string]interface{}{
		"event": event,
		"webhook": map[string]string{
			"id":   wh.ID,
			"name": wh.Name,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		result.Error = err.Error()
		return result
	}

	// 创建请求
	req, err := http.NewRequest("POST", wh.URL, bytes.NewBuffer(body))
	if err != nil {
		result.Error = err.Error()
		return result
	}

	// 设置 headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Event", event.Type)
	req.Header.Set("X-Webhook-ID", wh.ID)
	req.Header.Set("X-Webhook-Timestamp", event.Timestamp.Format(time.RFC3339))

	for k, v := range wh.Headers {
		req.Header.Set(k, v)
	}

	// 添加签名
	if wh.Secret != "" {
		signature := m.sign(body, wh.Secret)
		req.Header.Set("X-Webhook-Signature", signature)
	}

	// 发送请求
	resp, err := m.client.Do(req)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	defer resp.Body.Close()

	result.StatusCode = resp.StatusCode

	// 读取响应
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	result.Response = string(respBody)

	result.Success = resp.StatusCode >= 200 && resp.StatusCode < 300

	// 更新最后触发时间
	m.mu.Lock()
	wh.LastTriggered = time.Now()
	m.mu.Unlock()

	return result
}

// sign 生成签名
func (m *Manager) sign(payload []byte, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write(payload)
	return "sha256=" + hex.EncodeToString(h.Sum(nil))
}

// Test 测试 Webhook
func (m *Manager) Test(id string) (*DeliveryResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	wh, ok := m.webhooks[id]
	if !ok {
		return nil, fmt.Errorf("webhook '%s' not found", id)
	}

	testEvent := &Event{
		ID:        "test",
		Type:      "test",
		Source:    "magic-test",
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"message": "This is a test event from magic",
			"version": "1.0",
		},
	}

	return m.deliverToWebhook(wh, testEvent), nil
}

// GetDeliveryHistory 获取投递历史
func (m *Manager) GetDeliveryHistory(webhookID string, limit int) []*DeliveryResult {
	// 简化实现，实际应该从持久化存储读取
	return []*DeliveryResult{}
}

// AddHeader 添加自定义 header
func (m *Manager) AddHeader(id, key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	wh, ok := m.webhooks[id]
	if !ok {
		return fmt.Errorf("webhook '%s' not found", id)
	}

	wh.Headers[key] = value
	return m.save()
}

// RemoveHeader 移除自定义 header
func (m *Manager) RemoveHeader(id, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	wh, ok := m.webhooks[id]
	if !ok {
		return fmt.Errorf("webhook '%s' not found", id)
	}

	delete(wh.Headers, key)
	return m.save()
}

// SetRetryPolicy 设置重试策略
func (m *Manager) SetRetryPolicy(id string, policy RetryPolicy) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	wh, ok := m.webhooks[id]
	if !ok {
		return fmt.Errorf("webhook '%s' not found", id)
	}

	wh.RetryPolicy = policy
	return m.save()
}

// save 保存到文件
func (m *Manager) save() error {
	path := filepath.Join(m.dataDir, "webhooks.json")

	type serializableWebhook struct {
		*Webhook
	}

	webhooks := make([]*Webhook, 0, len(m.webhooks))
	for _, wh := range m.webhooks {
		webhooks = append(webhooks, wh)
	}

	data, err := json.MarshalIndent(webhooks, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// load 加载配置
func (m *Manager) load() error {
	path := filepath.Join(m.dataDir, "webhooks.json")

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var webhooks []*Webhook
	if err := json.Unmarshal(data, &webhooks); err != nil {
		return err
	}

	for _, wh := range webhooks {
		m.webhooks[wh.ID] = wh
	}

	return nil
}

// generateID 生成唯一 ID
func generateID() string {
	return fmt.Sprintf("wh_%d", time.Now().UnixNano())
}

// PredefinedEvents 预定义事件类型
var PredefinedEvents = []string{
	"chat.message",        // 聊天消息
	"chat.session_start",  // 会话开始
	"chat.session_end",    // 会话结束
	"tool.executed",       // 工具执行
	"skill.activated",     // 技能激活
	"skill.deactivated",   // 技能停用
	"error",               // 错误
	"warning",             // 警告
	"update.available",   // 有可用更新
	"budget.exceeded",     // 超出预算
	"mcp.connected",       // MCP 连接
	"mcp.disconnected",    // MCP 断开
}
