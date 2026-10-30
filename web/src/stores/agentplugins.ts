import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import * as agentPluginsApi from '@/api/agentplugins'
import type { AgentPlugin, OfficialPack } from '@/api/agentplugins'

export const useAgentPluginsStore = defineStore('agentplugins', () => {
  const plugins = ref<AgentPlugin[]>([])
  const loading = ref(false)
  const scanDir = ref('')
  // 官方技能包(内嵌"技能+连接器",一键安装)。
  const officialPacks = ref<OfficialPack[]>([])

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
      // 官方包列表一并刷新(installed 状态依赖插件目录)。
      officialPacks.value = await agentPluginsApi.getOfficialPacks().catch(() => [])
    } catch {
      plugins.value = []
    } finally {
      loading.value = false
    }
  }

  // 一键安装官方技能包:后端物化并 reload,这里同步插件与官方包两个列表。
  async function installOfficial(name: string) {
    pending.value[name] = true
    try {
      const res = await agentPluginsApi.installOfficialPack(name)
      plugins.value = await agentPluginsApi.getAgentPlugins()
      officialPacks.value = await agentPluginsApi.getOfficialPacks().catch(() => [])
      return res
    } finally {
      pending.value[name] = false
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

  // 拉取插件凭据掩码视图(授权表单数据源)。
  async function loadCredentials(name: string) {
    return agentPluginsApi.getPluginCredentials(name)
  }

  // 保存插件凭据并重连,成功后刷新插件列表(连接状态/错误分类会变)。
  async function saveCredentials(name: string, env: Record<string, string>) {
    pending.value[name] = true
    try {
      const res = await agentPluginsApi.setPluginCredentials(name, env)
      plugins.value = await agentPluginsApi.getAgentPlugins()
      return res
    } finally {
      pending.value[name] = false
    }
  }

  // 重连插件的单个 MCP 条目,完成后刷新列表。
  async function connectServer(name: string, server: string) {
    pending.value[name] = true
    try {
      const res = await agentPluginsApi.connectPluginServer(name, server)
      plugins.value = await agentPluginsApi.getAgentPlugins()
      return res
    } finally {
      pending.value[name] = false
    }
  }

  return {
    plugins,
    loading,
    scanDir,
    officialPacks,
    pending,
    installing,
    activePlugins,
    rejectedPlugins,
    totalSkills,
    totalMCPTools,
    loadPlugins,
    installOfficial,
    reload,
    install,
    uninstall,
    setEnabled,
    loadCredentials,
    saveCredentials,
    connectServer,
  }
})
