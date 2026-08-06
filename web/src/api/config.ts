import { request } from './client'

export interface ProviderInfo {
  api_key: string
  models: string[]
  base_url?: string
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

export interface Config {
  provider: string
  api_key: string
  base_url?: string
  temperature?: number
  max_tokens?: number
  gateway?: Record<string, unknown>
  working_dir?: string
  chat_mode?: string
  auto_link_goals?: boolean
  agent?: Record<string, unknown>
  memory?: Record<string, unknown>
  provider_config?: Record<string, unknown>
  providers?: Record<string, ProviderInfo>
  secret_redaction?: boolean
  cortex?: CortexConfig
  privacy?: PrivacyConfig
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
