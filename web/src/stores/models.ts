import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import * as modelsApi from '@/api/models'
import { useConfigStore } from './config'

export interface Model {
  id: string
  name: string
  provider: string
  description?: string
  contextLen?: number
}

export const useModelsStore = defineStore('models', () => {
  const models = ref<Model[]>([])
  const loading = ref(false)
  const configStore = useConfigStore()

  // Current model is derived from config
  const currentModel = computed<Model | null>(() => {
    const cfg = configStore.config
    if (!cfg) return null
    const provider = cfg.provider || ''
    const modelName = cfg.model || ''
    if (!provider || !modelName) return null
    return {
      id: `${provider}/${modelName}`,
      name: modelName,
      provider: provider,
    }
  })

  async function loadModels() {
    loading.value = true
    try {
      models.value = await modelsApi.getModels()
    } finally {
      loading.value = false
    }
  }

  async function loadCurrentModel() {
    // Current model is computed from config, just reload config
    await configStore.loadConfig()
  }

  async function setModel(modelId: string) {
    await modelsApi.setModel(modelId)
    await configStore.loadConfig()
  }

  return {
    models,
    loading,
    currentModel,
    loadModels,
    loadCurrentModel,
    setModel,
  }
})
