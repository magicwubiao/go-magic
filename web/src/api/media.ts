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
}

export interface ImageGenSettings {
  provider: string
  api_key_set: boolean
  api_key_hint: string
  base_url: string
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
