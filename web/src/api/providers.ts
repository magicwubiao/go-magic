import { request } from './client'

export interface Provider {
  id: string
  name: string
  type: string
  enabled: boolean
  config?: Record<string, string>
}

export async function getProviders(): Promise<Provider[]> {
  return request('/providers')
}

export async function getProvider(id: string): Promise<Provider> {
  return request(`/providers/${id}`)
}

export async function createProvider(provider: Omit<Provider, 'id'>): Promise<Provider> {
  return request('/providers', {
    method: 'POST',
    body: JSON.stringify(provider),
  })
}

export async function updateProvider(id: string, provider: Partial<Provider>): Promise<Provider> {
  return request(`/providers/${id}`, {
    method: 'PUT',
    body: JSON.stringify(provider),
  })
}

export async function deleteProvider(id: string): Promise<void> {
  return request(`/providers/${id}`, {
    method: 'DELETE',
  })
}
