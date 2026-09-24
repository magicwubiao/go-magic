import { resetSessionCache } from '@/utils/sessionCache'

const BASE_URL = '/api'

const RETRY_STATUS_CODES = [429, 500, 502, 503, 504]
const MAX_RETRIES = 3
const RETRY_DELAY_MS = 1000

export function getAuthToken(): string | null {
  return localStorage.getItem('auth_token')
}

export function setAuthToken(token: string | null): void {
  if (token) {
    localStorage.setItem('auth_token', token)
  } else {
    localStorage.removeItem('auth_token')
    // 清 token 是"身份切换"的唯一咽喉点（登出与 401 都走这里）。
    // 任何与当前用户绑定的内存缓存都必须一起失效，否则换账号后
    // 会先看到上一个用户的会话列表。见 utils/sessionCache.ts。
    resetSessionCache()
  }
}

export interface RequestOptions extends RequestInit {
  retries?: number
  skipAuth?: boolean
}

/** Error carrying the HTTP status, so callers can tell 401/429/5xx apart without
 *  string-matching on the message. */
export interface ApiError extends Error {
  status?: number
}

function apiError(message: string, status?: number): ApiError {
  const err = new Error(message) as ApiError
  if (status !== undefined) err.status = status
  return err
}

/**
 * 会话失效时统一收口：清掉本地 token，并把路由带回登录页。
 * 只在"尚未处于登录页"时改写一次 hash——一个页面挂载会并发发出十几个请求，
 * 每个 401 都写一次 hash 会反复触发路由，而且后续请求的错误会以 toast 的形式
 * 落在刚打开的认证页上。
 */
export function handleUnauthorized(): void {
  setAuthToken(null)
  if (!window.location.hash.startsWith('#/login')) {
    window.location.hash = '#/login'
  }
}

export async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const url = `${BASE_URL}${path}`
  const headers: Record<string, string> = {
    ...(options.headers as Record<string, string>),
  }

  const isFormData = options.body instanceof FormData
  const hasBody = options.body !== undefined && options.body !== null
  if (!isFormData && hasBody) {
    headers['Content-Type'] = 'application/json'
  }

  if (!options.skipAuth) {
    const token = getAuthToken()
    if (token) {
      headers['Authorization'] = `Bearer ${token}`
    }
  }

  const retries = options.retries ?? MAX_RETRIES

  return doRequest<T>(url, { ...options, headers }, retries)
}

async function doRequest<T>(url: string, options: RequestInit & { headers: Record<string, string> }, retries: number): Promise<T> {
  const controller = new AbortController()
  const timeoutId = setTimeout(() => controller.abort(), 30000)

  try {
    const response = await fetch(url, {
      ...options,
      signal: controller.signal,
    })

    if (response.status === 401) {
      handleUnauthorized()
      throw apiError('Unauthorized', 401)
    }

    if (response.status === 403) {
      throw apiError('Forbidden', 403)
    }

    if (RETRY_STATUS_CODES.includes(response.status) && retries > 0) {
      await new Promise(resolve => setTimeout(resolve, RETRY_DELAY_MS * (MAX_RETRIES - retries + 1)))
      return doRequest(url, options, retries - 1)
    }

    if (!response.ok) {
      const text = await response.text().catch(() => response.statusText)
      throw apiError(`HTTP ${response.status}: ${text}`, response.status)
    }

    const contentLength = response.headers.get('content-length')
    if (contentLength === '0' || response.status === 204) {
      return undefined as T
    }

    const text = await response.text()
    if (!text) {
      return undefined as T
    }

    try {
      return JSON.parse(text) as T
    } catch {
      return text as unknown as T
    }
  } finally {
    clearTimeout(timeoutId)
  }
}

export async function requestText(path: string, options: RequestOptions = {}): Promise<string> {
  const url = `${BASE_URL}${path}`
  const headers: Record<string, string> = {
    ...(options.headers as Record<string, string>),
  }

  if (!options.skipAuth) {
    const token = getAuthToken()
    if (token) {
      headers['Authorization'] = `Bearer ${token}`
    }
  }

  const response = await fetch(url, { ...options, headers })

  if (response.status === 401) {
    handleUnauthorized()
    throw apiError('Unauthorized', 401)
  }

  if (!response.ok) {
    const text = await response.text().catch(() => response.statusText)
    throw apiError(`HTTP ${response.status}: ${text}`, response.status)
  }

  return response.text()
}

export async function downloadFile(path: string, fileName: string): Promise<void> {
  const url = `${BASE_URL}${path}`
  const headers: Record<string, string> = {}

  const token = getAuthToken()
  if (token) {
    headers['Authorization'] = `Bearer ${token}`
  }

  const response = await fetch(url, { headers })

  if (!response.ok) {
    throw apiError(`HTTP ${response.status}`, response.status)
  }

  const blob = await response.blob()
  const urlObject = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = urlObject
  link.download = fileName
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
  URL.revokeObjectURL(urlObject)
}
