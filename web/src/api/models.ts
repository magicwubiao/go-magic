import { request } from './client'

export interface Model {
  id: string
  name: string
  provider: string
  description?: string
  context_length?: number
}

export interface ModelInfo {
  id: string
  name: string
  provider: string
  current: boolean
}

export async function getModels(): Promise<Model[]> {
  return request('/models')
}

export async function getModelInfo(): Promise<ModelInfo> {
  return request('/model/info')
}

export async function setModel(modelId: string): Promise<void> {
  // modelId format: "provider/model" (e.g., "openai/gpt-4")
  const [provider, model] = modelId.split('/')
  return request('/model/set', {
    method: 'POST',
    body: JSON.stringify({ 
      provider: provider,
      model: model || modelId, // fallback to full string if no slash
    }),
  })
}

export async function getModelOptions(): Promise<string[]> {
  return request('/model/options')
}
