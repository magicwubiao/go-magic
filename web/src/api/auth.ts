import { request } from './client'

export interface AuthStatus {
  /** 服务端是否已设置密码。 */
  configured: boolean
  /**
   * 调用方当前携带的 token 是否仍是有效会话（由服务端判定）。
   * 路由守卫必须用它而不是"本地有没有 token"——残留的失效 token 会让主界面
   * 带着死凭据发一堆 401，把错误提示弹到认证页上。
   */
  authenticated: boolean
}

export interface AuthResponse {
  ok: boolean
  token: string
}

export async function getAuthStatus(): Promise<AuthStatus> {
  return request('/auth/status')
}

export async function setupAuth(password: string): Promise<AuthResponse> {
  return request('/auth/setup', {
    method: 'POST',
    body: JSON.stringify({ password }),
  })
}

export async function login(password: string, remember = false): Promise<AuthResponse> {
  return request('/auth/login', {
    method: 'POST',
    body: JSON.stringify({ password, remember }),
  })
}

export async function logout(): Promise<{ ok: boolean }> {
  return request('/auth/logout', {
    method: 'POST',
  })
}