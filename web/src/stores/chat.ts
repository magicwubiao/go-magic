import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import type { Session, Message } from '@/api/sessions'
import * as sessionsApi from '@/api/sessions'

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

export const useChatStore = defineStore('chat', () => {
  const sessions = ref<Session[]>([])
  const activeSessionId = ref<string | null>(null)
  const messages = ref<Message[]>([])
  const streaming = ref(false)
  const streamContent = ref('')
  const error = ref<ChatError | null>(null)
  const toolCalls = ref<ToolCallEvent[]>([])
  let currentEventSource: EventSource | null = null
  let toolCallIdCounter = 0

  // Throttling for stream content updates
  let streamBuffer = ''
  let streamFlushTimer: ReturnType<typeof setTimeout> | null = null
  const STREAM_FLUSH_INTERVAL = 80 // ms

  const activeSession = computed(() =>
    sessions.value.find(s => s.id === activeSessionId.value)
  )

  const activeToolCalls = computed(() =>
    toolCalls.value.filter(tc => tc.status === 'running')
  )

  async function loadSessions(): Promise<void> {
    try {
      const allSessions = await sessionsApi.getSessions()
      sessions.value = allSessions.filter(s => !s.source || s.source === 'web')
    } catch (e) {
      console.error('Failed to load sessions:', e)
      sessions.value = []
    }
  }

  async function createSession(): Promise<Session | null> {
    try {
      error.value = null
      const session = await sessionsApi.createSession()
      sessions.value.unshift(session)
      activeSessionId.value = session.id
      messages.value = []
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
    try {
      const res = await sessionsApi.getSession(id)
      messages.value = res.messages || []
    } catch (e) {
      console.error('Failed to load session messages:', e)
      messages.value = []
    }
  }

  async function deleteSession(id: string): Promise<void> {
    try {
      await sessionsApi.deleteSession(id)
      sessions.value = sessions.value.filter(s => s.id !== id)
      if (activeSessionId.value === id) {
        activeSessionId.value = null
        messages.value = []
      }
    } catch (e) {
      console.error('Failed to delete session:', e)
    }
  }

  function closeEventSource(): void {
    if (currentEventSource) {
      currentEventSource.close()
      currentEventSource = null
    }
  }

  function flushStreamBuffer(): void {
    if (streamBuffer) {
      streamContent.value += streamBuffer
      streamBuffer = ''
    }
    streamFlushTimer = null
  }

  async function sendMessage(content: string, images?: string[], files?: sessionsApi.UploadedFile[]): Promise<void> {
    if (!activeSessionId.value) {
      const session = await createSession()
      if (!session) return
    }

    const sessionId = activeSessionId.value!

    // Add user message
    messages.value.push({
      id: Date.now().toString(),
      role: 'user',
      content,
      timestamp: new Date().toISOString(),
      session_id: sessionId,
      images,
      files,
    })

    // Start streaming
    streaming.value = true
    streamContent.value = ''
    streamBuffer = ''
    if (streamFlushTimer) {
      clearTimeout(streamFlushTimer)
      streamFlushTimer = null
    }
    toolCalls.value = []
    error.value = null

    // Close any existing EventSource
    closeEventSource()

    try {
      currentEventSource = sessionsApi.streamChat(sessionId, content, images, files)

      currentEventSource.onmessage = (event) => {
        try {
          const data = JSON.parse(event.data)

          // Handle structured tool events
          if (data.type === 'tool_start') {
            // Flush any pending stream content before tool event
            if (streamFlushTimer) {
              clearTimeout(streamFlushTimer)
              flushStreamBuffer()
            }
            const id = `tc_${++toolCallIdCounter}`
            toolCalls.value.push({
              id,
              name: data.name,
              args: data.args,
              status: 'running',
            })
            return
          }

          if (data.type === 'tool_result') {
            const tc = toolCalls.value.find(t => t.name === data.name && t.status === 'running')
            if (tc) {
              tc.success = data.success
              tc.duration = data.duration
              tc.content = data.content
              tc.status = data.success ? 'completed' : 'error'
            } else {
              toolCalls.value.push({
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

          // Handle normal delta with throttling
          if (data.delta) {
            streamBuffer += data.delta
            if (!streamFlushTimer) {
              streamFlushTimer = setTimeout(flushStreamBuffer, STREAM_FLUSH_INTERVAL)
            }
          }
          if (data.done) {
            // Flush remaining buffer
            if (streamFlushTimer) {
              clearTimeout(streamFlushTimer)
            }
            flushStreamBuffer()
            closeEventSource()
            streaming.value = false
            messages.value.push({
              id: Date.now().toString(),
              role: 'assistant',
              content: streamContent.value,
              timestamp: new Date().toISOString(),
              session_id: sessionId,
            })
            streamContent.value = ''
          }
        } catch (e) {
          console.error('Failed to parse stream event:', e)
        }
      }

      currentEventSource.onerror = () => {
        if (streamFlushTimer) {
          clearTimeout(streamFlushTimer)
          flushStreamBuffer()
        }
        closeEventSource()
        streaming.value = false
        error.value = { message: 'Connection lost' }
      }
    } catch (e) {
      streaming.value = false
      const errMsg = e instanceof Error ? e.message : 'Unknown error'
      error.value = { message: 'Failed to send message: ' + errMsg }
    }
  }

  function stopGeneration(): void {
    if (streamFlushTimer) {
      clearTimeout(streamFlushTimer)
      flushStreamBuffer()
    }
    closeEventSource()
    streaming.value = false
    // Mark any running tool calls as error
    for (const tc of toolCalls.value) {
      if (tc.status === 'running') {
        tc.status = 'error'
        tc.success = false
      }
    }
    if (streamContent.value) {
      messages.value.push({
        id: Date.now().toString(),
        role: 'assistant',
        content: streamContent.value + '\n\n*[Stopped by user]*',
        timestamp: new Date().toISOString(),
        session_id: activeSessionId.value || '',
      })
      streamContent.value = ''
    }
  }

  function cleanup(): void {
    if (streamFlushTimer) {
      clearTimeout(streamFlushTimer)
    }
    closeEventSource()
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
    loadSessions,
    createSession,
    selectSession,
    deleteSession,
    sendMessage,
    stopGeneration,
    cleanup,
  }
})
