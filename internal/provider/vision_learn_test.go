package provider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 正向学习：未知模型接受图片后，判定翻转为支持。
func TestRememberModelVisionPositive(t *testing.T) {
	resetLearnedVisionForTest()
	const model = "brand-new-flagship-2099"

	if ModelSupportsVision(model) {
		t.Fatal("unknown model must not be vision-capable before learning")
	}
	if !RememberModelVision(model) {
		t.Fatal("first positive learning must report a state change")
	}
	if RememberModelVision(model) {
		t.Fatal("re-learning an already-known positive must not report a change")
	}
	if !ModelSupportsVision(model) {
		t.Fatal("model must be remembered as vision-capable after acceptance")
	}
}

// 最新证据获胜：正反两个方向互相覆盖。
func TestLearnedVisionLatestEvidenceWins(t *testing.T) {
	resetLearnedVisionForTest()
	const model = "evidence-flip-model"

	RememberModelVision(model)
	RememberModelNoVision(model)
	if ModelSupportsVision(model) {
		t.Fatal("latest negative evidence must win")
	}

	RememberModelVision(model)
	if !ModelSupportsVision(model) {
		t.Fatal("latest positive evidence must win")
	}
}

// 持久化：学习结果落盘，模拟重启后仍生效；反向证据过 TTL 后失效。
func TestVisionLearnedPersistence(t *testing.T) {
	resetLearnedVisionForTest()
	path := filepath.Join(t.TempDir(), "vision_learned.json")
	SetVisionLearnedPath(path)

	if !RememberModelVision("persisted-vision-model") {
		t.Fatal("expected state change")
	}
	if !RememberModelNoVision("persisted-text-model") {
		t.Fatal("expected state change")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("learning cache not persisted: %v", err)
	}

	reloadVisionLearnedForTest() // 模拟进程重启
	if !ModelSupportsVision("persisted-vision-model") {
		t.Fatal("positive learning must survive restart")
	}
	if ModelSupportsVision("persisted-text-model") {
		t.Fatal("negative learning must survive restart")
	}

	// 手工把反向证据的时间戳改到 TTL 之外，重载后应视为无证据。
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}
	var parsed struct {
		Positive map[string]time.Time `json:"positive"`
		Negative map[string]time.Time `json:"negative"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse cache: %v", err)
	}
	parsed.Negative["persisted-text-model"] = time.Now().Add(-visionNegativeTTL - time.Hour)
	fresh, err := json.Marshal(parsed)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, fresh, 0o644); err != nil {
		t.Fatalf("write cache: %v", err)
	}

	resetLearnedVisionForTest()
	reloadVisionLearnedForTest()
	if _, known := learnedVisionVerdict("persisted-text-model"); known {
		t.Fatal("expired negative evidence must be treated as unknown")
	}
	if _, known := learnedVisionVerdict("persisted-vision-model"); !known {
		t.Fatal("positive evidence has no TTL and must stay known")
	}
}

// 加载损坏的缓存文件必须静默降级为空表，不能 panic 或污染判定。
func TestVisionLearnedCorruptFile(t *testing.T) {
	resetLearnedVisionForTest()
	path := filepath.Join(t.TempDir(), "vision_learned.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write corrupt cache: %v", err)
	}
	SetVisionLearnedPath(path)
	reloadVisionLearnedForTest()
	if _, known := learnedVisionVerdict("anything"); known {
		t.Fatal("corrupt cache must degrade to empty evidence")
	}
}

// 学习结果不得越过用户显式声明：VisionOverride=true 时即便学到了
// 「不支持」，prep 链路（WithAutoVision）也要强制开视觉。
func TestVisionOverrideBeatsLearnedEvidence(t *testing.T) {
	resetLearnedVisionForTest()
	yes := true
	RememberModelNoVision("glm-5.3-flash") // 已知启发式判 true 的模型
	cfg := &ConvertConfig{SupportVision: false, AutoVision: true, VisionOverride: &yes}
	if got := cfg.WithAutoVision("glm-5.3-flash"); !got.SupportVision {
		t.Fatal("VisionOverride=true must force SupportVision on despite learned negative")
	}
}
