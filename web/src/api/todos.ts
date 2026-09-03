import { request } from './client'

export interface TodoItem {
  id: string
  title: string
  description?: string
  status: 'pending' | 'in_progress' | 'completed' | 'cancelled'
  priority: 'high' | 'medium' | 'low'
  session_id?: string
  created_at: number
  updated_at: number
  completed_at?: number
  due_at?: number
}

export interface TodoListResponse {
  total: number
  pending_count: number
  in_progress_count: number
  completed_count: number
  todos: TodoItem[]
  filter_session_id?: string
}

export async function listTodos(params?: {
  filter_status?: string
  filter_priority?: string
  sort?: string
  session_id?: string
}): Promise<TodoListResponse> {
  const qs = new URLSearchParams()
  if (params) {
    Object.entries(params).forEach(([k, v]) => {
      if (v) qs.set(k, v as string)
    })
  }
  const q = qs.toString()
  return request(`/todos${q ? `?${q}` : ''}`)
}

export async function getTodo(id: string, opts?: { session_id?: string }): Promise<TodoItem> {
  const qs = new URLSearchParams()
  if (opts?.session_id) qs.set('session_id', opts.session_id)
  const q = qs.toString()
  return request(`/todos/${id}${q ? `?${q}` : ''}`)
}

export async function createTodo(data: {
  title: string
  description?: string
  priority?: string
  session_id?: string
}): Promise<any> {
  return request('/todos', {
    method: 'POST',
    body: JSON.stringify(data),
  })
}

export async function updateTodo(id: string, updates: Partial<TodoItem> & { session_id?: string }): Promise<any> {
  const qs = new URLSearchParams()
  if (updates?.session_id) qs.set('session_id', updates.session_id)
  const q = qs.toString()
  return request(`/todos/${id}${q ? `?${q}` : ''}`, {
    method: 'PUT',
    body: JSON.stringify(updates),
  })
}

export async function deleteTodo(id: string, opts?: { session_id?: string }): Promise<void> {
  const qs = new URLSearchParams()
  if (opts?.session_id) qs.set('session_id', opts.session_id)
  const q = qs.toString()
  return request(`/todos/${id}${q ? `?${q}` : ''}`, { method: 'DELETE' })
}