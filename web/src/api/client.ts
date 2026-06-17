const BASE_URL = '/api'

export function getAuthToken(): string | null {
  return localStorage.getItem('auth_token')
}

export async function request<T>(path: string, options: RequestInit = {}): Promise<T> {
  const url = `${BASE_URL}${path}`
  const headers: Record<string, string> = {
    ...options.headers as Record<string, string>,
  }

  // Only set Content-Type for JSON requests with body, not for FormData or bodyless requests
  const isFormData = options.body instanceof FormData
  const hasBody = options.body !== undefined && options.body !== null
  if (!isFormData && hasBody) {
    headers['Content-Type'] = 'application/json'
  }

  // Attach auth token if available
  const token = getAuthToken()
  if (token) {
    headers['Authorization'] = `Bearer ${token}`
  }

  const controller = new AbortController()
  const timeoutId = setTimeout(() => controller.abort(), 30000) // 30s timeout

  const response = await fetch(url, {
    ...options,
    headers,
    signal: controller.signal,
  }).finally(() => clearTimeout(timeoutId))

  if (response.status === 401) {
    // Clear invalid token and redirect to login
    localStorage.removeItem('auth_token')
    window.location.hash = '#/login'
    throw new Error('Unauthorized')
  }

  if (!response.ok) {
    const text = await response.text().catch(() => response.statusText)
    throw new Error(`HTTP ${response.status}: ${text}`)
  }

  // Handle empty responses (e.g., DELETE)
  const contentLength = response.headers.get('content-length')
  if (contentLength === '0' || response.status === 204) {
    return undefined as T
  }

  const text = await response.text()
  if (!text) {
    return undefined as T
  }
  return JSON.parse(text) as T
}
