<template>
  <div class="logs-view">
    <div class="view-header">
      <h2>Logs</h2>
      <div class="header-actions">
        <n-select
          v-model:value="levelFilter"
          :options="levelOptions"
          placeholder="Filter by level"
          clearable
          style="width: 150px"
        />
        <n-button @click="loadLogs" :loading="loading">Refresh</n-button>
        <n-button @click="clearLogs">Clear</n-button>
      </div>
    </div>

    <div class="logs-container">
      <div
        v-for="log in filteredLogs"
        :key="log.id"
        class="log-entry"
        :class="`log-${log.level}`"
      >
        <span class="log-time">{{ log.time }}</span>
        <span class="log-level">{{ log.level.toUpperCase() }}</span>
        <span class="log-message">{{ log.message }}</span>
      </div>

      <div v-if="filteredLogs.length === 0" class="empty-state">
        <div class="empty-icon">📜</div>
        <p>No logs available</p>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { NSelect, NButton, useMessage } from 'naive-ui'
import { apiService, Log } from '../api'

const message = useMessage()
const logs = ref<Log[]>([])
const levelFilter = ref<string | null>(null)
const loading = ref(false)

const levelOptions = [
  { label: 'Info', value: 'info' },
  { label: 'Warning', value: 'warning' },
  { label: 'Error', value: 'error' },
  { label: 'Debug', value: 'debug' },
]

const filteredLogs = computed(() => {
  if (!levelFilter.value) return logs.value
  return logs.value.filter(log => log.level === levelFilter.value)
})

let refreshInterval: number | null = null

onMounted(() => {
  loadLogs()
  // Auto-refresh every 5 seconds
  refreshInterval = window.setInterval(loadLogs, 5000)
})

onUnmounted(() => {
  if (refreshInterval) {
    clearInterval(refreshInterval)
  }
})

async function loadLogs() {
  loading.value = true
  try {
    const params = levelFilter.value ? { level: levelFilter.value } : undefined
    const response = await apiService.logs.list(params)
    logs.value = response.data.reverse()
  } catch (err) {
    message.error('Failed to load logs')
  } finally {
    loading.value = false
  }
}

async function clearLogs() {
  if (!confirm('Clear all logs?')) return
  
  try {
    await apiService.logs.clear()
    logs.value = []
    message.success('Logs cleared')
  } catch (err) {
    message.error('Failed to clear logs')
  }
}
</script>

<style scoped>
.logs-view {
  padding: 20px;
  height: 100%;
  display: flex;
  flex-direction: column;
}

.view-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
}

.view-header h2 {
  margin: 0;
}

.header-actions {
  display: flex;
  gap: 8px;
}

.logs-container {
  flex: 1;
  overflow-y: auto;
  background: var(--bg-secondary);
  border-radius: 12px;
  padding: 12px;
  font-family: 'Monaco', 'Menlo', monospace;
  font-size: 13px;
}

.log-entry {
  display: flex;
  gap: 12px;
  padding: 8px 0;
  border-bottom: 1px solid var(--border-color);
}

.log-entry:last-child {
  border-bottom: none;
}

.log-time {
  color: var(--text-secondary);
  flex-shrink: 0;
}

.log-level {
  flex-shrink: 0;
  width: 60px;
  font-weight: 600;
}

.log-info .log-level { color: #3b82f6; }
.log-warning .log-level { color: #f59e0b; }
.log-error .log-level { color: #ef4444; }
.log-debug .log-level { color: #6b7280; }

.log-message {
  flex: 1;
  word-break: break-word;
}

.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 200px;
  color: var(--text-secondary);
}

.empty-icon {
  font-size: 48px;
  margin-bottom: 16px;
}
</style>
