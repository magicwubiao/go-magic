import { request } from './client'

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
