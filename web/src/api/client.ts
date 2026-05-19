const BASE_URL = '/api'

export function getAuthToken(): string | null {
  return localStorage.getItem('auth_token')
}

export async function request<T>(path: string, options: RequestInit = {}): Promise<T> {
  const url = `${BASE_URL}${path}`
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...options.headers as Record<string, string>,
  }

  // Attach auth token if available
  const token = getAuthToken()
  if (token) {
    headers['Authorization'] = `Bearer ${token}`
  }

  const response = await fetch(url, {
    ...options,
    headers,
  })

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

  return response.json()
}
