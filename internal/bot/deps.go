package bot

import (
	"fmt"
	"os"

	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/internal/skills"
	"github.com/magicwubiao/go-magic/internal/tool"
	"github.com/magicwubiao/go-magic/pkg/config"
)

// buildBotDeps resolves provider + tool registry for one bot,
// honoring per-bot model/provider pins with global fallback.
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

	workDir := cfg.WorkingDir
	if workDir == "" {
		workDir, _ = os.Getwd()
	}
	registry := tool.NewRegistry()
	registry.RegisterBotTools(workDir)
	if skillMgr, err := skills.NewManager(); err == nil {
		registry.RegisterSkillTool(skillMgr)
	}

	return prov, registry, nil
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
