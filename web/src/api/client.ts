import { resetSessionCache } from '@/utils/sessionCache'
// 请求层的失败文案要跟随界面语言：这里直接读全局 i18n 实例，避免在
// "把错误抛给调用方之前"还要让每个调用点自己翻译一遍。
import { i18n } from '@/locales'

/** 取一条本地化文案；locale 未就绪时回退到 key（不会抛异常）。 */
function t(key: string, params?: Record<string, unknown>): string {
  try {
    return params ? (i18n.global.t as any)(key, params) : (i18n.global.t as any)(key)
  } catch {
    return key
  }
}

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
  /**
   * Per-request timeout in ms. Defaults to 30s, which is right for dashboard
   * calls but far too short for cross-machine relays: one relayed DM drives a
   * full agent turn on the remote box and legitimately runs for minutes.
   */
  timeoutMs?: number
}

/** Default request timeout: dashboard reads/writes, not agent turns. */
const DEFAULT_TIMEOUT_MS = 30000

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
  const timeoutMs = options.timeoutMs ?? DEFAULT_TIMEOUT_MS

  return doRequest<T>(url, { ...options, headers }, retries, timeoutMs)
}

async function doRequest<T>(
  url: string,
  options: RequestInit & { headers: Record<string, string> },
  retries: number,
  timeoutMs: number,
): Promise<T> {
  const controller = new AbortController()
  // 谁按下了 abort 决定报什么错：超时可以说"等了多久"，用户主动取消不该弹错。
  // 只留一个布尔不会踩并发：controller 与它都是本次调用的局部变量。
  let timedOut = false
  const timeoutId = setTimeout(() => {
    timedOut = true
    controller.abort()
  }, timeoutMs)

  // Forward a caller-side cancel (e.g. the user pressed "stop" on a long
  // cross-machine turn) into our controller, so the timeout and the caller's
  // abort share one signal path and the fetch is really torn down.
  const external = options.signal
  const forwardAbort = () => controller.abort()
  if (external) {
    if (external.aborted) controller.abort()
    else external.addEventListener('abort', forwardAbort, { once: true })
  }

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
      return doRequest(url, options, retries - 1, timeoutMs)
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
  } catch (err) {
    throw normalizeRequestError(err, timedOut, timeoutMs)
  } finally {
    clearTimeout(timeoutId)
    external?.removeEventListener('abort', forwardAbort)
  }
}

/**
 * 把 fetch 层那些"只有机器能看懂"的失败换成能行动的说法。
 *
 * 浏览器 abort 时抛的是 `DOMException: signal is aborted without reason`，
 * 网络不可达时是 `TypeError: Failed to fetch`——两者直接透到 UI 上就是
 * "信号中断，没有原因"。这里按来源分类：
 *   - 超时：说清等了多久、可能是什么慢（后端仍在跑，重试即可）
 *   - 调用方主动取消：原样抛出，保留 AbortError 语义给上层静默处理
 *   - 网络不可达：提示服务没起来 / 网络断了，而不是"失败"
 * 其它错误（HTTP 状态、JSON 解析）保持原样，调用方按 status 分支。
 */
function normalizeRequestError(err: unknown, timedOut: boolean, timeoutMs: number): unknown {
  if (err instanceof DOMException && err.name === 'AbortError') {
    if (!timedOut) return err
    return apiError(t('common.requestTimeout', { seconds: Math.round(timeoutMs / 1000) }))
  }
  if (err instanceof TypeError) {
    // fetch 只在"连不上"时抛 TypeError（DNS、连接被拒、CORS、离线）。
    return apiError(t('common.requestUnreachable'))
  }
  return err
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
