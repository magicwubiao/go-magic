package bot

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/internal/skills"
	"github.com/magicwubiao/go-magic/internal/tool"
	"github.com/magicwubiao/go-magic/pkg/config"
	"github.com/magicwubiao/go-magic/pkg/log"
)

// buildBotDeps resolves provider + tool registry for one bot,
// honoring per-bot model/provider pins with global fallback, pruning tools to
// the bot's allowlist, restricting its skills, and giving the bot an isolated
// workdir (with its own .env) so credentials never leak across bots.
func buildBotDeps(globalCfg *config.Config, botCfg *Config) (provider.Provider, *tool.Registry, error) {
	cfg := *globalCfg // Shallow copy so we can override pins

	if botCfg.Provider != "" {
		if _, ok := cfg.Providers[botCfg.Provider]; !ok {
			return nil, nil, fmt.Errorf("bot %q pinned to unknown provider %q", botCfg.Name, botCfg.Provider)
		}
		cfg.Provider = botCfg.Provider
	}

	provCfg, ok := cfg.Providers[cfg.Provider]
	if !ok {
		return nil, nil, fmt.Errorf("provider %q not configured for bot %q", cfg.Provider, botCfg.Name)
	}

	// Per-bot model pin overrides the provider's default model.
	effectiveProvCfg := provCfg
	if botCfg.Model != "" {
		effectiveProvCfg.Models = []string{botCfg.Model}
	}

	prov, err := config.CreateProviderFor(cfg.Provider, effectiveProvCfg)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create provider for bot %q: %w", botCfg.Name, err)
	}
	// 视觉 / 文件转换策略必须装到 provider 实例上，理由与会话侧 server 的
	// buildConvertConfig 完全一致：不装的话 BaseProvider.ConvertCfg 保持 nil，
	// 转换层把每个模型都当纯文本模型，image_url 部件被静默换成
	// "(image attachment omitted: current model does not support vision)"
	// 占位文本 —— 模型因此永远看不到用户发的图，只能复述"当前模型不支持
	// 视觉识别"，再试图装 OCR 自救（群聊里表现为整轮卡死）。会话链路一直在
	// 装，bot 链路漏了。
	provider.ApplyConvertConfig(prov, botConvertConfig(globalCfg, effectiveProvCfg, prov))

	// Per-bot isolated workdir: bots/<name>/. Tools that root themselves in a
	// working directory (terminal, file ops) stay inside this bot's sandbox,
	// so one bot cannot read or modify another bot's files by accident.
	botWorkDir := botWorkDirFor(globalCfg, botCfg)
	if err := os.MkdirAll(botWorkDir, 0755); err != nil {
		return nil, nil, fmt.Errorf("failed to create bot workdir for %q: %w", botCfg.Name, err)
	}

	// Write the bot's own .env (credentials). The file lives inside the bot's
	// isolated workdir, so scripts run by this bot can source it while other
	// bots never see it.
	if len(botCfg.Env) > 0 {
		if err := writeDotEnv(filepath.Join(botWorkDir, ".env"), botCfg.Env); err != nil {
			return nil, nil, fmt.Errorf("failed to write .env for bot %q: %w", botCfg.Name, err)
		}
	}

	registry := tool.NewRegistry()
	registry.RegisterBotTools(botWorkDir)
	if skillMgr, err := skills.NewManager(); err == nil {
		if len(botCfg.Skills) > 0 {
			// Restrict the skill tool to this bot's skill allowlist.
			registry.RegisterSkillTool(newSkillFilter(skillMgr, botCfg.Skills))
		} else {
			registry.RegisterSkillTool(skillMgr)
		}
	}

	// Tool allowlist: prune everything the bot is not allowed to call.
	// message_agent is registered later (in buildAgent) so it always survives.
	if len(botCfg.Tools) > 0 {
		allowed := make(map[string]bool, len(botCfg.Tools))
		for _, t := range botCfg.Tools {
			allowed[strings.ToLower(strings.TrimSpace(t))] = true
		}
		var dropped []string
		for _, name := range registry.List() {
			if !allowed[strings.ToLower(name)] {
				registry.Unregister(name)
				dropped = append(dropped, name)
			}
		}
		if len(dropped) > 0 {
			log.Infof("[BotMode] Bot %q tool allowlist pruned %d tool(s): %v", botCfg.Name, len(dropped), dropped)
		}
	}

	return prov, registry, nil
}

// botConvertConfig derives the file-conversion / vision policy for one bot's
// provider, mirroring Server.buildConvertConfig (internal/server/server.go) so
// that a bot and a web session pinned to the same provider/model treat image
// parts identically. Precedence:
//
//  1. an explicit per-provider "vision" declaration (config Providers[].vision)
//     wins over name-based guessing, which is best-effort and lags new
//     releases;
//  2. otherwise provider.ModelSupportsVision(model), which itself consults the
//     runtime learning cache before the catalog and the name heuristics.
//
// AutoVision stays on so the value is re-evaluated per request (the model can
// be switched without rebuilding the agent). Without this policy installed,
// the conversion layer never emits image_url parts and every picture a user
// shares with a bot degrades to a "does not support vision" placeholder.
func botConvertConfig(globalCfg *config.Config, provCfg config.ProviderConfig, prov provider.Provider) *provider.ConvertConfig {
	conv := &provider.ConvertConfig{
		StrategyName: "auto",
		AutoVision:   true,
	}
	if globalCfg != nil {
		conv.UploadURLPrefix = globalCfg.Server.UploadURLPrefix
		conv.StrategyName = globalCfg.Server.GetFileStrategy()
	}
	// The live provider already carries the per-bot model pin (Models[0] was
	// overwritten above), so prefer the instance over the config copy.
	model := ""
	if prov != nil {
		if m, ok := prov.(interface{ GetModel() string }); ok {
			model = m.GetModel()
		}
	}
	if model == "" {
		model = provCfg.GetCurrentModel()
	}
	conv.SupportVision = provider.ModelSupportsVision(model)
	if provCfg.Vision != nil {
		conv.VisionOverride = provCfg.Vision
		conv.SupportVision = *provCfg.Vision
	}
	return conv
}

// skillFilter wraps a skills manager and hides any skill outside the bot's
// allowlist, so the skill tool only lists/invokes permitted skills.
type skillFilter struct {
	inner   tool.SkillInfoProvider
	allowed map[string]bool
}

func newSkillFilter(inner tool.SkillInfoProvider, allowlist []string) *skillFilter {
	allowed := make(map[string]bool, len(allowlist))
	for _, s := range allowlist {
		allowed[strings.ToLower(strings.TrimSpace(s))] = true
	}
	return &skillFilter{inner: inner, allowed: allowed}
}

func (f *skillFilter) ListSkills() []string {
	all := f.inner.ListSkills()
	out := make([]string, 0, len(all))
	for _, s := range all {
		if f.allowed[strings.ToLower(s)] {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

func (f *skillFilter) GetSkillInfo(name string) (string, []string, string, error) {
	if !f.allowed[strings.ToLower(name)] {
		return "", nil, "", fmt.Errorf("skill %q is not allowed for this bot", name)
	}
	return f.inner.GetSkillInfo(name)
}

// botWorkDirFor returns the per-bot isolated working directory:
// <WorkingDir | cwd>/bots/<sanitized-name>. Shared by buildBotDeps (which
// creates it and roots the bot tools there) and by the turn runner (which
// injects it into the turn ctx via tool.WithWorkDir) — the two must agree,
// otherwise relative paths written by bot tools land somewhere the approval
// hook's scope check and the file tools disagree on.
func botWorkDirFor(globalCfg *config.Config, botCfg *Config) string {
	workDir := ""
	if globalCfg != nil {
		workDir = globalCfg.WorkingDir
	}
	if workDir == "" {
		workDir, _ = os.Getwd()
	}
	return filepath.Join(workDir, "bots", sanitizeDirComponent(botCfg.Name))
}

// sanitizeDirComponent makes a bot name safe to use as a directory name.
func sanitizeDirComponent(name string) string {
	name = strings.ToLower(name)
	// 空名与点名必须在字符替换之前拦截："."/".." 会被下面的 Map 替换成
	// "_"/"__"，导致这个守卫永远匹配不上（死代码），点目录名以 "_. 音" 的
	// 形式漏进路径。提前拦截后 ".." 明确回落为 "bot"。
	if name == "" || name == "." || name == ".." {
		return "bot"
	}
	name = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r == '-' || r == '_':
			return r
		default:
			return '_'
		}
	}, name)
	if name == "" {
		return "bot"
	}
	return name
}

// writeDotEnv serializes a key/value map into .env format (KEY=VALUE lines,
// values quoted when they contain spaces or special chars).
func writeDotEnv(path string, env map[string]string) error {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for _, k := range keys {
		v := env[k]
		if strings.ContainsAny(v, " \t\"'#$&;|<>`") {
			v = `"` + strings.ReplaceAll(strings.ReplaceAll(v, `\`, `\\`), `"`, `\"`) + `"`
		}
		fmt.Fprintf(&sb, "%s=%s\n", k, v)
	}
	return os.WriteFile(path, []byte(sb.String()), 0600)
}

// getToolsSchema converts registry tools into OpenAI function-calling schemas.
func getToolsSchema(registry *tool.Registry) []map[string]interface{} {
	tools := []map[string]interface{}{}
	for _, tName := range registry.List() {
		t, err := registry.Get(tName)
		if err != nil || t.Name() == "" {
			continue
		}
		desc := t.Description()
		if desc == "" {
			desc = t.Name() + " tool"
		}
		tools = append(tools, map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":        t.Name(),
				"description": desc,
				"parameters":  t.Schema(),
			},
		})
	}
	return tools
}
