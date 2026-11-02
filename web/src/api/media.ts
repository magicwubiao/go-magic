import { request } from './client'

// media.ts — 媒体能力（生图 / 生视频 / 语音）设置的接口层。
//
// 与后端 /api/media/config 一一对应：
//  - GET 只回密钥掩码（api_key_set / api_key_hint），明文永不出后端；
//  - PUT 里 api_key **省略 = 保持原值**，传空串才是清除。所以 patch 的字段
//    必须是"可选"，不能默认写 ""——那会把用户已配好的 key 洗掉。

export interface MediaProviderOption {
  name: string
  available: boolean
  builtin: boolean
  /** 仅图片：该 provider 是否支持编辑（决定 image_edit 是否可用） */
  editable?: boolean
  /** 仅语音：能力标记 */
  supports_tts?: boolean
  supports_asr?: boolean

  // ---- 以下是后端 catalog 下发的静态元信息（插件贡献的后端可能没有） ----
  /** 展示名，如 "可灵 Kling"、"MiniMax 海螺 H3"；缺省时前端回落成 name */
  label?: string
  default_model?: string
  /** 推荐模型列表，用于把模型输入框换成可搜索下拉 */
  models?: string[]
  default_base_url?: string
  /** 去哪拿密钥：设置页直接渲染成"获取密钥"链接 */
  console_url?: string
  docs_url?: string
  /** 密钥形态提示，如 "sk-... （百炼 API-KEY）" */
  key_hint?: string
  /** t2i / edit / t2v / i2v / tts / asr */
  capabilities?: string[]
  /** 该厂商实际接受的画幅与分辨率：设置页据此裁剪下拉，避免选到必然被拒的值 */
  ratios?: string[]
  resolutions?: string[]
  min_duration?: number
  max_duration?: number
  default_voice?: string
  notes?: string
}

export interface ImageGenSettings {
  provider: string
  api_key_set: boolean
  api_key_hint: string
  base_url: string
  model: string
  default_size: string
  default_style: string
  output_dir: string
  providers: MediaProviderOption[]
  available: boolean
  env_overrides: string[]
}

export interface VideoGenSettings {
  provider: string
  api_key_set: boolean
  api_key_hint: string
  base_url: string
  model: string
  default_duration: number
  default_ratio: string
  default_resolution: string
  output_dir: string
  providers: MediaProviderOption[]
  available: boolean
  env_overrides: string[]
}

export interface VoiceSettings {
  provider: string
  model: string
  voice: string
  language: string
  api_key_set: boolean
  api_key_hint: string
  api_url: string
  region: string
  group_id: string
  speed: number
  pitch: number
  providers: MediaProviderOption[]
  available: boolean
  env_overrides: string[]
}

export interface MediaConfig {
  config_path: string
  image_gen: ImageGenSettings
  video_gen: VideoGenSettings
  voice: VoiceSettings
}

export interface ImageGenPatch {
  api_key?: string
  provider?: string
  base_url?: string
  model?: string
  default_size?: string
  default_style?: string
  output_dir?: string
}

export interface VideoGenPatch {
  api_key?: string
  provider?: string
  base_url?: string
  model?: string
  default_duration?: number
  default_ratio?: string
  default_resolution?: string
  output_dir?: string
}

export interface VoicePatch {
  api_key?: string
  provider?: string
  model?: string
  voice?: string
  language?: string
  api_url?: string
  region?: string
  group_id?: string
  speed?: number
  pitch?: number
}

export interface MediaConfigPatch {
  image_gen?: ImageGenPatch
  video_gen?: VideoGenPatch
  voice?: VoicePatch
}

export async function getMediaConfig(): Promise<MediaConfig> {
  return request('/media/config', { timeoutMs: 30000 })
}

/** 保存后后端会立即把配置注入运行中的工具，返回的是新的完整快照。 */
export async function updateMediaConfig(patch: MediaConfigPatch): Promise<MediaConfig> {
  return request('/media/config', {
    method: 'PUT',
    body: JSON.stringify(patch),
    // 写盘 + 重建工具配置，比普通读接口慢，但远不到 30s；给足余量避免
    // 网络抖动被当成失败（用户会以为没保存）。
    timeoutMs: 60000,
  })
}

/**
 * 清除某一段（或全部）媒体配置：后端把整段换成零值（密钥、模型、地址、
 * 分厂商凭据一起清），落盘并热应用，返回新的完整快照。
 */
export async function resetMediaConfig(
  section: 'image' | 'video' | 'voice' | 'all',
): Promise<MediaConfig> {
  return request(`/media/config?section=${section}`, {
    method: 'DELETE',
    timeoutMs: 60000,
  })
}

// ---- 试运行 ----
export interface MediaTestResult {
  status: 'success' | 'not_configured' | string
  /** 生成的样张/音频绝对路径；前端用它换 read 票据做内嵌预览 */
  file_path?: string
  mime?: string
  provider?: string
  /** 仅语音：合成时长（秒） */
  duration?: number
  format?: string
  message?: string
  configuration_help?: string
}

/**
 * testMediaConfig 用已保存的配置真实调一次图片生成 / 语音合成。
 *
 * 视频刻意不支持：一次要几分钟还烧钱，不适合塞进设置页的交互里。
 * 后端最长 3 分钟返回，前端超时给足余量。
 */
export async function testMediaConfig(section: 'image' | 'voice'): Promise<MediaTestResult> {
  return request('/media/test', {
    method: 'POST',
    body: JSON.stringify({ section }),
    timeoutMs: 200000,
    // 试运行是**计费**请求：客户端层严禁自动重试（5xx 时默认会重试 3 次，
    // 意味着一次点击最多烧 4 次生成费）。失败就如实展示，由用户决定再试。
    retries: 0,
  })
}
