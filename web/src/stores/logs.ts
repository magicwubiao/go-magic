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
  // Track active abort controllers per stream invocation, so multiple
  // components can stream independently without interfering with each other.
  const controllers = new Set<AbortController>()

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

  function handleLogLine(rawData: string): void {
    try {
      const data = JSON.parse(rawData) as { line?: string; message?: string; level?: string; timestamp?: string }
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

  // Each call returns an independent AbortController so multiple components
  // can stream concurrently; stopping one does not affect the others.
  async function startStreaming(): Promise<AbortController> {
    streaming.value = true
    error.value = null
    const controller = await logsApi.streamLogs(
      (rawData: string) => {
        handleLogLine(rawData)
      },
      (e: Error) => {
        error.value = { message: e.message }
        controllers.delete(controller)
        if (controllers.size === 0) {
          streaming.value = false
        }
      },
    )
    controllers.add(controller)
    return controller
  }

  function stopStreaming(controller?: AbortController): void {
    if (controller) {
      controller.abort()
      controllers.delete(controller)
    } else {
      // Stop all active streams (e.g. on full cleanup)
      controllers.forEach((c) => c.abort())
      controllers.clear()
    }
    if (controllers.size === 0) {
      streaming.value = false
    }
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
