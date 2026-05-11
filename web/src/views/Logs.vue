<template>
  <div class="logs-view">
    <n-card title="Logs">
      <template #header-extra>
        <n-space>
          <n-select
            v-model:value="selectedLogFile"
            :options="logFileOptions"
            style="width: 200px"
            placeholder="Select log file"
          />
          <n-select
            v-model:value="selectedLevel"
            :options="levelOptions"
            style="width: 120px"
            placeholder="Log level"
          />
          <n-button size="small" @click="refreshLogs">
            <template #icon>
              <n-icon :component="Refresh" />
            </template>
          </n-button>
          <n-button size="small" @click="downloadLogs">
            <template #icon>
              <n-icon :component="Download" />
            </template>
            Download
          </n-button>
        </n-space>
      </template>

      <!-- Search -->
      <n-input
        v-model:value="searchQuery"
        placeholder="Search logs... (regex supported)"
        clearable
        style="margin-bottom: 12px"
      >
        <template #prefix>
          <n-icon :component="Search" />
        </template>
      </n-input>

      <!-- Logs Table -->
      <n-data-table
        :columns="columns"
        :data="filteredLogs"
        :pagination="pagination"
        :bordered="false"
        :row-key="(row: LogEntry) => row.id"
        :max-height="600"
        virtual-scroll
      />

      <!-- Log Detail Modal -->
      <n-modal v-model:show="showDetail" preset="card" :title="`Log Detail - ${selectedLog?.timestamp}`" style="width: 800px">
        <n-descriptions v-if="selectedLog" :column="1" label-placement="top">
          <n-descriptions-item label="Timestamp">
            {{ selectedLog.timestamp }}
          </n-descriptions-item>
          <n-descriptions-item label="Level">
            <n-tag :type="getLevelType(selectedLog.level)" size="small">
              {{ selectedLog.level.toUpperCase() }}
            </n-tag>
          </n-descriptions-item>
          <n-descriptions-item label="Source">
            {{ selectedLog.source }}
          </n-descriptions-item>
          <n-descriptions-item label="Message">
            <n-pre>{{ selectedLog.message }}</n-pre>
          </n-descriptions-item>
          <n-descriptions-item v-if="selectedLog.stack" label="Stack Trace">
            <n-pre class="stack-trace">{{ selectedLog.stack }}</n-pre>
          </n-descriptions-item>
          <n-descriptions-item v-if="selectedLog.metadata" label="Metadata">
            <n-code :code="JSON.stringify(selectedLog.metadata, null, 2)" language="json" />
          </n-descriptions-item>
        </n-descriptions>
      </n-modal>
    </n-card>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import {
  NCard,
  NButton,
  NIcon,
  NSpace,
  NSelect,
  NInput,
  NDataTable,
  NTag,
  NModal,
  NDescriptions,
  NDescriptionsItem,
  NPre,
  NCode,
  NText,
} from 'naive-ui'
import {
  Refresh,
  Download,
  Search,
  InformationCircle,
  Warning,
  AlertCircle,
  CloseCircle,
} from '@vicons/ionicons5'

interface LogEntry {
  id: string
  timestamp: string
  level: 'debug' | 'info' | 'warn' | 'error'
  source: string
  message: string
  stack?: string
  metadata?: Record<string, any>
}

const logs = ref<LogEntry[]>([])
const selectedLogFile = ref('all')
const selectedLevel = ref<string | null>(null)
const searchQuery = ref('')
const selectedLog = ref<LogEntry | null>(null)
const showDetail = ref(false)
const autoRefresh = ref(true)

const pagination = reactive({
  page: 1,
  pageSize: 50,
  showSizePicker: true,
  pageSizes: [20, 50, 100, 200],
})

const logFileOptions = [
  { label: 'All Logs', value: 'all' },
  { label: 'Agent Logs', value: 'agent' },
  { label: 'Gateway Logs', value: 'gateway' },
  { label: 'Error Logs', value: 'error' },
  { label: 'Access Logs', value: 'access' },
]

const levelOptions = [
  { label: 'All Levels', value: null },
  { label: 'Debug', value: 'debug' },
  { label: 'Info', value: 'info' },
  { label: 'Warning', value: 'warn' },
  { label: 'Error', value: 'error' },
]

const columns = [
  {
    title: 'Time',
    key: 'timestamp',
    width: 180,
    render: (row: LogEntry) => formatTimestamp(row.timestamp),
  },
  {
    title: 'Level',
    key: 'level',
    width: 80,
    render: (row: LogEntry) =>
      h(
        NTag,
        { type: getLevelType(row.level), size: 'small' },
        { default: () => row.level.toUpperCase() }
      ),
  },
  {
    title: 'Source',
    key: 'source',
    width: 120,
  },
  {
    title: 'Message',
    key: 'message',
    ellipsis: { tooltip: true },
    render: (row: LogEntry) => highlightSearch(row.message),
  },
  {
    title: 'Action',
    key: 'actions',
    width: 80,
    render: (row: LogEntry) =>
      h(
        NButton,
        {
          size: 'tiny',
          quaternary: true,
          onClick: () => {
            selectedLog.value = row
            showDetail.value = true
          },
        },
        { default: () => 'View' }
      ),
  },
]

const filteredLogs = computed(() => {
  let result = logs.value

  // Filter by log file
  if (selectedLogFile.value !== 'all') {
    result = result.filter((log) => log.source === selectedLogFile.value)
  }

  // Filter by level
  if (selectedLevel.value) {
    const levelIndex = ['debug', 'info', 'warn', 'error']
    const selectedIndex = levelIndex.indexOf(selectedLevel.value)
    const minIndex = selectedIndex
    result = result.filter((log) => levelIndex.indexOf(log.level) >= minIndex)
  }

  // Search filter
  if (searchQuery.value) {
    try {
      const regex = new RegExp(searchQuery.value, 'i')
      result = result.filter(
        (log) =>
          regex.test(log.message) ||
          regex.test(log.source) ||
          (log.stack && regex.test(log.stack))
      )
    } catch {
      // Invalid regex, use simple string search
      const query = searchQuery.value.toLowerCase()
      result = result.filter(
        (log) =>
          log.message.toLowerCase().includes(query) ||
          log.source.toLowerCase().includes(query)
      )
    }
  }

  return result
})

function getLevelType(level: string): 'default' | 'info' | 'warning' | 'error' {
  switch (level) {
    case 'debug':
      return 'default'
    case 'info':
      return 'info'
    case 'warn':
      return 'warning'
    case 'error':
      return 'error'
    default:
      return 'default'
  }
}

function formatTimestamp(timestamp: string): string {
  try {
    const date = new Date(timestamp)
    return date.toLocaleTimeString('en-US', {
      hour12: false,
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    })
  } catch {
    return timestamp
  }
}

function highlightSearch(text: string): any {
  if (!searchQuery.value) return text

  try {
    const regex = new RegExp(`(${searchQuery.value})`, 'gi')
    const parts = text.split(regex)

    return h('span', {}, [
      parts.map((part, i) =>
        regex.test(part)
          ? h('mark', { style: { background: 'yellow' } }, part)
          : part
      ),
    ])
  } catch {
    return text
  }
}

async function loadLogs() {
  try {
    const params = new URLSearchParams()
    if (selectedLogFile.value !== 'all') {
      params.set('file', selectedLogFile.value)
    }
    if (selectedLevel.value) {
      params.set('level', selectedLevel.value)
    }

    const res = await fetch(`/api/logs?${params}`)
    if (res.ok) {
      logs.value = await res.json()
    }
  } catch (e) {
    console.error('Failed to load logs:', e)
  }
}

async function refreshLogs() {
  await loadLogs()
}

function downloadLogs() {
  const content = filteredLogs.value
    .map(
      (log) =>
        `${log.timestamp} [${log.level.toUpperCase()}] [${log.source}] ${log.message}${
          log.stack ? '\n' + log.stack : ''
        }`
    )
    .join('\n')

  const blob = new Blob([content], { type: 'text/plain' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `logs-${new Date().toISOString().split('T')[0]}.txt`
  a.click()
  URL.revokeObjectURL(url)
}

// Auto-refresh
let refreshInterval: number | null = null

onMounted(() => {
  loadLogs()
  if (autoRefresh.value) {
    refreshInterval = window.setInterval(loadLogs, 5000)
  }
})

onUnmounted(() => {
  if (refreshInterval) {
    clearInterval(refreshInterval)
  }
})
</script>

<style lang="scss" scoped>
.logs-view {
  padding: 16px;
}

.stack-trace {
  background: #f5f5f5;
  padding: 12px;
  border-radius: 4px;
  overflow-x: auto;
  white-space: pre-wrap;
  font-size: 12px;
}
</style>
