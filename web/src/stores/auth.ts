import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import * as authApi from '@/api/auth'
import { getAuthToken, setAuthToken } from '@/api/client'

export const useAuthStore = defineStore('auth', () => {
  const token = ref<string | null>(getAuthToken())
  const configured = ref(false)
  const loading = ref(false)
  const error = ref<string | null>(null)

  const isAuthenticated = computed(() => !!token.value)

  async function checkStatus(): Promise<void> {
    try {
      const status = await authApi.getAuthStatus()
      configured.value = status.configured
      // If auth is not configured, clear any stale token
      if (!status.configured && token.value) {
        token.value = null
        setAuthToken(null)
      }
    } catch {
      configured.value = false
    }
  }

  async function setup(password: string): Promise<boolean> {
    loading.value = true
    error.value = null
    try {
      const res = await authApi.setupAuth(password)
      token.value = res.token
      setAuthToken(res.token)
      configured.value = true
      return true
    } catch (e) {
      error.value = e instanceof Error ? e.message : 'Setup failed'
      return false
    } finally {
      loading.value = false
    }
  }

  async function login(password: string, remember = false): Promise<boolean> {
    loading.value = true
    error.value = null
    try {
      const res = await authApi.login(password, remember)
      token.value = res.token
      setAuthToken(res.token)
      return true
    } catch (e) {
      error.value = e instanceof Error ? e.message : 'Login failed'
      return false
    } finally {
      loading.value = false
    }
  }

  async function logout(): Promise<void> {
    try {
      // Invalidate the session server-side so the token can't be reused.
      await authApi.logout()
    } catch {
      // best-effort: still clear the local token even if the network call fails
    }
    token.value = null
    setAuthToken(null)
  }

  return {
    token,
    configured,
    loading,
    error,
    isAuthenticated,
    checkStatus,
    setup,
    login,
    logout,
  }
})