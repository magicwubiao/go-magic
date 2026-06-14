import { request } from './client'

export interface ModelInfo {
  id: string
  name: string
  description?: string
  context_len?: number
}

export interface Model {
  id: string
  name: string
  provider: string
  description?: string
  context_length?: number
  is_current?: boolean
}

// Provider model options from API
export interface ProviderModelOption {
  name: string
  slug: string
  models: string[]
  total_models: number
  is_current: boolean
}

export interface ModelOptionsResponse {
  model: string
  provider: string
  providers: ProviderModelOption[]
}

export interface SetModelResponse {
  ok: boolean
  scope: string
  provider: string
  model: string
  message?: string
}

export async function getModels(): Promise<Model[]> {
  return request('/models')
}

export async function getModelInfo(): Promise<{
  model: string
  provider: string
  model_display_name?: string
  auto_context_length: number
  capabilities: {
    supports_tools: boolean
    supports_vision: boolean
    supports_reasoning: boolean
    context_window: number
    max_output_tokens: number
    model_family: string
  }
}> {
  return request('/model/info')
}

export async function setModel(modelId: string, provider?: string): Promise<SetModelResponse> {
  // Parse "provider/model" format or use separate params
  let prov = provider
  let model = modelId

  if (!prov && modelId.includes('/')) {
    const parts = modelId.split('/')
    prov = parts[0]
    model = parts.slice(1).join('/')
  }

  return request('/model/set', {
    method: 'POST',
    body: JSON.stringify({
      provider: prov,
      model: model,
    }),
  })
}

export async function getModelOptions(): Promise<ModelOptionsResponse> {
  return request('/model/options')
}
