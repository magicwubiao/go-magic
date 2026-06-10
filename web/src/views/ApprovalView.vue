<template>
  <div>
    <n-space justify="space-between" style="margin-bottom: 16px;">
      <h2>{{ t('approval.title') }}</h2>
      <n-space>
        <n-badge :value="pendingApprovals.length" :max="99">
          <n-button @click="loadAll" :loading="loading">{{ t('common.refresh') }}</n-button>
        </n-badge>
      </n-space>
    </n-space>

    <n-alert v-if="error" type="error" style="margin-bottom: 16px;" closable @close="error = null">
      {{ error }}
    </n-alert>

    <n-spin :show="loading">
      <n-tabs v-model:value="activeTab" type="line" animated>
        <!-- Tab 1: Dashboard -->
        <n-tab-pane name="dashboard" :tab="t('approval.tabs.dashboard')">
          <n-grid :cols="4" :x-gap="16" style="margin-bottom: 24px;">
            <n-gi>
              <n-card size="small">
                <n-statistic :label="t('approval.stats.totalRequests')" :value="stats.totalRequests" />
              </n-card>
            </n-gi>
            <n-gi>
              <n-card size="small">
                <n-statistic :label="t('approval.stats.autoApproved')">
                  <template #default>
                    <n-text type="success">{{ formatPercent(stats.autoApproved, stats.totalRequests) }}</n-text>
                  </template>
                </n-statistic>
              </n-card>
            </n-gi>
            <n-gi>
              <n-card size="small">
                <n-statistic :label="t('approval.stats.userApproved')">
                  <template #default>
                    <n-text type="info">{{ formatPercent(stats.userApproved, stats.totalRequests) }}</n-text>
                  </template>
                </n-statistic>
              </n-card>
            </n-gi>
            <n-gi>
              <n-card size="small">
                <n-statistic :label="t('approval.stats.userDenied')">
                  <template #default>
                    <n-text type="error">{{ formatPercent(stats.userDenied, stats.totalRequests) }}</n-text>
                  </template>
                </n-statistic>
              </n-card>
            </n-gi>
          </n-grid>

          <n-empty v-if="stats.totalRequests === 0" :description="t('approval.stats.noData')" style="margin-bottom: 24px;" />

          <template v-if="stats.totalRequests > 0">
            <n-card size="small" :title="t('approval.stats.riskDistribution')" style="margin-bottom: 24px;">
              <n-space vertical>
                <div v-for="level in riskLevels" :key="level.key">
                  <n-space align="center" justify="space-between" style="margin-bottom: 4px;">
                    <n-tag :type="riskTagType(level.key)" size="small">{{ t(`approval.riskLevels.${level.key}`) }}</n-tag>
                    <n-text depth="3" style="font-size: 12px;">{{ getRiskCount(level.key) }}</n-text>
                  </n-space>
                  <n-progress
                    type="line"
                    :percentage="getRiskPercent(level.key)"
                    :color="riskColor(level.key)"
                    :show-indicator="false"
                    :height="8"
                    :border-radius="4"
                  />
                </div>
              </n-space>
            </n-card>

            <n-card v-if="stats.topCommands?.length" size="small" :title="t('approval.stats.topCommands')">
              <n-data-table
                :columns="topCommandColumns"
                :data="stats.topCommands || []"
                :bordered="false"
                size="small"
              />
            </n-card>
          </template>
        </n-tab-pane>

        <!-- Tab 2: History -->
        <n-tab-pane name="history" :tab="t('approval.tabs.history')">
          <n-space justify="space-between" style="margin-bottom: 16px;">
            <n-text depth="3">{{ historyRecords.length ? `${historyRecords.length} ${t('approval.history.records')}` : t('approval.history.noRecords') }}</n-text>
            <n-popconfirm @positive-click="handleClearHistory">
              <template #trigger>
                <n-button type="error" size="small" :disabled="!historyRecords.length">{{ t('approval.history.clearHistory') }}</n-button>
              </template>
              {{ t('approval.history.clearConfirm') }}
            </n-popconfirm>
          </n-space>
          <n-data-table
            v-if="historyRecords.length"
            :columns="historyColumns"
            :data="historyRecords"
            :bordered="false"
            size="small"
            :pagination="historyPagination"
            :row-key="(row: any) => row.id"
          />
          <n-empty v-else :description="t('approval.history.noRecords')" />
        </n-tab-pane>

        <!-- Tab 3: Patterns -->
        <n-tab-pane name="patterns" :tab="t('approval.tabs.patterns')">
          <h4 style="margin-bottom: 12px;">{{ t('approval.patterns.trusted') }}</h4>
          <n-data-table
            v-if="trustedCommands.length"
            :columns="trustedColumns"
            :data="trustedCommands"
            :bordered="false"
            size="small"
            style="margin-bottom: 24px;"
          />
          <n-empty v-else :description="t('approval.patterns.noTrusted')" style="margin-bottom: 24px;" />

          <h4 style="margin-bottom: 12px;">{{ t('approval.patterns.denied') }}</h4>
          <n-data-table
            v-if="deniedCommands.length"
            :columns="deniedColumns"
            :data="deniedCommands"
            :bordered="false"
            size="small"
          />
          <n-empty v-else :description="t('approval.patterns.noDenied')" />
        </n-tab-pane>

        <!-- Tab 4: Pending -->
        <n-tab-pane name="pending" :tab="pendingTabTitle">
          <template v-if="pendingApprovals.length > 0">
            <n-space justify="end" style="margin-bottom: 12px;">
              <n-button size="small" type="warning" @click="handleBatchResolve(true)">{{ t('approval.pending.batchApprove') }}</n-button>
              <n-button size="small" type="error" @click="handleBatchResolve(false)">{{ t('approval.pending.batchDeny') }}</n-button>
            </n-space>
            <n-space vertical>
              <n-alert v-for="item in pendingApprovals" :key="item.id" type="warning" :bordered="true" style="padding: 12px 16px;">
                <n-space vertical style="width: 100%;">
                  <n-space align="center" justify="space-between">
                    <n-space align="center">
                      <n-text strong>{{ t('approval.pending.command') }}</n-text>
                      <n-tag :type="riskTagType(item.riskLevel || 'medium')" size="small">
                        {{ t(`approval.riskLevels.${riskLevelKey(item.riskLevel || 'medium')}`) }}
                      </n-tag>
                    </n-space>
                    <n-text depth="3" style="font-size: 12px;">{{ formatTime(item.createdAt) }}</n-text>
                  </n-space>
                  <n-text code style="word-break: break-all; font-size: 13px;">{{ item.command }}</n-text>
                  <n-space v-if="item.args?.length" vertical style="margin-top: 4px;">
                    <n-text depth="3" style="font-size: 12px;">{{ t('approval.pending.arguments') }}:</n-text>
                    <n-text v-for="(arg, idx) in item.args" :key="idx" code depth="3" style="font-size: 12px; word-break: break-all;">
                      {{ arg }}
                    </n-text>
                  </n-space>
                  <n-space v-if="item.workingDir" style="margin-top: 4px;">
                    <n-text depth="3" style="font-size: 12px;">{{ t('approval.pending.workingDir') }}: {{ item.workingDir }}</n-text>
                  </n-space>
                  <n-space justify="end" style="margin-top: 8px;">
                    <n-button type="error" size="small" @click="handleResolve(item.id, false)">{{ t('approval.pending.deny') }}</n-button>
                    <n-button type="primary" size="small" @click="handleResolve(item.id, true)">{{ t('approval.pending.approve') }}</n-button>
                  </n-space>
                </n-space>
              </n-alert>
            </n-space>
          </template>
          <n-empty v-else :description="t('approval.pending.noPending')" />
        </n-tab-pane>
      </n-tabs>
    </n-spin>

    <!-- Deny Reason Modal -->
    <n-modal v-model:show="showDenyModal" preset="dialog" :title="t('approval.pending.denyReason')">
      <n-input
        v-model:value="denyReason"
        type="textarea"
        :rows="3"
        :placeholder="t('approval.pending.denyReasonPlaceholder')"
      />
      <template #action>
        <n-space justify="end">
          <n-button @click="showDenyModal = false">{{ t('common.cancel') }}</n-button>
          <n-button type="error" @click="confirmDeny">{{ t('approval.pending.deny') }}</n-button>
        </n-space>
      </template>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, h, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { NButton, NSpace, NTag, useMessage } from 'naive-ui'
import { request } from '@/api/client'

const { t } = useI18n()
const message = useMessage()

// Loading & error
const loading = ref(false)
const error = ref<string | null>(null)
const activeTab = ref('dashboard')

// Stats
interface ApprovalStats {
  totalRequests: number
  autoApproved: number
  userApproved: number
  userDenied: number
  riskDistribution: Record<string, number>
  topCommands: { command: string; count: number; riskLevel: string }[]
}

const stats = ref<ApprovalStats>({
  totalRequests: 0,
  autoApproved: 0,
  userApproved: 0,
  userDenied: 0,
  riskDistribution: {},
  topCommands: [],
})

// History
interface HistoryRecord {
  id: string
  command: string
  normalized?: string
  riskLevel: string
  decision: string
  strategy: string
  duration_ms: number
  workingDir?: string
  timestamp: string
}

const historyRecords = ref<HistoryRecord[]>([])
const historyPagination = ref({ pageSize: 20 })

// Patterns
interface TrustedCommand {
  pattern: string
  count: number
  lastSeen: string
}

interface DeniedCommand {
  pattern: string
  count: number
}

const trustedCommands = ref<TrustedCommand[]>([])
const deniedCommands = ref<DeniedCommand[]>([])

// Pending
interface PendingApproval {
  id: string
  command: string
  args?: string[]
  workingDir?: string
  riskLevel: string
  createdAt: string
}

const pendingApprovals = ref<PendingApproval[]>([])

// Deny modal
const showDenyModal = ref(false)
const denyReason = ref('')
let denyCallback: ((reason: string) => void) | null = null

// Auto-switch to pending tab when there are pending items
watch(() => pendingApprovals.value.length, (newLen) => {
  if (newLen > 0 && activeTab.value !== 'pending') {
    activeTab.value = 'pending'
  }
})

// Pending tab title with badge
const pendingTabTitle = computed(() => {
  const count = pendingApprovals.value.length
  return count > 0 ? `${t('approval.tabs.pending')} (${count})` : t('approval.tabs.pending')
})

// Risk levels
const riskLevels = [
  { key: 'low' },
  { key: 'medium' },
  { key: 'high' },
  { key: 'critical' },
]

function riskColor(level: string): string {
  const colors: Record<string, string> = {
    low: '#18a058',
    medium: '#f0a020',
    high: '#d03050',
    critical: '#7b2ff2',
    '1': '#18a058',
    '2': '#f0a020',
    '3': '#d03050',
    '4': '#7b2ff2',
  }
  return colors[level] || '#2080f0'
}

function riskTagType(level: string): 'success' | 'warning' | 'error' | 'info' {
  const map: Record<string, 'success' | 'warning' | 'error' | 'info'> = {
    low: 'success',
    medium: 'warning',
    high: 'error',
    critical: 'error',
    '1': 'success',
    '2': 'warning',
    '3': 'error',
    '4': 'error',
  }
  return map[level] || 'info'
}

function riskLevelKey(level: string | number): string {
  const map: Record<string, string> = {
    low: 'low', medium: 'medium', high: 'high', critical: 'critical',
    '1': 'low', '2': 'medium', '3': 'high', '4': 'critical',
  }
  return map[String(level)] || 'medium'
}

function getRiskCount(level: string): number {
  return stats.value?.riskDistribution?.[level] || 0
}

function getRiskPercent(level: string): number {
  const total = stats.value?.totalRequests || 1
  const count = getRiskCount(level)
  return Math.round((count / total) * 100)
}

function formatPercent(value: number, total: number): string {
  if (!total) return '0%'
  return Math.round((value / total) * 100) + '%'
}

function formatTime(ts: string): string {
  if (!ts) return '-'
  try {
    const d = new Date(ts)
    return d.toLocaleString()
  } catch {
    return ts
  }
}

function truncate(str: string, len: number): string {
  if (!str) return ''
  return str.length > len ? str.substring(0, len) + '...' : str
}

// Top commands columns
const topCommandColumns = computed(() => [
  {
    title: '#',
    key: 'index',
    width: 40,
    render: (_row: any, index: number) => index + 1,
  },
  {
    title: t('approval.history.command'),
    key: 'command',
    ellipsis: { tooltip: true },
  },
  {
    title: t('approval.patterns.count'),
    key: 'count',
    width: 80,
  },
  {
    title: t('approval.history.riskLevel'),
    key: 'riskLevel',
    width: 100,
    render: (row: any) => h(NTag, { type: riskTagType(row.riskLevel || 'medium'), size: 'small' }, { default: () => t(`approval.riskLevels.${riskLevelKey(row.riskLevel || 'medium')}`) }),
  },
])

// History columns
const historyColumns = computed(() => [
  {
    title: t('approval.history.timestamp'),
    key: 'timestamp',
    width: 180,
    render: (row: HistoryRecord) => formatTime(row.timestamp),
  },
  {
    title: t('approval.history.command'),
    key: 'command',
    ellipsis: { tooltip: true },
    render: (row: HistoryRecord) => h('span', { style: 'font-family: monospace; font-size: 12px;' }, truncate(row.command, 60)),
  },
  {
    title: t('approval.history.riskLevel'),
    key: 'riskLevel',
    width: 80,
    render: (row: HistoryRecord) => h(NTag, { type: riskTagType(row.riskLevel || 'medium'), size: 'small' }, { default: () => t(`approval.riskLevels.${riskLevelKey(row.riskLevel || 'medium')}`) }),
  },
  {
    title: t('approval.history.decision'),
    key: 'decision',
    width: 100,
    render: (row: HistoryRecord) => {
      const typeMap: Record<string, 'success' | 'error' | 'warning' | 'info'> = {
        approved: 'success',
        auto_approved: 'success',
        denied: 'error',
        timeout: 'warning',
      }
      return h(NTag, { type: typeMap[row.decision] || 'info', size: 'small' }, { default: () => t(`approval.decisions.${row.decision}`) })
    },
  },
  {
    title: t('approval.history.strategy'),
    key: 'strategy',
    width: 80,
  },
  {
    title: t('approval.history.duration'),
    key: 'duration',
    width: 80,
    render: (row: HistoryRecord) => row.duration_ms ? `${row.duration_ms}ms` : '-',
  },
])

// Trusted columns
const trustedColumns = computed(() => [
  {
    title: t('approval.patterns.pattern'),
    key: 'pattern',
    ellipsis: { tooltip: true },
  },
  {
    title: t('approval.patterns.count'),
    key: 'count',
    width: 80,
  },
  {
    title: t('approval.patterns.lastSeen'),
    key: 'lastSeen',
    width: 180,
    render: (row: TrustedCommand) => formatTime(row.lastSeen),
  },
  {
    title: t('approval.patterns.actions'),
    key: 'actions',
    width: 120,
    render: (row: TrustedCommand) => h(
      NButton,
      { size: 'small', type: 'warning', onClick: () => handleRemoveTrust(row.pattern) },
      { default: () => t('approval.patterns.removeTrust') }
    ),
  },
])

// Denied columns
const deniedColumns = computed(() => [
  {
    title: t('approval.patterns.pattern'),
    key: 'pattern',
    ellipsis: { tooltip: true },
  },
  {
    title: t('approval.patterns.count'),
    key: 'count',
    width: 80,
  },
  {
    title: t('approval.patterns.actions'),
    key: 'actions',
    width: 120,
    render: (row: DeniedCommand) => h(
      NButton,
      { size: 'small', type: 'warning', onClick: () => handleClearDenial(row.pattern) },
      { default: () => t('approval.patterns.clearDenial') }
    ),
  },
])

// API functions
async function fetchApprovalHistory(limit: number, offset: number): Promise<{ records: HistoryRecord[]; total: number }> {
  return request(`/approval/history?limit=${limit}&offset=${offset}`)
}

async function fetchApprovalStats(): Promise<ApprovalStats> {
  const raw = await request<{
    total_requests: number
    auto_approved: number
    user_approved: number
    user_denied: number
    by_risk_level: Record<string, number>
    top_commands: { pattern: string; count: number }[]
  }>('/approval/stats')
  return {
    totalRequests: raw.total_requests || 0,
    autoApproved: raw.auto_approved || 0,
    userApproved: raw.user_approved || 0,
    userDenied: raw.user_denied || 0,
    riskDistribution: raw.by_risk_level || {},
    topCommands: (raw.top_commands || []).map(c => ({
      command: c.pattern,
      count: c.count,
      riskLevel: c.risk_level || 'medium',
    })),
  }
}

async function fetchPendingApprovals(): Promise<PendingApproval[]> {
  return request('/approval/pending')
}

async function resolvePendingApproval(id: string, approved: boolean, reason: string): Promise<void> {
  return request(`/approval/pending/${id}/resolve`, {
    method: 'POST',
    body: JSON.stringify({ approved, reason }),
  })
}

async function fetchTrustedCommands(): Promise<TrustedCommand[]> {
  const result = await request<{ patterns: Array<{ pattern: string; count: number; last_seen: string }>; total: number }>('/approval/patterns/trusted')
  return (result.patterns || []).map(p => ({
    pattern: p.pattern,
    count: p.count,
    lastSeen: p.last_seen,
  }))
}

async function fetchDeniedCommands(): Promise<DeniedCommand[]> {
  const result = await request<{ patterns: Array<{ pattern: string; count: number }>; total: number }>('/approval/patterns/denied')
  return (result.patterns || []).map(p => ({
    pattern: p.pattern,
    count: p.count,
  }))
}

async function clearHistory(olderThanHours: number): Promise<void> {
  return request('/approval/clear-history', {
    method: 'POST',
    body: JSON.stringify({ older_than_hours: olderThanHours }),
  })
}

// Handlers
async function loadAll(): Promise<void> {
  loading.value = true
  error.value = null
  try {
    await Promise.all([
      loadStats(),
      loadHistory(),
      loadPatterns(),
      loadPending(),
    ])
  } catch (e) {
    error.value = t('approval.failedToLoad')
  } finally {
    loading.value = false
  }
}

async function loadStats(): Promise<void> {
  try {
    stats.value = await fetchApprovalStats()
  } catch {
    // silent
  }
}

async function loadHistory(): Promise<void> {
  try {
    const result = await fetchApprovalHistory(100, 0)
    historyRecords.value = result.records || []
    historyPagination.value = { ...historyPagination.value, itemCount: result.total || 0 }
  } catch {
    // silent
  }
}

async function loadPatterns(): Promise<void> {
  try {
    const [trusted, denied] = await Promise.all([
      fetchTrustedCommands(),
      fetchDeniedCommands(),
    ])
    trustedCommands.value = trusted || []
    deniedCommands.value = denied || []
  } catch {
    // silent
  }
}

let pendingLoading = false

async function loadPending(): Promise<void> {
  if (pendingLoading) return
  pendingLoading = true
  try {
    pendingApprovals.value = await fetchPendingApprovals()
  } catch {
    // silent
  } finally {
    pendingLoading = false
  }
}

async function handleClearHistory(): Promise<void> {
  pausePendingPoll()
  loading.value = true
  try {
    await clearHistory(168) // 7 days
    message.success(t('approval.history.cleared'))
    await loadHistory()
    await loadStats()
  } catch {
    message.error(t('approval.history.clearFailed'))
  } finally {
    loading.value = false
    resumePendingPoll()
  }
}

async function handleRemoveTrust(pattern: string): Promise<void> {
  pausePendingPoll()
  loading.value = true
  try {
    await request('/approval/patterns/trusted', {
      method: 'DELETE',
      body: JSON.stringify({ pattern }),
    })
    await loadPatterns()
    await loadStats()
  } catch {
    message.error(t('approval.failedToLoad'))
  } finally {
    loading.value = false
    resumePendingPoll()
  }
}

async function handleClearDenial(pattern: string): Promise<void> {
  pausePendingPoll()
  loading.value = true
  try {
    await request('/approval/patterns/denied', {
      method: 'DELETE',
      body: JSON.stringify({ pattern }),
    })
    await loadPatterns()
    await loadStats()
  } catch {
    message.error(t('approval.failedToLoad'))
  } finally {
    loading.value = false
    resumePendingPoll()
  }
}

async function handleResolve(id: string, approved: boolean): Promise<void> {
  if (approved) {
    try {
      await resolvePendingApproval(id, true, 'approved')
      message.success(t('approval.pending.resolved'))
      await loadPending()
      await loadStats()
    } catch {
      message.error(t('approval.pending.resolveFailed'))
    }
  } else {
    denyReason.value = ''
    denyCallback = async (reason: string) => {
      try {
        await resolvePendingApproval(id, false, reason || 'denied')
        message.success(t('approval.pending.resolved'))
        await loadPending()
        await loadStats()
      } catch {
        message.error(t('approval.pending.resolveFailed'))
      }
    }
    showDenyModal.value = true
  }
}

async function confirmDeny(): Promise<void> {
  showDenyModal.value = false
  if (denyCallback) {
    await denyCallback(denyReason.value)
    denyCallback = null
  }
}

async function handleBatchResolve(approved: boolean): Promise<void> {
  if (approved) {
    try {
      for (const item of pendingApprovals.value) {
        await resolvePendingApproval(item.id, true, 'batch approved')
      }
      message.success(t('approval.pending.batchResolved'))
      await loadPending()
      await loadStats()
    } catch {
      message.error(t('approval.pending.resolveFailed'))
    }
  } else {
    denyReason.value = ''
    denyCallback = async (reason: string) => {
      try {
        for (const item of pendingApprovals.value) {
          await resolvePendingApproval(item.id, false, reason || 'batch denied')
        }
        message.success(t('approval.pending.batchResolved'))
        await loadPending()
        await loadStats()
      } catch {
        message.error(t('approval.pending.resolveFailed'))
      }
    }
    showDenyModal.value = true
  }
}

let pendingPollTimer: ReturnType<typeof setInterval> | null = null
let pendingPollPaused = false

function pausePendingPoll() {
  pendingPollPaused = true
}

function resumePendingPoll() {
  pendingPollPaused = false
}

onMounted(() => {
  loadAll()
  pendingPollTimer = setInterval(() => {
    if (!pendingPollPaused) {
      loadPending()
    }
  }, 5000)
})

onUnmounted(() => {
  if (pendingPollTimer) {
    clearInterval(pendingPollTimer)
    pendingPollTimer = null
  }
})
</script>
