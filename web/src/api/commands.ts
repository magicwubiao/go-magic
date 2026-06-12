import { request } from './client'
import { i18n } from '@/locales'

export interface Command {
  name: string
  description: string
  usage: string
  aliases: string[]
  flags?: Array<{
    name: string
    short?: string
    description: string
    type: string
    default?: any
    required?: boolean
  }>
  category: string
}

export interface CommandResult {
  success: boolean
  result: string
}

const BUILTIN_NAMES: Array<Omit<Command, 'description'>> = [
  { name: 'help', usage: '/help', aliases: ['/?'], category: 'general' },
  { name: 'new', usage: '/new', aliases: ['/n'], category: 'session' },
  { name: 'clear', usage: '/clear', aliases: ['/c'], category: 'session' },
  { name: 'compress', usage: '/compress', aliases: [], category: 'session' },
  { name: 'retry', usage: '/retry', aliases: ['/r'], category: 'session' },
  { name: 'undo', usage: '/undo', aliases: [], category: 'session' },
  { name: 'export', usage: '/export', aliases: [], category: 'session' },
]

export async function getCommandList(): Promise<Command[]> {
  try {
    const result = await request<Command[]>('/commands')
    if (Array.isArray(result) && result.length > 0) return result
  } catch (e) { console.warn('Using builtin commands') }
  const t = (key: string) => i18n.global.t(key)
  return BUILTIN_NAMES.map(cmd => ({
    ...cmd,
    description: t(`chat.commands.${cmd.name}`),
  }))
}

export async function executeCommand(command: string, sessionId?: string): Promise<CommandResult> {
  return request('/commands/execute', {
    method: 'POST',
    body: JSON.stringify({
      command,
      session_id: sessionId
    })
  })
}