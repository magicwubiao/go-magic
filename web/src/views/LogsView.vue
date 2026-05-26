<template>
  <div>
    <n-space justify="space-between" style="margin-bottom: 16px;">
      <h2>{{ t('logs.title') }}</h2>
      <n-space>
        <n-select v-model:value="logLevel" :options="levelOptions" style="width: 120px;" clearable :placeholder="t('common.all')" />
        <n-button @click="loadLogs" :loading="loading">{{ t('common.refresh') }}</n-button>
        <n-button :type="streaming ? 'error' : 'primary'" @click="toggleStreaming">
          {{ streaming ? t('logs.stopStream') : t('logs.startStream') }}
        </n-button>
      </n-space>
    </n-space>

    <n-alert v-if="error" type="error" style="margin-bottom: 16px;" closable @close="error = null">
      {{ error }}
    </n-alert>

    <n-spin :show="loading">
      <div
        ref="logContainer"
        style="height: 600px; overflow-y: auto; font-family: 'Consolas', 'Monaco', monospace; font-size: 13px; background: #f8f8f8; color: #333; padding: 12px; border-radius: 4px;"
      >
        <div
          v-for="(line, index) in filteredLines"
          :key="index"
          style="white-space: pre-wrap; word-break: break-all; padding: 2px 0;"
          :style="getLineStyle(line)"
        >
          {{ line }}
        </div>
        <n-empty v-if="lines.length === 0 && !loading" :description="t('logs.noLogs')" />
      </div>
    </n-spin>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { request } from '@/api/client'

const { t } = useI18n()

const lines = ref<string[]>([])
const loading = ref(false)
const streaming = ref(false)
const error = ref<string | null>(null)
const logLevel = ref<string | null>(null)
const logContainer = ref<HTMLDivElement>()
let eventSource: EventSource | null = null

const levelOptions = [
  { label: 'DEBUG', value: 'debug' },
  { label: 'INFO', value: 'info' },
  { label: 'WARN', value: 'warn' },
  { label: 'ERROR', value: 'error' },
]

const filteredLines = computed(() => {
  if (!logLevel.value) return lines.value
  const levelUpper = logLevel.value.toUpperCase()
  return lines.value.filter(line => line.includes(`[${levelUpper}]`))
})

function getLineStyle(line: string): Record<string, string> {
  if (line.includes('[ERROR]')) return { color: '#dc2626' }
  if (line.includes('[WARN]')) return { color: '#d97706' }
  if (line.includes('[INFO]')) return { color: '#2563eb' }
  if (line.includes('[DEBUG]')) return { color: '#6b7280' }
  return {}
}

async function loadLogs(): Promise<void> {
  loading.value = true
  error.value = null
  try {
    const res = await request<{ file: string; lines: string[] }>('/logs?limit=200')
    lines.value = res.lines || []
    scrollToBottom()
  } catch (e) {
    error.value = t('logs.failedToLoad')
    lines.value = []
  } finally {
    loading.value = false
  }
}

function toggleStreaming(): void {
  if (streaming.value) {
    stopStreaming()
  } else {
    startStreaming()
  }
}

function startStreaming(): void {
  if (eventSource) return
  streaming.value = true
  error.value = null
  eventSource = new EventSource('/api/logs/tail')
  eventSource.onmessage = (event) => {
    try {
      const data = JSON.parse(event.data)
      if (typeof data === 'string') {
        lines.value.push(data)
      } else if (data.line) {
        lines.value.push(data.line)
      } else if (data.message) {
        const line = `[${data.timestamp || new Date().toISOString()}] [${(data.level || 'info').toUpperCase()}] ${data.message}`
        lines.value.push(line)
      }
      if (lines.value.length > 1000) {
        lines.value = lines.value.slice(-1000)
      }
      scrollToBottom()
    } catch {
      // ignore
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

function scrollToBottom(): void {
  setTimeout(() => {
    if (logContainer.value) {
      logContainer.value.scrollTop = logContainer.value.scrollHeight
    }
  }, 10)
}

onMounted(() => {
  loadLogs()
})

onUnmounted(() => {
  stopStreaming()
})
</script>
