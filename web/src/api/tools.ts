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
  tools: string[]
  enabled: boolean
}

export async function getTools(): Promise<Tool[]> {
  return request('/toolsets')
}

export async function getToolCategories(): Promise<string[]> {
  return request('/tools/categories')
}

export async function getTool(id: string): Promise<Tool> {
  return request(`/tools/${id}`)
}

export async function getToolsets(): Promise<Toolset[]> {
  return request('/tools/toolsets')
}

export async function enableToolset(id: string): Promise<void> {
  return request(`/tools/toolsets/${id}/enable`, { method: 'POST' })
}

export async function disableToolset(id: string): Promise<void> {
  return request(`/tools/toolsets/${id}/disable`, { method: 'POST' })
}
