import { request, getAuthToken } from './client'

const BASE_URL = '/api'

export interface Room {
  id: string
  name: string
  topic: string
  members: string[]
  max_rounds: number
  max_messages: number
  created_at: number
  updated_at: number
}

export interface RoomMessage {
  id: string
  from: string
  content: string
  timestamp: number
  _sending?: boolean
}

export interface RoomSendResult {
  room_id: string
  needs_user: boolean
  messages: RoomMessage[]
}

export async function getRooms(): Promise<Room[]> {
  return request('/rooms')
}

export async function createRoom(data: Partial<Room>): Promise<Room> {
  return request('/rooms', { method: 'POST', body: JSON.stringify(data) })
}

export async function getRoom(id: string): Promise<Room> {
  return request(`/rooms/${encodeURIComponent(id)}`)
}

export async function updateRoom(id: string, data: Partial<Room>): Promise<Room> {
  return request(`/rooms/${encodeURIComponent(id)}`, { method: 'PUT', body: JSON.stringify(data) })
}

export async function deleteRoom(id: string): Promise<{ deleted: string }> {
  return request(`/rooms/${encodeURIComponent(id)}`, { method: 'DELETE' })
}

export async function getRoomMessages(id: string): Promise<RoomMessage[]> {
  return request(`/rooms/${encodeURIComponent(id)}/messages`)
}

/**
 * Blocking room send. A coordinated multi-bot round (up to max_rounds) can
 * easily exceed the default 30s request timeout, so this uses a dedicated
 * fetch with a 5-minute cap and no auto-retry (retrying a live room round
 * would double-post).
 */
export async function sendRoomMessage(
  id: string,
  message: string,
  target?: string,
  signal?: AbortSignal
): Promise<RoomSendResult> {
  const token = getAuthToken()
  const headers: Record<string, string> = { 'Content-Type': 'application/json' }
  if (token) headers['Authorization'] = `Bearer ${token}`

  const controller = new AbortController()
  const timeoutId = setTimeout(() => controller.abort(), 5 * 60 * 1000)
  const onOuterAbort = () => controller.abort()
  signal?.addEventListener('abort', onOuterAbort)

  try {
    const resp = await fetch(`${BASE_URL}/rooms/${encodeURIComponent(id)}/send`, {
      method: 'POST',
      headers,
      body: JSON.stringify({ message, target }),
      signal: controller.signal,
    })
    if (!resp.ok) {
      const text = await resp.text().catch(() => resp.statusText)
      throw new Error(`HTTP ${resp.status}: ${text}`)
    }
    return (await resp.json()) as RoomSendResult
  } finally {
    clearTimeout(timeoutId)
    signal?.removeEventListener('abort', onOuterAbort)
  }
}
