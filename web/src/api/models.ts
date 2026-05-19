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
  return request('/model/set', {
    method: 'POST',
    body: JSON.stringify({ model: modelId }),
  })
}

export async function getModelOptions(): Promise<string[]> {
  return request('/model/options')
}
