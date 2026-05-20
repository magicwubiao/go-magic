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
  tools: Tool[]
  enabled: boolean
}

export async function getTools(): Promise<Tool[]> {
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
