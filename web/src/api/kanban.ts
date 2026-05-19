import { request } from './client'

export interface KanbanTask {
  id: string
  title: string
  description: string
  status: 'triage' | 'todo' | 'ready' | 'running' | 'blocked' | 'done' | 'archived'
  priority: 'low' | 'medium' | 'high' | 'critical'
  assignee?: string
  tags: string[]
  created_at: number
  updated_at: number
}

export interface KanbanBoard {
  tasks: KanbanTask[]
  columns: { key: string; title: string; count: number }[]
}

export async function getBoard(): Promise<KanbanBoard> {
  return request('/kanban/board')
}

export async function createTask(task: Partial<KanbanTask>): Promise<KanbanTask> {
  return request('/kanban/tasks', { method: 'POST', body: JSON.stringify(task) })
}

export async function updateTask(id: string, updates: Partial<KanbanTask>): Promise<KanbanTask> {
  return request(`/kanban/tasks/${id}`, { method: 'PUT', body: JSON.stringify(updates) })
}

export async function deleteTask(id: string): Promise<void> {
  return request(`/kanban/tasks/${id}`, { method: 'DELETE' })
}

export async function moveTask(id: string, status: string): Promise<KanbanTask> {
  return request(`/kanban/tasks/${id}/move`, { method: 'POST', body: JSON.stringify({ status }) })
}
