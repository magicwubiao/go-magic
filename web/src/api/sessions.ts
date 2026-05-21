import { request, getAuthToken } from './client'

export interface Session {
  id: string
  title: string
  source: string
  model: string
  profile?: string
  started_at: number
  last_active: number
  is_active: boolean
  message_count: number
  input_tokens: number
  output_tokens: number
  preview: string
}

export interface Message {
  id: string
  session_id: string
  role: 'user' | 'assistant' | 'system' | 'tool'
  content: string
  timestamp: string
  tool_calls?: unknown[]
  tool_name?: string
  tool_call_id?: string
}

export async function getSessions(): Promise<Session[]> {
  const res = await request<{ sessions: Session[] }>('/sessions')
  return res.sessions || []
}

export async function getSession(id: string): Promise<{ session_id: string; messages: Message[] }> {
  return request(`/sessions/${id}/messages`)
}

export async function createSession(): Promise<Session> {
  return request('/sessions', { method: 'POST' })
}

export async function deleteSession(id: string): Promise<void> {
  return request(`/sessions/${id}`, { method: 'DELETE' })
}

export async function sendMessage(sessionId: string, content: string): Promise<void> {
  return request(`/sessions/${sessionId}/messages`, {
    method: 'POST',
    body: JSON.stringify({ content }),
  })
}

export function streamChat(sessionId: string, content: string): EventSource {
  const token = getAuthToken()
  let url = `/api/sessions/${sessionId}/stream?content=${encodeURIComponent(content)}`
  if (token) {
    url += `&token=${encodeURIComponent(token)}`
  }
  return new EventSource(url)
}
