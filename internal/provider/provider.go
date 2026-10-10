package provider

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/magicwubiao/go-magic/pkg/catalog"
	"github.com/magicwubiao/go-magic/pkg/types"
)

// Message is an alias for types.Message
type Message = types.Message

// ChatResponse represents a chat response with optional usage info
type ChatResponse struct {
	Content          string           `json:"content"`
	ReasoningContent string           `json:"reasoning_content,omitempty"`
	ToolCalls        []types.ToolCall `json:"tool_calls,omitempty"`
	Usage            *Usage           `json:"usage,omitempty"`
}

// ModelInfo represents information about a supported model
type ModelInfo struct {
	ID          string `json:"id"`          // Model ID used in API calls
	Name        string `json:"name"`        // Human-readable name
	Description string `json:"description"` // Model description
	ContextLen  int    `json:"context_len"` // Context window size (0 = unknown)
	// Vision declares whether this model accepts image_url parts. For IDs
	// present in the curated registry this flag is authoritative: vision
	// detection (ModelSupportsVision) checks it BEFORE falling back to
	// name-pattern heuristics, which lag new releases and occasionally
	// misfire (e.g. "o3" matching the text-only o3-mini).
	Vision bool `json:"vision"`
}

// Modeler is an optional interface for providers that support multiple models.
// Providers implementing this interface allow dynamic model switching.
type Modeler interface {
	// SetModel sets the current model. Returns error if model is not supported.
	SetModel(model string) error
	// GetModel returns the current model ID.
	GetModel() string
	// ListModels returns the list of supported models.
	ListModels() []ModelInfo
}

// Provider is the interface for LLM providers.
type Provider interface {
	Chat(ctx context.Context, messages []Message) (*ChatResponse, error)
	Name() string
}

// ToolCaller is an optional interface for providers that support tool calling.
type ToolCaller interface {
	ChatWithTools(ctx context.Context, messages []Message, tools []map[string]interface{}) (*ChatResponse, error)
}

// Streamer is an optional interface for providers that support streaming.
type Streamer interface {
	Stream(ctx context.Context, messages []Message, handler StreamHandler) error
}

// StreamingToolCaller is for providers that support both streaming and tool calling
type StreamingToolCaller interface {
	StreamWithTools(ctx context.Context, messages []Message, tools []map[string]interface{}, handler StreamHandler) error
}

// CapableProvider is an optional interface for providers that declare their capabilities
type CapableProvider interface {
	GetCapabilities() *Capabilities
}

// ConvertConfigProvider is an optional interface for providers that support file conversion config
type ConvertConfigProvider interface {
	SetConvertConfig(cfg *ConvertConfig)
}

// Registry manages provider instances.
type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
}

// NewRegistry creates a new provider registry
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]Provider),
	}
}

// Register registers a provider in the registry
func (r *Registry) Register(p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[p.Name()] = p
}

// Get returns a provider by name
func (r *Registry) Get(name string) (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("provider %s not found", name)
	}
	return p, nil
}

// List returns all registered provider names
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	return names
}

// GetCapabilities returns the capabilities of a provider, or default if not specified
func GetCapabilities(p Provider) *Capabilities {
	if cp, ok := p.(CapableProvider); ok {
		return cp.GetCapabilities()
	}
	return DefaultCapabilities()
}

// GetModeler returns the Modeler interface if supported by the provider
func GetModeler(p Provider) (Modeler, bool) {
	if m, ok := p.(Modeler); ok {
		return m, true
	}
	return nil, false
}

// IsModelSupported checks if a model is supported by the provider
func IsModelSupported(p Provider, model string) bool {
	m, ok := GetModeler(p)
	if !ok {
		return false
	}
	for _, mi := range m.ListModels() {
		if mi.ID == model {
			return true
		}
	}
	return false
}

// GetDefaultModels returns the default models for a provider. Data derives
// from the single catalog source (pkg/catalog) — do NOT add model entries
// here; edit pkg/catalog/catalog.go instead. Unknown providers return nil.
func GetDefaultModels(providerName string) []ModelInfo {
	models := catalog.Models(providerName)
	if len(models) == 0 {
		return nil
	}
	out := make([]ModelInfo, len(models))
	for i, m := range models {
		out[i] = ModelInfo{
			ID:          m.ID,
			Name:        m.Name,
			Description: m.Description,
			ContextLen:  m.ContextLen,
			// Vision 三态收敛为 bool：nil（未知）按 false 透出。权威判定
			// 由 modelRegistryVision 直接查 catalog 的 *bool，不经过这里。
			Vision: m.Vision != nil && *m.Vision,
		}
	}
	return out
}

// ============================ 模型上下文窗口 ============================
//
// **单一数据源是 pkg/catalog**：每个模型的窗口登记在它的 Model.ContextLen 上。
//
// 这里曾有一份按模型名推断窗口/输出/家族的规则表（model_window.go，48 条规则），
// 2026-10-10 删除。理由是它与 catalog 是**两份必须同步的数据**：目录里写 1M、
// 名称表推 64K，谁优先都要吵架；而模型换代又快，等于同一个事实要维护两处。
// 现在只有两条来源，按权威性排序（见 ModelContextLenFor）：
//
//  1. pkg/catalog —— 本包各 provider 的模型列表本就由它派生（GetDefaultModels），
//     展示与决策同源；
//  2. provider 自报的模型列表 —— 用户配置的 providers.<name>.models、以及
//     /fetch-models 在线拉取的结果，带 ContextLen 时同样可用（它们通常没有）。
//
// 查不到就是 0（未知），由调用方按 DefaultModelContextLen 兜底 —— 这条兜底路径
// 现在只影响"把压缩触发点往下收紧"的安全方向，所以不需要为了模型换代而维护任何表。

// DefaultModelContextLen 是"模型窗口未知"时的**假设窗口**（token）。
//
// 它现在的职责收窄为三条兜底：agent 侧压缩阈值（× 60% = 76800）、历史字节硬上限
// （× 4 = 512000）、以及 server 侧展示用的 context_window。
//
// **不需要跟着模型换代更新**：大窗口模型的触发点已由
// agent.compressThresholdCeilingTokens(200K) 封顶，而不是按假设窗口的比例放大；
// 抬高本值不会让新模型"用上更大的窗口"，只会把"未知窗口"这条支路的取值往上推，
// 而它恰是最危险的（见下）。所以 1M 级世代的到来不构成抬高它的理由。
//
// 为什么取 128,000 而不是 256K/1M —— 失效方向不对称：
//   - 猜小 ⇒ 只是多压缩几次。滚动摘要落地后压缩是低损的（history 中摘要恒 ≤ 1 条），
//     属软失败。
//   - 猜大 ⇒ 请求超过模型真实窗口，而上下文超限**不会自愈**：分类器为
//     FailoverContextOverflow 给出的 Action "compress" / Compress 字段全仓无人消费
//     （agent 的四处调用点只读 Abort 与 Delay）⇒ 立即重试 → 再撞 → 被重复失败
//     检测升级成回合失败。
const DefaultModelContextLen = 128000

// DefaultModelMaxOutput 是"输出上限未知"时的兜底（**仅用于展示**：server 的
// max_output_tokens 字段；请求路径不受它约束）。
//
// 旧值 4096 是 GPT-3.5 时代的上限；当前世代即便最低档也在 8K–10K 以上
// （Amazon Nova 10,000 / Gemini 64K / ERNIE 65,536 / Claude 128K），故取 16,384。
//
// catalog 不登记"最大输出"（各家的口径混乱：有的给单次上限、有的给推理+输出的
// 总预算），所以这里不再逐模型推断，统一用这个兜底值。
const DefaultModelMaxOutput = 16384

// ModelContextLen 按模型 ID 在 pkg/catalog 里查上下文窗口（token），未登记返回 0。
//
// 大小写不敏感、忽略首尾空白；聚合网关那种带供应商前缀的 ID 也能命中
// （"openai/gpt-6.1-sol"、"groq/meta-llama/llama-4-..."——前缀可以有任意多段，
// 所以按"以 /<目录 ID> 结尾"匹配，而不是只按最后一段切）。
func ModelContextLen(modelName string) int {
	name := strings.ToLower(strings.TrimSpace(modelName))
	if name == "" {
		return 0
	}
	// 1) 整串精确匹配（绝大多数情况）
	if m, ok := catalogModelByID(name); ok && m.ContextLen > 0 {
		return m.ContextLen
	}
	// 2) 带前缀匹配：name 以 "/<目录 ID>" 结尾
	for _, p := range catalog.All() {
		for _, m := range p.Models {
			if m.ContextLen > 0 && strings.HasSuffix(name, "/"+strings.ToLower(m.ID)) {
				return m.ContextLen
			}
		}
	}
	return 0
}

// catalogModelByID 在整份目录里按 ID（大小写不敏感）查一条模型条目。
func catalogModelByID(id string) (catalog.Model, bool) {
	for _, p := range catalog.All() {
		for _, m := range p.Models {
			if strings.EqualFold(m.ID, id) {
				return m, true
			}
		}
	}
	return catalog.Model{}, false
}

// ModelContextLenFor 解析某个 provider **当前模型**的上下文窗口（token）。
//
// 顺序：目录（pkg/catalog）→ provider 自报的模型列表 → 0（未知）。
// 目录优先是因为它既权威又与展示路径同源；而 provider 的列表可能来自用户配置或
// 在线拉取（带不上窗口），也可能只包含一个手动添加的条目（ContextLen 0）。
//
// 拿不到就返回 0，调用方用 DefaultModelContextLen 兜底。
func ModelContextLenFor(p Provider) int {
	if p == nil {
		return 0
	}
	modeler, ok := GetModeler(p)
	if !ok {
		return 0
	}
	model := modeler.GetModel()
	if model == "" {
		return 0
	}
	if n := ModelContextLen(model); n > 0 {
		return n
	}
	for _, mi := range modeler.ListModels() {
		if mi.ID == model && mi.ContextLen > 0 {
			return mi.ContextLen
		}
	}
	return 0
}
