import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import * as configApi from '@/api/config'
import * as providersApi from '@/api/providers'
import type { Config } from '@/api/config'

export interface ConfigError {
  message: string
  code?: string
}

export interface ProviderInput {
  name: string
  apiKey?: string
  baseUrl?: string
  models: string[]
}

export const useConfigStore = defineStore('config', () => {
  const config = ref<Config | null>(null)
  const defaults = ref<Config | null>(null)
  const loading = ref(false)
  const error = ref<ConfigError | null>(null)

  const currentProvider = computed(() => config.value?.provider || 'openai')
  const currentModel = computed(() => config.value?.model || '')

  async function loadConfig(): Promise<void> {
    loading.value = true
    error.value = null
    try {
      config.value = await configApi.getConfig()
    } catch (e) {
      const errMsg = e instanceof Error ? e.message : 'Unknown error'
      error.value = { message: 'Failed to load config: ' + errMsg }
      config.value = null
    } finally {
      loading.value = false
    }
  }

  async function loadDefaults(): Promise<void> {
    try {
      defaults.value = await configApi.getConfigDefaults()
    } catch (e) {
      console.error('Failed to load defaults:', e)
      defaults.value = null
    }
  }

  async function updateConfig(updates: Partial<Config>): Promise<void> {
    loading.value = true
    error.value = null
    try {
      config.value = await configApi.updateConfig(updates)
    } catch (e) {
      const errMsg = e instanceof Error ? e.message : 'Unknown error'
      error.value = { message: 'Failed to update config: ' + errMsg }
      throw e
    } finally {
      loading.value = false
    }
  }

  async function saveProvider(input: ProviderInput): Promise<void> {
    loading.value = true
    error.value = null
    try {
      const existing = await providersApi.getProvider(input.name).catch(() => null)
      if (existing) {
        await providersApi.updateProvider(input.name, {
          api_key: input.apiKey,
          base_url: input.baseUrl,
          models: input.models,
        })
      } else {
        await providersApi.createProvider({
          name: input.name,
          api_key: input.apiKey,
          base_url: input.baseUrl,
          models: input.models,
          enabled: true,
        })
      }
    } catch (e) {
      const errMsg = e instanceof Error ? e.message : 'Unknown error'
      error.value = { message: 'Failed to save provider: ' + errMsg }
      throw e
    } finally {
      loading.value = false
    }
  }

  return {
    config,
    defaults,
    loading,
    error,
    currentProvider,
    currentModel,
    loadConfig,
    loadDefaults,
    updateConfig,
    saveProvider,
  }
})
