import { request, getAuthToken } from './client'

// AgentPlugin 对应后端 agentplugin.Summary 返回的单个插件摘要。
export interface AgentPluginMCPEntry {
  name: string
  type: string
  connected: boolean
  tools: number
  error?: string
}

export interface AgentPlugin {
  name: string
  root: string
  data_dir: string
  version: string
  description: string
  rejected: boolean
  fatal_error?: string
  mcp_disabled: boolean
  enabled: boolean
  warnings: string[]
  skills: string[]
  mcp_servers: AgentPluginMCPEntry[]
}

export interface AgentPluginsReloadResult {
  ok: boolean
  count: number
  dir: string
  skills: number
}

export interface AgentPluginInstallResult {
  ok: boolean
  name: string
  dir: string
}

// 返回所有已加载的 Agent Plugin 摘要。
export async function getAgentPlugins(): Promise<AgentPlugin[]> {
  return request('/agent-plugins')
}

// 重新扫描并加载所有 Agent Plugin(后端会先停止旧 MCP 运行时)。
export async function reloadAgentPlugins(): Promise<AgentPluginsReloadResult> {
  return request('/agent-plugins/reload', { method: 'POST' })
}

// 通过上传 zip 安装插件。file 为 zip 文件,name 为插件目录名(可选)。
// 使用原生 fetch 上传 multipart(不走 request 封装,因后者会强制 JSON content-type)。
export async function installAgentPlugin(file: File, name?: string): Promise<AgentPluginInstallResult> {
  const form = new FormData()
  form.append('file', file)
  if (name) form.append('name', name)

  const headers: Record<string, string> = {}
  const token = getAuthToken()
  if (token) headers['Authorization'] = `Bearer ${token}`

  const resp = await fetch('/api/agent-plugins/install', {
    method: 'POST',
    headers,
    body: form,
  })
  if (!resp.ok) {
    const text = await resp.text().catch(() => resp.statusText)
    throw new Error(`HTTP ${resp.status}: ${text}`)
  }
  return resp.json()
}

// 卸载插件:删除插件目录并重新加载。
export async function uninstallAgentPlugin(name: string): Promise<{ ok: boolean; name: string; dir: string }> {
  return request(`/agent-plugins/${encodeURIComponent(name)}/uninstall`, { method: 'POST' })
}

// 启用插件。
export async function enableAgentPlugin(name: string): Promise<{ ok: boolean; name: string; disabled: boolean }> {
  return request(`/agent-plugins/${encodeURIComponent(name)}/enable`, { method: 'POST' })
}

// 禁用插件。
export async function disableAgentPlugin(name: string): Promise<{ ok: boolean; name: string; disabled: boolean }> {
  return request(`/agent-plugins/${encodeURIComponent(name)}/disable`, { method: 'POST' })
}
