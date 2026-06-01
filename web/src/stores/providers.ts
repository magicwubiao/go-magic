import { defineStore } from 'pinia'
import { ref } from 'vue'
import * as providersApi from '@/api/providers'
import type { Provider } from '@/api/providers'
import { useModelsStore } from './models'

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
      providers.value = await providersApi.getProviders()
    } catch (e) {
      const errMsg = e instanceof Error ? e.message : 'Unknown error'
      error.value = { message: 'Failed to load providers: ' + errMsg }
      providers.value = []
    } finally {
      loading.value = false
    }
  }

  async function createProvider(provider: Omit<Provider, 'id'>): Promise<Provider | null> {
    try {
      error.value = null
      const newProvider = await providersApi.createProvider(provider)
      providers.value.push(newProvider)
      return newProvider
    } catch (e) {
      const errMsg = e instanceof Error ? e.message : 'Unknown error'
      error.value = { message: 'Failed to create provider: ' + errMsg }
      return null
    }
  }

  async function updateProvider(id: string, updates: Partial<Provider>): Promise<Provider | null> {
    try {
      error.value = null
      const updated = await providersApi.updateProvider(id, updates)
      const idx = providers.value.findIndex(p => p.id === id)
      if (idx >= 0) {
        providers.value[idx] = updated
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
      // Also refresh models since they are derived from providers
      const modelsStore = useModelsStore()
      await modelsStore.loadModels()
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
