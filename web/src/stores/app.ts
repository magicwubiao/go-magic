import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import type { Session, Message, Toolset, Skill, CronJob } from '@/types'

export const useAppStore = defineStore('app', () => {
  // State
  const currentSession = ref<Session | null>(null)
  const sessions = ref<Session[]>([])
  const toolsets = ref<Toolset[]>([])
  const skills = ref<Skill[]>([])
  const cronJobs = ref<CronJob[]>([])
  const isConnected = ref(false)
  const isLoading = ref(false)
  const error = ref<string | null>(null)

  // Computed
  const sessionCount = computed(() => sessions.value.length)
  const enabledToolsets = computed(() => toolsets.value.filter((t) => t.enabled))
  const activeCronJobs = computed(() => cronJobs.value.filter((c) => c.active))

  // Actions
  async function fetchSessions() {
    try {
      const response = await fetch('/api/sessions')
      sessions.value = await response.json()
    } catch (e) {
      error.value = 'Failed to fetch sessions'
    }
  }

  async function createSession(name?: string) {
    try {
      const response = await fetch('/api/sessions', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name }),
      })
      const session = await response.json()
      sessions.value.unshift(session)
      currentSession.value = session
      return session
    } catch (e) {
      error.value = 'Failed to create session'
      throw e
    }
  }

  async function deleteSession(id: string) {
    try {
      await fetch(`/api/sessions/${id}`, { method: 'DELETE' })
      sessions.value = sessions.value.filter((s) => s.id !== id)
      if (currentSession.value?.id === id) {
        currentSession.value = sessions.value[0] || null
      }
    } catch (e) {
      error.value = 'Failed to delete session'
      throw e
    }
  }

  async function fetchToolsets() {
    try {
      const response = await fetch('/api/toolsets')
      toolsets.value = await response.json()
    } catch (e) {
      error.value = 'Failed to fetch toolsets'
    }
  }

  async function toggleToolset(name: string, enabled: boolean) {
    try {
      const url = enabled ? `/api/toolsets/${name}/enable` : `/api/toolsets/${name}/disable`
      await fetch(url, { method: 'POST' })
      const toolset = toolsets.value.find((t) => t.name === name)
      if (toolset) {
        toolset.enabled = enabled
      }
    } catch (e) {
      error.value = 'Failed to toggle toolset'
      throw e
    }
  }

  async function fetchSkills() {
    try {
      const response = await fetch('/api/skills')
      skills.value = await response.json()
    } catch (e) {
      error.value = 'Failed to fetch skills'
    }
  }

  async function fetchCronJobs() {
    try {
      const response = await fetch('/api/cron')
      cronJobs.value = await response.json()
    } catch (e) {
      error.value = 'Failed to fetch cron jobs'
    }
  }

  function setError(msg: string | null) {
    error.value = msg
  }

  return {
    // State
    currentSession,
    sessions,
    toolsets,
    skills,
    cronJobs,
    isConnected,
    isLoading,
    error,
    // Computed
    sessionCount,
    enabledToolsets,
    activeCronJobs,
    // Actions
    fetchSessions,
    createSession,
    deleteSession,
    fetchToolsets,
    toggleToolset,
    fetchSkills,
    fetchCronJobs,
    setError,
  }
})
