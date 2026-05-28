import { defineStore } from 'pinia'
import { ref } from 'vue'
import * as providersApi from '@/api/providers'
import type { Provider, ProviderPayload } from '@/api/providers'

export interface ProviderError {
  message: string
  code?: string
}

export const useProvidersStore = defineStore('providers', () => {
  const providers = ref<Provider[]>([])
  const loading = ref(false)
  const error = ref<ProviderError | null>(null)

  async function loadProviders(): Promise<void> {
    loading.value = true
    error.value = null
    try {
      const data = await providersApi.getProviders()
      // Normalize the data to ensure id field exists
      providers.value = data.map(p => ({
        ...p,
        id: p.id || p.name,
        name: p.name || p.id,
      }))
    } catch (e) {
      const errMsg = e instanceof Error ? e.message : 'Unknown error'
      error.value = { message: 'Failed to load providers: ' + errMsg }
      providers.value = []
    } finally {
      loading.value = false
    }
  }

  async function createProvider(provider: ProviderPayload): Promise<Provider | null> {
    try {
      error.value = null
      const newProvider = await providersApi.createProvider(provider)
      return {
        ...newProvider,
        id: newProvider.id || provider.name,
        name: newProvider.name || provider.name,
      }
    } catch (e) {
      const errMsg = e instanceof Error ? e.message : 'Unknown error'
      error.value = { message: 'Failed to create provider: ' + errMsg }
      return null
    }
  }

  async function updateProvider(id: string, updates: ProviderPayload): Promise<Provider | null> {
    try {
      error.value = null
      const updated = await providersApi.updateProvider(id, updates)
      const idx = providers.value.findIndex(p => p.id === id)
      if (idx >= 0) {
        providers.value[idx] = {
          ...updated,
          id: updated.id || id,
          name: updated.name || id,
        }
      }
      return updated
    } catch (e) {
      const errMsg = e instanceof Error ? e.message : 'Unknown error'
      error.value = { message: 'Failed to update provider: ' + errMsg }
      return null
    }
  }

  async function deleteProvider(id: string): Promise<boolean> {
    try {
      error.value = null
      await providersApi.deleteProvider(id)
      providers.value = providers.value.filter(p => p.id !== id)
      return true
    } catch (e) {
      const errMsg = e instanceof Error ? e.message : 'Unknown error'
      error.value = { message: 'Failed to delete provider: ' + errMsg }
      return false
    }
  }

  return {
    providers,
    loading,
    error,
    loadProviders,
    createProvider,
    updateProvider,
    deleteProvider,
  }
})
