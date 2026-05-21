import { request } from './client'

export interface Config {
  provider: string
  model: string
  api_key: string
  base_url?: string
  temperature?: number
  max_tokens?: number
  gateway?: Record<string, unknown>
  working_dir?: string
  agent?: Record<string, unknown>
  memory?: Record<string, unknown>
  provider_config?: Record<string, unknown>
  providers?: unknown[]
  secret_redaction?: boolean
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
