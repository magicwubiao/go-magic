import { request } from './client'

export interface AuthStatus {
  configured: boolean
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