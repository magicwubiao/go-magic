import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import * as authApi from '@/api/auth'
import { getAuthToken, setAuthToken } from '@/api/client'

/**
 * 认证失败的机器可读分类，认证页据此挑文案。
 *
 * 早先是把异常的 message 直接当展示文案，实测会先渲染一帧原始英文
 * "Unauthorized" 再被中文覆盖（无头探针能观察到这帧闪现），所以上移到分类码。
 */
export type AuthErrorCode = 'rate_limited' | 'unauthorized' | 'conflict' | 'server' | 'network'

function classifyAuthError(e: unknown): AuthErrorCode {
  const status = (e as { status?: number } | null)?.status
  if (status === 429) return 'rate_limited'
  if (status === 401) return 'unauthorized'
  if (status === 409) return 'conflict'
  // 拿到过 HTTP 响应就是服务端问题；连响应都没有才是网络/超时。
  if (typeof status === 'number') return 'server'
  return 'network'
}

export const useAuthStore = defineStore('auth', () => {
  const token = ref<string | null>(getAuthToken())
  const configured = ref(false)
  const loading = ref(false)
  const errorCode = ref<AuthErrorCode | null>(null)

  const isAuthenticated = computed(() => !!token.value)

  /**
   * 拉取服务端认证状态。
   *
   * 用服务端的 `authenticated`（会话是否仍然有效）而不是"本地有没有 token"：
   * 过期/被吊销/换 magicHome 之后，localStorage 里的残留 token 曾让守卫放行到
   * 主界面，十几个并发请求全部 401，错误提示就弹到了认证页上。顺手清掉无效 token。
   *
   * 判定用 `=== false`：老后端不返回该字段时退回旧行为，不能把"字段缺失"当成失效。
   */
  async function checkStatus(): Promise<void> {
    try {
      const status = await authApi.getAuthStatus()
      configured.value = status.configured
      if ((!status.configured || status.authenticated === false) && token.value) {
        token.value = null
        setAuthToken(null)
      }
    } catch {
      configured.value = false
    }
  }

  function clearError(): void {
    errorCode.value = null
  }

  async function setup(password: string): Promise<boolean> {
    loading.value = true
    errorCode.value = null
    try {
      const res = await authApi.setupAuth(password)
      token.value = res.token
      setAuthToken(res.token)
      configured.value = true
      return true
    } catch (e) {
      errorCode.value = classifyAuthError(e)
      return false
    } finally {
      loading.value = false
    }
  }

  async function login(password: string, remember = false): Promise<boolean> {
    loading.value = true
    errorCode.value = null
    try {
      const res = await authApi.login(password, remember)
      token.value = res.token
      setAuthToken(res.token)
      return true
    } catch (e) {
      errorCode.value = classifyAuthError(e)
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
    errorCode,
    isAuthenticated,
    checkStatus,
    clearError,
    setup,
    login,
    logout,
  }
})
