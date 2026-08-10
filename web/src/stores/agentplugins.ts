import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import * as agentPluginsApi from '@/api/agentplugins'
import type { AgentPlugin } from '@/api/agentplugins'

export const useAgentPluginsStore = defineStore('agentplugins', () => {
  const plugins = ref<AgentPlugin[]>([])
  const loading = ref(false)
  const scanDir = ref('')

  // 安装/卸载/启停操作的进行中状态(按插件名记录),用于禁用对应按钮。
  const pending = ref<Record<string, boolean>>({})
  const installing = ref(false)

  const activePlugins = computed(() => plugins.value.filter(p => !p.rejected && p.enabled))
  const rejectedPlugins = computed(() => plugins.value.filter(p => p.rejected))
  const totalSkills = computed(() =>
    plugins.value.filter(p => p.enabled).reduce((sum, p) => sum + (p.skills?.length || 0), 0),
  )
  const totalMCPTools = computed(() =>
    plugins.value
      .filter(p => p.enabled)
      .reduce(
        (sum, p) => sum + (p.mcp_servers?.reduce((s, m) => s + (m.tools || 0), 0) || 0),
        0,
      ),
  )

  async function loadPlugins() {
    loading.value = true
    try {
      plugins.value = await agentPluginsApi.getAgentPlugins()
    } catch {
      plugins.value = []
    } finally {
      loading.value = false
    }
  }

  async function reload() {
    loading.value = true
    try {
      const res = await agentPluginsApi.reloadAgentPlugins()
      scanDir.value = res.dir || ''
      plugins.value = await agentPluginsApi.getAgentPlugins()
      return res
    } finally {
      loading.value = false
    }
  }

  async function install(file: File, name?: string) {
    installing.value = true
    try {
      const res = await agentPluginsApi.installAgentPlugin(file, name)
      // 安装后刷新列表(后端已 reload,这里同步前端状态)。
      plugins.value = await agentPluginsApi.getAgentPlugins()
      return res
    } finally {
      installing.value = false
    }
  }

  async function uninstall(name: string) {
    pending.value[name] = true
    try {
      await agentPluginsApi.uninstallAgentPlugin(name)
      plugins.value = await agentPluginsApi.getAgentPlugins()
    } finally {
      pending.value[name] = false
    }
  }

  async function setEnabled(name: string, enabled: boolean) {
    pending.value[name] = true
    try {
      if (enabled) {
        await agentPluginsApi.enableAgentPlugin(name)
      } else {
        await agentPluginsApi.disableAgentPlugin(name)
      }
      plugins.value = await agentPluginsApi.getAgentPlugins()
    } finally {
      pending.value[name] = false
    }
  }

  return {
    plugins,
    loading,
    scanDir,
    pending,
    installing,
    activePlugins,
    rejectedPlugins,
    totalSkills,
    totalMCPTools,
    loadPlugins,
    reload,
    install,
    uninstall,
    setEnabled,
  }
})
