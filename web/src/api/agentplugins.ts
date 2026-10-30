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
  /** 命令是否能在本机 PATH 上找到(目录装出来的插件才有) */
  binary_found?: boolean
  /** 目录条目 id(有值即可一键安装) */
  catalog_id?: string
  /** 当前平台的安装命令(仅展示,一键安装走后端任务) */
  install_commands?: string[]
  /** 当前平台的登录命令(仅展示) */
  login_commands?: string[]
}

/**
 * 一条"为什么还用不了":缺凭据 / 命令没装 / 清单被拒。
 * 启用前用它拦一下,把原因摆在动作之前。
 */
export interface AgentPluginBlocker {
  kind: 'auth' | 'connect' | 'missing_binary' | 'rejected' | string
  scope: 'mcp' | 'cli' | 'plugin' | string
  /** MCP 条目名或 CLI 连接器名 */
  name?: string
  /** auth: 缺的凭据 key */
  keys?: string[]
  /** missing_binary: 缺的命令 */
  command?: string
  /** missing_binary: 目录条目 id(有则可一键安装) */
  catalog_id?: string
  /** connect: 连接失败原因 */
  detail?: string
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
  /** 全部前置条件都满足(缺凭据 / 命令没装都会有 blocker) */
  ready?: boolean
  /** 非空即"启用了也用不了",前端据此拦一下并给出一键修复入口 */
  blockers?: AgentPluginBlocker[]
  /** 从内置目录装来的插件才有:目录种类与条目 id */
  catalog_kind?: string
  catalog_id?: string
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

export interface AgentPluginInstallResult {
  ok: boolean
  name: string
  dir: string
}

// 目录插件包：内置 MCP 连接器目录 / CLI 应用目录里的条目，安装后就是一个
// 真正的 Agent Plugin（可启用/禁用/卸载/授权）。
export interface CatalogPack {
  kind: 'mcp' | 'cli'
  id: string
  /** 物化后的插件名，也是目录名 */
  name: string
  display_name: string
  description: string
  category?: string
  category_label?: string
  tags?: string[]
  /** 仅 MCP 条目有值（stdio / streamable-http / sse），纯展示 */
  transport?: string
  /** 安装后需要用户填写的凭据 key */
  required_env?: string[]
  /** stdio 模板参数（仅 MCP，用于"调整参数"覆盖安装） */
  args?: string[]
  /** CLI 条目的工具名（MCP 要连上才知道） */
  tools?: string[]
  binary?: string
  binary_found?: boolean
  install_commands?: string[]
  login_commands?: string[]
  docs_url?: string
  installed: boolean
}

export interface CatalogInstallResult extends AgentPluginInstallResult {
  required_env?: string[]
}

// 返回所有已加载的 Agent Plugin 摘要。
export async function getAgentPlugins(): Promise<AgentPlugin[]> {
  return request('/agent-plugins')
}

// 目录插件包列表（kind=mcp|cli，含 installed 状态）。
export async function getCatalogPacks(kind: 'mcp' | 'cli'): Promise<CatalogPack[]> {
  return request(`/agent-plugins/catalog?kind=${kind}`)
}

// 把目录条目安装成插件。args 仅对 stdio 类 MCP 条目生效（覆盖模板参数）。
// 重复安装返回 409，由调用方提示先卸载。
export async function installCatalogPack(
  kind: 'mcp' | 'cli',
  id: string,
  args?: string[],
): Promise<CatalogInstallResult> {
  return request('/agent-plugins/catalog/install', {
    method: 'POST',
    body: JSON.stringify({ kind, id, args: args && args.length ? args : undefined }),
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

// 启用插件。未就绪(缺凭据 / 命令没装)时后端返回 409 + blockers,
// 调用方要么先补前置条件,要么显式 force=true 表示"我知道,先启用"。
export async function enableAgentPlugin(
  name: string,
  force = false,
): Promise<{ ok: boolean; name: string; disabled: boolean }> {
  const qs = force ? '?force=1' : ''
  return request(`/agent-plugins/${encodeURIComponent(name)}/enable${qs}`, { method: 'POST' })
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

// ---------------------------------------------------------------------------
// 自动授权:能直接拿到的凭据(环境变量 / 别名 / 已登录 CLI / 别的插件填过的)
// 由服务端自己填好并重连,只把真正拿不到的 key 留给用户。
// ---------------------------------------------------------------------------

export interface AutoAuthorizeResult {
  ok: boolean
  plugin: string
  /** 自动填上的 key 与来源(env:GH_TOKEN / cli:gh auth token / plugin:<name>) */
  filled: { key: string; source: string }[]
  /** 仍需用户填写的 key;空 = 授权已完成 */
  missing: string[]
  entries?: { name: string; ok: boolean; connected: boolean; error?: string; error_kind?: string }[]
}

export async function authorizePluginAuto(name: string): Promise<AutoAuthorizeResult> {
  return request(`/agent-plugins/${encodeURIComponent(name)}/credentials/auto`, { method: 'POST' })
}

// ---------------------------------------------------------------------------
// CLI 一键安装:只执行内置目录登记的安装命令(请求体只传 id / 插件名)。
// 长任务走"提交 + 轮询",避免 HTTP 超时把用户晾在转圈里。
// ---------------------------------------------------------------------------

export type CLIInstallState = 'running' | 'done' | 'failed'

export interface CLIInstallJob {
  id: string
  catalog_id: string
  plugin?: string
  state: CLIInstallState
  commands: string[]
  /** 子进程输出(可能被截断,truncated=true 时前端提示去终端手动装) */
  output: string
  binary: string
  binary_found: boolean
  error?: string
  truncated?: boolean
}

export interface CLIInstallStartResult {
  ok: boolean
  /** 同一包已有在途任务时复用,不会重复拉起包管理器 */
  reused?: boolean
  job: CLIInstallJob
}

/** 按目录条目安装(before install);或按已安装插件反查目录条目安装。 */
export async function startCLIInstall(body: {
  id?: string
  plugin?: string
}): Promise<CLIInstallStartResult> {
  return request('/agent-plugins/cli/install', {
    method: 'POST',
    body: JSON.stringify({ kind: 'cli', ...body }),
  })
}

export async function getCLIInstallJob(id: string): Promise<{ ok: boolean; job: CLIInstallJob }> {
  return request(`/agent-plugins/cli/install/${encodeURIComponent(id)}`)
}
