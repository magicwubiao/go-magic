import { request } from './client'

export interface Goal {
  id: string
  title: string
  description: string
  status: 'active' | 'completed' | 'abandoned'
  progress: number
  session_ids: string[]
  created_at: number
  updated_at: number
  completed_at?: number
}

export async function getGoals(status?: string): Promise<Goal[]> {
  const url = status ? `/goals?status=${status}` : '/goals'
  return request(url)
}

export async function getGoal(id: string): Promise<Goal> {
  return request(`/goals/${id}`)
}

export async function getCurrentGoal(): Promise<Goal | null> {
  return request('/goals/current')
}

export async function createGoal(title: string, description: string): Promise<Goal> {
  return request('/goals', {
    method: 'POST',
    body: JSON.stringify({ title, description }),
  })
}

export async function updateGoal(id: string, updates: Partial<Goal>): Promise<Goal> {
  return request(`/goals/${id}`, {
    method: 'PUT',
    body: JSON.stringify(updates),
  })
}

export async function deleteGoal(id: string): Promise<void> {
  return request(`/goals/${id}`, { method: 'DELETE' })
}

export async function linkSession(goalId: string, sessionId: string): Promise<void> {
  return request(`/goals/${goalId}/link`, {
    method: 'POST',
    body: JSON.stringify({ session_id: sessionId }),
  })
}

export async function unlinkSession(goalId: string, sessionId: string): Promise<void> {
  return request(`/goals/${goalId}/unlink`, {
    method: 'POST',
    body: JSON.stringify({ session_id: sessionId }),
  })
}

export async function decomposeGoal(goalId: string): Promise<{ goal_id: string; count: number; tasks: any[] }> {
  return request(`/goals/${goalId}/decompose`, { method: 'POST' })
}

export interface GoalAnalysis {
  goal_id: string
  title: string
  current_progress: number
  suggested_progress: number
  reason: string
  completed: boolean
}

export async function analyzeGoal(goalId: string, conversation: string): Promise<GoalAnalysis> {
  return request(`/goals/${goalId}/analyze`, {
    method: 'POST',
    body: JSON.stringify({ conversation }),
  })
}