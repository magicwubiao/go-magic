import { request } from './client'

export interface Tool {
  id: string
  name: string
  description: string
  category: string
  enabled: boolean
}

export interface Toolset {
  id: string
  name: string
  description: string
  // 服务端把每个 toolset 的 tools 存为 []string（工具名），见 buildToolsets。
  tools: string[]
  enabled: boolean
}

export interface ToolStatistics {
  tool_name: string
  total_calls: number
  success_calls: number
  failed_calls: number
  success_rate: number
  avg_duration_ms: number
  last_used: string
  trend: string
}

export interface ToolsetStatistics {
  toolset_name: string
  total_calls: number
  tool_stats: Record<string, number>
  last_used: string
}

// GET /api/tools 返回的是纯字符串数组（所有 toolset 的工具名摊平，
// 见 internal/server/tools.go handleTools），不是对象。
export async function getTools(): Promise<string[]> {
  return request('/tools')
}

export async function getToolsets(): Promise<Toolset[]> {
  return request('/toolsets')
}

export async function getToolCategories(): Promise<string[]> {
  return request('/tools/categories')
}

export async function enableToolset(id: string): Promise<void> {
  return request(`/tools/toolsets/${id}/enable`, { method: 'POST' })
}

export async function disableToolset(id: string): Promise<void> {
  return request(`/tools/toolsets/${id}/disable`, { method: 'POST' })
}

export async function getTool(id: string): Promise<Tool> {
  return request(`/tools/${id}`)
}

export async function updateTool(id: string, data: Partial<Tool>): Promise<Tool> {
  return request(`/tools/${id}`, {
    method: 'PUT',
    body: JSON.stringify(data),
  })
}

// Statistics APIs
export async function getToolStatistics(): Promise<ToolStatistics[]> {
  return request('/tools/statistics')
}

export async function getToolsetStatistics(): Promise<ToolsetStatistics[]> {
  return request('/toolsets/statistics')
}
