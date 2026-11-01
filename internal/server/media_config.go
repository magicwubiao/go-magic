package server

// media_config.go — 「媒体能力」设置页的服务端接口（图片 / 视频 / 语音）。
//
// 为什么要这个接口：image_gen / video_gen / voice 三段配置早就在 config.json 里
// （pkg/config、internal/voice 都有结构定义），工具也早就实现完了，但 Web UI
// 里**没有任何入口**，服务端也没有读写 API —— 用户想把图生起来只能手改
// magic home 下的 config.json。这才是"生图能不能用"的实际瓶颈。
//
// 三条设计约束：
//
//  1. **密钥只进不出**。GET 只返回是否已设置 + 掩码尾号，绝不回传明文；
//     PUT 里 api_key 字段**缺省 = 保持不变**，显式 "" = 清除。缺省即"不改"
//     是必须的：前端只改 provider 时不该被迫重填一遍 key。
//  2. **保存即生效**。写盘后立刻把新配置注入已注册的工具实例并重置可用性探测
//     缓存；否则用户填完 key 回到对话页，工具清单还是旧的（"填了没用"）。
//  3. **校验在服务端**。provider 必须是已注册的后端；视频时长/画幅/分辨率按
//     厂商共同支持的范围钳制。这些值最终会拼进厂商请求，前端校验只是体验，
//     服务端不校验就等于没有。

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/magicwubiao/go-magic/internal/tool"
	"github.com/magicwubiao/go-magic/internal/voice"
	appconfig "github.com/magicwubiao/go-magic/pkg/config"
)

// 环境变量是 config.json 的**覆盖层**：部署时改一份 env 就能切后端。
// 面板必须把这件事说清楚，否则"我改了没生效"会被当成 bug 反复排查。
var mediaEnvKeys = map[string][]string{
	"image_gen": {"IMAGE_GEN_API_KEY", "IMAGE_GEN_PROVIDER", "IMAGE_GEN_BASE_URL", "IMAGE_GEN_OUTPUT_DIR"},
	"video_gen": {"VIDEO_GEN_API_KEY", "VIDEO_GEN_PROVIDER", "VIDEO_GEN_BASE_URL", "VIDEO_GEN_MODEL", "VIDEO_GEN_OUTPUT_DIR"},
	"voice":     {"VOICE_API_KEY"},
}

// builtinVoiceProviders 是 voice.Manager 内置的 provider（不含插件贡献的）。
// 与 internal/voice 的 registerDefaultProviders 保持一致。
var builtinVoiceProviders = []string{"openai", "elevenlabs", "azure", "google", "whisper"}

type mediaConfigPatch struct {
	ImageGen *imageGenPatch `json:"image_gen"`
	VideoGen *videoGenPatch `json:"video_gen"`
	Voice    *voicePatch    `json:"voice"`
}

// 指针字段 = "请求里带了这一项才改"。JSON 里缺省与显式空串必须区分开。
type imageGenPatch struct {
	APIKey          *string `json:"api_key"`
	Provider        *string `json:"provider"`
	BaseURL         *string `json:"base_url"`
	Model           *string `json:"model"`
	DefaultSize     *string `json:"default_size"`
	DefaultStyle    *string `json:"default_style"`
	OutputDirectory *string `json:"output_dir"`
}

type videoGenPatch struct {
	APIKey            *string `json:"api_key"`
	Provider          *string `json:"provider"`
	BaseURL           *string `json:"base_url"`
	Model             *string `json:"model"`
	DefaultDuration   *int    `json:"default_duration"`
	DefaultRatio      *string `json:"default_ratio"`
	DefaultResolution *string `json:"default_resolution"`
	OutputDirectory   *string `json:"output_dir"`
}

type voicePatch struct {
	APIKey   *string  `json:"api_key"`
	Provider *string  `json:"provider"`
	Model    *string  `json:"model"`
	Voice    *string  `json:"voice"`
	Language *string  `json:"language"`
	APIURL   *string  `json:"api_url"`
	Region   *string  `json:"region"`
	Speed    *float64 `json:"speed"`
	Pitch    *float64 `json:"pitch"`
}

// handleMediaConfig 处理 GET / PUT /api/media/config。
func (s *Server) handleMediaConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		jsonResponse(w, s.mediaConfigSnapshot())
	case http.MethodPut, http.MethodPost:
		s.handleMediaConfigUpdate(w, r)
	default:
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// mediaConfigSnapshot 组装当前配置（密钥已脱敏）。
func (s *Server) mediaConfigSnapshot() map[string]interface{} {
	cfg := s.cfgSnapshot()

	var voiceCfg *voice.VoiceConfig
	if cfg != nil {
		voiceCfg = cfg.Voice
	}

	return map[string]interface{}{
		"config_path": s.mediaConfigPath(),
		"image_gen":   s.imageGenSnapshot(cfg),
		"video_gen":   s.videoGenSnapshot(cfg),
		"voice":       s.voiceSnapshot(voiceCfg),
	}
}

func (s *Server) mediaConfigPath() string {
	home := s.magicHome
	if home == "" {
		home = appconfig.GetMagicHome()
	}
	if home == "" {
		return ""
	}
	return home + string(os.PathSeparator) + "config.json"
}

// providerEntry 组装一个 provider 条目：运行态字段 + catalog 静态元信息。
//
// 两段信息必须一起下发。前端要同时回答"这家现在能不能用"（available）和
// "这家是什么、去哪儿拿 key、支持哪些画幅"（catalog）；拆成两个接口拿会让
// 下拉列表在切换瞬间闪一次空标签，也让"哪个 provider 该配哪个 key"变成前端猜测。
func providerEntry(kind tool.MediaKind, name string, runtime map[string]interface{}) map[string]interface{} {
	if runtime == nil {
		runtime = map[string]interface{}{}
	}
	runtime["name"] = name

	info, ok := tool.ProviderInfo(kind, name)
	if !ok {
		// 插件贡献的后端没有静态元信息：前端按 name 原样显示，不硬凑标签。
		return runtime
	}
	runtime["label"] = info.Label
	runtime["default_model"] = info.DefaultModel
	runtime["models"] = info.Models
	runtime["default_base_url"] = info.DefaultBaseURL
	runtime["console_url"] = info.ConsoleURL
	runtime["docs_url"] = info.DocsURL
	runtime["key_hint"] = info.KeyHint
	runtime["capabilities"] = info.Capabilities
	runtime["ratios"] = info.Ratios
	runtime["resolutions"] = info.Resolutions
	runtime["min_duration"] = info.MinDuration
	runtime["max_duration"] = info.MaxDuration
	runtime["default_voice"] = info.DefaultVoice
	runtime["notes"] = info.Notes
	return runtime
}

func (s *Server) imageGenSnapshot(appCfg *appconfig.Config) map[string]interface{} {
	var c *appconfig.ImageGenConfig
	if appCfg != nil {
		c = appCfg.ImageGen
	}
	provider := string(tool.ProviderDALLE)
	apiKey, baseURL, model, size, style, outDir := "", "", "", "", "", ""
	if c != nil {
		if strings.TrimSpace(c.Provider) != "" {
			provider = c.Provider
		}
		apiKey, baseURL, model = c.APIKey, c.BaseURL, c.Model
		size, style, outDir = c.DefaultSize, c.DefaultStyle, c.OutputDirectory
	}

	// 用工具本身的实际配置算"是否可用"，而不是只看 key：插件后端不需要 key，
	// 面板必须能显示"没有 key 但可用"这种合法状态。
	toolCfg := tool.ImageGenConfigFrom(appCfg)
	available := tool.NewImageGenerationTool(toolCfg).Available()

	providers := make([]map[string]interface{}, 0)
	for _, name := range tool.RegisteredImageProviders() {
		backend, ok := tool.LookupImageBackend(name)
		if !ok {
			continue
		}
		providers = append(providers, providerEntry(tool.MediaKindImage, name, map[string]interface{}{
			"available": backend.Available(toolCfg),
			"editable":  isImageEditor(name),
			"builtin":   isBuiltinImageProvider(name),
		}))
	}

	return map[string]interface{}{
		"provider":      provider,
		"api_key_set":   apiKey != "",
		"api_key_hint":  maskSecret(apiKey),
		"base_url":      baseURL,
		"model":         model,
		"default_size":  size,
		"default_style": style,
		"output_dir":    outDir,
		"providers":     providers,
		"available":     available,
		"env_overrides": setEnvKeys("image_gen"),
	}
}

func (s *Server) videoGenSnapshot(appCfg *appconfig.Config) map[string]interface{} {
	var c *appconfig.VideoGenConfig
	if appCfg != nil {
		c = appCfg.VideoGen
	}
	provider := string(tool.ProviderDashScope)
	apiKey, baseURL, model, ratio, resolution, outDir := "", "", "", "", "", ""
	duration := 0
	if c != nil {
		if strings.TrimSpace(c.Provider) != "" {
			provider = c.Provider
		}
		apiKey, baseURL, model = c.APIKey, c.BaseURL, c.Model
		ratio, resolution, outDir = c.DefaultRatio, c.DefaultResolution, c.OutputDirectory
		duration = c.DefaultDuration
	}

	toolCfg := tool.VideoGenConfigFrom(appCfg)
	available := tool.NewVideoGenerationTool(toolCfg).Available()

	providers := make([]map[string]interface{}, 0)
	for _, name := range tool.RegisteredVideoProviders() {
		backend, ok := tool.LookupVideoBackend(name)
		if !ok {
			continue
		}
		providers = append(providers, providerEntry(tool.MediaKindVideo, name, map[string]interface{}{
			"available": backend.Available(toolCfg),
			"builtin":   isBuiltinVideoProvider(name),
		}))
	}

	return map[string]interface{}{
		"provider":           provider,
		"api_key_set":        apiKey != "",
		"api_key_hint":       maskSecret(apiKey),
		"base_url":           baseURL,
		"model":              model,
		"default_duration":   duration,
		"default_ratio":      ratio,
		"default_resolution": resolution,
		"output_dir":         outDir,
		"providers":          providers,
		"available":          available,
		"env_overrides":      setEnvKeys("video_gen"),
	}
}

func (s *Server) voiceSnapshot(c *voice.VoiceConfig) map[string]interface{} {
	// 与 TTS/ASR 工具的缺省一致：没写 voice 段时生效的是 openai，
	// 面板要显示"实际会用的那个"，否则用户看到空下拉不知道该不该动。
	provider, model, voiceName, language, apiKey, apiURL, region := "openai", "", "", "", "", "", ""
	speed, pitch := 0.0, 0.0
	if c != nil {
		if strings.TrimSpace(c.Provider) != "" {
			provider = c.Provider
		}
		model, voiceName, language = c.Model, c.Voice, c.Language
		apiKey, apiURL, region = c.APIKey, c.APIURL, c.Region
		speed, pitch = c.Speed, c.Pitch
	}

	providers := make([]map[string]interface{}, 0, len(builtinVoiceProviders))
	available := false
	for _, name := range builtinVoiceProviders {
		ok := voiceProviderConfigured(c, name)
		if ok {
			available = true
		}
		providers = append(providers, providerEntry(tool.MediaKindSpeech, name, map[string]interface{}{
			"available":    ok,
			"builtin":      true,
			"supports_tts": name != "whisper",
			"supports_asr": name != "elevenlabs",
		}))
	}
	// 插件贡献的语音后端：内核不知道它有哪些声音，但要出现在列表里，
	// 否则用户在设置页看不到自己装的能力。
	pluginCfg := tool.LoadVoiceConfig()
	for _, name := range tool.RegisteredSpeechProviders() {
		hasTTS, hasASR := tool.SpeechCapability(name)
		backend, ok := tool.LookupSpeechBackend(name)
		if !ok {
			continue
		}
		avail := backend.Available(pluginCfg)
		if avail {
			available = true
		}
		providers = append(providers, providerEntry(tool.MediaKindSpeech, name, map[string]interface{}{
			"available":    avail,
			"builtin":      false,
			"supports_tts": hasTTS,
			"supports_asr": hasASR,
		}))
	}

	return map[string]interface{}{
		"provider":      provider,
		"model":         model,
		"voice":         voiceName,
		"language":      language,
		"api_key_set":   apiKey != "",
		"api_key_hint":  maskSecret(apiKey),
		"api_url":       apiURL,
		"region":        region,
		"speed":         speed,
		"pitch":         pitch,
		"providers":     providers,
		"available":     available,
		"env_overrides": setEnvKeys("voice"),
	}
}

// handleMediaConfigUpdate 处理 PUT：校验 → 落盘 → 热应用。
func (s *Server) handleMediaConfigUpdate(w http.ResponseWriter, r *http.Request) {
	var patch mediaConfigPatch
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&patch); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}

	// 校验在拿锁之前完成：校验只读注册表与请求体，不碰 s.cfg，
	// 放在锁里做只会拉长临界区。
	if err := validateMediaPatch(&patch); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.acquireServerMu("handleMediaConfigUpdate")
	s.lockCfgForWrite() // copy-on-write：下面原地改的是私有副本

	if s.cfg.ImageGen == nil {
		s.cfg.ImageGen = &appconfig.ImageGenConfig{}
	}
	if s.cfg.VideoGen == nil {
		s.cfg.VideoGen = &appconfig.VideoGenConfig{}
	}
	if s.cfg.Voice == nil {
		s.cfg.Voice = voice.DefaultVoiceConfig()
	}
	applyImageGenPatch(s.cfg.ImageGen, patch.ImageGen)
	applyVideoGenPatch(s.cfg.VideoGen, patch.VideoGen)
	applyVoicePatch(s.cfg.Voice, patch.Voice)

	err := s.persistConfig(true)
	s.unlockCfgForWrite()
	s.releaseServerMu()

	if err != nil {
		jsonError(w, http.StatusInternalServerError, "failed to save config: "+err.Error())
		return
	}

	// 保存即生效：把新配置注入已注册的工具实例，并让"读一次就缓存"的
	// 可用性探测重新计算（语音段就是这种缓存）。
	s.hotApplyMediaConfig()

	jsonResponse(w, s.mediaConfigSnapshot())
}

// hotApplyMediaConfig 把磁盘上的媒体配置注入正在运行的工具实例。
//
// 工具实例在启动时各持一份配置副本（NewImageGenerationTool(cfg)），只改
// s.cfg 不会影响它们 —— 这是"面板里改了、对话里还是旧后端"的根因。
func (s *Server) hotApplyMediaConfig() {
	// 用内存里的配置组装，不重新读盘：server 的 magicHome 可能与
	// config.Load() 解析出的目录不同（可由启动参数指定），读盘会拿到
	// 另一份配置，表现为"保存成功但工具还是旧后端"。
	cfg := s.cfgSnapshot()
	imgCfg := tool.ImageGenConfigFrom(cfg)
	vidCfg := tool.VideoGenConfigFrom(cfg)

	if s.toolReg != nil {
		if t, err := s.toolReg.Get("image_gen"); err == nil {
			if it, ok := t.(*tool.ImageGenerationTool); ok {
				it.SetConfig(imgCfg)
			}
		}
		if t, err := s.toolReg.Get("image_edit"); err == nil {
			if it, ok := t.(*tool.ImageEditTool); ok {
				it.SetConfig(imgCfg)
			}
		}
		if t, err := s.toolReg.Get("video_generate"); err == nil {
			if it, ok := t.(*tool.VideoGenerationTool); ok {
				it.SetConfig(vidCfg)
			}
		}
	}
	// TTS/ASR 每次 Execute 都从配置读 voice 段，不需要注入；但可用性判定
	// 用了 sync.Once 缓存，必须重置，否则设置页保存后工具清单不变。
	tool.ResetEnvProbes()
}

// ---- 校验 ----

func validateMediaPatch(p *mediaConfigPatch) error {
	if p == nil {
		return fmt.Errorf("empty request body")
	}
	if p.ImageGen != nil && p.ImageGen.Provider != nil {
		name := strings.TrimSpace(*p.ImageGen.Provider)
		if name != "" {
			if _, ok := tool.LookupImageBackend(name); !ok {
				return fmt.Errorf("unknown image provider %q (available: %s)",
					name, strings.Join(tool.RegisteredImageProviders(), ", "))
			}
		}
	}
	if p.VideoGen != nil {
		if p.VideoGen.Provider != nil {
			name := strings.TrimSpace(*p.VideoGen.Provider)
			if name != "" {
				if _, ok := tool.LookupVideoBackend(name); !ok {
					return fmt.Errorf("unknown video provider %q (available: %s)",
						name, strings.Join(tool.RegisteredVideoProviders(), ", "))
				}
			}
		}
		if p.VideoGen.DefaultDuration != nil {
			d := *p.VideoGen.DefaultDuration
			// 0 = 用后端默认；非 0 则钳到厂商共同支持的范围（3~15 秒）。
			if d != 0 && (d < 3 || d > 15) {
				return fmt.Errorf("default_duration must be 0 (provider default) or between 3 and 15 seconds, got %d", d)
			}
		}
		if p.VideoGen.DefaultRatio != nil {
			if v := strings.TrimSpace(*p.VideoGen.DefaultRatio); v != "" && !oneOf(v, "16:9", "9:16", "1:1") {
				return fmt.Errorf("default_ratio must be 16:9, 9:16 or 1:1, got %q", v)
			}
		}
		if p.VideoGen.DefaultResolution != nil {
			if v := strings.TrimSpace(*p.VideoGen.DefaultResolution); v != "" && !oneOf(v, "480p", "720p", "1080p") {
				return fmt.Errorf("default_resolution must be 480p, 720p or 1080p, got %q", v)
			}
		}
	}
	if p.Voice != nil {
		if p.Voice.Provider != nil {
			name := strings.TrimSpace(*p.Voice.Provider)
			if name != "" && !oneOf(name, builtinVoiceProviders...) {
				if _, ok := tool.LookupSpeechBackend(name); !ok {
					return fmt.Errorf("unknown voice provider %q (builtin: %s)",
						name, strings.Join(builtinVoiceProviders, ", "))
				}
			}
		}
		if p.Voice.Speed != nil {
			if v := *p.Voice.Speed; v != 0 && (v < 0.5 || v > 2.0) {
				return fmt.Errorf("voice speed must be between 0.5 and 2.0, got %v", v)
			}
		}
		if p.Voice.Pitch != nil {
			if v := *p.Voice.Pitch; v < -20 || v > 20 {
				return fmt.Errorf("voice pitch must be between -20 and 20, got %v", v)
			}
		}
	}
	return nil
}

// ---- 应用补丁（只改请求里出现的字段） ----

func applyImageGenPatch(c *appconfig.ImageGenConfig, p *imageGenPatch) {
	if c == nil || p == nil {
		return
	}
	if p.APIKey != nil {
		c.APIKey = strings.TrimSpace(*p.APIKey)
	}
	if p.Provider != nil {
		c.Provider = strings.TrimSpace(*p.Provider)
	}
	if p.BaseURL != nil {
		c.BaseURL = strings.TrimSpace(*p.BaseURL)
	}
	if p.Model != nil {
		c.Model = strings.TrimSpace(*p.Model)
	}
	if p.DefaultSize != nil {
		c.DefaultSize = strings.TrimSpace(*p.DefaultSize)
	}
	if p.DefaultStyle != nil {
		c.DefaultStyle = strings.TrimSpace(*p.DefaultStyle)
	}
	if p.OutputDirectory != nil {
		c.OutputDirectory = strings.TrimSpace(*p.OutputDirectory)
	}
}

func applyVideoGenPatch(c *appconfig.VideoGenConfig, p *videoGenPatch) {
	if c == nil || p == nil {
		return
	}
	if p.APIKey != nil {
		c.APIKey = strings.TrimSpace(*p.APIKey)
	}
	if p.Provider != nil {
		c.Provider = strings.TrimSpace(*p.Provider)
	}
	if p.BaseURL != nil {
		c.BaseURL = strings.TrimSpace(*p.BaseURL)
	}
	if p.Model != nil {
		c.Model = strings.TrimSpace(*p.Model)
	}
	if p.DefaultDuration != nil {
		c.DefaultDuration = *p.DefaultDuration
	}
	if p.DefaultRatio != nil {
		c.DefaultRatio = strings.TrimSpace(*p.DefaultRatio)
	}
	if p.DefaultResolution != nil {
		c.DefaultResolution = strings.TrimSpace(*p.DefaultResolution)
	}
	if p.OutputDirectory != nil {
		c.OutputDirectory = strings.TrimSpace(*p.OutputDirectory)
	}
}

func applyVoicePatch(c *voice.VoiceConfig, p *voicePatch) {
	if c == nil || p == nil {
		return
	}
	if p.APIKey != nil {
		c.APIKey = strings.TrimSpace(*p.APIKey)
	}
	if p.Provider != nil {
		c.Provider = strings.TrimSpace(*p.Provider)
	}
	if p.Model != nil {
		c.Model = strings.TrimSpace(*p.Model)
	}
	if p.Voice != nil {
		c.Voice = strings.TrimSpace(*p.Voice)
	}
	if p.Language != nil {
		c.Language = strings.TrimSpace(*p.Language)
	}
	if p.APIURL != nil {
		c.APIURL = strings.TrimSpace(*p.APIURL)
	}
	if p.Region != nil {
		c.Region = strings.TrimSpace(*p.Region)
	}
	if p.Speed != nil {
		c.Speed = *p.Speed
	}
	if p.Pitch != nil {
		c.Pitch = *p.Pitch
	}
}

// ---- 小工具 ----

// maskSecret 只暴露尾号：面板需要"看起来配了没有"，但明文 key 绝不出后端。
// 太短的 key 直接全遮，避免把 4 位以内的秘密原样吐出来。
func maskSecret(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	r := []rune(s)
	if len(r) <= 8 {
		return strings.Repeat("•", 8)
	}
	return "••••" + string(r[len(r)-4:])
}

// setEnvKeys 返回当前真正设置过的环境变量名。
func setEnvKeys(section string) []string {
	keys := mediaEnvKeys[section]
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		if v, ok := os.LookupEnv(k); ok && strings.TrimSpace(v) != "" {
			out = append(out, k)
		}
	}
	return out
}

func oneOf(v string, candidates ...string) bool {
	for _, c := range candidates {
		if v == c {
			return true
		}
	}
	return false
}

// isImageEditor 报告某个图片 provider 是否支持编辑（决定 image_edit 能不能用）。
func isImageEditor(name string) bool {
	_, editor, ok := tool.LookupImageEditor(name)
	return ok && editor != nil
}

func isBuiltinImageProvider(name string) bool {
	return oneOf(name, string(tool.ProviderDALLE), string(tool.ProviderStableDiffusion),
		string(tool.ProviderMidjourney), string(tool.ProviderTogether))
}

func isBuiltinVideoProvider(name string) bool {
	return oneOf(name, string(tool.ProviderDashScope), string(tool.ProviderVolcengine))
}

// voiceProviderConfigured 判断某个内置 voice provider 是否配好了凭据。
// 与 voice.Manager.getProviderAPIKey 同一条优先链：provider 专属凭据 > 顶层 key。
func voiceProviderConfigured(c *voice.VoiceConfig, name string) bool {
	if c == nil {
		return false
	}
	if creds, ok := c.Providers[name]; ok && strings.TrimSpace(creds.APIKey) != "" {
		return true
	}
	if strings.TrimSpace(c.APIKey) != "" {
		return true
	}
	// whisper 支持本地运行，但内核无法判断本机是否装了 whisper.cpp，这里
	// 不谎报"可用"——真的本地部署走插件后端更准确。
	return false
}
