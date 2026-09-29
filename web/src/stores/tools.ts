import { defineStore } from 'pinia'
import { ref } from 'vue'
import * as toolsApi from '@/api/tools'
import type { Toolset } from '@/api/tools'

export const useToolsStore = defineStore('tools', () => {
  const tools = ref<string[]>([])
  const toolsets = ref<Toolset[]>([])
  const categories = ref<string[]>([])
  const loading = ref(false)

  async function loadTools() {
    loading.value = true
    try {
      tools.value = await toolsApi.getTools()
    } finally {
      loading.value = false
    }
  }

  async function loadToolsets() {
    toolsets.value = await toolsApi.getToolsets()
  }

  async function loadCategories() {
    categories.value = await toolsApi.getToolCategories()
  }

  async function toggleToolset(id: string, enabled: boolean) {
    if (enabled) {
      await toolsApi.enableToolset(id)
    } else {
      await toolsApi.disableToolset(id)
    }
    const toolset = toolsets.value.find(t => t.id === id)
    if (toolset) toolset.enabled = enabled
  }

  return {
    tools,
    toolsets,
    categories,
    loading,
    loadTools,
    loadToolsets,
    loadCategories,
    toggleToolset,
  }
})
