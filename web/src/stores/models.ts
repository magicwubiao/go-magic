import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import * as modelsApi from '@/api/models'
import type { Model, ModelInfo } from '@/api/models'

export const useModelsStore = defineStore('models', () => {
  const models = ref<Model[]>([])
  const currentModel = ref<ModelInfo | null>(null)
  const options = ref<string[]>([])
  const loading = ref(false)

  const providers = computed(() => {
    const set = new Set(models.value.map(m => m.provider))
    return Array.from(set)
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
    currentModel.value = await modelsApi.getModelInfo()
  }

  async function loadOptions() {
    options.value = await modelsApi.getModelOptions()
  }

  async function setModel(modelId: string) {
    await modelsApi.setModel(modelId)
    await loadCurrentModel()
  }

  return {
    models,
    currentModel,
    options,
    loading,
    providers,
    loadModels,
    loadCurrentModel,
    loadOptions,
    setModel,
  }
})
