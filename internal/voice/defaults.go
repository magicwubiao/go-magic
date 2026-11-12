package voice

import "strings"

// defaults.go — 内置语音厂商的默认模型 / 默认音色（唯一真源）。
//
// 为什么真源在本包而不是 media_catalog（internal/tool）：
//  1. 依赖方向只能是 tool → voice（tts_tool / asr_tool 都要构造 Manager），
//     让 voice 反向去读 catalog 会成环；
//  2. voice 要能被 `magic voice` 独立使用，默认值必须在本包内自洽，
//     不能指望调用方每次都替它填好；
//  3. 最重要的一条：默认值必须贴着**真正发请求的那段代码**。曾经 catalog
//     写 gpt-4o-mini-tts、这里的回落还是 tts-1，界面写着 A、请求发的是 B，
//     这类不一致从界面上看不出来，只能靠调用失败去撞。
//
// catalog 里这 5 家的 DefaultModel / DefaultVoice 直接引用下面的函数，不再各写一份。
// azure / google 不在表里：它们的请求体里根本没有 model 字段（Azure 靠 SSML 的
// voice 名，Google 靠 voice.languageCode + name），catalog 里那两个模型名只是
// 设置页下拉的占位，不参与请求，也就没有"两套值"的问题。

// BuiltinSpeechDefault 一家厂商的默认模型与默认音色。空字符串表示"该厂商不需要"。
type BuiltinSpeechDefault struct {
	Model string
	Voice string
}

// builtinSpeechDefaults 内置语音厂商的推荐值。
//
// 只放"默认值"，不放可选清单：可选模型是产品/UI 的事，留在 catalog；
// 这里只回答"用户没填的时候到底发什么"。
var builtinSpeechDefaults = map[string]BuiltinSpeechDefault{
	// gpt-4o-mini-tts 是当前推荐款（支持 instructions 控制语气），tts-1 系列是上一代。
	"openai": {Model: "gpt-4o-mini-tts", Voice: "alloy"},
	// eleven_multilingual_v2 做默认而不是 eleven_v3：v3 表现力更强，但要求输入较长，
	// 一句话的短文本容易被拒。
	"elevenlabs": {Model: "eleven_multilingual_v2", Voice: "21m00Tcm4TlvDq8ikWAM"},
	// whisper 是**本地** CLI（跑 whisper.cpp 尺寸档），不是 OpenAI 的 whisper-1；
	// 托管版 Whisper 走 provider openai，模型 whisper-1。
	"whisper": {Model: "base"},
}

// DefaultModelFor 返回该内置厂商的默认模型；未知厂商或该厂商不需要模型时返回 ""。
func DefaultModelFor(provider string) string {
	return builtinSpeechDefaults[provider].Model
}

// DefaultVoiceFor 返回该内置厂商的默认音色；未知厂商或该厂商不需要音色时返回 ""。
func DefaultVoiceFor(provider string) string {
	return builtinSpeechDefaults[provider].Voice
}

// azureVoicesByLanguage 是 Azure 的"语言 → 默认音色"表。
//
// Azure 的音色 ID 自带语言（en-US-JennyNeural 读中文会带浓重口音），所以它没有
// 单一默认音色，只有"某门语言的默认音色"。这张表既用于发请求时的回落，也用于
// 设置页显示"留空实际会用哪个"——两处共用，避免又出现两套对照关系。
var azureVoicesByLanguage = map[string]string{
	"en-US": "en-US-JennyNeural",
	"en-GB": "en-GB-SoniaNeural",
	"zh-CN": "zh-CN-XiaoxiaoNeural",
	"zh-HK": "zh-HK-HiuMaanNeural",
	"ja-JP": "ja-JP-NanamiNeural",
	"ko-KR": "ko-KR-SunHiNeural",
	"de-DE": "de-DE-KatjaNeural",
	"fr-FR": "fr-FR-DeniseNeural",
	"es-ES": "es-ES-ElviraNeural",
}

// DefaultVoiceForLanguage 返回"这门语言下留空音色会用哪个"。
//
// 只对 azure / google 有意义：其余厂商的音色与语言无关（ElevenLabs 的音色是
// 模型无关的 voice ID，换语言用同一个）。language 为空时按 en-US 处理，
// 与各家 provider 内部的默认语言保持一致。
func DefaultVoiceForLanguage(provider, language string) string {
	lang := strings.TrimSpace(language)
	if lang == "" {
		lang = "en-US"
	}
	switch provider {
	case "azure":
		if v, ok := azureVoicesByLanguage[lang]; ok {
			return v
		}
		// 未知语言回落 en-US（与 AzureProvider 原来的行为一致）。
		return azureVoicesByLanguage["en-US"]
	case "google":
		// Google 的默认音色就是"语言 + 标准档"。
		return lang + "-Standard-C"
	}
	return ""
}
