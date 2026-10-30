import { request, getAuthToken } from './client'

// AgentPlugin 对应后端 agentplugin.Summary 返回的单个插件摘要。
export interface AgentPluginMCPEntry {
  name: string
  type: string
  connected: boolean
  tools: number
  error?: string
  // 错误分类(auth/network/protocol/…),auth 时 UI 亮"授权"按钮。
  error_kind?: string
  // mcp.json 里值为空的 env key,即需要用户填写的凭据占位。
  required_env?: string[]
}

// 插件内 cli.json 声明的 CLI 连接器(本实现的扩展组件,无连接阶段)。
export interface AgentPluginCLIEntry {
  name: string
  command: string
  /** 静态校验失败原因;非空表示该连接器不生效 */
  error?: string
  /** 已注册的工具名(ap_<plugin>_cli_<connector>_<tool>) */
  tools: string[]
  /** env 中留空的键,即需要用户填写的凭据占位 */
  required_env?: string[]
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
  /** cli.json 整个组件被禁用(文件损坏/版本不匹配) */
  cli_disabled?: boolean
  enabled: boolean
  warnings: string[]
  skills: string[]
  mcp_servers: AgentPluginMCPEntry[]
  cli_connectors?: AgentPluginCLIEntry[]
  // manifest 可选字段:图标(插件内相对路径)与示例问法。
  icon?: string
  examples?: string[]
}

// 插件凭据掩码视图:GET 返回值一律 ***(已设置)或 ''(待填)。
export interface AgentPluginCredentials {
  plugin: string
  env: Record<string, string>
  required_env: Record<string, string[]>
}

export interface AgentPluginsReloadResult {
  ok: boolean
  count: number
  dir: string
  skills: number
}

// 官方技能包(内嵌于二进制的"技能 + CLI 连接器"插件)的展示元信息。
export interface OfficialPack {
  name: string
  version?: string
  description?: string
  examples?: string[]
  skills?: string[]
  /** 非空 = 安装后需要用户在"授权"表单里填的凭据 */
  required_env?: string[]
  installed: boolean
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

// 官方技能包列表(含 installed 状态)。
export async function getOfficialPacks(): Promise<OfficialPack[]> {
  return request('/agent-plugins/official')
}

// 一键安装官方技能包。重复安装返回 409,由调用方提示先卸载。
export async function installOfficialPack(name: string): Promise<AgentPluginInstallResult> {
  return request('/agent-plugins/official/install', {
    method: 'POST',
    body: JSON.stringify({ name }),
  })
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

// 获取插件凭据掩码视图(值一律 *** 或 '',拿不到真实值)。
export async function getPluginCredentials(name: string): Promise<AgentPluginCredentials> {
  return request(`/agent-plugins/${encodeURIComponent(name)}/credentials`)
}

// 保存插件凭据并触发重连。值为 *** 表示保持原值,空串表示删除该 key。
// 返回每个 MCP 条目的重连结果(含分类错误)。
export async function setPluginCredentials(
  name: string,
  env: Record<string, string>,
): Promise<{ ok: boolean; plugin: string; entries: { name: string; ok: boolean; connected: boolean; error?: string; error_kind?: string }[] }> {
  return request(`/agent-plugins/${encodeURIComponent(name)}/credentials`, {
    method: 'POST',
    body: JSON.stringify({ env }),
  })
}

// 重连插件的单个 MCP 条目(凭据之外的"重试"入口)。
export async function connectPluginServer(name: string, server: string): Promise<{ ok: boolean; error?: string }> {
  return request(
    `/agent-plugins/${encodeURIComponent(name)}/mcp/${encodeURIComponent(server)}/connect`,
    { method: 'POST' },
  )
}
