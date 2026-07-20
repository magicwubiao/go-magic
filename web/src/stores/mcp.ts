import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import * as mcpApi from '@/api/mcp'
import type { MCPServer, MCPTool, MCPConfig } from '@/api/mcp'

export const useMCPStore = defineStore('mcp', () => {
  const servers = ref<MCPServer[]>([])
  const serverTools = ref<Record<string, MCPTool[]>>({})
  const loading = ref(false)
  const healthStatus = ref<Record<string, boolean>>({})

  const connectedServers = computed(() => servers.value.filter(s => s.connected))
  const disconnectedServers = computed(() => servers.value.filter(s => !s.connected))

  async function loadServers() {
    loading.value = true
    try {
      servers.value = await mcpApi.getMCPServers()
    } finally {
      loading.value = false
    }
  }

  async function loadServerTools(name: string) {
    try {
      const tools = await mcpApi.getMCPServerTools(name)
      serverTools.value[name] = tools
      return tools
    } catch {
      serverTools.value[name] = []
      return []
    }
  }

  async function connectServer(name: string, config: MCPConfig) {
    await mcpApi.connectMCPServer(name, config)
    await loadServers()
  }

  async function disconnectServer(name: string) {
    await mcpApi.disconnectMCPServer(name)
    await loadServers()
  }

  async function healthCheck(name?: string) {
    const results = await mcpApi.healthCheckMCPServer(name)
    results.forEach(r => {
      healthStatus.value[r.name] = r.healthy
    })
    return results
  }

  async function reconnectServer(name: string) {
    await mcpApi.reconnectMCPServer(name)
    await loadServers()
  }

  async function refreshTools(name: string) {
    const tools = await mcpApi.refreshMCPServerTools(name)
    serverTools.value[name] = tools
    return tools
  }

  async function addServer(name: string, config: MCPConfig) {
    await mcpApi.addMCPServer(name, config)
    await loadServers()
  }

  async function updateServer(name: string, config: MCPConfig) {
    await mcpApi.updateMCPServer(name, config)
    await loadServers()
  }

  async function removeServer(name: string) {
    await mcpApi.removeMCPServer(name)
    delete serverTools.value[name]
    delete healthStatus.value[name]
    await loadServers()
  }

  function getTools(name: string): MCPTool[] {
    return serverTools.value[name] || []
  }

  function getHealthStatus(name: string): boolean | undefined {
    return healthStatus.value[name]
  }

  return {
    servers,
    serverTools,
    loading,
    healthStatus,
    connectedServers,
    disconnectedServers,
    loadServers,
    loadServerTools,
    connectServer,
    disconnectServer,
    healthCheck,
    reconnectServer,
    refreshTools,
    addServer,
    updateServer,
    removeServer,
    getTools,
    getHealthStatus,
  }
})