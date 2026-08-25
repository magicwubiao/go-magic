import { defineStore } from 'pinia'
import { ref } from 'vue'
import * as botsApi from '@/api/bots'
import type { Bot, BotRoutine, BotMessage } from '@/api/bots'

export const useBotsStore = defineStore('bots', () => {
  const bots = ref<Bot[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)
  // True when the backend reports bot mode is not running (503), so the UI
  // can show a warning banner and disable creation instead of failing blindly.
  const modeDisabled = ref(false)

  // Per-bot chat state (single active chat panel)
  const activeBotName = ref<string | null>(null)
  const messages = ref<BotMessage[]>([])
  const routines = ref<BotRoutine[]>([])
  const chatLoading = ref(false)
  const sending = ref(false)

  async function loadBots(): Promise<void> {
    loading.value = true
    error.value = null
    try {
      bots.value = await botsApi.getBots()
      modeDisabled.value = false
    } catch (e) {
      error.value = e instanceof Error ? e.message : 'Unknown error'
      // 503 = bot mode not running on the server
      modeDisabled.value = /HTTP 503/.test(error.value) || error.value.includes('bot mode')
      bots.value = []
    } finally {
      loading.value = false
    }
  }

  async function createBot(bot: Partial<Bot>): Promise<Bot> {
    const created = await botsApi.createBot(bot)
    await loadBots()
    return created
  }

  async function updateBot(name: string, updates: Partial<Bot>): Promise<Bot> {
    const updated = await botsApi.updateBot(name, updates)
    const idx = bots.value.findIndex(b => b.name === name)
    if (idx >= 0) bots.value[idx] = { ...bots.value[idx], ...updated }
    return updated
  }

  async function deleteBot(name: string): Promise<void> {
    await botsApi.deleteBot(name)
    bots.value = bots.value.filter(b => b.name !== name)
    if (activeBotName.value === name) {
      closeChat()
    }
  }

  function openChat(name: string) {
    activeBotName.value = name
    messages.value = []
    routines.value = []
    void refreshChat()
  }

  function closeChat() {
    activeBotName.value = null
    messages.value = []
    routines.value = []
  }

  async function refreshChat() {
    if (!activeBotName.value) return
    const name = activeBotName.value
    chatLoading.value = true
    try {
      const [msgs, rts] = await Promise.all([
        botsApi.getBotMessages(name),
        botsApi.getBotRoutines(name),
      ])
      if (activeBotName.value === name) {
        messages.value = msgs
        routines.value = rts
      }
    } catch {
      messages.value = []
      routines.value = []
    } finally {
      chatLoading.value = false
    }
  }

  async function sendMessage(text: string) {
    const name = activeBotName.value
    if (!name || !text.trim()) return

    // Local optimistic user bubble
    const localId = 'local_' + Date.now()
    messages.value.push({
      id: localId,
      role: 'user',
      content: text,
      timestamp: Date.now(),
    })
    sending.value = true
    // Streaming assistant bubble: grows as deltas arrive.
    const streamId = 'stream_' + Date.now()
    messages.value.push({
      id: streamId,
      role: 'assistant',
      content: '',
      timestamp: Date.now(),
    })
    try {
      // Prefer SSE streaming; falls back to blocking endpoint internally.
      const reply = await botsApi.sendBotChatStream(name, text, {
        onDelta: (delta) => {
          const bubble = messages.value.find(m => m.id === streamId)
          if (bubble) {
            bubble.content += delta
            bubble.timestamp = Date.now()
          }
        },
      })
      // Replace the growing bubble with the authoritative reply.
      const idx = messages.value.findIndex(m => m.id === streamId)
      if (idx >= 0) {
        if (reply.content) {
          messages.value[idx] = reply
        } else {
          messages.value.splice(idx, 1)
        }
      }
    } catch (e) {
      // Drop the empty streaming bubble on error.
      const idx = messages.value.findIndex(m => m.id === streamId)
      if (idx >= 0 && !messages.value[idx].content) {
        messages.value.splice(idx, 1)
      }
      throw e
    } finally {
      sending.value = false
      // Refresh history so optimistic + server states converge
      void refreshMessagesOnly()
    }
  }

  async function refreshMessagesOnly() {
    if (!activeBotName.value) return
    try {
      const msgs = await botsApi.getBotMessages(activeBotName.value)
      if (activeBotName.value && msgs.length > 0) {
        messages.value = msgs
      }
    } catch {
      /* keep current view */
    }
  }

  async function addRoutine(routine: { name: string; schedule: string; prompt: string }) {
    if (!activeBotName.value) return
    const rt = await botsApi.createBotRoutine(activeBotName.value, routine)
    routines.value.push(rt)
  }

  async function removeRoutine(routineId: string) {
    if (!activeBotName.value) return
    await botsApi.deleteBotRoutine(activeBotName.value, routineId)
    routines.value = routines.value.filter(r => r.id !== routineId)
  }

  return {
    bots, loading, error, modeDisabled,
    activeBotName, messages, routines, chatLoading, sending,
    loadBots, createBot, updateBot, deleteBot,
    openChat, closeChat, refreshChat, sendMessage,
    addRoutine, removeRoutine,
  }
})
