import { request } from './client'

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

export function streamLogs(): EventSource {
  return new EventSource('/api/logs/tail')
}
