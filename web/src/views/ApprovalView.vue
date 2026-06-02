<template>
  <div>
    <n-space justify="space-between" style="margin-bottom: 16px;">
      <h2>{{ t('approval.title') }}</h2>
      <n-button @click="loadAll" :loading="loading">{{ t('common.refresh') }}</n-button>
    </n-space>

    <n-alert v-if="error" type="error" style="margin-bottom: 16px;" closable @close="error = null">
      {{ error }}
    </n-alert>

    <n-spin :show="loading">
      <n-tabs type="line" animated>
        <!-- Tab 1: Dashboard -->
        <n-tab-pane :name="'dashboard'" :tab="t('approval.tabs.dashboard')">
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
                    {{ formatPercent(stats.autoApproved, stats.totalRequests) }}
                  </template>
                </n-statistic>
              </n-card>
            </n-gi>
            <n-gi>
              <n-card size="small">
                <n-statistic :label="t('approval.stats.userApproved')">
                  <template #default>
                    {{ formatPercent(stats.userApproved, stats.totalRequests) }}
                  </template>
                </n-statistic>
              </n-card>
            </n-gi>
            <n-gi>
              <n-card size="small">
                <n-statistic :label="t('approval.stats.userDenied')">
                  <template #default>
                    {{ formatPercent(stats.userDenied, stats.totalRequests) }}
                  </template>
                </n-statistic>
              </n-card>
            </n-gi>
          </n-grid>

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

          <n-card size="small" :title="t('approval.stats.topCommands')">
            <n-data-table
              :columns="topCommandColumns"
              :data="stats.topCommands || []"
              :bordered="false"
              size="small"
            />
          </n-card>
        </n-tab-pane>

        <!-- Tab 2: History -->
        <n-tab-pane :name="'history'" :tab="t('approval.tabs.history')">
          <n-space justify="space-between" style="margin-bottom: 16px;">
            <n-text depth="3">{{ t('approval.history.noRecords') }}</n-text>
            <n-popconfirm @positive-click="handleClearHistory">
              <template #trigger>
                <n-button type="error" size="small">{{ t('approval.history.clearHistory') }}</n-button>
              </template>
              {{ t('approval.history.clearConfirm') }}
            </n-popconfirm>
          </n-space>
          <n-data-table
            :columns="historyColumns"
            :data="historyRecords"
            :bordered="false"
            size="small"
            :pagination="historyPagination"
            :row-key="(row: any) => row.id"
          />
        </n-tab-pane>

        <!-- Tab 3: Patterns -->
        <n-tab-pane :name="'patterns'" :tab="t('approval.tabs.patterns')">
          <h4 style="margin-bottom: 12px;">{{ t('approval.patterns.trusted') }}</h4>
          <n-data-table
            :columns="trustedColumns"
            :data="trustedCommands"
            :bordered="false"
            size="small"
            style="margin-bottom: 24px;"
          />
          <n-empty v-if="!trustedCommands.length" :description="t('approval.patterns.noTrusted')" style="margin-bottom: 24px;" />

          <h4 style="margin-bottom: 12px;">{{ t('approval.patterns.denied') }}</h4>
          <n-data-table
            :columns="deniedColumns"
            :data="deniedCommands"
            :bordered="false"
            size="small"
          />
          <n-empty v-if="!deniedCommands.length" :description="t('approval.patterns.noDenied')" />
        </n-tab-pane>

        <!-- Tab 4: Settings -->
        <n-tab-pane :name="'settings'" :tab="t('approval.tabs.settings')">
          <n-card size="small">
            <n-form label-placement="left" label-width="140">
              <n-form-item :label="t('approval.settings.strategy')">
                <n-select
                  v-model:value="settings.strategy"
                  :options="strategyOptions"
                  style="width: 320px;"
                  @update:value="handleStrategyChange"
                />
              </n-form-item>
              <n-form-item :label="t('approval.settings.trustThreshold')">
                <n-input-number
                  v-model:value="settings.trustThreshold"
                  :min="1"
                  :max="100"
                  style="width: 160px;"
                  @update:value="handleSettingsChange"
                />
              </n-form-item>
              <n-form-item :label="t('approval.settings.enableLearning')">
                <n-switch v-model:value="settings.enableLearning" @update:value="handleSettingsChange" />
              </n-form-item>
              <n-form-item :label="t('approval.settings.cliConfirm')">
                <n-switch v-model:value="settings.cliConfirm" @update:value="handleSettingsChange" />
              </n-form-item>
            </n-form>
          </n-card>

          <n-card size="small" :title="t('approval.settings.whitelist')" style="margin-top: 16px;">
            <n-space style="margin-bottom: 12px; flex-wrap: wrap;">
              <n-tag
                v-for="pattern in settings.whitelist"
                :key="pattern"
                closable
                @close="handleRemoveWhitelist(pattern)"
                type="info"
                size="medium"
              >
                {{ pattern }}
              </n-tag>
              <n-text v-if="!settings.whitelist.length" depth="3">{{ t('common.noData') }}</n-text>
            </n-space>
            <n-space>
              <n-input
                v-model:value="newWhitelistPattern"
                :placeholder="t('approval.settings.whitelistPlaceholder')"
                style="width: 300px;"
                @keyup.enter="handleAddWhitelist"
              />
              <n-button type="primary" @click="handleAddWhitelist">{{ t('approval.settings.addWhitelist') }}</n-button>
            </n-space>
          </n-card>
        </n-tab-pane>

        <!-- Tab 5: Pending (only shown when there are pending items) -->
        <n-tab-pane v-if="pendingApprovals.length > 0" :name="'pending'" :tab="t('approval.tabs.pending')">
          <n-space vertical>
            <n-card v-for="item in pendingApprovals" :key="item.id" size="small">
              <n-space vertical>
                <n-space align="center" justify="space-between">
                  <n-text strong>{{ t('approval.pending.command') }}</n-text>
                  <n-tag :type="riskTagType(item.riskLevel)" size="small">
                    {{ t(`approval.riskLevels.${item.riskLevel}`) }}
                  </n-tag>
                </n-space>
                <n-text code style="word-break: break-all;">{{ item.command }}</n-text>
                <n-text depth="3" style="font-size: 12px;">{{ t('approval.pending.createdAt') }}: {{ formatTime(item.createdAt) }}</n-text>
                <n-space justify="end">
                  <n-button type="error" size="small" @click="handleResolve(item.id, false)">{{ t('approval.pending.deny') }}</n-button>
                  <n-button type="primary" size="small" @click="handleResolve(item.id, true)">{{ t('approval.pending.approve') }}</n-button>
                </n-space>
              </n-space>
            </n-card>
          </n-space>
          <n-empty v-if="!pendingApprovals.length" :description="t('approval.pending.noPending')" />
        </n-tab-pane>
      </n-tabs>
    </n-spin>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, h } from 'vue'
import { useI18n } from 'vue-i18n'
import { NButton, NSpace, NTag, useMessage } from 'naive-ui'

const { t } = useI18n()
const message = useMessage()

const API_BASE = '/api/approval'

// Loading & error
const loading = ref(false)
const error = ref<string | null>(null)

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
  riskLevel: string
  decision: string
  strategy: string
  duration: number
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

// Settings
interface ApprovalSettings {
  strategy: string
  trustThreshold: number
  enableLearning: boolean
  cliConfirm: boolean
  whitelist: string[]
}

const settings = ref<ApprovalSettings>({
  strategy: 'smart',
  trustThreshold: 5,
  enableLearning: true,
  cliConfirm: true,
  whitelist: [],
})

const newWhitelistPattern = ref('')

// Pending
interface PendingApproval {
  id: string
  command: string
  riskLevel: string
  createdAt: string
}

const pendingApprovals = ref<PendingApproval[]>([])

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
  }
  return colors[level] || '#2080f0'
}

function riskTagType(level: string): 'success' | 'warning' | 'error' | 'info' {
  const map: Record<string, 'success' | 'warning' | 'error' | 'info'> = {
    low: 'success',
    medium: 'warning',
    high: 'error',
    critical: 'error',
  }
  return map[level] || 'info'
}

function getRiskCount(level: string): number {
  return stats.value.riskDistribution[level] || 0
}

function getRiskPercent(level: string): number {
  const total = stats.value.totalRequests || 1
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

// Strategy options
const strategyOptions = computed(() => [
  { label: t('approval.settings.strategies.manual'), value: 'manual' },
  { label: t('approval.settings.strategies.auto'), value: 'auto' },
  { label: t('approval.settings.strategies.smart'), value: 'smart' },
  { label: t('approval.settings.strategies.whitelist'), value: 'whitelist' },
])

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
    render: (row: any) => h(NTag, { type: riskTagType(row.riskLevel), size: 'small' }, { default: () => t(`approval.riskLevels.${row.riskLevel}`) }),
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
    render: (row: HistoryRecord) => h(NTag, { type: riskTagType(row.riskLevel), size: 'small' }, { default: () => t(`approval.riskLevels.${row.riskLevel}`) }),
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
    render: (row: HistoryRecord) => row.duration ? `${row.duration}ms` : '-',
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
async function fetchApprovalStatus(): Promise<ApprovalSettings> {
  const res = await fetch(`${API_BASE}/status`)
  if (!res.ok) throw new Error('Failed to fetch approval status')
  return res.json()
}

async function fetchApprovalHistory(limit: number, offset: number): Promise<{ records: HistoryRecord[]; total: number }> {
  const res = await fetch(`${API_BASE}/history?limit=${limit}&offset=${offset}`)
  if (!res.ok) throw new Error('Failed to fetch approval history')
  return res.json()
}

async function fetchApprovalStats(): Promise<ApprovalStats> {
  const res = await fetch(`${API_BASE}/stats`)
  if (!res.ok) throw new Error('Failed to fetch approval stats')
  return res.json()
}

async function fetchPendingApprovals(): Promise<PendingApproval[]> {
  const res = await fetch(`${API_BASE}/pending`)
  if (!res.ok) throw new Error('Failed to fetch pending approvals')
  return res.json()
}

async function resolvePendingApproval(id: string, approved: boolean, reason: string): Promise<void> {
  const res = await fetch(`${API_BASE}/pending/${id}/resolve`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ approved, reason }),
  })
  if (!res.ok) throw new Error('Failed to resolve pending approval')
}

async function fetchTrustedCommands(): Promise<TrustedCommand[]> {
  const res = await fetch(`${API_BASE}/patterns/trusted`)
  if (!res.ok) throw new Error('Failed to fetch trusted commands')
  return res.json()
}

async function fetchDeniedCommands(): Promise<DeniedCommand[]> {
  const res = await fetch(`${API_BASE}/patterns/denied`)
  if (!res.ok) throw new Error('Failed to fetch denied commands')
  return res.json()
}

async function addWhitelist(pattern: string): Promise<void> {
  const res = await fetch(`${API_BASE}/whitelist`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ pattern }),
  })
  if (!res.ok) throw new Error('Failed to add whitelist')
}

async function removeWhitelist(pattern: string): Promise<void> {
  const res = await fetch(`${API_BASE}/whitelist`, {
    method: 'DELETE',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ pattern }),
  })
  if (!res.ok) throw new Error('Failed to remove whitelist')
}

async function setStrategy(strategy: string): Promise<void> {
  const res = await fetch(`${API_BASE}/settings`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ strategy }),
  })
  if (!res.ok) throw new Error('Failed to set strategy')
}

async function clearHistory(olderThanHours: number): Promise<void> {
  const res = await fetch(`${API_BASE}/clear-history`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ older_than_hours: olderThanHours }),
  })
  if (!res.ok) throw new Error('Failed to clear history')
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
      loadSettings(),
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

async function loadSettings(): Promise<void> {
  try {
    const status = await fetchApprovalStatus()
    settings.value = {
      strategy: status.strategy || 'smart',
      trustThreshold: status.trustThreshold || 5,
      enableLearning: status.enableLearning !== false,
      cliConfirm: status.cliConfirm !== false,
      whitelist: status.whitelist || [],
    }
  } catch {
    // silent
  }
}

async function loadPending(): Promise<void> {
  try {
    pendingApprovals.value = await fetchPendingApprovals()
  } catch {
    // silent
  }
}

async function handleClearHistory(): Promise<void> {
  try {
    await clearHistory(168) // 7 days
    message.success(t('approval.history.cleared'))
    await loadHistory()
    await loadStats()
  } catch {
    message.error(t('approval.history.clearFailed'))
  }
}

async function handleRemoveTrust(pattern: string): Promise<void> {
  try {
    const res = await fetch(`${API_BASE}/patterns/trusted`, {
      method: 'DELETE',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ pattern }),
    })
    if (!res.ok) throw new Error()
    await loadPatterns()
  } catch {
    // silent
  }
}

async function handleClearDenial(pattern: string): Promise<void> {
  try {
    const res = await fetch(`${API_BASE}/patterns/denied`, {
      method: 'DELETE',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ pattern }),
    })
    if (!res.ok) throw new Error()
    await loadPatterns()
  } catch {
    // silent
  }
}

async function handleStrategyChange(value: string): Promise<void> {
  try {
    await setStrategy(value)
    message.success(t('approval.settings.saved'))
  } catch {
    message.error(t('approval.settings.saveFailed'))
  }
}

async function handleSettingsChange(): Promise<void> {
  try {
    const res = await fetch(`${API_BASE}/settings`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(settings.value),
    })
    if (!res.ok) throw new Error()
    message.success(t('approval.settings.saved'))
  } catch {
    message.error(t('approval.settings.saveFailed'))
  }
}

async function handleAddWhitelist(): Promise<void> {
  const pattern = newWhitelistPattern.value.trim()
  if (!pattern) return
  try {
    await addWhitelist(pattern)
    newWhitelistPattern.value = ''
    settings.value.whitelist.push(pattern)
    message.success(t('approval.settings.saved'))
  } catch {
    message.error(t('approval.settings.saveFailed'))
  }
}

async function handleRemoveWhitelist(pattern: string): Promise<void> {
  try {
    await removeWhitelist(pattern)
    settings.value.whitelist = settings.value.whitelist.filter(p => p !== pattern)
    message.success(t('approval.settings.saved'))
  } catch {
    message.error(t('approval.settings.saveFailed'))
  }
}

async function handleResolve(id: string, approved: boolean): Promise<void> {
  try {
    await resolvePendingApproval(id, approved, approved ? 'approved' : 'denied')
    message.success(t('approval.pending.resolved'))
    await loadPending()
    await loadStats()
  } catch {
    message.error(t('approval.pending.resolveFailed'))
  }
}

onMounted(() => {
  loadAll()
})
</script>
