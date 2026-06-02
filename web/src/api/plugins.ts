import { request } from './client'

export interface Plugin {
  id: string
  name: string
  version: string
  description: string
  author: string
  enabled: boolean
  type: string
}

export interface PluginStatistics {
  plugin_id: string
  plugin_name: string
  total_calls: number
  success_calls: number
  failed_calls: number
  success_rate: number
  avg_duration_ms: number
  last_used: string
  trend: string
}

export async function getPlugins(): Promise<Plugin[]> {
  return request('/dashboard/plugins')
}

export async function rescanPlugins(): Promise<Plugin[]> {
  return request('/dashboard/plugins/rescan', { method: 'POST' })
}

export async function getPlugin(id: string): Promise<Plugin> {
  return request(`/dashboard/plugins/${id}`)
}

export async function enablePlugin(id: string): Promise<void> {
  return request(`/dashboard/plugins/${id}/enable`, { method: 'POST' })
}

export async function disablePlugin(id: string): Promise<void> {
  return request(`/dashboard/plugins/${id}/disable`, { method: 'POST' })
}

export async function installPlugin(url: string): Promise<Plugin> {
  return request('/dashboard/agent-plugins/install', {
    method: 'POST',
    body: JSON.stringify({ url }),
  })
}

export async function deletePlugin(id: string): Promise<void> {
  return request(`/dashboard/plugins/${id}`, { method: 'DELETE' })
}

// Statistics API
export async function getPluginStatistics(): Promise<PluginStatistics[]> {
  return request('/plugins/statistics')
}
