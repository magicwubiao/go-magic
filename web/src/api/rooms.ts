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

export interface RoomAttachment {
  name: string
  url: string
  mime?: string
}

export interface RoomMessage {
  id: string
  from: string
  content: string
  timestamp: number
  attachments?: RoomAttachment[]
  _sending?: boolean
}

/**
 * Attachment payload for a room send — same shape as the bot chat payload
 * (shared parseChatPayload on the backend): images ride the vision channel,
 * everything else the files channel.
 */
export interface RoomSendPayload {
  images?: string[]
  imageUrls?: string[]
  imageNames?: string[]
  files?: { name: string; filename: string; url: string }[]
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

export interface RoomStatus {
  /** True while a coordinated round is in flight (or queued) for this room. */
  running: boolean
}

/**
 * Round-in-flight probe. A room round is serial over its members and each
 * member turn has its own multi-minute budget, so "is it still going?" cannot
 * be inferred from the blocking send call — the UI polls this and the room log
 * together to show members replying live.
 */
export async function getRoomStatus(id: string): Promise<RoomStatus> {
  return request(`/rooms/${encodeURIComponent(id)}/status`)
}

/**
 * Stop the room's in-flight round server-side: remaining members and rounds
 * are skipped and the turn that is currently speaking is canceled. Aborting
 * the send fetch alone does NOT do this — the backend never derives "stop"
 * from a dropped HTTP client.
 */
export async function stopRoomRound(id: string): Promise<{ stopped: boolean }> {
  return request(`/rooms/${encodeURIComponent(id)}/stop`, { method: 'POST' })
}

/**
 * Blocking room send. A coordinated multi-bot round (up to max_rounds) can
 * easily exceed the default 30s request timeout, so this uses a dedicated
 * fetch with its own cap and no auto-retry (retrying a live room round would
 * double-post).
 *
 * The cap is only how long THIS call waits: the round keeps running on the
 * backend, and the caller keeps polling getRoomStatus/getRoomMessages until
 * it finishes (see stores/rooms.ts). Worst case is members × rounds turn
 * timeouts, hence the generous 30 minutes.
 */
export async function sendRoomMessage(
  id: string,
  message: string,
  target?: string,
  signal?: AbortSignal,
  payload?: RoomSendPayload
): Promise<RoomSendResult> {
  const token = getAuthToken()
  const headers: Record<string, string> = { 'Content-Type': 'application/json' }
  if (token) headers['Authorization'] = `Bearer ${token}`

  const controller = new AbortController()
  const timeoutId = setTimeout(() => controller.abort(), 30 * 60 * 1000)
  const onOuterAbort = () => controller.abort()
  signal?.addEventListener('abort', onOuterAbort)

  try {
    const body = payload && Object.keys(payload).length ? { message, target, ...payload } : { message, target }
    const resp = await fetch(`${BASE_URL}/rooms/${encodeURIComponent(id)}/send`, {
      method: 'POST',
      headers,
      body: JSON.stringify(body),
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
