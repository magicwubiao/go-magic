import { defineStore } from 'pinia'
import { ref } from 'vue'
import * as pluginsApi from '@/api/plugins'
import type { Plugin } from '@/api/plugins'

export const usePluginsStore = defineStore('plugins', () => {
  const plugins = ref<Plugin[]>([])
  const loading = ref(false)

  async function loadPlugins() {
    loading.value = true
    try {
      plugins.value = await pluginsApi.getPlugins()
    } catch {
      plugins.value = []
    } finally {
      loading.value = false
    }
  }

  async function rescan() {
    loading.value = true
    try {
      plugins.value = await pluginsApi.rescanPlugins()
    } finally {
      loading.value = false
    }
  }

  async function enablePlugin(id: string) {
    await pluginsApi.enablePlugin(id)
    const p = plugins.value.find(p => p.id === id)
    if (p) p.enabled = true
  }

  async function disablePlugin(id: string) {
    await pluginsApi.disablePlugin(id)
    const p = plugins.value.find(p => p.id === id)
    if (p) p.enabled = false
  }

  async function installPlugin(url: string) {
    const newPlugin = await pluginsApi.installPlugin(url)
    plugins.value.push(newPlugin)
    return newPlugin
  }

  async function deletePlugin(id: string) {
    await pluginsApi.deletePlugin(id)
    plugins.value = plugins.value.filter(p => p.id !== id)
  }

  return { plugins, loading, loadPlugins, rescan, enablePlugin, disablePlugin, installPlugin, deletePlugin }
})
