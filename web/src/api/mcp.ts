import { request } from './client'

export interface MCPServer {
  name: string
  transport: string
  connected: boolean
  /** 是否存在于 config.json 的 mcp.servers（连接失败的服务器也在列表里） */
  configured: boolean
  tool_count: number
  last_health_check?: string
  /** 最近一次连接/健康检查失败的原因（连接成功或从未尝试时为空） */
  last_error?: string
  command?: string
  args?: string[]
  /** 只返回 key，值一律是 *** */
  env?: Record<string, string>
  /** 自定义请求头，值一律是 ***（认证头放这里） */
  headers?: Record<string, string>
  url?: string
  /** connected / unauthorized / error / disconnected / configured */
  state?: string
  /** 失败分类：auth / network / server / protocol / not_found / tool */
  error_kind?: string
  /** 针对 error_kind 的一句话建议 */
  error_hint?: string
  server_name?: string
  server_version?: string
  protocol?: string
  /** 启动时是否自动连接 */
  enabled?: boolean
  /** 限定生效的分身；空 = 所有分身 */
  profiles?: string[]
  /** 被屏蔽的工具（不注册给模型） */
  disabled_tools?: string[]
  require_approval?: boolean
  /** 懒连接：启动不连接，首次调用才建连（工具清单来自上次缓存） */
  lazy?: boolean
  /** OAuth 状态视图（配置了 oauth 才有） */
  oauth?: MCPOAuthView
}

export interface MCPTool {
  name: string
  description: string
  inputSchema: Record<string, any>
}

export interface MCPConfig {
  command: string
  args: string[]
  env?: string[]
  /** 自定义请求头（Authorization 等），值为 *** 时后端还原成磁盘真值 */
  headers?: Record<string, string>
  transport: string
  url?: string
  enabled?: boolean
  profiles?: string[]
  disabled_tools?: string[]
  require_approval?: boolean
  lazy?: boolean
  /** 启用 OAuth 2.1 授权（发现/注册/PKCE/自动刷新） */
  oauth?: MCPOAuthConfig
}

/** POST /api/mcp/servers 的结果（JSON 导入可能一次加多个） */
export interface MCPImportResult {
  success: boolean
  added: string[]
  failed: { name: string; error: string }[]
  servers: MCPServer[]
  warning?: string
}

export async function getMCPServers(): Promise<MCPServer[]> {
  return request('/mcp/servers')
}

/** 连接器级 OAuth 配置（保存到 config.json） */
export interface MCPOAuthConfig {
  client_id?: string
  client_secret?: string
  scopes?: string[]
  redirect_port?: number
  resource?: string
}

/** OAuth 运行状态视图（API 返回，绝不含 token） */
export interface MCPOAuthView {
  enabled: boolean
  client_id?: string
  /** *** 掩码回显；原样回传时后端保留磁盘真值 */
  client_secret?: string
  scopes?: string[]
  has_token: boolean
  /** authorized / awaiting_authorization / failed / expired / none */
  state: string
  error?: string
}

/** oauth/start 的返回 */
export interface MCPOAuthStartResult {
  authorize_url?: string
  already_authorized?: boolean
  status: MCPOAuthView
}

/**
 * 发起 OAuth 2.1 授权。返回 authorize_url 供浏览器打开；
 * already_authorized=true 表示已有有效 token，无需再走流程。
 */
export async function startMCPOAuth(name: string): Promise<MCPOAuthStartResult> {
  return request(`/mcp/servers/${name}/oauth/start`, { method: 'POST' })
}

/** 轮询 OAuth 授权状态 */
export async function getMCPOAuthStatus(name: string): Promise<MCPOAuthView> {
  return request(`/mcp/servers/${name}/oauth/status`)
}

/** 登出：删除 token 并终止进行中的授权流程 */
export async function signOutMCPOAuth(name: string): Promise<void> {
  await request(`/mcp/servers/${name}/oauth/signout`, { method: 'POST' })
}

/** 试连结果：ok=false 时带错误分类与建议（HTTP 仍是 200） */
export interface MCPTestResult {
  name: string
  ok: boolean
  tool_count?: number
  elapsed_ms?: number
  error?: string
  error_kind?: string
  error_hint?: string
}

/** 试连一个已保存的连接器（不会改变它的启用状态） */
export async function testMCPServer(name: string): Promise<MCPTestResult> {
  return request(`/mcp/servers/${name}/test`, { method: 'POST' })
}

/** 单个工具的调用统计 */
export interface MCPToolStat {
  calls: number
  failures: number
  last_call_at?: string
  last_error?: string
}

/** 某个连接器下各工具的调用统计（进程内累计，重启归零） */
export async function getMCPToolStats(name: string): Promise<Record<string, MCPToolStat>> {
  return request(`/mcp/servers/${name}/stats`)
}

/** 单独启用/屏蔽某个工具 */
export async function setMCPToolEnabled(name: string, tool: string, enabled: boolean): Promise<void> {
  return request(`/mcp/servers/${name}/tools/${encodeURIComponent(tool)}`, {
    method: 'PUT',
    body: JSON.stringify({ enabled }),
  })
}

/** 内置连接器目录中的一项 */
export interface CatalogConnector {
  id: string
  name: string
  description: string
  category: string
  transport: string
  command?: string
  args?: string[]
  url?: string
  requires?: {
    key: string
    label: string
    /** env=子进程环境变量 / header=远程请求头 / arg=命令行参数占位符（Args 里的 {{KEY}}） */
    where: 'env' | 'header' | 'arg'
    secret: boolean
    required: boolean
    placeholder?: string
    help_url?: string
  }[]
  docs_url?: string
  tags?: string[]
  default_name: string
}

export interface CatalogCategory {
  id: string
  label: string
}

export async function getMCPCatalog(q?: string, category?: string): Promise<{
  connectors: CatalogConnector[]
  categories: CatalogCategory[]
}> {
  const params = new URLSearchParams()
  if (q) params.set('q', q)
  if (category) params.set('category', category)
  const qs = params.toString()
  return request(`/mcp/catalog${qs ? `?${qs}` : ''}`)
}

export async function getMCPServer(name: string): Promise<MCPServer> {
  return request(`/mcp/servers/${name}`)
}

export async function getMCPServerTools(name: string): Promise<MCPTool[]> {
  return request(`/mcp/servers/${name}/tools`)
}

export async function connectMCPServer(name: string, config: MCPConfig): Promise<void> {
  return request(`/mcp/servers/${name}/connect`, {
    method: 'POST',
    body: JSON.stringify(config),
  })
}

export async function disconnectMCPServer(name: string): Promise<void> {
  return request(`/mcp/servers/${name}/disconnect`, {
    method: 'POST',
  })
}

export async function healthCheckMCPServer(name?: string): Promise<{ name: string; healthy: boolean; error?: string }[]> {
  const path = name ? `/mcp/servers/${name}/health` : '/mcp/health'
  return request(path)
}

export async function reconnectMCPServer(name: string): Promise<void> {
  return request(`/mcp/servers/${name}/reconnect`, {
    method: 'POST',
  })
}

export async function refreshMCPServerTools(name: string): Promise<MCPTool[]> {
  return request(`/mcp/servers/${name}/tools/refresh`, {
    method: 'POST',
  })
}

export async function addMCPServer(name: string, config: MCPConfig): Promise<void> {
  return request(`/mcp/servers/${name}`, {
    method: 'PUT',
    body: JSON.stringify(config),
  })
}

/**
 * 用一段 JSON 添加服务器（POST 的 body 原样透传，由后端解析）。
 * 支持外部生态的写法：{"mcpServers": {...}}、{"servers": {...}}、裸的
 * name → 配置 map，以及数组形态。
 */
export async function addMCPServersFromJSON(payload: string): Promise<MCPImportResult> {
  return request('/mcp/servers', {
    method: 'POST',
    body: payload,
  })
}

/**
 * 把后端返回的错误压成一句人话。request() 抛的是
 * `HTTP 400: {"error":"..."}`，直接弹给用户太难读。
 */
export function mcpErrorMessage(e: any): string {
  const raw = String(e?.message ?? e ?? '')
  const start = raw.indexOf('{')
  if (start >= 0) {
    try {
      const parsed = JSON.parse(raw.slice(start))
      if (parsed && typeof parsed.error === 'string' && parsed.error) return parsed.error
    } catch {
      // 不是 JSON，按原样返回
    }
  }
  return raw
}

export async function updateMCPServer(name: string, config: MCPConfig): Promise<void> {
  return request(`/mcp/servers/${name}`, {
    method: 'PUT',
    body: JSON.stringify(config),
  })
}

export async function removeMCPServer(name: string): Promise<void> {
  return request(`/mcp/servers/${name}`, {
    method: 'DELETE',
  })
}

/**
 * 批量重连：后端没有批量端点，这里在前端并发扇出，单个失败不阻塞其余。
 * 返回每个服务器的结果，供界面汇总提示（成功 N 个 / 失败 M 个 + 原因）。
 */
export async function reconnectAllMCPServers(names: string[]): Promise<{ name: string; ok: boolean; error?: string }[]> {
  const results = await Promise.all(
    names.map(async (name) => {
      try {
        await reconnectMCPServer(name)
        return { name, ok: true }
      } catch (e: any) {
        return { name, ok: false, error: mcpErrorMessage(e) }
      }
    }),
  )
  return results
}

/**
 * 把一个服务器配置序列化成可直接粘贴到别处（或 config.json）的 JSON 片段。
 * env 的值是 *** 掩码 —— 这是后端接口的约定，导出时如实保留，避免误导。
 */
export function serverConfigToJSON(server: MCPServer): string {
  const entry: Record<string, any> = {}
  if (server.transport === 'sse' || server.transport === 'http') {
    entry.type = server.transport
    entry.url = server.url || ''
  } else {
    entry.command = server.command || ''
    if (server.args?.length) entry.args = server.args
    entry.type = 'stdio'
  }
  if (server.env && Object.keys(server.env).length) entry.env = server.env
  if (server.headers && Object.keys(server.headers).length) entry.headers = server.headers
  if (server.disabled_tools?.length) entry.disabled_tools = server.disabled_tools
  if (server.profiles?.length) entry.profiles = server.profiles
  if (server.require_approval) entry.require_approval = true
  if (server.lazy) entry.lazy = true
  if (server.oauth) entry.oauth = server.oauth
  if (server.enabled === false) entry.enabled = false
  return JSON.stringify({ mcpServers: { [server.name]: entry } }, null, 2)
}