import { request } from './client'

export interface ProviderInfo {
  api_key: string
  models: string[]
  base_url?: string
  // Explicit vision-capability declaration persisted via PUT/POST
  // /api/providers/{name}: true/false, or absent/null for auto-detection.
  vision?: boolean | null
}

export interface PrivacyConfig {
  enabled: boolean
  redact_phone: boolean
  redact_email: boolean
  redact_id_card: boolean
  redact_bank_card: boolean
  redact_ip: boolean
  redact_address: boolean
  custom_patterns?: Record<string, string>
}

export interface BotModeConfig {
  enabled: boolean
  /** Adds a short bot-to-bot messaging protocol section to every bot's system prompt. */
  inject_bot_protocol?: boolean
  /** Cap on messages kept in each bot's canonical chat. 0 = default (200). */
  history_window?: number
  /** Cap on how long a single bot turn may run (minutes). 0 = default (5). */
  turn_timeout_minutes?: number
  /** Shared secret remote instances must present when DMing bots via /api/relay/v1/dm. */
  relay_token?: string
}

export interface ServerConfig {
  upload_url_prefix?: string
  file_strategy?: string
}

export interface Config {
  provider: string
  api_key: string
  /** Deprecated in backend; use providers[].models[0] instead. */
  model?: string
  base_url?: string
  temperature?: number
  max_tokens?: number
  gateway?: Record<string, unknown>
  working_dir?: string
  /**
   * 自动化浏览器持久 profile 目录（Chrome --user-data-dir）。
   * 缺省 = ~/.magic/browser-profile；显式空串 = 每次全新临时 profile。支持 `~` 开头。
   */
  browser_profile_dir?: string
  /**
   * 自动化浏览器是否无头运行。缺省 = true（无头，适合服务器/容器）。
   * 显式 false = 开有头窗口，需要显示服务（本机桌面，或 Xvfb/noVNC），
   * 且仅在需要人工登录（扫码/验证码）时才用得上。
   * 环境变量 BROWSER_HEADLESS 优先级高于本字段。
   */
  browser_headless?: boolean
  chat_mode?: string
  agent?: Record<string, unknown>
  memory?: Record<string, unknown>
  provider_config?: Record<string, unknown>
  providers?: Record<string, ProviderInfo>
  secret_redaction?: boolean
  cortex?: CortexConfig
  privacy?: PrivacyConfig
  bot_mode?: BotModeConfig
  server?: ServerConfig
}

export interface CortexConfig {
  enabled: boolean
  skill_min_pattern_freq?: number
}

export async function getConfig(): Promise<Config> {
  return request('/config')
}

export async function updateConfig(config: Partial<Config>): Promise<Config> {
  return request('/config', {
    method: 'PUT',
    body: JSON.stringify(config),
  })
}

export async function getConfigDefaults(): Promise<Config> {
  return request('/config/defaults')
}
