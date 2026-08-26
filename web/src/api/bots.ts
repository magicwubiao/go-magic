import { request, getAuthToken } from './client'

const BASE_URL = '/api'

export interface BotRuntime {
  online: boolean
  session_id?: string
  queue_depth?: number
  history_length?: number
  active_routines?: number
}

export interface Bot {
  name: string
  mention_tag: string
  title: string
  description: string
  system_prompt: string
  model: string
  provider: string
  created_at: number
  updated_at: number
  runtime?: BotRuntime
}

export interface BotRoutine {
  id: string
  name: string
  schedule: string
  prompt: string
  enabled: boolean
  last_run?: number
  last_status: string
  created_at: number
}

export interface BotMessage {
  id: string
  role: 'user' | 'assistant'
  from?: string
  content: string
  timestamp: number
  _streaming?: boolean
}

export async function getBots(): Promise<Bot[]> {
  return request('/bots')
}

export async function createBot(bot: Partial<Bot>): Promise<Bot> {
  return request('/bots', { method: 'POST', body: JSON.stringify(bot) })
}

export async function getBot(name: string): Promise<Bot> {
  return request(`/bots/${name}`)
}

export async function updateBot(name: string, updates: Partial<Bot>): Promise<Bot> {
  return request(`/bots/${name}`, { method: 'PUT', body: JSON.stringify(updates) })
}

export async function deleteBot(name: string): Promise<void> {
  return request(`/bots/${name}`, { method: 'DELETE' })
}

export async function getBotMessages(name: string): Promise<BotMessage[]> {
  return request(`/bots/${name}/messages`)
}

export async function sendBotChat(name: string, message: string): Promise<BotMessage> {
  return request(`/bots/${name}/chat`, {
    method: 'POST',
    body: JSON.stringify({ message }),
  })
}

export interface BotChatStreamEvents {
  onDelta?: (text: string) => void
  signal?: AbortSignal
}

/**
 * Streaming variant of sendBotChat (SSE). Resolves with the full assistant
 * reply once the stream finishes. Falls back to the non-streaming endpoint
 * when EventSource-style streaming is unavailable (non-2xx before body).
 */
export async function sendBotChatStream(
  name: string,
  message: string,
  events: BotChatStreamEvents = {}
): Promise<BotMessage> {
  const token = getAuthToken()
  const headers: Record<string, string> = { 'Content-Type': 'application/json' }
  if (token) headers['Authorization'] = `Bearer ${token}`

  const resp = await fetch(`${BASE_URL}/bots/${encodeURIComponent(name)}/chat/stream`, {
    method: 'POST',
    headers,
    body: JSON.stringify({ message }),
    signal: events.signal,
  })
  if (!resp.ok || !resp.body) {
    // Fallback to synchronous endpoint on any pre-stream failure.
    return sendBotChat(name, message)
  }

  const reader = resp.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  let finalText = ''

  const handleEvent = (raw: string) => {
    const line = raw.replace(/^data:\s*/, '').trim()
    if (!line) return
    try {
      const evt = JSON.parse(line)
      if (typeof evt.delta === 'string' && evt.delta) {
        events.onDelta?.(evt.delta)
        finalText += evt.delta
      } else if (typeof evt.final === 'string' && evt.final) {
        // Authoritative full reply from server.
        finalText = evt.final
      } else if (typeof evt.error === 'string' && evt.error) {
        throw new Error(evt.error)
      }
    } catch {
      /* ignore malformed keep-alive chunks */
    }
  }

  for (;;) {
    const { done, value } = await reader.read()
    if (done) break
    buffer += decoder.decode(value, { stream: true })
    let idx: number
    while ((idx = buffer.indexOf('\n\n')) >= 0) {
      const chunk = buffer.slice(0, idx)
      buffer = buffer.slice(idx + 2)
      for (const part of chunk.split('\n')) handleEvent(part)
    }
  }
  if (buffer.trim()) handleEvent(buffer)

  return {
    id: 'stream_' + Date.now(),
    role: 'assistant',
    content: finalText,
    timestamp: Date.now(),
  }
}

export async function getBotRoutines(name: string): Promise<BotRoutine[]> {
  return request(`/bots/${name}/routines`)
}

export async function createBotRoutine(
  name: string,
  routine: { name: string; schedule: string; prompt: string }
): Promise<BotRoutine> {
  return request(`/bots/${name}/routines`, {
    method: 'POST',
    body: JSON.stringify(routine),
  })
}

export async function deleteBotRoutine(name: string, routineId: string): Promise<void> {
  return request(`/bots/${name}/routines/${routineId}`, { method: 'DELETE' })
}

export interface BotRoutineUpdates {
  name?: string
  schedule?: string
  prompt?: string
  enabled?: boolean
}

export async function updateBotRoutine(
  name: string,
  routineId: string,
  updates: BotRoutineUpdates
): Promise<BotRoutine> {
  return request(`/bots/${name}/routines/${routineId}`, {
    method: 'PATCH',
    body: JSON.stringify(updates),
  })
}