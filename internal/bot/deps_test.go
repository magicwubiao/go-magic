package bot

import (
	"testing"

	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/pkg/config"
)

// TestBuildBotDepsInstallsConvertConfig 是线上问题的回归：bot 链路建 provider
// 时漏装 ConvertConfig（会话链路 server.buildConvertConfig 一直在装），于是
// BaseProvider.ConvertCfg 为 nil，转换层把每个模型都当纯文本模型，image_url
// 部件被换成 "(image attachment omitted: ...)" 占位文本。
//
// 症状：用户给 bot 发图，模型永远看不到，只能复述"当前模型不支持视觉识别"，
// 然后试图安装 OCR 依赖自救；群聊回合逐成员串行，整轮因此卡死数分钟。
func TestBuildBotDepsInstallsConvertConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GO_MAGIC_HOME", home)

	cfg := &config.Config{
		Provider: "custom",
		// 不设 WorkingDir 时 botWorkDirFor 会回落到进程 cwd，测试会把
		// bots/<name> 建进仓库；固定到临时目录。
		WorkingDir: t.TempDir(),
		Providers: map[string]config.ProviderConfig{
			"custom": {APIKey: "k", Models: []string{"gpt-4o-mini"}},
		},
	}

	prov, _, err := buildBotDeps(cfg, &Config{Name: "alice"})
	if err != nil {
		t.Fatalf("buildBotDeps: %v", err)
	}
	conv := provider.GetConvertConfig(prov)
	if conv == nil {
		t.Fatal("bot provider has no ConvertConfig: every image a user shares degrades to the 'does not support vision' placeholder")
	}
	if !conv.AutoVision {
		t.Error("AutoVision must stay on: the vision verdict has to be re-evaluated per request when the model changes")
	}
	if !conv.SupportVision {
		t.Error("gpt-4o-mini is vision-capable, SupportVision should be true")
	}

	// 显式声明（Providers[].vision）优先于名字判定，与会话链路同口径。
	no := false
	cfg.Providers["custom"] = config.ProviderConfig{
		APIKey: "k", Models: []string{"gpt-4o-mini"}, Vision: &no,
	}
	prov, _, err = buildBotDeps(cfg, &Config{Name: "bob"})
	if err != nil {
		t.Fatalf("buildBotDeps with explicit vision=false: %v", err)
	}
	conv = provider.GetConvertConfig(prov)
	if conv == nil {
		t.Fatal("ConvertConfig missing on the second provider")
	}
	if conv.VisionOverride == nil || *conv.VisionOverride {
		t.Errorf("explicit vision=false not carried as VisionOverride: %+v", conv)
	}
	if conv.SupportVision {
		t.Error("explicit vision=false must win over the vision-capable model name")
	}
}
