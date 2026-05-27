import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import type { Session, Message } from '@/api/sessions'
import * as sessionsApi from '@/api/sessions'

export interface ChatError {
  message: string
  code?: string
}

export const useChatStore = defineStore('chat', () => {
  const sessions = ref<Session[]>([])
  const activeSessionId = ref<string | null>(null)
  const messages = ref<Message[]>([])
  const streaming = ref(false)
  const streamContent = ref('')
  const error = ref<ChatError | null>(null)
  let currentEventSource: EventSource | null = null

  const activeSession = computed(() =>
    sessions.value.find(s => s.id === activeSessionId.value)
  )

  async function loadSessions(): Promise<void> {
    try {
      const allSessions = await sessionsApi.getSessions()
      // Filter out gateway sessions (only show web sessions)
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

  async function sendMessage(content: string): Promise<void> {
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
    })

    // Start streaming
    streaming.value = true
    streamContent.value = ''
    error.value = null

    // Close any existing EventSource
    closeEventSource()

    try {
      currentEventSource = sessionsApi.streamChat(sessionId, content)

      currentEventSource.onmessage = (event) => {
        try {
          const data = JSON.parse(event.data) as { delta?: string; done?: boolean }
          if (data.delta) {
            streamContent.value += data.delta
          }
          if (data.done) {
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
    closeEventSource()
    streaming.value = false
    // Save partial content as a message if there's any
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
    loadSessions,
    createSession,
    selectSession,
    deleteSession,
    sendMessage,
    stopGeneration,
    cleanup,
  }
})
