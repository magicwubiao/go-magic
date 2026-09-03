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
  due_date?: string | null
  estimated_hours?: number
  goal_id?: string
  parent_count?: number
  child_count?: number
  comment_count?: number
}

export interface KanbanComment {
  id: string
  task_id: string
  author: string
  body: string
  created_at: number
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

// --- Advanced APIs ---

export async function getTaskComments(taskId: string): Promise<KanbanComment[]> {
  return request(`/kanban/tasks/${taskId}/comments`)
}

export async function addTaskComment(taskId: string, author: string, body: string): Promise<KanbanComment> {
  return request(`/kanban/tasks/${taskId}/comments`, {
    method: 'POST',
    body: JSON.stringify({ author, body }),
  })
}

export async function getTaskChildren(taskId: string): Promise<KanbanTask[]> {
  return request(`/kanban/tasks/${taskId}/children`)
}

export async function triageTask(taskId: string): Promise<KanbanTask> {
  return request(`/kanban/tasks/${taskId}/triage`, { method: 'POST' })
}

export async function blockTask(taskId: string, reason: string): Promise<KanbanTask> {
  return request(`/kanban/tasks/${taskId}/block`, {
    method: 'POST',
    body: JSON.stringify({ reason }),
  })
}

export async function splitTask(taskId: string): Promise<KanbanTask[]> {
  return request(`/kanban/tasks/${taskId}/split`, { method: 'POST' })
}
