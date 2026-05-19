import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import * as toolsApi from '@/api/tools'
import type { Tool, Toolset } from '@/api/tools'

export const useToolsStore = defineStore('tools', () => {
  const tools = ref<Tool[]>([])
  const toolsets = ref<Toolset[]>([])
  const categories = ref<string[]>([])
  const loading = ref(false)

  const enabledTools = computed(() => tools.value.filter(t => t.enabled))

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

  async function enableToolset(id: string) {
    await toolsApi.enableToolset(id)
    await loadToolsets()
  }

  async function disableToolset(id: string) {
    await toolsApi.disableToolset(id)
    await loadToolsets()
  }

  return {
    tools,
    toolsets,
    categories,
    loading,
    enabledTools,
    loadTools,
    loadToolsets,
    loadCategories,
    enableToolset,
    disableToolset,
  }
})
