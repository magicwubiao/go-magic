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
  // Parse "provider/model" format
  const parts = modelId.split('/')
  const provider = parts[0]
  const model = parts.slice(1).join('/') // Handle model names with "/" in them

  return request('/model/set', {
    method: 'POST',
    body: JSON.stringify({
      provider: provider,
      model: model,
    }),
  })
}

export async function getModelOptions(): Promise<string[]> {
  return request('/model/options')
}
