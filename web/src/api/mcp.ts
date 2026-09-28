import { request } from './client'

export interface MCPServer {
  name: string
  transport: string
  connected: boolean
  /** 是否存在于 config.json 的 mcp.servers（连接失败的服务器也在列表里） */
  configured: boolean
  tool_count: number
  last_health_check?: string
  command?: string
  args?: string[]
  /** 只返回 key，值一律是 *** */
  env?: Record<string, string>
  url?: string
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
  transport: string
  url?: string
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