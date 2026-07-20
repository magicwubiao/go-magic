import { request } from './client'

export interface MCPServer {
  name: string
  transport: string
  connected: boolean
  tool_count: number
  last_health_check?: string
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