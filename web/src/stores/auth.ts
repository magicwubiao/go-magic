import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import * as authApi from '@/api/auth'

export const useAuthStore = defineStore('auth', () => {
  const token = ref<string | null>(localStorage.getItem('auth_token'))
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
        localStorage.removeItem('auth_token')
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
      localStorage.setItem('auth_token', res.token)
      configured.value = true
      return true
    } catch (e) {
      const errMsg = e instanceof Error ? e.message : 'Setup failed'
      error.value = errMsg
      return false
    } finally {
      loading.value = false
    }
  }

  async function login(password: string): Promise<boolean> {
    loading.value = true
    error.value = null
    try {
      const res = await authApi.login(password)
      token.value = res.token
      localStorage.setItem('auth_token', res.token)
      return true
    } catch (e) {
      const errMsg = e instanceof Error ? e.message : 'Login failed'
      error.value = errMsg
      return false
    } finally {
      loading.value = false
    }
  }

  function logout(): void {
    token.value = null
    localStorage.removeItem('auth_token')
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
