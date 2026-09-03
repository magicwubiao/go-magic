import { request, getAuthToken } from './client'

export interface LogEntry {
  timestamp: string
  level: string
  message: string
  source: string
}

export interface LogsResponse {
  file: string
  lines: string[]
}

export async function getLogs(limit = 100): Promise<LogsResponse> {
  return request(`/logs?limit=${limit}`)
}

// 使用 fetch + ReadableStream 模拟 SSE，支持通过 Authorization header 传递 token，
// 避免 EventSource 不支持自定义 header 导致的无鉴权问题。
export async function streamLogs(
  onMessage: (line: string) => void,
  onError?: (e: Error) => void,
): Promise<AbortController> {
  const controller = new AbortController()
  const token = getAuthToken()

  fetch('/api/logs/tail', {
    method: 'GET',
    headers: {
      'Authorization': token ? `Bearer ${token}` : '',
      'Accept': 'text/event-stream',
    },
    signal: controller.signal,
  })
    .then(async (response) => {
      if (!response.ok || !response.body) {
        throw new Error(`HTTP ${response.status}`)
      }
      const reader = response.body.getReader()
      const decoder = new TextDecoder()
      let buffer = ''
      while (true) {
        const { done, value } = await reader.read()
        if (done) break
        buffer += decoder.decode(value, { stream: true })
        const lines = buffer.split('\n')
        buffer = lines.pop() || ''
        for (const line of lines) {
          if (line.startsWith('data: ')) {
            onMessage(line.slice(6))
          }
        }
      }
    })
    .catch((e: unknown) => {
      if (e instanceof Error && e.name !== 'AbortError' && onError) {
        onError(e)
      }
    })

  return controller
}
