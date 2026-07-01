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
  }
}

export interface RequestOptions extends RequestInit {
  retries?: number
  skipAuth?: boolean
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
      setAuthToken(null)
      window.location.hash = '#/login'
      throw new Error('Unauthorized')
    }

    if (response.status === 403) {
      throw new Error('Forbidden')
    }

    if (RETRY_STATUS_CODES.includes(response.status) && retries > 0) {
      await new Promise(resolve => setTimeout(resolve, RETRY_DELAY_MS * (MAX_RETRIES - retries + 1)))
      return doRequest(url, options, retries - 1)
    }

    if (!response.ok) {
      const text = await response.text().catch(() => response.statusText)
      throw new Error(`HTTP ${response.status}: ${text}`)
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
    setAuthToken(null)
    window.location.hash = '#/login'
    throw new Error('Unauthorized')
  }

  if (!response.ok) {
    const text = await response.text().catch(() => response.statusText)
    throw new Error(`HTTP ${response.status}: ${text}`)
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
    throw new Error(`HTTP ${response.status}`)
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
