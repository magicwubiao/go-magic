import { request } from './client'

export interface Provider {
  id: string
  name: string
  enabled: boolean
  api_key?: string
  base_url?: string
  model?: string
  config?: Record<string, string>
  isBuiltin?: boolean
}

export interface ProviderPayload {
  name: string
  api_key?: string
  base_url?: string
  model?: string
}

export async function getProviders(): Promise<Provider[]> {
  return request('/providers')
}

export async function getProvider(id: string): Promise<Provider> {
  return request(`/providers/${id}`)
}

export async function createProvider(provider: ProviderPayload): Promise<Provider> {
  // Use name as the provider id
  const id = provider.name
  return request(`/providers/${id}`, {
    method: 'POST',
    body: JSON.stringify({
      base_url: provider.base_url,
      model: provider.model,
      api_key: provider.api_key,
    }),
  })
}

export async function updateProvider(id: string, provider: ProviderPayload): Promise<Provider> {
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
