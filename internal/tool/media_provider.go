package tool

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/magicwubiao/go-magic/internal/util"
	"github.com/magicwubiao/go-magic/internal/voice"
)

// media_provider.go — 媒体生成的「后端（backend / provider）」扩展点。
//
// 这里划的是**能力与供应商的边界**，不是工具边界。四条不变量，改动前务必先读：
//
//  1. **工具名属于内核**。image_gen / image_edit / video_generate 永远只有这三个，
//     模型看到的 schema 也永远是这一份。新增一家厂商只扩充 provider 列表，绝不新增
//     kling_generate / runway_generate 这类工具名 —— 工具一裂变，模型的选择准确率
//     立刻掉（video_generate 单个工具已有 15 个参数）。
//  2. **产物落盘属于内核**。后端只负责"拿到字节，或拿到一个临时直链"；下载、建目录、
//     扩展名推断、temp+rename 全部由内核做（见 downloadAndSaveImages /
//     downloadAndSaveVideo）。厂商直链普遍 24h 过期（见 video_gen_tool.go 头注释），
//     所以 **URL 绝不透传给模型**，只作落盘输入与人工复核字段。
//  3. **可用性属于内核**。Available 的语义是"这个后端在当前配置下能不能用"，由内核在
//     ListWithSchemas 时统一裁剪。后端不要自己判断"没配 key 就返回一段提示文本"。
//  4. **凭据属于内核**。APIKey / BaseURL / Timeout 全在 ImageGenConfig /
//     VideoGenConfig，插件后端由适配器注入，不另立一套 ${CRED:} 引用 —— 两处都能填
//     key 而行为不一致，是必然出 bug 的设计。
//
// 注册表并发安全，同名后端**后注册者覆盖先注册者**（插件重连/热更新需要）。
// 卸载插件时调 Unregister* 清掉，避免 config 里还指着它、运行时却已消失的"残影"。

// ImageRequest 一次图片生成的归一化请求。
//
// 后端拿到的是已经处理过默认值与区间钳制的值，不需要再解析工具参数、也不该再看
// params map —— 工具层与后端层之间只有这一个结构，插件适配器实现同一接口即可。
type ImageRequest struct {
	Prompt         string
	NegativePrompt string
	Style          string
	Size           string
	Count          int
	Seed           *int

	// InputImage 是图生图 / 编辑场景的输入图字节（image_edit 用）。
	// 文生图时为空。后端不支持编辑时返回明确错误，而不是静默忽略。
	InputImage []byte
	// MaskPath 是编辑场景的局部重绘遮罩路径，仅支持的后端使用。
	MaskPath string
}

// ImageBackend 图片生成后端。
//
// 内置的 4 个（dall-e / stable-diffusion / midjourney / together）在
// media_backend_builtin.go 里以适配器形式注册；插件贡献的后端只要实现同一接口，
// 工具名、schema、落盘链路、可用性裁剪全都不用动。
type ImageBackend interface {
	// Name 是 provider 标识，必须与 config.json 的 image_gen.provider 取值一致（小写）。
	Name() string
	// Available 判断当前配置下该后端能否工作。返回 false 时 image_gen / image_edit
	// 不会出现在模型工具清单里。
	Available(cfg *ImageGenConfig) bool
	// Generate 只负责产出结果，不写文件、不做目录管理。
	// 返回的 ImageInfo 允许带 URL（内核会立刻下载落盘）或 Base64Data / Data。
	Generate(ctx context.Context, cfg *ImageGenConfig, req ImageRequest) (*ImageGenerationResult, error)
}

// ImageEditRequest 一次图片编辑（重绘 / 扩图 / 变体）的归一化请求。
//
// 与 ImageRequest 分开而不是塞进去当可选字段：编辑与生成是两条**能力不同**的
// 路径，混成一个结构会让"这个后端到底支不支持编辑"变成字段判空的隐式约定。
type ImageEditRequest struct {
	Prompt   string
	EditType string // inpaint / outpaint / variation
	Size     string
	Count    int
	// Image 是源图字节，由工具层读取后传入（后端不碰文件系统路径）。
	Image []byte
	// MaskPath 是局部重绘遮罩路径，仅支持的后端使用。
	MaskPath string
}

// ImageEditor 是 ImageBackend 的**可选**编辑能力。
//
// 单独拆成接口而不是塞进 ImageBackend：4 个内置后端里只有 dall-e 支持编辑，
// 强制每家实现一个必然返回 "not supported" 的方法，等于把不可用伪装成可用。
// image_edit 只在"后端可用 + 实现了 ImageEditor"时暴露给模型。
type ImageEditor interface {
	Edit(ctx context.Context, cfg *ImageGenConfig, req ImageEditRequest) (*ImageGenerationResult, error)
}

// LookupImageEditor 查找同时具备编辑能力的图片后端。
func LookupImageEditor(name string) (ImageBackend, ImageEditor, bool) {
	b, ok := LookupImageBackend(name)
	if !ok {
		return nil, nil, false
	}
	editor, ok := b.(ImageEditor)
	if !ok {
		return b, nil, false
	}
	return b, editor, true
}

// RegisteredImageEditors 返回支持编辑的 provider 名称（升序），用于错误提示。
func RegisteredImageEditors() []string {
	imageBackendsMu.RLock()
	defer imageBackendsMu.RUnlock()
	names := make([]string, 0, len(imageBackends))
	for name, entry := range imageBackends {
		if _, ok := entry.backend.(ImageEditor); ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// VideoRequest 一次视频生成的归一化请求。
type VideoRequest struct {
	Prompt         string
	NegativePrompt string

	// ImageURL 是公网可达的首帧图地址（图生视频）。
	ImageURL string
	// ImagePath 是本地首帧图路径。是否支持由后端决定：volcengine 支持 base64
	// data URI，dashscope 只收公网地址，不支持的后端必须返回明确错误。
	ImagePath string

	Model      string
	Duration   int
	Ratio      string
	Resolution string
}

// VideoResult 后端产出的视频指纹：一个临时直链 + 任务 ID。
//
// 这里刻意只给 URL：视频可能几十 MB，塞进内存不划算；内核的 downloadAndSaveVideo
// 当场取回并 temp+rename 落盘，模型最终只看到本地路径。
//
// 后面四个字段是**实际生效的参数**，不是请求值：各厂商会把 duration 吸附到自己的
// 档位、把 ratio 换成像素比、把 resolution 换成 "2K" 这类自家标识。工具层回显
// 时优先用这里的值，否则用户会以为参数生效了（"我明明要 8 秒，结果 5 秒"）。
type VideoResult struct {
	URL    string
	TaskID string

	Duration   int
	Ratio      string
	Resolution string
	Model      string
}

// VideoBackend 视频生成后端（异步任务型：提交 → 轮询 → 出片由后端内部完成）。
type VideoBackend interface {
	Name() string
	Available(cfg *VideoGenConfig) bool
	Generate(ctx context.Context, cfg *VideoGenConfig, req VideoRequest) (*VideoResult, error)
}

var (
	imageBackendsMu sync.RWMutex
	imageBackends   = make(map[string]imageBackendEntry)

	videoBackendsMu sync.RWMutex
	videoBackends   = make(map[string]videoBackendEntry)
)

// normalizeBackendName 统一 provider 标识：大小写不敏感、去空白。
// config.json / 环境变量 / 工具参数三处都可能手写成 "DALL-E"，不该因此查不到后端。
func normalizeBackendName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// effectiveImageProvider 返回实际生效的图片 provider。
//
// 零值回退是必须的：手工构造的 &ImageGenConfig{APIKey: "..."} 不带 Provider，
// 若直接拿去查注册表就会查不到、工具变成"不可用"，而用户明明配了 key。
// 生产路径（LoadImageGenConfig）本来就会填默认值，这里兜的是 SetConfig /
// 测试 / 部分配置合并这几种会漏字段的入口。
func effectiveImageProvider(cfg *ImageGenConfig) ImageProvider {
	if cfg == nil {
		return ProviderDALLE
	}
	if p := strings.TrimSpace(string(cfg.Provider)); p != "" {
		return ImageProvider(p)
	}
	return ProviderDALLE
}

// effectiveVideoProvider 同 effectiveImageProvider，默认 dashscope。
func effectiveVideoProvider(cfg *VideoGenConfig) VideoProvider {
	if cfg == nil {
		return ProviderDashScope
	}
	if p := strings.TrimSpace(string(cfg.Provider)); p != "" {
		return VideoProvider(p)
	}
	return ProviderDashScope
}

// imageBackendEntry / videoBackendEntry 是注册表条目。
//
// owner 为空表示内核内置后端。owner 存在的唯一目的是"卸载只清自己的"：
// 插件 A 注册 comfyui、插件 B 又注册同名 comfyui（后注册者生效），A 被卸载时
// 不能把 B 的后端一起删掉 —— 只按 key 删做不到这个区分。
type imageBackendEntry struct {
	backend ImageBackend
	owner   string
}

type videoBackendEntry struct {
	backend VideoBackend
	owner   string
}

// RegisterImageBackend 注册内核内置图片后端，同名覆盖。
func RegisterImageBackend(b ImageBackend) {
	registerImageBackend("", b)
}

// RegisterImageBackendOwned 注册由某个所有者（插件）贡献的图片后端。
// 同名覆盖仍按"后注册者胜"，但卸载时走 UnregisterImageBackendsByOwner，
// 只删除 owner 仍然匹配的条目。
func RegisterImageBackendOwned(owner string, b ImageBackend) {
	registerImageBackend(owner, b)
}

func registerImageBackend(owner string, b ImageBackend) {
	if b == nil {
		return
	}
	name := normalizeBackendName(b.Name())
	if name == "" {
		return
	}
	imageBackendsMu.Lock()
	defer imageBackendsMu.Unlock()
	imageBackends[name] = imageBackendEntry{backend: b, owner: owner}
}

// UnregisterImageBackend 按名字无条件移除（内置后端下线、测试清理用）。
// 插件卸载请用 UnregisterImageBackendsByOwner，避免误删别人的同名后端。
func UnregisterImageBackend(name string) {
	imageBackendsMu.Lock()
	defer imageBackendsMu.Unlock()
	delete(imageBackends, normalizeBackendName(name))
}

// UnregisterImageBackendsByOwner 移除某个所有者贡献的全部图片后端，返回删除数量。
// 已被其他所有者覆盖的同名条目不删（owner 不匹配）。
func UnregisterImageBackendsByOwner(owner string) int {
	if owner == "" {
		return 0
	}
	imageBackendsMu.Lock()
	defer imageBackendsMu.Unlock()
	removed := 0
	for name, entry := range imageBackends {
		if entry.owner == owner {
			delete(imageBackends, name)
			removed++
		}
	}
	return removed
}

// LookupImageBackend 按名称查找图片后端。名字支持别名（dalle → dall-e，见 media_catalog.go）。
//
// 查找顺序是"原名优先，别名兜底"：内置别名表只覆盖常见手写变体，
// 若某个插件恰好把后端命名为 alias 里的词，原名命中就不该被改写成别的 provider。
func LookupImageBackend(name string) (ImageBackend, bool) {
	key := normalizeBackendName(name)
	imageBackendsMu.RLock()
	defer imageBackendsMu.RUnlock()
	if entry, ok := imageBackends[key]; ok {
		return entry.backend, true
	}
	if canonical := CanonicalProviderName(MediaKindImage, name); canonical != key {
		entry, ok := imageBackends[canonical]
		return entry.backend, ok
	}
	return nil, false
}

// RegisteredImageProviders 返回已注册的图片 provider 名称（升序，便于日志与测试断言）。
func RegisteredImageProviders() []string {
	imageBackendsMu.RLock()
	defer imageBackendsMu.RUnlock()
	names := make([]string, 0, len(imageBackends))
	for name := range imageBackends {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// isBuiltinImageBackend 与 isBuiltinVideoBackend 同理：image_gen 段也只有一份凭据。
func isBuiltinImageBackend(name string) bool {
	_, ok := ProviderInfo(MediaKindImage, name)
	return ok
}

// RegisterVideoBackend 注册内核内置视频后端，同名覆盖。
func RegisterVideoBackend(b VideoBackend) {
	registerVideoBackend("", b)
}

// RegisterVideoBackendOwned 注册由某个所有者（插件）贡献的视频后端。
func RegisterVideoBackendOwned(owner string, b VideoBackend) {
	registerVideoBackend(owner, b)
}

func registerVideoBackend(owner string, b VideoBackend) {
	if b == nil {
		return
	}
	name := normalizeBackendName(b.Name())
	if name == "" {
		return
	}
	videoBackendsMu.Lock()
	defer videoBackendsMu.Unlock()
	videoBackends[name] = videoBackendEntry{backend: b, owner: owner}
}

// UnregisterVideoBackend 按名字无条件移除（内置后端下线、测试清理用）。
func UnregisterVideoBackend(name string) {
	videoBackendsMu.Lock()
	defer videoBackendsMu.Unlock()
	delete(videoBackends, normalizeBackendName(name))
}

// UnregisterVideoBackendsByOwner 移除某个所有者贡献的全部视频后端。
func UnregisterVideoBackendsByOwner(owner string) int {
	if owner == "" {
		return 0
	}
	videoBackendsMu.Lock()
	defer videoBackendsMu.Unlock()
	removed := 0
	for name, entry := range videoBackends {
		if entry.owner == owner {
			delete(videoBackends, name)
			removed++
		}
	}
	return removed
}

// LookupVideoBackend 按名称查找视频后端（支持 hailuo / seedance 这类别名）。
func LookupVideoBackend(name string) (VideoBackend, bool) {
	key := normalizeBackendName(name)
	videoBackendsMu.RLock()
	defer videoBackendsMu.RUnlock()
	if entry, ok := videoBackends[key]; ok {
		return entry.backend, true
	}
	if canonical := CanonicalProviderName(MediaKindVideo, name); canonical != key {
		entry, ok := videoBackends[canonical]
		return entry.backend, ok
	}
	return nil, false
}

// isBuiltinVideoBackend 报告 name 是否指向一个**内核自带**的视频后端。
//
// 只有一个用途：模型在参数里把 provider 换成另一家时，判断这次请求能不能借用
// config.json 里那份凭据。内置厂商的 key / base_url / model 是全局单值（video_gen
// 段只存一份），换成别家就等于拿 A 的密钥打 B 的接口；插件后端自带凭据，不受此限。
//
// 判据用 catalog（= 内核认可的厂商清单）而不是注册表 owner：插件覆盖同名内置
// 实现时凭据仍来自 config，同样不能借给别家。
func isBuiltinVideoBackend(name string) bool {
	_, ok := ProviderInfo(MediaKindVideo, name)
	return ok
}

// RegisteredVideoProviders 返回已注册的视频 provider 名称（升序）。
func RegisteredVideoProviders() []string {
	videoBackendsMu.RLock()
	defer videoBackendsMu.RUnlock()
	names := make([]string, 0, len(videoBackends))
	for name := range videoBackends {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// maxAudioDownloadBytes 语音产物下载上限。1 小时 mp3（128kbps）约 57MB，
// 64MB 足够覆盖正常场景，同时挡住"插件给个链接指向一个 4GB 文件"。
const maxAudioDownloadBytes = 64 << 20

// MaxAudioDownloadBytes 是语音产物下载上限的导出值，供插件适配层复用同一口径。
const MaxAudioDownloadBytes = maxAudioDownloadBytes

// DownloadMediaBytes 带体积上限地取回一个媒体直链。
//
// 插件后端的返回值有两类：字节（b64 / data URI）或临时直链。直链必须在**当下**
// 取回（厂商链接普遍 24h 过期，见 video_gen_tool.go 头注释），而取回的动作属于
// 内核：体积上限、超时、HTTP 状态判断都由这里统一，插件不各自实现一遍。
func DownloadMediaBytes(ctx context.Context, url string, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		maxBytes = maxImageDownloadBytes
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := util.GetHTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	// Content-Length 只是提示（可能缺失或撒谎），下面仍按实际读取量兜底。
	if resp.ContentLength > maxBytes {
		return nil, fmt.Errorf("media too large: %d bytes (limit %d)", resp.ContentLength, maxBytes)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("media exceeds %d bytes", maxBytes)
	}
	return data, nil
}

// extensionFromMime 把 MIME 映射成文件扩展名，未知返回空串（由调用方兜底）。
// 插件后端经常只知道 MIME 不知道扩展名，落盘时不能因此写成 ".png" 硬编码。
func extensionFromMime(mime string) string {
	switch strings.ToLower(strings.TrimSpace(mime)) {
	case "image/png":
		return "png"
	case "image/jpeg", "image/jpg":
		return "jpg"
	case "image/webp":
		return "webp"
	case "image/gif":
		return "gif"
	case "video/mp4":
		return "mp4"
	case "video/webm":
		return "webm"
	case "audio/mpeg":
		return "mp3"
	case "audio/wav", "audio/x-wav":
		return "wav"
	}
	return ""
}

// ---- 语音（TTS / ASR）----
//
// 语音与图片 / 视频的结构差异：内置语音能力早就有一套 voice.Manager +
// VoiceProvider（openai / azure / google / elevenlabs / whisper），它自己管
// 配置合并、预设、格式转换。这里**不替换它**，只加一个并列的扩展点：
// 工具先查注册表（插件贡献的后端），查不到再回落到 voice.Manager。
//
// 这样做的代价是"内置语音后端不在注册表里"，收益是不动 voice 包的任何行为，
// 也不会让 GetProviders()/GetAvailableVoices() 这些既有语义跟着漂。
// 若将来要把内置也搬进来，可以按 image 的做法逐家适配，一次一家。

// SpeechRequest 一次语音合成的归一化请求。
type SpeechRequest struct {
	Text     string
	Voice    string
	Language string
	Model    string
	Format   string // 期望格式：mp3 / wav / ogg
	Speed    float64
	Pitch    float64
}

// SpeechAudio 后端产出的音频。Format 为空时由内核按 Mime 推断，仍推断不出
// 就按请求里的 format 落盘 —— 不允许"字节有了但没有扩展名"。
type SpeechAudio struct {
	Data   []byte
	Format string
	Mime   string
}

// TranscriptionRequest 一次语音识别的归一化请求。
type TranscriptionRequest struct {
	Audio    []byte
	FileName string
	Language string
	Model    string
}

// SpeechBackend 语音后端的基础能力（名字 + 可用性）。
//
// 可用性拿不到 voice.VoiceConfig 也不影响插件后端：它靠自己的 MCP 连接状态
// 判断，cfg 可能为 nil。内置 manager 路径不走这里。
type SpeechBackend interface {
	Name() string
	Available(cfg *voice.VoiceConfig) bool
}

// TTSBackend 合成能力。只做 STT 的后端不实现它。
type TTSBackend interface {
	SpeechBackend
	Synthesize(ctx context.Context, cfg *voice.VoiceConfig, req SpeechRequest) (*SpeechAudio, error)
}

// ASRBackend 识别能力。只做 TTS 的后端不实现它。
type ASRBackend interface {
	SpeechBackend
	Transcribe(ctx context.Context, cfg *voice.VoiceConfig, req TranscriptionRequest) (string, error)
}

var (
	speechBackendsMu sync.RWMutex
	// TTS / ASR 分成两张表，不存一个"两能力都想实现"的接口值。
	//
	// 原因：插件可能只提供合成、也可能只提供识别，还可能两家不同插件用同一个
	// provider 名字各提供一半。如果只存一个 SpeechBackend 再做类型断言，"声明了
	// 但没有对应工具"的适配器会把能力谎报成存在（断言只看静态类型）。分表则
	// 注册时由具体实现决定进哪张表，谎报不了。
	speechTTSBackends = make(map[string]speechBackendEntry)
	speechASRBackends = make(map[string]speechBackendEntry)
)

type speechBackendEntry struct {
	backend SpeechBackend
	owner   string
}

// RegisterSpeechBackend 注册内核内置语音后端（当前没有，留给将来）。
func RegisterSpeechBackend(b SpeechBackend) { registerSpeechBackend("", b) }

// RegisterSpeechBackendOwned 注册由某个所有者（插件）贡献的语音后端。
//
// 同一个对象实现 TTSBackend 就进合成表、实现 ASRBackend 就进识别表；两个都
// 实现则两张表都有。插件也可以对同一名字注册两个单能力对象。
func RegisterSpeechBackendOwned(owner string, b SpeechBackend) { registerSpeechBackend(owner, b) }

func registerSpeechBackend(owner string, b SpeechBackend) {
	if b == nil {
		return
	}
	name := normalizeBackendName(b.Name())
	if name == "" {
		return
	}
	entry := speechBackendEntry{backend: b, owner: owner}
	speechBackendsMu.Lock()
	defer speechBackendsMu.Unlock()
	if _, ok := b.(TTSBackend); ok {
		speechTTSBackends[name] = entry
	}
	if _, ok := b.(ASRBackend); ok {
		speechASRBackends[name] = entry
	}
}

// UnregisterSpeechBackend 按名字无条件移除（测试清理用）。
func UnregisterSpeechBackend(name string) {
	key := normalizeBackendName(name)
	speechBackendsMu.Lock()
	defer speechBackendsMu.Unlock()
	delete(speechTTSBackends, key)
	delete(speechASRBackends, key)
}

// UnregisterSpeechBackendsByOwner 移除某个所有者贡献的全部语音后端。
func UnregisterSpeechBackendsByOwner(owner string) int {
	if owner == "" {
		return 0
	}
	speechBackendsMu.Lock()
	defer speechBackendsMu.Unlock()
	removed := 0
	for name, entry := range speechTTSBackends {
		if entry.owner == owner {
			delete(speechTTSBackends, name)
			removed++
		}
	}
	for name, entry := range speechASRBackends {
		if entry.owner == owner {
			delete(speechASRBackends, name)
			removed++
		}
	}
	return removed
}

// LookupTTSBackend 按名称查找合成后端（支持 qwen-tts → cosyvoice 这类别名）。
func LookupTTSBackend(name string) (TTSBackend, bool) {
	key := normalizeBackendName(name)
	speechBackendsMu.RLock()
	entry, ok := speechTTSBackends[key]
	if !ok {
		if canonical := CanonicalProviderName(MediaKindSpeech, name); canonical != key {
			entry, ok = speechTTSBackends[canonical]
		}
	}
	speechBackendsMu.RUnlock()
	if !ok {
		return nil, false
	}
	b, ok := entry.backend.(TTSBackend)
	return b, ok
}

// LookupASRBackend 按名称查找识别后端。
func LookupASRBackend(name string) (ASRBackend, bool) {
	key := normalizeBackendName(name)
	speechBackendsMu.RLock()
	entry, ok := speechASRBackends[key]
	if !ok {
		if canonical := CanonicalProviderName(MediaKindSpeech, name); canonical != key {
			entry, ok = speechASRBackends[canonical]
		}
	}
	speechBackendsMu.RUnlock()
	if !ok {
		return nil, false
	}
	b, ok := entry.backend.(ASRBackend)
	return b, ok
}

// LookupSpeechBackend 按名称查找语音后端（先合成表，再识别表）。
// 返回的接口只用来问 Name/Available，具体能力请用 LookupTTSBackend / LookupASRBackend。
func LookupSpeechBackend(name string) (SpeechBackend, bool) {
	key := normalizeBackendName(name)
	canonical := CanonicalProviderName(MediaKindSpeech, name)
	speechBackendsMu.RLock()
	defer speechBackendsMu.RUnlock()
	for _, k := range []string{key, canonical} {
		if entry, ok := speechTTSBackends[k]; ok {
			return entry.backend, true
		}
		if entry, ok := speechASRBackends[k]; ok {
			return entry.backend, true
		}
	}
	return nil, false
}

// RegisteredSpeechProviders 返回已注册的语音后端名称（合成 + 识别，去重升序）。
func RegisteredSpeechProviders() []string {
	speechBackendsMu.RLock()
	defer speechBackendsMu.RUnlock()
	seen := make(map[string]struct{}, len(speechTTSBackends)+len(speechASRBackends))
	for name := range speechTTSBackends {
		seen[name] = struct{}{}
	}
	for name := range speechASRBackends {
		seen[name] = struct{}{}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// RegisteredTTSProviders 返回已注册且支持合成的后端名称（升序）。
func RegisteredTTSProviders() []string {
	return sortedSpeechNames(speechTTSBackends)
}

// RegisteredASRProviders 返回已注册且支持识别的后端名称（升序）。
func RegisteredASRProviders() []string {
	return sortedSpeechNames(speechASRBackends)
}

// sortedSpeechNames 要求调用方已持锁或传入内部 map 时的读锁保护 ——
// 这里统一自己加读锁，避免调用方忘记。
func sortedSpeechNames(m map[string]speechBackendEntry) []string {
	speechBackendsMu.RLock()
	defer speechBackendsMu.RUnlock()
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// SpeechCapability 报告某个名字提供哪些能力（"tts" / "asr"），用于能力查询工具。
func SpeechCapability(name string) (hasTTS, hasASR bool) {
	key := normalizeBackendName(name)
	speechBackendsMu.RLock()
	defer speechBackendsMu.RUnlock()
	_, hasTTS = speechTTSBackends[key]
	_, hasASR = speechASRBackends[key]
	return hasTTS, hasASR
}
