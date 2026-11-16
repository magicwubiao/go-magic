package acp

// catalog.go —— 内置 ACP agent 模板（"一键接入"的数据源）。
//
// 定位：让不想读文档的用户从"自己猜命令"变成"选一个模板、补一个 Key"。
// 这里只放**结构性事实**（命令、参数、env 变量名）；面向用户的名称与描述
// 放在前端 i18n 里（acp.tpl.<id>.*），保持文案可翻译、可免 @ 等保留字符。
//
// 维护约定：新模板 = 加一个条目 + 前端补 acp.tpl.<id>.{name,desc,envHint}。
// 包名写错会让 npx 装不上，加之前先实际跑一遍确认。

import (
	"os/exec"
	"runtime"
)

// ACPTemplateEnvKey 是模板要求的凭据/环境变量。
type ACPTemplateEnvKey struct {
	Name     string `json:"name"`
	Required bool   `json:"required"`
	// Masked 表示值是凭据（管理页按 *** 掩码；纯引用 ${VAR} 仍会透传）。
	Masked bool `json:"masked"`
}

// ACPTemplate 是一个"选完就能跑"的 agent 预设。
type ACPTemplate struct {
	ID string `json:"id"`
	// Command/Args 按原样预填表单；Args 里的适配器包名可能以 @ 开头，
	// 那是给 npx 的字面量，与 i18n 无关（前端不经过 t() 渲染）。
	Command     string              `json:"command"`
	Args        []string            `json:"args"`
	EnvKeys     []ACPTemplateEnvKey `json:"env_keys"`
	AutoApprove []string            `json:"auto_approve"`
	// NeedsRuntime 列出运行命令依赖的可执行文件，探测接口据此报"本机可用"。
	// npx 会顺带覆盖 node（npm 生态里装了 npx 必有 node），探测时一并检查。
	NeedsRuntime []string `json:"needs_runtime"`
}

// ACPTemplates 返回内置模板。顺序即前端展示顺序（新增往尾部加，别重排）。
//
// 包名全部经 npm registry 实际验证（2026-11-16）：
//   - @zed-industries/claude-code-acp 0.16.2、@zed-industries/codex-acp 0.16.0
//   - @google/gemini-cli 0.68.0 与 @qwen-code/qwen-code 0.27.4、@iflow-ai/iflow-cli 0.5.19
//     都原生支持 --experimental-acp（stdio），不存在独立的 "-acp" 适配器包
//     （@google/gemini-cli-acp 在 npm 上是 404，别再用）。
//   - Qwen 的 `qwen serve` 是 HTTP+SSE，不是 stdio ACP，接入不走那条路。
func ACPTemplates() []ACPTemplate {
	return []ACPTemplate{
		{
			ID:      "claude-code",
			Command: "npx",
			Args:    []string{"-y", "@zed-industries/claude-code-acp"},
			EnvKeys: []ACPTemplateEnvKey{
				{Name: "ANTHROPIC_API_KEY", Required: true, Masked: true},
			},
			AutoApprove:  DefaultAllowedKinds, // 默认只放行只读类，写/执行仍需显式勾选
			NeedsRuntime: []string{"npx"},
		},
		{
			// Codex 走 Zed 维护的适配器（npm 分发的独立二进制）；既支持
			// OPENAI_API_KEY，也支持本机 ChatGPT 登录态，所以 Key 标可选。
			ID:      "codex",
			Command: "npx",
			Args:    []string{"-y", "@zed-industries/codex-acp"},
			EnvKeys: []ACPTemplateEnvKey{
				{Name: "OPENAI_API_KEY", Required: false, Masked: true},
			},
			AutoApprove:  DefaultAllowedKinds,
			NeedsRuntime: []string{"npx"},
		},
		{
			ID:      "gemini",
			Command: "npx",
			Args:    []string{"-y", "@google/gemini-cli", "--experimental-acp"},
			EnvKeys: []ACPTemplateEnvKey{
				{Name: "GEMINI_API_KEY", Required: true, Masked: true},
			},
			AutoApprove:  DefaultAllowedKinds,
			NeedsRuntime: []string{"npx"},
		},
		{
			// Qwen Code（gemini-cli fork）支持 --experimental-acp；凭据走
			// 本机 qwen 登录态（免费额度）或 DASHSCOPE_API_KEY，都不强制预填。
			ID:      "qwen",
			Command: "npx",
			Args:    []string{"-y", "@qwen-code/qwen-code", "--experimental-acp"},
			EnvKeys: []ACPTemplateEnvKey{
				{Name: "DASHSCOPE_API_KEY", Required: false, Masked: true},
			},
			AutoApprove:  DefaultAllowedKinds,
			NeedsRuntime: []string{"npx"},
		},
		{
			// iFlow CLI（国产模型聚合，glm/kimi/qwen 免费额度）原生支持
			// --experimental-acp（>=0.2.23）。没有 API key：首次需要在本机
			// 跑一次 `iflow` 交互登录，登录态落在本机配置里。
			ID:           "iflow",
			Command:      "npx",
			Args:         []string{"-y", "@iflow-ai/iflow-cli", "--experimental-acp"},
			EnvKeys:      nil,
			AutoApprove:  DefaultAllowedKinds,
			NeedsRuntime: []string{"npx"},
		},
	}
}

// ACPRuntimeProbe 是"本机能不能直接跑"的探测结果。
//
// 只做 PATH 探测（exec.LookPath），不真的起进程——起进程属于 test 接口的职责。
// Windows 的 LookPath 会按 PATHEXT 找到 npx.cmd，无需特判；但 npx 实际是
// cmd 包装脚本，这里只证明"用户装过 Node"，不证明网络可用。
type ACPRuntimeProbe struct {
	Binary string `json:"binary"`
	Found  bool   `json:"found"`
	Path   string `json:"path,omitempty"`
}

// DetectACPRuntimes 对 binaries 去重后逐个 LookPath。
// 返回顺序与传入一致；同一二进制只探测一次。
func DetectACPRuntimes(binaries []string) []ACPRuntimeProbe {
	seen := make(map[string]bool, len(binaries))
	out := make([]ACPRuntimeProbe, 0, len(binaries))
	for _, b := range binaries {
		if b == "" || seen[b] {
			continue
		}
		seen[b] = true
		probe := ACPRuntimeProbe{Binary: b}
		// Go 1.19+ 的 LookPath 在 Windows 上按 PATHEXT 解析（npx → npx.cmd），
		// 找不到时 Err 不为 nil，Path 为空。
		path, err := exec.LookPath(b)
		if err == nil {
			probe.Found = true
			probe.Path = path
		}
		out = append(out, probe)
	}
	return out
}

// ACPCatalogResponse 是 catalog 接口的响应体。
type ACPCatalogResponse struct {
	Templates []ACPTemplate     `json:"templates"`
	Runtimes  []ACPRuntimeProbe `json:"runtimes"`
	Platform  string            `json:"platform"` // runtime.GOOS，前端可用于提示
}

// BuildACPCatalog 把模板与运行时探测合成一份（server 层直接用）。
func BuildACPCatalog() ACPCatalogResponse {
	templates := ACPTemplates()

	// 收集全部 needs_runtime 做一次探测，前端不用发多次请求。
	var binaries []string
	for _, t := range templates {
		binaries = append(binaries, t.NeedsRuntime...)
	}

	return ACPCatalogResponse{
		Templates: templates,
		Runtimes:  DetectACPRuntimes(binaries),
		Platform:  runtime.GOOS,
	}
}
