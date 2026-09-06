import { request } from './client'

export interface Provider {
  id: string
  name: string
  enabled: boolean
  api_key?: string
  base_url?: string
  model?: string
  models?: string[]  // Supported models list
  config?: Record<string, string>
  isBuiltin?: boolean
  // Explicit vision-capability declaration: true/false overrides name-based
  // auto-detection, null/undefined = auto.
  vision?: boolean | null
}

export async function getProviders(): Promise<Provider[]> {
  return request('/providers')
}

export async function getProvider(id: string): Promise<Provider> {
  return request(`/providers/${id}`)
}

export async function createProvider(provider: Omit<Provider, 'id'>): Promise<Provider> {
  // Use name as the provider id
  const id = provider.name || 'custom'
  return request(`/providers/${id}`, {
    method: 'POST',
    body: JSON.stringify({
      name: provider.name,
      base_url: provider.base_url,
      model: provider.model,
      api_key: provider.api_key,
      models: provider.models,
      vision: provider.vision ?? null,
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
      models: provider.models,
      // Always ship the key: null clears the declaration (back to auto
      // detection), true/false sets it. Omitted key would keep the old value.
      vision: provider.vision === undefined ? null : provider.vision,
    }),
  })
}

export async function deleteProvider(id: string): Promise<void> {
  return request(`/providers/${id}`, {
    method: 'DELETE',
  })
}

export interface ProviderTestResult {
  ok: boolean
  model?: string
  latencyMs?: number
  replyChars?: number
  error?: string
}

// Verify provider connectivity with a real lightweight chat round-trip.
// Overrides let the UI test unsaved form values (api_key/base_url/model)
// before anything is persisted. Retries are disabled: a failing endpoint
// should surface its error immediately, not after 3 blind retries.
export async function testProvider(
  id: string,
  overrides?: { api_key?: string; base_url?: string; model?: string }
): Promise<ProviderTestResult> {
  return request(`/providers/${id}/test`, {
    method: 'POST',
    retries: 0,
    body: JSON.stringify(overrides ?? {}),
  })
}
