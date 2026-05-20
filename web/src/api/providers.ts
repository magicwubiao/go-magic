import { request } from './client'

export interface Provider {
  id: string
  name: string
  type: string
  enabled: boolean
  api_key?: string
  base_url?: string
  model?: string
  config?: Record<string, string>
  isBuiltin?: boolean
}

export async function getProviders(): Promise<Provider[]> {
  return request('/providers')
}

export async function getProvider(id: string): Promise<Provider> {
  return request(`/providers/${id}`)
}

export async function createProvider(provider: Omit<Provider, 'id'>): Promise<Provider> {
  // Use name as the provider id
  const id = provider.name || provider.type || 'custom'
  return request(`/providers/${id}`, {
    method: 'POST',
    body: JSON.stringify({
      base_url: provider.base_url,
      model: provider.model,
      api_key: provider.api_key,
    }),
  })
}

export async function updateProvider(id: string, provider: Partial<Provider>): Promise<Provider> {
  return request(`/providers/${id}`, {
    method: 'PUT',
    body: JSON.stringify({
      base_url: provider.base_url,
      model: provider.model,
      api_key: provider.api_key,
    }),
  })
}

export async function deleteProvider(id: string): Promise<void> {
  return request(`/providers/${id}`, {
    method: 'DELETE',
  })
}
