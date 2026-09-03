<template>
  <div>
    <n-space justify="space-between" style="margin-bottom: 16px;">
      <h2>{{ t('logs.title') }}</h2>
      <n-space>
        <n-select v-model:value="logLevel" :options="levelOptions" style="width: 120px;" clearable :placeholder="t('common.all')" />
        <n-button @click="loadLogs" :loading="loading">{{ t('common.refresh') }}</n-button>
        <n-button :type="logsStore.streaming ? 'error' : 'primary'" @click="toggleStreaming">
          {{ logsStore.streaming ? t('logs.stopStream') : t('logs.startStream') }}
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
        <n-empty v-if="displayLines.length === 0 && !loading" :description="t('logs.noLogs')" />
      </div>
    </n-spin>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useLogsStore } from '@/stores/logs'

const { t } = useI18n()
const logsStore = useLogsStore()

const loading = ref(false)
const error = ref<string | null>(null)
const logLevel = ref<string | null>(null)
const logContainer = ref<HTMLDivElement>()
let abortController: AbortController | null = null

const levelOptions = [
  { label: 'DEBUG', value: 'debug' },
  { label: 'INFO', value: 'info' },
  { label: 'WARN', value: 'warn' },
  { label: 'ERROR', value: 'error' },
]

function formatLogLine(line: { timestamp: string; level: string; message: string; source: string }): string {
  const level = (line.level || 'info').toUpperCase()
  const source = line.source ? ` ${line.source}` : ''
  return `[${line.timestamp}] [${level}]${source} ${line.message}`
}

const displayLines = computed(() => logsStore.logs.map(formatLogLine))

const filteredLines = computed(() => {
  if (!logLevel.value) return displayLines.value
  const levelUpper = logLevel.value.toUpperCase()
  return displayLines.value.filter(line => line.includes(`[${levelUpper}]`))
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
    await logsStore.loadLogs(200)
    scrollToBottom()
  } catch {
    error.value = t('logs.failedToLoad')
  } finally {
    loading.value = false
  }
}

async function toggleStreaming(): Promise<void> {
  if (logsStore.streaming) {
    stopStreaming()
  } else {
    await startStreaming()
  }
}

async function startStreaming(): Promise<void> {
  if (abortController) return
  error.value = null
  try {
    abortController = await logsStore.startStreaming()
  } catch (e) {
    const errMsg = e instanceof Error ? e.message : String(e)
    error.value = errMsg
  }
}

function stopStreaming(): void {
  if (abortController) {
    logsStore.stopStreaming(abortController)
    abortController = null
  }
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
