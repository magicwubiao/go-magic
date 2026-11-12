package voice

import "testing"

// defaults_test.go — 默认模型 / 音色的来源与跨厂商隔离。

// 五家内置 provider 共用过一个 m.config.Model：用户在设置页把 provider 切到
// ElevenLabs、模型栏还留着 gpt-4o-mini-tts 时，请求就会带着 OpenAI 的模型名
// 发给 ElevenLabs（必 404）。配置里的 model/voice 只属于"当前选中的那一家"。
func TestProviderConfigsDoNotShareModelAcrossProviders(t *testing.T) {
	m := NewManager(&VoiceConfig{Provider: "openai", Model: "gpt-4o-mini-tts", Voice: "alloy"})

	openai, ok := m.providers["openai"].(*OpenAIProvider)
	if !ok {
		t.Fatal("openai provider missing or wrong type")
	}
	if openai.config.Model != "gpt-4o-mini-tts" || openai.config.Voice != "alloy" {
		t.Errorf("openai got model=%q voice=%q, want the configured values",
			openai.config.Model, openai.config.Voice)
	}

	eleven, ok := m.providers["elevenlabs"].(*ElevenLabsProvider)
	if !ok {
		t.Fatal("elevenlabs provider missing or wrong type")
	}
	if eleven.config.Model != DefaultModelFor("elevenlabs") {
		t.Errorf("elevenlabs model = %q, want its own default %q",
			eleven.config.Model, DefaultModelFor("elevenlabs"))
	}
	if eleven.config.Voice != DefaultVoiceFor("elevenlabs") {
		t.Errorf("elevenlabs voice = %q, want its own default %q",
			eleven.config.Voice, DefaultVoiceFor("elevenlabs"))
	}
}

// 用户什么都没填时，每家都要落到自己的推荐值，而不是空字符串（空模型会被
// 厂商按最老的一档处理）。
func TestEmptyConfigFallsBackToPerProviderDefaults(t *testing.T) {
	m := NewManager(&VoiceConfig{Provider: "openai"})

	if got := m.modelFor("openai"); got != DefaultModelFor("openai") {
		t.Errorf("modelFor(openai) = %q, want %q", got, DefaultModelFor("openai"))
	}
	if got := m.voiceFor("openai"); got != DefaultVoiceFor("openai") {
		t.Errorf("voiceFor(openai) = %q, want %q", got, DefaultVoiceFor("openai"))
	}
	if got := m.modelFor("whisper"); got == "" {
		t.Error("modelFor(whisper) is empty; local whisper needs a size tier")
	}
}

// 配置里的值必须能被选中 provider 用上（不能因为隔离把用户显式设置吃掉）。
func TestConfiguredModelAppliesToSelectedProvider(t *testing.T) {
	m := NewManager(&VoiceConfig{Provider: "elevenlabs", Model: "eleven_v3", Voice: "AZnzlk1XvdvUeBnXmlld"})

	if got := m.modelFor("elevenlabs"); got != "eleven_v3" {
		t.Errorf("modelFor(elevenlabs) = %q, want eleven_v3", got)
	}
	if got := m.modelFor("openai"); got != DefaultModelFor("openai") {
		t.Errorf("modelFor(openai) = %q, want %q (must not inherit eleven_v3)", got, DefaultModelFor("openai"))
	}
}

// Azure / Google 没有单一默认音色：音色 ID 自带语言（en-US-JennyNeural 读中文
// 会带浓重口音），所以默认值必须由 language 算，而不是钉死一个值。
func TestDefaultVoiceForLanguage(t *testing.T) {
	cases := []struct{ provider, language, want string }{
		{"azure", "zh-CN", "zh-CN-XiaoxiaoNeural"},
		{"azure", "en-US", "en-US-JennyNeural"},
		{"azure", "", "en-US-JennyNeural"},           // 空语言 = provider 内部的 en-US
		{"azure", "unknown-XX", "en-US-JennyNeural"}, // 未知语言回落 en-US
		{"google", "en-US", "en-US-Standard-C"},
		{"google", "zh-CN", "zh-CN-Standard-C"},
		{"google", "", "en-US-Standard-C"},
		// 音色与语言无关的厂商要返回空，让调用方去用静态默认值。
		{"openai", "zh-CN", ""},
		{"elevenlabs", "ja-JP", ""},
	}
	for _, c := range cases {
		if got := DefaultVoiceForLanguage(c.provider, c.language); got != c.want {
			t.Errorf("DefaultVoiceForLanguage(%q, %q) = %q, want %q", c.provider, c.language, got, c.want)
		}
	}
}

// Azure 的请求必须和上表一致（两边共用同一张表，这里钉住"确实共用了"）。
func TestAzureProviderUsesLanguageDefault(t *testing.T) {
	p := NewAzureProvider(&ProviderConfig{APIKey: "k", Region: "eastasia", Language: "zh-CN"})
	if p.config.Voice != "" {
		t.Fatalf("test setup: voice should be empty, got %q", p.config.Voice)
	}
	if got := DefaultVoiceForLanguage("azure", p.config.Language); got != "zh-CN-XiaoxiaoNeural" {
		t.Errorf("azure zh-CN default = %q, want zh-CN-XiaoxiaoNeural", got)
	}
}
