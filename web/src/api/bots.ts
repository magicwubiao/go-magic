import { request, getAuthToken } from './client'

const BASE_URL = '/api'

export interface BotRuntime {
  online: boolean
  session_id?: string
  queue_depth?: number
  history_length?: number
  active_routines?: number
  last_active?: number
}

export interface Bot {
  name: string
  mention_tag: string
  title: string
  description: string
  system_prompt: string
  model: string
  provider: string
  tools?: string[]
  skills?: string[]
  memory?: string
  avatar?: string
  env?: Record<string, string>
  hidden?: boolean
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

/**
 * Update payload for a bot. `clear_tools` / `clear_skills` / `clear_env`
 * disambiguate "send an empty list to wipe the whitelist" from "field not
 * provided (keep current value)" — empty list + clear flag = nil whitelist.
 */
export interface BotUpdate {
  title?: string
  description?: string
  system_prompt?: string
  model?: string
  provider?: string
  tools?: string[]
  skills?: string[]
  memory?: string
  avatar?: string
  env?: Record<string, string>
  hidden?: boolean
  clear_tools?: boolean
  clear_skills?: boolean
  clear_env?: boolean
}

export async function updateBot(name: string, updates: BotUpdate): Promise<Bot> {
  return request(`/bots/${name}`, { method: 'PUT', body: JSON.stringify(updates) })
}

export async function deleteBot(name: string): Promise<void> {
  return request(`/bots/${name}`, { method: 'DELETE' })
}

/** Clone a bot's full profile under a new name (fresh chat history). */
export async function cloneBot(name: string, newName: string): Promise<Bot> {
  return request(`/bots/${name}/clone`, { method: 'POST', body: JSON.stringify({ name: newName }) })
}

export async function getBotMessages(name: string): Promise<BotMessage[]> {
  return request(`/bots/${name}/messages`)
}

/** Wipe the bot's canonical chat history (server + UI reload afterwards). */
export async function clearBotMessages(name: string): Promise<void> {
  return request(`/bots/${name}/messages`, { method: 'DELETE' })
}

/** Trigger an immediate one-off run of a routine outside its schedule. */
export async function runBotRoutineNow(name: string, routineId: string): Promise<void> {
  return request(`/bots/${name}/routines/${encodeURIComponent(routineId)}/run`, {
    method: 'POST',
  })
}

export async function sendBotChat(name: string, message: string): Promise<BotMessage> {
  return request(`/bots/${name}/chat`, {
    method: 'POST',
    body: JSON.stringify({ message }),
  })
}

/** True when the bot has a turn in flight or queued on the server. */
export async function getBotRunning(name: string): Promise<boolean> {
  const resp = await request<{ running: boolean }>(`/bots/${name}/running`)
  return !!resp.running
}

/** Explicitly cancel the bot's in-flight turn. Returns true if one was canceled. */
export async function cancelBotTurn(name: string): Promise<boolean> {
  const resp = await request<{ canceled: boolean }>(`/bots/${name}/cancel`, { method: 'POST' })
  return !!resp.canceled
}

/**
 * Thrown when the SSE stream ends (connection dropped, e.g. mobile browser
 * backgrounded) before the server sent its terminal {"done":true} event.
 * Carries the partial text received so far.
 */
export class StreamIncompleteError extends Error {
  partial: string
  constructor(partial: string) {
    super('bot chat stream ended before completion')
    this.name = 'StreamIncompleteError'
    this.partial = partial
  }
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
  let sawDone = false

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
      } else if (evt.done) {
        sawDone = true
      }
    } catch (e) {
      if (e instanceof Error && e.message && !(e instanceof SyntaxError)) throw e
      /* ignore malformed keep-alive chunks */
    }
  }

  try {
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
  } catch (e) {
    // User-initiated abort (switching bots) is not an incomplete stream.
    if (e instanceof DOMException && e.name === 'AbortError') throw e
    // Network-level failure mid-stream (mobile background kills the fetch):
    // treat like an early stream end so the store can poll for recovery
    // instead of losing the turn.
    if (e instanceof Error) {
      throw new StreamIncompleteError(finalText)
    }
    throw e
  }

  if (!sawDone) {
    throw new StreamIncompleteError(finalText)
  }

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