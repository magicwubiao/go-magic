<template>
  <div class="logs-view">
    <n-card title="Logs">
      <template #header-extra>
        <n-space>
          <n-select v-model:value="logLevel" :options="levelOptions" style="width: 120px" />
          <n-button @click="refreshLogs">Refresh</n-button>
          <n-button @click="downloadLogs">Download</n-button>
        </n-space>
      </template>

      <div class="log-container">
        <div v-for="(log, i) in filteredLogs" :key="i" :class="['log-entry', log.level]">
          <span class="log-time">{{ log.time }}</span>
          <n-tag :type="getLevelType(log.level)" size="small">{{ log.level.toUpperCase() }}</n-tag>
          <span class="log-message">{{ log.message }}</span>
        </div>
      </div>
    </n-card>
  </div>
</template>

<script setup>
import { ref, computed } from 'vue'

const logLevel = ref('all')
const levelOptions = [
  { label: 'All', value: 'all' },
  { label: 'Debug', value: 'debug' },
  { label: 'Info', value: 'info' },
  { label: 'Warn', value: 'warn' },
  { label: 'Error', value: 'error' }
]

const logs = ref([
  { time: '10:30:15', level: 'info', message: 'Application started' },
  { time: '10:30:16', level: 'debug', message: 'Loading configuration from ~/.go-magic' },
  { time: '10:30:17', level: 'info', message: 'Provider initialized: openai' },
  { time: '10:30:18', level: 'warn', message: 'Memory system not configured' },
  { time: '10:31:00', level: 'info', message: 'New session created: abc123' },
  { time: '10:32:30', level: 'error', message: 'API request failed: rate limit exceeded' }
])

const filteredLogs = computed(() => {
  if (logLevel.value === 'all') return logs.value
  return logs.value.filter(l => l.level === logLevel.value)
})

const getLevelType = (level) => {
  const types = { debug: 'default', info: 'info', warn: 'warning', error: 'error' }
  return types[level] || 'default'
}

const refreshLogs = () => console.log('Refresh logs')
const downloadLogs = () => console.log('Download logs')
</script>

<style scoped>
.log-container {
  background: #1a1a2e;
  border-radius: 8px;
  padding: 12px;
  max-height: calc(100vh - 300px);
  overflow-y: auto;
  font-family: 'Monaco', 'Menlo', monospace;
  font-size: 13px;
}

.log-entry {
  display: flex;
  gap: 12px;
  padding: 4px 0;
  border-bottom: 1px solid #2a2a3e;
}

.log-entry:last-child {
  border-bottom: none;
}

.log-time {
  color: #888;
  flex-shrink: 0;
}

.log-message {
  color: #ddd;
}
</style>
