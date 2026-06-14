import { defineStore } from 'pinia'
import { ref, computed, reactive } from 'vue'
import type { Session, Message } from '@/api/sessions'
import * as sessionsApi from '@/api/sessions'
import * as commandsApi from '@/api/commands'
import { i18n } from '@/locales'

export interface ChatError {
  message: string
  code?: string
}

export interface ToolCallEvent {
  id: string
  name: string
  args: string
  success?: boolean
  duration?: string
  content?: string
  status: 'running' | 'completed' | 'error'
}

export interface TaskProgress {
  phase: string
  detail: string
  percent: number
  iteration: number
  maxIterations: number
  tokensUsed: number
  tokensRemaining: number
}

interface SessionState {
  messages: Message[]
  streaming: boolean
  streamContent: string
  streamBuffer: string
  toolCalls: ToolCallEvent[]
  taskProgress: TaskProgress | null
}

function $t(key: string, params?: Record<string, string | number>): string {
  return i18n.global.t(key, params)
}

export const useChatStore = defineStore('chat', () => {
  const sessions = ref<Session[]>([])
  const activeSessionId = ref<string | null>(null)
  const error = ref<ChatError | null>(null)

  const builtinCommands: Array<Omit<commandsApi.Command, 'description'>> = [
    { name: 'help', usage: '/help', aliases: [], category: 'general' },
    { name: 'new', usage: '/new', aliases: [], category: 'session' },
    { name: 'clear', usage: '/clear', aliases: [], category: 'session' },
    { name: 'undo', usage: '/undo', aliases: [], category: 'session' },
  ]

  const commands = computed<commandsApi.Command[]>(() => {
    return builtinCommands.map(cmd => ({
      ...cmd,
      description: $t(`chat.commands.${cmd.name}`),
    }))
  })
  
  const sessionStates = ref<Record<string, SessionState>>({})
  const sessionEventSources = ref<Record<string, EventSource | null>>({})
  const sessionFlushTimers = ref<Record<string, ReturnType<typeof setTimeout> | null>>({})
  
  let toolCallIdCounter = 0

  const STREAM_FLUSH_INTERVAL = 80

  const activeSession = computed(() =>
    sessions.value.find(s => s.id === activeSessionId.value)
  )

  const activeSessionState = computed(() => {
    if (!activeSessionId.value) return null
    return sessionStates.value[activeSessionId.value] || null
  })

  const messages = computed(() => {
    const state = activeSessionState.value
    return state?.messages || []
  })

  const streaming = computed(() => {
    const state = activeSessionState.value
    return state?.streaming || false
  })

  const streamContent = computed(() => {
    const state = activeSessionState.value
    return state?.streamContent || ''
  })

  const toolCalls = computed(() => {
    const state = activeSessionState.value
    return state?.toolCalls || []
  })

  const activeToolCalls = computed(() => {
    return toolCalls.value.filter(tc => tc.status === 'running')
  })

  const taskProgress = computed(() => {
    const state = activeSessionState.value
    return state?.taskProgress || null
  })

  const isLongTask = computed(() => {
    if (!taskProgress.value) return false
    const tp = taskProgress.value
    return tp.maxIterations > 20 || tp.percent > 0
  })

  function getOrCreateSessionState(sessionId: string): SessionState {
    let state = sessionStates.value[sessionId]
    if (!state) {
      state = reactive({
        messages: [],
        streaming: false,
        streamContent: '',
        streamBuffer: '',
        toolCalls: [],
        taskProgress: null,
      })
      sessionStates.value = { ...sessionStates.value, [sessionId]: state }
    }
    return state
  }

  async function loadSessions(): Promise<void> {
    try {
      const allSessions = await sessionsApi.getSessions()
      sessions.value = allSessions.filter(s => !s.source || s.source === 'web')
      if (!activeSessionId.value && sessions.value.length > 0) {
        const first = sessions.value[0]
        activeSessionId.value = first.id
        getOrCreateSessionState(first.id)
      }
    } catch (e) {
      console.error('Failed to load sessions:', e)
      sessions.value = []
    }
  }

  async function loadCommands(): Promise<void> {
    try {
      const serverCmds = await commandsApi.getCommandList()
      if (Array.isArray(serverCmds) && serverCmds.length > 0) {
        console.warn('Server commands not yet integrated; using builtin translated list')
      }
    } catch (e) {
      console.warn('Failed to load commands from server, using defaults')
    }
  }

  function isCommand(input: string): boolean {
    return input.trim().startsWith('/')
  }

  async function executeCommand(input: string): Promise<void> {
    const trimmed = input.trim()
    if (!trimmed) return

    const parts = trimmed.split(/\s+/)
    const cmd = parts[0].toLowerCase()

    if (!activeSessionId.value && cmd !== '/new') {
      await createSession()
      if (!activeSessionId.value) return
    }

    let result: string | null = null

    switch (cmd) {
      case '/help': {
        const lines = commands.value.map(c => `  ${c.usage.padEnd(20)} ${c.description}`)
        result = `**${$t('chat.commands.available')}**：\n\n${lines.join('\n')}\n\n${$t('chat.commands.hint')}`
        break
      }
      case '/new': {
        await createSession()
        result = $t('chat.commands.created')
        break
      }
      case '/clear': {
        if (activeSessionId.value) {
          const state = sessionStates.value[activeSessionId.value]
          if (state) state.messages = []
        }
        result = $t('chat.commands.cleared')
        break
      }
      case '/undo': {
        if (activeSessionId.value) {
          const state = sessionStates.value[activeSessionId.value]
          if (state && state.messages.length > 0) {
            state.messages = state.messages.slice(0, -1)
            result = $t('chat.commands.undone')
          } else {
            result = $t('chat.commands.noUndo')
          }
        } else {
          result = $t('chat.commands.noSession')
        }
        break
      }
      default: {
        result = $t('chat.commands.unknown', { cmd })
      }
    }

    if (result && activeSessionId.value) {
      addSystemMessage(result)
    }
  }

  function addSystemMessage(content: string): void {
    if (!activeSessionId.value) return
    
    const sessionId = activeSessionId.value
    const state = getOrCreateSessionState(sessionId)
    state.messages.push({
      id: Date.now().toString(),
      role: 'system',
      content,
      timestamp: new Date().toISOString(),
      session_id: sessionId,
    })
  }

  function autocompleteCommand(input: string): string[] {
    if (!input.startsWith('/')) return []

    const partial = input.toLowerCase().slice(1)
    const suggestions: string[] = []
    const cmdList = commands.value

    for (const cmd of cmdList) {
      if (cmd.name.toLowerCase().startsWith(partial)) {
        suggestions.push('/' + cmd.name)
      }
      for (const alias of cmd.aliases || []) {
        if (alias.toLowerCase().startsWith(partial)) {
          suggestions.push(alias)
        }
      }
    }

    return [...new Set(suggestions)].slice(0, 10)
  }

  async function createSession(): Promise<Session | null> {
    try {
      error.value = null
      const session = await sessionsApi.createSession()
      sessions.value = [session, ...sessions.value]
      activeSessionId.value = session.id
      getOrCreateSessionState(session.id)
      return session
    } catch (e) {
      const errMsg = e instanceof Error ? e.message : 'Unknown error'
      error.value = { message: 'Failed to create session: ' + errMsg }
      console.error('Failed to create session:', e)
      return null
    }
  }

  async function selectSession(id: string): Promise<void> {
    activeSessionId.value = id
    const state = getOrCreateSessionState(id)
    
    if (state.messages.length === 0) {
      try {
        const res = await sessionsApi.getSession(id)
        state.messages = res.messages || []
      } catch (e) {
        console.error('Failed to load session messages:', e)
        state.messages = []
      }
    }
  }

  async function deleteSession(id: string): Promise<void> {
    try {
      await sessionsApi.deleteSession(id)
      sessions.value = sessions.value.filter(s => s.id !== id)
      
      if (sessionFlushTimers.value[id]) {
        clearTimeout(sessionFlushTimers.value[id]!)
      }
      if (sessionEventSources.value[id]) {
        sessionEventSources.value[id]!.close()
      }
      
      const newStates = { ...sessionStates.value }
      delete newStates[id]
      sessionStates.value = newStates
      
      const newEventSources = { ...sessionEventSources.value }
      delete newEventSources[id]
      sessionEventSources.value = newEventSources
      
      if (activeSessionId.value === id) {
        activeSessionId.value = null
      }
    } catch (e) {
      console.error('Failed to delete session:', e)
    }
  }

  async function renameSession(id: string, name: string): Promise<void> {
    try {
      await sessionsApi.renameSession(id, name)
      const session = sessions.value.find(s => s.id === id)
      if (session) {
        session.title = name
      }
    } catch (e) {
      console.error('Failed to rename session:', e)
    }
  }

  function flushStreamBuffer(sessionId: string): void {
    const state = sessionStates.value[sessionId]
    if (!state) return
    
    if (state.streamBuffer) {
      state.streamContent += state.streamBuffer
      state.streamBuffer = ''
    }
    sessionFlushTimers.value = { ...sessionFlushTimers.value, [sessionId]: null }
  }

  async function sendMessage(content: string, images?: string[], files?: sessionsApi.UploadedFile[]): Promise<void> {
    if (!activeSessionId.value) {
      const session = await createSession()
      if (!session) return
    }

    const sessionId = activeSessionId.value!
    const state = getOrCreateSessionState(sessionId)

    state.messages.push({
      id: Date.now().toString(),
      role: 'user',
      content,
      timestamp: new Date().toISOString(),
      session_id: sessionId,
      images,
      files,
    })

    state.streaming = true
    state.streamContent = ''
    state.streamBuffer = ''
    state.toolCalls = []
    state.taskProgress = null
    error.value = null

    if (sessionFlushTimers.value[sessionId]) {
      clearTimeout(sessionFlushTimers.value[sessionId]!)
      sessionFlushTimers.value = { ...sessionFlushTimers.value, [sessionId]: null }
    }
    if (sessionEventSources.value[sessionId]) {
      sessionEventSources.value[sessionId]!.close()
      sessionEventSources.value = { ...sessionEventSources.value, [sessionId]: null }
    }

    try {
      const eventSource = sessionsApi.streamChat(sessionId, content, images, files)
      sessionEventSources.value = { ...sessionEventSources.value, [sessionId]: eventSource }

      eventSource.onmessage = (event) => {
        try {
          const data = JSON.parse(event.data)

          if (data.type === 'progress') {
            state.taskProgress = {
              phase: data.phase || 'executing',
              detail: data.detail || '',
              percent: data.percent || 0,
              iteration: data.iteration || 0,
              maxIterations: data.maxIterations || 0,
              tokensUsed: data.tokensUsed || 0,
              tokensRemaining: data.tokensRemaining || 0,
            }
            return
          }

          if (data.type === 'ping') {
            return
          }

          if (data.type === 'tool_start') {
            if (sessionFlushTimers.value[sessionId]) {
              clearTimeout(sessionFlushTimers.value[sessionId]!)
              sessionFlushTimers.value = { ...sessionFlushTimers.value, [sessionId]: null }
            }
            flushStreamBuffer(sessionId)
            
            const id = `tc_${++toolCallIdCounter}`
            state.toolCalls.push({
              id,
              name: data.name,
              args: data.args,
              status: 'running',
            })
            return
          }

          if (data.type === 'tool_result') {
            const tc = state.toolCalls.find(t => t.name === data.name && t.status === 'running')
            if (tc) {
              tc.success = data.success
              tc.duration = data.duration
              tc.content = data.content
              tc.status = data.success ? 'completed' : 'error'
            } else {
              state.toolCalls.push({
                id: `tc_${++toolCallIdCounter}`,
                name: data.name,
                args: '',
                success: data.success,
                duration: data.duration,
                content: data.content,
                status: data.success ? 'completed' : 'error',
              })
            }
            return
          }

          if (data.error) {
            state.streaming = false
            state.taskProgress = null
            
            if (sessionEventSources.value[sessionId]) {
              sessionEventSources.value[sessionId]!.close()
              sessionEventSources.value = { ...sessionEventSources.value, [sessionId]: null }
            }
            
            error.value = { message: data.error }
            console.error('Stream error:', data.error)
            return
          }

          if (data.delta) {
            state.streamBuffer += data.delta
            
            if (!sessionFlushTimers.value[sessionId]) {
              const timer = setTimeout(() => {
                flushStreamBuffer(sessionId)
              }, STREAM_FLUSH_INTERVAL)
              sessionFlushTimers.value = { ...sessionFlushTimers.value, [sessionId]: timer }
            }
          }
          
          if (data.done) {
            if (sessionFlushTimers.value[sessionId]) {
              clearTimeout(sessionFlushTimers.value[sessionId]!)
              sessionFlushTimers.value = { ...sessionFlushTimers.value, [sessionId]: null }
            }
            
            flushStreamBuffer(sessionId)
            
            if (sessionEventSources.value[sessionId]) {
              sessionEventSources.value[sessionId]!.close()
              sessionEventSources.value = { ...sessionEventSources.value, [sessionId]: null }
            }
            
            state.streaming = false
            state.taskProgress = null
            
            const finalContent = state.streamContent
            state.messages.push({
              id: Date.now().toString(),
              role: 'assistant' as const,
              content: finalContent,
              timestamp: new Date().toISOString(),
              session_id: sessionId,
            })
            state.streamContent = ''
            
            loadSessions()
          }
        } catch (e) {
          console.error('Failed to parse stream event:', e)
        }
      }

      eventSource.onerror = () => {
        if (sessionFlushTimers.value[sessionId]) {
          clearTimeout(sessionFlushTimers.value[sessionId]!)
          sessionFlushTimers.value = { ...sessionFlushTimers.value, [sessionId]: null }
        }
        flushStreamBuffer(sessionId)
        if (sessionEventSources.value[sessionId]) {
          sessionEventSources.value[sessionId]!.close()
          sessionEventSources.value = { ...sessionEventSources.value, [sessionId]: null }
        }
        
        state.streaming = false
        
        if (!state.streamContent && !state.messages.some(m => m.role === 'assistant' && m.session_id === sessionId)) {
          error.value = { message: 'Connection lost' }
        }
      }
    } catch (e) {
      state.streaming = false
      const errMsg = e instanceof Error ? e.message : 'Unknown error'
      error.value = { message: 'Failed to send message: ' + errMsg }
    }
  }

  function stopGeneration(): void {
    if (!activeSessionId.value) return
    
    const sessionId = activeSessionId.value
    const state = getOrCreateSessionState(sessionId)
    
    if (sessionFlushTimers.value[sessionId]) {
      clearTimeout(sessionFlushTimers.value[sessionId]!)
      sessionFlushTimers.value = { ...sessionFlushTimers.value, [sessionId]: null }
    }
    flushStreamBuffer(sessionId)
    
    if (sessionEventSources.value[sessionId]) {
      sessionEventSources.value[sessionId]!.close()
      sessionEventSources.value = { ...sessionEventSources.value, [sessionId]: null }
    }
    
    state.streaming = false
    state.taskProgress = null
    
    for (const tc of state.toolCalls) {
      if (tc.status === 'running') {
        tc.status = 'error'
        tc.success = false
      }
    }
    
    if (state.streamContent) {
      state.messages.push({
        id: Date.now().toString(),
        role: 'assistant',
        content: state.streamContent + '\n\n*[Stopped by user]*',
        timestamp: new Date().toISOString(),
        session_id: sessionId,
      })
      state.streamContent = ''
    }
  }

  function cleanup(): void {
    for (const sessionId of Object.keys(sessionStates.value)) {
      if (sessionFlushTimers.value[sessionId]) {
        clearTimeout(sessionFlushTimers.value[sessionId]!)
      }
      if (sessionEventSources.value[sessionId]) {
        sessionEventSources.value[sessionId]!.close()
      }
    }
    sessionStates.value = {}
    sessionEventSources.value = {}
    sessionFlushTimers.value = {}
  }

  return {
    sessions,
    activeSessionId,
    messages,
    streaming,
    streamContent,
    error,
    activeSession,
    toolCalls,
    activeToolCalls,
    taskProgress,
    isLongTask,
    commands,
    loadSessions,
    loadCommands,
    createSession,
    selectSession,
    deleteSession,
    renameSession,
    sendMessage,
    stopGeneration,
    cleanup,
    isCommand,
    executeCommand,
    autocompleteCommand,
    addSystemMessage,
    getOrCreateSessionState,
  }
})