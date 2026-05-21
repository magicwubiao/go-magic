import { defineStore } from 'pinia'
import { ref } from 'vue'
import * as logsApi from '@/api/logs'

export interface LogLine {
  timestamp: string
  level: string
  message: string
  source: string
}

export interface LogsError {
  message: string
  code?: string
}

export const useLogsStore = defineStore('logs', () => {
  const logs = ref<LogLine[]>([])
  const streaming = ref(false)
  const loading = ref(false)
  const error = ref<LogsError | null>(null)
  let eventSource: EventSource | null = null

  function parseLogLine(line: string): LogLine {
    // Try to parse format: [2026-05-19 14:42:52] [INFO] source message
    const match = line.match(/\[(\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2})\]\s+\[(\w+)\]\s*(.*)/)
    if (match) {
      const rest = match[3] || ''
      const spaceIdx = rest.indexOf(' ')
      if (spaceIdx > 0) {
        return {
          timestamp: match[1],
          level: match[2].toLowerCase(),
          source: rest.substring(0, spaceIdx),
          message: rest.substring(spaceIdx + 1),
        }
      }
      return {
        timestamp: match[1],
        level: match[2].toLowerCase(),
        source: '',
        message: rest,
      }
    }
    // Fallback: treat entire line as message
    return {
      timestamp: new Date().toISOString(),
      level: 'info',
      source: '',
      message: line,
    }
  }

  async function loadLogs(limit = 100): Promise<void> {
    loading.value = true
    error.value = null
    try {
      const res = await logsApi.getLogs(limit)
      logs.value = (res.lines || []).map(parseLogLine)
    } catch (e) {
      const errMsg = e instanceof Error ? e.message : 'Unknown error'
      error.value = { message: 'Failed to load logs: ' + errMsg }
      logs.value = []
    } finally {
      loading.value = false
    }
  }

  function startStreaming(): void {
    // Prevent multiple EventSource instances
    if (eventSource) {
      return
    }
    streaming.value = true
    error.value = null
    eventSource = logsApi.streamLogs()
    eventSource.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data) as { line?: string; message?: string; level?: string; timestamp?: string }
        if (typeof data === 'string') {
          logs.value.unshift(parseLogLine(data))
        } else if (data.line) {
          logs.value.unshift(parseLogLine(data.line))
        } else if (data.message) {
          logs.value.unshift({
            timestamp: data.timestamp || new Date().toISOString(),
            level: (data.level || 'info').toLowerCase(),
            source: '',
            message: data.message,
          })
        }
        if (logs.value.length > 1000) {
          logs.value = logs.value.slice(0, 1000)
        }
      } catch {
        // ignore parse errors
      }
    }
    eventSource.onerror = () => {
      streaming.value = false
      if (eventSource) {
        eventSource.close()
        eventSource = null
      }
    }
  }

  function stopStreaming(): void {
    if (eventSource) {
      eventSource.close()
      eventSource = null
    }
    streaming.value = false
  }

  function cleanup(): void {
    stopStreaming()
  }

  return {
    logs,
    streaming,
    loading,
    error,
    loadLogs,
    startStreaming,
    stopStreaming,
    cleanup,
  }
})
