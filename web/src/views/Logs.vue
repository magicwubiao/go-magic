<template>
  <div class="logs-view">
    <n-space vertical :size="16">
      <n-space justify="space-between" align="center">
        <h2>{{ $t('logs.title') }}</h2>
        <n-space>
          <n-select
            v-model:value="logFile"
            :options="logFiles"
            style="width: 200px"
            placeholder="Select log file"
          />
          <n-select
            v-model:value="logLevel"
            :options="levelOptions"
            style="width: 120px"
          />
          <n-button @click="refreshLogs">
            <template #icon>
              <n-icon :component="Refresh" />
            </template>
          </n-button>
        </n-space>
      </n-space>

      <n-input
        v-model:value="searchQuery"
        :placeholder="$t('logs.search')"
        clearable
      >
        <template #prefix>
          <n-icon :component="Search" />
        </template>
      </n-input>

      <n-data-table
        :columns="columns"
        :data="filteredLogs"
        :pagination="pagination"
        :row-key="(row: any) => row.time"
        max-height="calc(100vh - 300px)"
      />
    </n-space>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, h } from 'vue'
import { NIcon, NTag, NButton } from 'naive-ui'
import { Refresh, Search, Download } from '@vicons/ionicons5'
import { logApi } from '@/api'

const logs = ref<any[]>([])
const searchQuery = ref('')
const logFile = ref('agent')
const logLevel = ref('all')

const logFiles = [
  { label: 'Agent Logs', value: 'agent' },
  { label: 'Gateway Logs', value: 'gateway' },
  { label: 'Error Logs', value: 'error' },
]

const levelOptions = [
  { label: 'All', value: 'all' },
  { label: 'Debug', value: 'debug' },
  { label: 'Info', value: 'info' },
  { label: 'Warning', value: 'warning' },
  { label: 'Error', value: 'error' },
]

const pagination = {
  pageSize: 50,
}

const columns = [
  {
    title: 'Time',
    key: 'time',
    width: 180,
  },
  {
    title: 'Level',
    key: 'level',
    width: 100,
    render: (row: any) => {
      const typeMap: Record<string, any> = {
        debug: 'default',
        info: 'info',
        warning: 'warning',
        error: 'error',
      }
      return h(NTag, { type: typeMap[row.level], size: 'small' }, () => row.level)
    },
  },
  {
    title: 'Message',
    key: 'message',
    ellipsis: true,
  },
  {
    title: 'Actions',
    key: 'actions',
    width: 80,
    render: (row: any) => {
      return h(
        NButton,
        { size: 'small', quaternary: true, onClick: () => downloadLog(row) },
        () => h(NIcon, () => h(Download))
      )
    },
  },
]

const filteredLogs = computed(() => {
  let result = logs.value

  if (logLevel.value !== 'all') {
    result = result.filter((l) => l.level === logLevel.value)
  }

  if (searchQuery.value) {
    const query = searchQuery.value.toLowerCase()
    result = result.filter(
      (l) =>
        l.message.toLowerCase().includes(query) ||
        l.level.toLowerCase().includes(query)
    )
  }

  return result
})

async function loadLogs() {
  try {
    const response = await logApi.list({ file: logFile.value })
    logs.value = response.data
  } catch (e) {
    console.error('Failed to load logs:', e)
  }
}

function refreshLogs() {
  loadLogs()
}

function downloadLog(log: any) {
  // Download single log entry
  const text = `[${log.time}] [${log.level}] ${log.message}`
  const blob = new Blob([text], { type: 'text/plain' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `log-${log.time}.txt`
  a.click()
  URL.revokeObjectURL(url)
}

onMounted(() => {
  loadLogs()
})
</script>

<style lang="scss" scoped>
.logs-view {
  h2 {
    margin: 0;
  }
}
</style>
