import { request, getAuthToken } from './client'

export interface UploadedFile {
  id: string
  name: string
  filename: string
  url: string
  size: number
  data?: string  // base64 data URL for reliable message sending
}

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
  images?: string[]
}

export async function getSessions(limit: number = 20, offset: number = 0): Promise<{ sessions: Session[]; total: number }> {
  const res = await request<{ sessions: Session[]; total: number }>(`/sessions?limit=${limit}&offset=${offset}`)
  return { sessions: res.sessions || [], total: res.total || 0 }
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

export async function renameSession(id: string, name: string): Promise<void> {
  return request(`/sessions/${id}`, {
    method: 'PUT',
    body: JSON.stringify({ name }),
  })
}

export async function sendMessage(sessionId: string, content: string): Promise<void> {
  return request(`/sessions/${sessionId}/messages`, {
    method: 'POST',
    body: JSON.stringify({ content }),
  })
}

export async function uploadFile(file: File): Promise<UploadedFile> {
  const formData = new FormData()
  formData.append('file', file)

  const token = getAuthToken()
  const headers: Record<string, string> = {}
  if (token) {
    headers['Authorization'] = `Bearer ${token}`
  }

  // Read file as base64 for reliable message sending
  const base64Data = await fileToBase64(file)

  const response = await fetch('/api/upload', {
    method: 'POST',
    headers,
    body: formData,
  })

  if (!response.ok) {
    throw new Error(`Upload failed: ${response.statusText}`)
  }

  const result: UploadedFile = await response.json()
  result.url = addTokenToUrl(result.url)
  // Store base64 data for message sending
  result.data = base64Data
  return result
}

// Convert File to base64 data URL
async function fileToBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(reader.result as string)
    reader.onerror = reject
    reader.readAsDataURL(file)
  })
}

export interface FileItem {
  filename: string
  size: number
  url: string
  updated: string
}

export function addTokenToUrl(url: string): string {
  const token = getAuthToken()
  if (!token) return url
  const sep = url.includes('?') ? '&' : '?'
  return `${url}${sep}token=${encodeURIComponent(token)}`
}

export async function listFiles(): Promise<FileItem[]> {
  const res = await request<{ files: FileItem[] }>('/files')
  return (res.files || []).map(f => ({
    ...f,
    url: addTokenToUrl(f.url),
  }))
}

export async function deleteFile(filename: string): Promise<void> {
  return request(`/files/${filename}`, { method: 'DELETE' })
}

export function streamChat(sessionId: string, content: string, images?: string[], files?: UploadedFile[]): EventSource {
  const token = getAuthToken()
  let url = `/api/sessions/${sessionId}/stream?content=${encodeURIComponent(content)}`
  if (images && images.length) {
    url += `&images=${encodeURIComponent(JSON.stringify(images))}`
  }
  if (files && files.length) {
    // Send file info with base64 content embedded directly
    // This avoids file system access issues on the backend
    const filesData = files.map(f => ({
      name: f.name,
      filename: f.filename,
      url: f.url,
      // Include data URL if available (for immediate use)
      data: f.data || null
    }))
    url += `&files=${encodeURIComponent(JSON.stringify(filesData))}`
  }
  if (token) {
    url += `&token=${encodeURIComponent(token)}`
  }
  return new EventSource(url)
}

export interface SessionGoal {
  id: string
  title: string
  status: string
  progress: number
}

export async function getSessionGoals(sessionId: string): Promise<SessionGoal[]> {
  const res = await request<{ session_id: string; goals: SessionGoal[] }>(`/sessions/${sessionId}/goals`)
  return res.goals || []
}

export async function unlinkSessionGoal(goalId: string, sessionId: string): Promise<void> {
  return request(`/goals/${goalId}/unlink`, {
    method: 'POST',
    body: JSON.stringify({ session_id: sessionId }),
  })
}