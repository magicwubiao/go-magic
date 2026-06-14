import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import * as modelsApi from '@/api/models'
import type { ModelOptionsResponse } from '@/api/models'
import { useConfigStore } from './config'

export interface Model {
  id: string
  name: string
  provider: string
  description?: string
  contextLen?: number
  isCurrent?: boolean
}

export interface ProviderOption {
  name: string
  slug: string
  models: string[]
  totalModels: number
  isCurrent: boolean
}

export const useModelsStore = defineStore('models', () => {
  const models = ref<Model[]>([])
  const loading = ref(false)
  const configStore = useConfigStore()

  // Provider options with multiple models
  const providerOptions = ref<ProviderOption[]>([])
  const currentProvider = ref<string>('')
  const currentModel = ref<string>('')

  // Current model is derived from config
  const currentModelInfo = computed<Model | null>(() => {
    if (!currentProvider.value || !currentModel.value) return null
    return {
      id: `${currentProvider.value}/${currentModel.value}`,
      name: currentModel.value,
      provider: currentProvider.value,
      isCurrent: true,
    }
  })

  // Model options for select dropdown
  const modelSelectOptions = computed(() => {
    return models.value.map(m => ({
      label: `${m.provider} / ${m.name}${m.description ? ` (${m.description})` : ''}`,
      value: m.id,
    }))
  })

  async function loadModels() {
    loading.value = true
    try {
      // Load from model options API which includes all provider models
      const options = await modelsApi.getModelOptions()
      currentProvider.value = options.provider
      currentModel.value = options.model

      // Convert provider options
      providerOptions.value = options.providers.map(p => ({
        name: p.name,
        slug: p.slug,
        models: p.models,
        totalModels: p.total_models,
        isCurrent: p.is_current,
      }))

      // Flatten to models list
      models.value = []
      for (const p of options.providers) {
        for (const m of p.models) {
          models.value.push({
            id: `${p.name}/${m}`,
            name: m,
            provider: p.name,
            isCurrent: p.is_current && m === options.model,
          })
        }
      }
    } finally {
      loading.value = false
    }
  }

  async function loadCurrentModel() {
    // Reload from config - get current model from provider's models[0]
    await configStore.loadConfig()
    const cfg = configStore.config
    if (cfg) {
      currentProvider.value = cfg.provider || ''
      // Get current model from first element of provider's models array
      const prov = cfg.providers?.[cfg.provider]
      currentModel.value = prov?.models?.[0] || ''
    }
  }

  async function setModel(modelId: string, providerId?: string) {
    const result = await modelsApi.setModel(modelId, providerId)
    // Reload to get updated state
    await loadModels()
    // Update current values from response
    if (result.provider && result.model) {
      currentProvider.value = result.provider
      currentModel.value = result.model
    }
  }

  return {
    models,
    loading,
    currentModelInfo,
    modelSelectOptions,
    providerOptions,
    currentProvider,
    currentModel,
    loadModels,
    loadCurrentModel,
    setModel,
  }
})
