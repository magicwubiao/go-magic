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
      <n-tabs v-model:value="activeTab" type="line" animated>
        <!-- Tab 1: Pending (most important, put first) -->
        <n-tab-pane name="pending" :tab="pendingTabTitle">
          <template v-if="pendingApprovals.length > 0">
            <n-space justify="space-between" align="center" style="margin-bottom: 12px;">
              <n-space align="center">
                <n-text depth="3" style="font-size: 13px;">
                  {{ pendingApprovals.length }} {{ t('approval.pending.items') }}
                </n-text>
                <n-text depth="3" style="font-size: 12px;">
                  {{ t('approval.pending.shortcutHint') }}
                </n-text>
              </n-space>
              <n-space>
                <n-button size="small" type="warning" :loading="batchLoading" @click="handleBatchResolve(true)">
                  {{ t('approval.pending.batchApprove') }}
                </n-button>
                <n-button size="small" type="error" :loading="batchLoading" @click="handleBatchResolve(false)">
                  {{ t('approval.pending.batchDeny') }}
                </n-button>
              </n-space>
            </n-space>
            <n-space vertical>
              <n-alert
                v-for="(item, idx) in pendingApprovals"
                :key="item.id"
                :type="riskAlertType(item.riskLevel)"
                :bordered="true"
                style="padding: 12px 16px;"
              >
                <n-space vertical style="width: 100%;">
                  <n-space align="center" justify="space-between">
                    <n-space align="center">
                      <n-tag strong size="small" round :bordered="false" :type="riskTagType(item.riskLevel)">
                        #{{ idx + 1 }}
                      </n-tag>
                      <n-text strong>{{ t('approval.pending.command') }}</n-text>
                      <n-tag :type="riskTagType(item.riskLevel)" size="small" :bordered="false">
                        {{ t(`approval.riskLevels.${riskLevelKey(item.riskLevel)}`) }}
                      </n-tag>
                    </n-space>
                    <n-space align="center" size="small">
                      <n-text v-if="item.sessionId" depth="3" style="font-size: 12px;" code>
                        {{ t('approval.pending.sessionId') }}: {{ item.sessionId.substring(0, 8) }}
                      </n-text>
                      <n-text depth="3" style="font-size: 12px;">{{ formatTime(item.createdAt) }}</n-text>
                      <n-tag v-if="item.expiresAt" size="tiny" :type="expiryTagType(item.expiresAt)" round :bordered="false">
                        {{ t('approval.pending.expiresIn') }}: {{ formatExpiry(item.expiresAt) }}
                      </n-tag>
                    </n-space>
                  </n-space>
                  <n-text code style="word-break: break-all; font-size: 13px; white-space: pre-wrap;">{{ sanitizeCommand(item.command) }}</n-text>
                  <n-space justify="end" style="margin-top: 8px;">
                    <n-button
                      type="error"
                      size="small"
                      :loading="resolvingId === item.id && lastResolveAction === false"
                      :disabled="resolvingId === item.id && lastResolveAction === true"
                      @click="handleResolve(item.id, false)"
                    >
                      <template #icon><span style="font-weight: bold;">D</span></template>
                      {{ t('approval.pending.deny') }}
                    </n-button>
                    <n-button
                      type="primary"
                      size="small"
                      :loading="resolvingId === item.id && lastResolveAction === true"
                      :disabled="resolvingId === item.id && lastResolveAction === false"
                      @click="handleResolve(item.id, true)"
                    >
                      <template #icon><span style="font-weight: bold;">A</span></template>
                      {{ t('approval.pending.approve') }}
                    </n-button>
                  </n-space>
                </n-space>
              </n-alert>
            </n-space>
          </template>
          <n-empty v-else :description="t('approval.pending.noPending')" />
        </n-tab-pane>

        <!-- Tab 2: Dashboard -->
        <n-tab-pane name="dashboard" :tab="t('approval.tabs.dashboard')">
          <n-grid cols="1 s:2 m:4" responsive="screen" :x-gap="16" :y-gap="16" style="margin-bottom: 24px;">
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

        <!-- Tab 3: History -->
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

        <!-- Tab 4: Patterns -->
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

        <!-- Tab 5: Settings -->
        <n-tab-pane name="settings" :tab="t('approval.tabs.settings')">
          <n-spin :show="settingsLoading">
            <n-card size="small" style="margin-bottom: 16px;">
              <n-space vertical>
                <n-form-item :label="t('approval.settings.strategy')">
                  <n-select v-model:value="settingsForm.strategy" :options="strategyOptions" />
                </n-form-item>
                <n-form-item :label="t('approval.settings.trustThreshold')">
                  <n-input-number v-model:value="settingsForm.trustThreshold" :min="0" :max="100" style="width: 100%;" />
                  <template #feedback>
                    <n-text depth="3" style="font-size: 12px;">{{ t('approval.settings.trustThresholdHint') }}</n-text>
                  </template>
                </n-form-item>
                <n-form-item :label="t('approval.settings.enableLearning')">
                  <n-switch v-model:value="settingsForm.enableLearning" />
                  <template #feedback>
                    <n-text depth="3" style="font-size: 12px;">{{ t('approval.settings.enableLearningHint') }}</n-text>
                  </template>
                </n-form-item>
                <n-form-item :label="t('approval.settings.cliConfirm')">
                  <n-switch v-model:value="settingsForm.cliConfirm" />
                  <template #feedback>
                    <n-text depth="3" style="font-size: 12px;">{{ t('approval.settings.cliConfirmHint') }}</n-text>
                  </template>
                </n-form-item>
                <n-space>
                  <n-button type="primary" :loading="settingsSaving" @click="handleSaveSettings">
                    {{ t('common.save') }}
                  </n-button>
                </n-space>
              </n-space>
            </n-card>

            <n-card size="small" :title="t('approval.settings.whitelist')">
              <n-space align="center" style="margin-bottom: 12px;">
                <n-input
                  v-model:value="newWhitelistPattern"
                  :placeholder="t('approval.settings.whitelistPlaceholder')"
                  style="max-width: 400px;"
                  @keyup.enter="handleAddWhitelist"
                />
                <n-button type="primary" size="small" :loading="whitelistAdding" @click="handleAddWhitelist">
                  {{ t('approval.settings.addWhitelist') }}
                </n-button>
              </n-space>
              <n-data-table
                v-if="whitelistRows.length"
                :columns="whitelistColumns"
                :data="whitelistRows"
                :bordered="false"
                size="small"
              />
              <n-empty v-else :description="t('approval.settings.whitelistEmpty')" />
            </n-card>
          </n-spin>
        </n-tab-pane>
      </n-tabs>
    </n-spin>

    <!-- Deny Reason Modal -->
    <n-modal v-model:show="showDenyModal" preset="dialog" :title="denyModalTitle" :mask-closable="false" @after-leave="onDenyModalClose">
      <n-input
        v-model:value="denyReason"
        type="textarea"
        :rows="3"
        :placeholder="t('approval.pending.denyReasonPlaceholder')"
        :disabled="denySubmitting"
      />
      <template #action>
        <n-space justify="end">
          <n-button :disabled="denySubmitting" @click="cancelDeny">{{ t('common.cancel') }}</n-button>
          <n-button type="error" :loading="denySubmitting" @click="confirmDeny">{{ t('approval.pending.deny') }}</n-button>
        </n-space>
      </template>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, h, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { NButton, NSpace, NTag, useMessage, useNotification } from 'naive-ui'
import {
  getApprovalHistory,
  getApprovalStats,
  getPendingApprovals,
  resolvePendingApproval,
  getTrustedPatterns,
  getDeniedPatterns,
  removeTrustedPattern,
  clearDeniedPattern,
  getWhitelist,
  addWhitelist,
  removeWhitelist,
  getSettings,
  saveSettings,
  clearHistory,
  type ApprovalHistoryRecord,
  type ApprovalStats,
  type PendingApproval,
  type TrustedPattern,
  type DeniedPattern,
  type ApprovalSettings,
} from '@/api/approval'

const { t } = useI18n()
const message = useMessage()
const notification = useNotification()

// Loading & error
const loading = ref(false)
const error = ref<string | null>(null)
const activeTab = ref('pending')

// Stats
const stats = ref<ApprovalStats>({
  totalRequests: 0,
  autoApproved: 0,
  userApproved: 0,
  userDenied: 0,
  riskDistribution: {},
  topCommands: [],
})

// History
const historyRecords = ref<ApprovalHistoryRecord[]>([])
const historyPagination = ref({ pageSize: 20 })

// Patterns
const trustedCommands = ref<TrustedPattern[]>([])
const deniedCommands = ref<DeniedPattern[]>([])

// Pending
const pendingApprovals = ref<PendingApproval[]>([])

// Settings
const settingsForm = ref<ApprovalSettings>({
  strategy: 'manual',
  trustThreshold: 5,
  enableLearning: false,
  cliConfirm: true,
  whitelist: [],
})
const newWhitelistPattern = ref('')
const whitelistAdding = ref(false)
const settingsLoading = ref(false)
const settingsSaving = ref(false)
let settingsLoaded = false

// Deny modal
const showDenyModal = ref(false)
const denyReason = ref('')
const denySubmitting = ref(false)
const denyIsBatch = ref(false)
let denyCallback: ((reason: string) => Promise<void>) | null = null

// Per-item resolving state (prevents double clicks)
const resolvingId = ref<string | null>(null)
const lastResolveAction = ref<boolean | null>(null)
const batchLoading = ref(false)

// Track previous pending count to detect new arrivals (for notification)
let prevPendingCount = 0
let lastNotifiedPendingKey: string | null = null

// Track previous pending length for auto-switch tab logic
let prevPendingLen = 0

// Force re-render tick for expiry countdowns
const nowTick = ref(Date.now())

// Auto-switch to pending tab only when count goes from 0 to >0 (avoid interrupting user)
watch(() => pendingApprovals.value.length, (newLen) => {
  if (newLen > 0 && prevPendingLen === 0 && activeTab.value !== 'pending' && activeTab.value !== 'dashboard') {
    activeTab.value = 'pending'
  }
  prevPendingLen = newLen
})

// Lazy load settings when settings tab is first opened
watch(activeTab, (newTab) => {
  if (newTab === 'settings' && !settingsLoaded) {
    settingsLoaded = true
    loadSettings()
  }
})

// Pending tab title with badge
const pendingTabTitle = computed(() => {
  const count = pendingApprovals.value.length
  return count > 0 ? `${t('approval.tabs.pending')} (${count})` : t('approval.tabs.pending')
})

// Deny modal title
const denyModalTitle = computed(() => {
  return denyIsBatch.value ? t('approval.pending.batchDeny') : t('approval.pending.denyReason')
})

// Strategy options for settings dropdown
const strategyOptions = computed(() => [
  { label: t('approval.settings.strategies.manual'), value: 'manual' },
  { label: t('approval.settings.strategies.auto'), value: 'auto' },
  { label: t('approval.settings.strategies.smart'), value: 'smart' },
  { label: t('approval.settings.strategies.whitelist'), value: 'whitelist' },
])

// Whitelist rows for data table
const whitelistRows = computed(() => settingsForm.value.whitelist.map(p => ({ pattern: p })))

// Risk levels
const riskLevels = [
  { key: 'low' },
  { key: 'medium' },
  { key: 'high' },
  { key: 'critical' },
]

// Risk weight for sorting (higher = more dangerous = first)
function riskWeight(level: string): number {
  const map: Record<string, number> = {
    critical: 4, high: 3, medium: 2, low: 1,
    '4': 4, '3': 3, '2': 2, '1': 1, '0': 1,
  }
  return map[String(level)] || 0
}

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

function riskAlertType(level: string): 'success' | 'warning' | 'error' | 'info' {
  const w = riskWeight(level)
  if (w >= 3) return 'error'
  if (w === 2) return 'warning'
  if (w === 1) return 'info'
  return 'info'
}

function riskLevelKey(level: string | number): string {
  const map: Record<string, string> = {
    low: 'low', medium: 'medium', high: 'high', critical: 'critical',
    '0': 'low', '1': 'low', '2': 'medium', '3': 'high', '4': 'critical',
  }
  return map[String(level)] || 'low'
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

function formatExpiry(expiresAt: string): string {
  if (!expiresAt) return '-'
  // 引用 nowTick 让 computed 每 5 秒重新计算
  void nowTick.value
  try {
    const ms = new Date(expiresAt).getTime() - Date.now()
    if (ms <= 0) return t('approval.pending.expired')
    const s = Math.floor(ms / 1000)
    if (s < 60) return `${s}s`
    const m = Math.floor(s / 60)
    const rem = s % 60
    return `${m}m ${rem}s`
  } catch {
    return '-'
  }
}

function expiryTagType(expiresAt: string): 'error' | 'warning' | 'success' {
  void nowTick.value
  try {
    const ms = new Date(expiresAt).getTime() - Date.now()
    if (ms <= 0) return 'error'
    if (ms < 30_000) return 'warning' // < 30s
    return 'success'
  } catch {
    return 'success'
  }
}

function truncate(str: string, len: number): string {
  if (!str) return ''
  return str.length > len ? str.substring(0, len) + '...' : str
}

// 脱敏命令中的敏感信息（token、密钥等），避免在 UI 中泄露
function sanitizeCommand(cmd: string): string {
  if (!cmd) return ''
  let result = cmd
  // Bearer token: Authorization: Bearer xxx
  result = result.replace(/Bearer\s+[\w.\-]+/gi, 'Bearer ****')
  // API key/secret/password 赋值: api_key=xxx, token: "xxx", secret=yyy 等
  result = result.replace(/(api[_-]?key|token|secret|password|passwd)(\s*[=:]\s*)["']?[\w.\-]+["']?/gi, '$1$2****')
  // AWS access key id
  result = result.replace(/AKIA[0-9A-Z]{16}/g, 'AKIA****')
  // PEM 私钥块
  result = result.replace(/-----BEGIN[\s\S]*?-----END[A-Z\s]+KEY-----/g, '[REDACTED KEY]')
  return result
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
    render: (row: any) => sanitizeCommand(row.command || ''),
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
    render: (row: any) => h(NTag, { type: riskTagType(row.riskLevel || 'low'), size: 'small' }, { default: () => t(`approval.riskLevels.${riskLevelKey(row.riskLevel || 'low')}`) }),
  },
])

// History columns
const historyColumns = computed(() => [
  {
    title: t('approval.history.timestamp'),
    key: 'timestamp',
    width: 180,
    render: (row: ApprovalHistoryRecord) => formatTime(row.timestamp),
  },
  {
    title: t('approval.history.command'),
    key: 'command',
    ellipsis: { tooltip: true },
    render: (row: ApprovalHistoryRecord) => h('span', { style: 'font-family: monospace; font-size: 12px;' }, truncate(sanitizeCommand(row.command), 60)),
  },
  {
    title: t('approval.history.riskLevel'),
    key: 'riskLevel',
    width: 80,
    render: (row: ApprovalHistoryRecord) => h(NTag, { type: riskTagType(row.riskLevel || 'low'), size: 'small' }, { default: () => t(`approval.riskLevels.${riskLevelKey(row.riskLevel || 'low')}`) }),
  },
  {
    title: t('approval.history.decision'),
    key: 'decision',
    width: 100,
    render: (row: ApprovalHistoryRecord) => {
      const typeMap: Record<string, 'success' | 'error' | 'warning' | 'info'> = {
        approved: 'success',
        auto_approved: 'success',
        denied: 'error',
        timeout: 'warning',
      }
      // 如果 i18n key 不存在，fallback 显示原始值
      const key = `approval.decisions.${row.decision}`
      const translated = t(key)
      const text = translated === key ? row.decision : translated
      return h(NTag, { type: typeMap[row.decision] || 'info', size: 'small' }, { default: () => text })
    },
  },
  {
    title: t('approval.history.strategy'),
    key: 'strategy',
    width: 100,
    render: (row: ApprovalHistoryRecord) => {
      // 翻译策略，key 不存在时 fallback 显示原始值
      const key = `approval.settings.strategies.${row.strategy}`
      const translated = t(key)
      return translated === key ? row.strategy : translated
    },
  },
  {
    title: t('approval.history.duration'),
    key: 'duration',
    width: 80,
    render: (row: ApprovalHistoryRecord) => row.durationMs ? `${row.durationMs}ms` : '-',
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
    title: t('approval.history.riskLevel'),
    key: 'riskLevel',
    width: 80,
    render: (row: TrustedPattern) => h(NTag, { type: riskTagType(row.riskLevel || 'low'), size: 'small' }, { default: () => t(`approval.riskLevels.${riskLevelKey(row.riskLevel || 'low')}`) }),
  },
  {
    title: t('approval.patterns.lastSeen'),
    key: 'lastSeen',
    width: 180,
    render: (row: TrustedPattern) => formatTime(row.lastSeen),
  },
  {
    title: t('approval.patterns.actions'),
    key: 'actions',
    width: 120,
    render: (row: TrustedPattern) => h(
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
    title: t('approval.history.riskLevel'),
    key: 'riskLevel',
    width: 80,
    render: (row: DeniedPattern) => h(NTag, { type: riskTagType(row.riskLevel || 'low'), size: 'small' }, { default: () => t(`approval.riskLevels.${riskLevelKey(row.riskLevel || 'low')}`) }),
  },
  {
    title: t('approval.patterns.actions'),
    key: 'actions',
    width: 120,
    render: (row: DeniedPattern) => h(
      NButton,
      { size: 'small', type: 'warning', onClick: () => handleClearDenial(row.pattern) },
      { default: () => t('approval.patterns.clearDenial') }
    ),
  },
])

// Whitelist columns
const whitelistColumns = computed(() => [
  {
    title: t('approval.settings.whitelist'),
    key: 'pattern',
    ellipsis: { tooltip: true },
  },
  {
    title: t('common.actions'),
    key: 'actions',
    width: 100,
    render: (row: { pattern: string }) => h(
      NButton,
      { size: 'small', type: 'error', onClick: () => handleRemoveWhitelist(row.pattern) },
      { default: () => t('common.remove') }
    ),
  },
])

// Handlers
async function loadAll(): Promise<void> {
  loading.value = true
  error.value = null
  // 使用 allSettled 让各数据源独立失败，互不影响
  const results = await Promise.allSettled([
    loadStats(),
    loadHistory(),
    loadPatterns(),
    loadPending(),
  ])
  const failures = results.filter(r => r.status === 'rejected')
  if (failures.length > 0) {
    error.value = t('approval.failedToLoad')
  }
  loading.value = false
}

async function loadStats(): Promise<void> {
  stats.value = await getApprovalStats()
}

async function loadHistory(): Promise<void> {
  const result = await getApprovalHistory(100, 0)
  historyRecords.value = result.records || []
  historyPagination.value = { ...historyPagination.value, itemCount: result.total || 0 }
}

async function loadPatterns(): Promise<void> {
  const [trusted, denied] = await Promise.all([
    getTrustedPatterns(),
    getDeniedPatterns(),
  ])
  trustedCommands.value = trusted || []
  deniedCommands.value = denied || []
}

let pendingLoading = false

async function loadPending(): Promise<void> {
  if (pendingLoading) return
  pendingLoading = true
  try {
    const next = await getPendingApprovals()
    // 按风险权重降序（critical 优先），再按创建时间升序（旧的优先）
    next.sort((a, b) => {
      const wDiff = riskWeight(b.riskLevel) - riskWeight(a.riskLevel)
      if (wDiff !== 0) return wDiff
      const ta = new Date(a.createdAt).getTime() || 0
      const tb = new Date(b.createdAt).getTime() || 0
      return ta - tb
    })
    // 检测新增项以触发通知
    const prevIds = new Set(pendingApprovals.value.map(p => p.id))
    const newItems = next.filter(p => !prevIds.has(p.id))
    if (newItems.length > 0 && pendingApprovals.value.length > 0) {
      notifyNewPending(newItems.length)
    } else if (newItems.length > 0 && prevPendingCount === 0 && lastNotifiedPendingKey !== newItems[0]?.id) {
      notifyNewPending(newItems.length)
      lastNotifiedPendingKey = newItems[0]?.id || null
    }
    prevPendingCount = next.length
    pendingApprovals.value = next
  } finally {
    pendingLoading = false
  }
}

async function loadSettings(): Promise<void> {
  settingsLoading.value = true
  try {
    const [settings, whitelist] = await Promise.all([
      getSettings(),
      getWhitelist(),
    ])
    settingsForm.value = {
      strategy: settings.strategy,
      trustThreshold: settings.trustThreshold,
      enableLearning: settings.enableLearning,
      cliConfirm: settings.cliConfirm,
      whitelist: [...whitelist],
    }
  } catch {
    message.error(t('approval.settings.loadSettingsFailed'))
  } finally {
    settingsLoading.value = false
  }
}

function notifyNewPending(count: number) {
  const title = t('approval.pending.newNotificationTitle')
  const body = t('approval.pending.newNotificationBody', { count })

  // 优先使用浏览器原生通知
  let nativeNotified = false
  if (typeof Notification !== 'undefined' && Notification.permission === 'granted') {
    try {
      new Notification(title, { body })
      nativeNotified = true
    } catch {
      // 原生通知失败则降级
    }
  }

  // 原生通知不可用或未授权时，使用 naive-ui 通知作为备选
  if (!nativeNotified) {
    notification.warning({
      title,
      content: body,
      duration: 3000,
    })
  }

  // 同时显示消息条提示
  message.warning(t('approval.pending.newPending', { count }), { duration: 3000 })
}

async function handleClearHistory(): Promise<void> {
  pausePendingPoll()
  loading.value = true
  try {
    await clearHistory(168) // 7 days
    message.success(t('approval.history.cleared'))
    await Promise.all([loadHistory(), loadStats()])
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
    await removeTrustedPattern(pattern)
    await Promise.all([loadPatterns(), loadStats()])
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
    await clearDeniedPattern(pattern)
    await Promise.all([loadPatterns(), loadStats()])
  } catch {
    message.error(t('approval.failedToLoad'))
  } finally {
    loading.value = false
    resumePendingPoll()
  }
}

async function handleResolve(id: string, approved: boolean): Promise<void> {
  if (resolvingId.value === id) return // prevent double click
  resolvingId.value = id
  lastResolveAction.value = approved
  if (approved) {
    pausePendingPoll()
    try {
      await resolvePendingApproval(id, true, 'approved')
      message.success(t('approval.pending.resolved'))
      // 审批后同时刷新 pending/stats/history/patterns
      await Promise.all([
        loadPending(),
        loadStats(),
        loadHistory(),
        loadPatterns(),
      ])
    } catch {
      message.error(t('approval.pending.resolveFailed'))
    } finally {
      resolvingId.value = null
      lastResolveAction.value = null
      resumePendingPoll()
    }
  } else {
    denyReason.value = ''
    denyIsBatch.value = false
    denyCallback = async (reason: string) => {
      pausePendingPoll()
      try {
        await resolvePendingApproval(id, false, reason || 'denied')
        message.success(t('approval.pending.resolved'))
        await Promise.all([
          loadPending(),
          loadStats(),
          loadHistory(),
          loadPatterns(),
        ])
      } catch {
        message.error(t('approval.pending.resolveFailed'))
      } finally {
        resolvingId.value = null
        lastResolveAction.value = null
        resumePendingPoll()
      }
    }
    showDenyModal.value = true
  }
}

function cancelDeny() {
  showDenyModal.value = false
  resolvingId.value = null
  lastResolveAction.value = null
  denyCallback = null
}

function onDenyModalClose() {
  denyReason.value = ''
  denySubmitting.value = false
}

async function confirmDeny(): Promise<void> {
  if (denySubmitting.value) return
  const cb = denyCallback
  denyCallback = null
  if (!cb) {
    showDenyModal.value = false
    return
  }
  denySubmitting.value = true
  try {
    await cb(denyReason.value)
    showDenyModal.value = false
  } catch {
    denyCallback = cb
  } finally {
    denySubmitting.value = false
  }
}

async function handleBatchResolve(approved: boolean): Promise<void> {
  if (batchLoading.value) return
  if (pendingApprovals.value.length === 0) return
  if (approved) {
    pausePendingPoll()
    batchLoading.value = true
    try {
      for (const item of pendingApprovals.value) {
        await resolvePendingApproval(item.id, true, 'batch approved')
      }
      message.success(t('approval.pending.batchResolved'))
      await Promise.all([
        loadPending(),
        loadStats(),
        loadHistory(),
        loadPatterns(),
      ])
    } catch {
      message.error(t('approval.pending.resolveFailed'))
      // 中途失败时也要刷新 UI，避免已成功项仍显示为待审批
      await Promise.all([
        loadPending(),
        loadStats(),
        loadHistory(),
        loadPatterns(),
      ])
    } finally {
      batchLoading.value = false
      resumePendingPoll()
    }
  } else {
    denyReason.value = ''
    denyIsBatch.value = true
    denyCallback = async (reason: string) => {
      pausePendingPoll()
      batchLoading.value = true
      try {
        for (const item of pendingApprovals.value) {
          await resolvePendingApproval(item.id, false, reason || 'batch denied')
        }
        message.success(t('approval.pending.batchResolved'))
        await Promise.all([
          loadPending(),
          loadStats(),
          loadHistory(),
          loadPatterns(),
        ])
      } catch {
        message.error(t('approval.pending.resolveFailed'))
        // 中途失败时也要刷新 UI
        await Promise.all([
          loadPending(),
          loadStats(),
          loadHistory(),
          loadPatterns(),
        ])
      } finally {
        batchLoading.value = false
        resumePendingPoll()
      }
    }
    showDenyModal.value = true
  }
}

async function handleAddWhitelist(): Promise<void> {
  const pattern = newWhitelistPattern.value.trim()
  if (!pattern) return
  whitelistAdding.value = true
  pausePendingPoll()
  try {
    await addWhitelist(pattern)
    settingsForm.value.whitelist = [...settingsForm.value.whitelist, pattern]
    newWhitelistPattern.value = ''
    message.success(t('approval.settings.whitelistAdded'))
  } catch {
    message.error(t('approval.settings.whitelistAddFailed'))
  } finally {
    whitelistAdding.value = false
    resumePendingPoll()
  }
}

async function handleRemoveWhitelist(pattern: string): Promise<void> {
  pausePendingPoll()
  try {
    await removeWhitelist(pattern)
    settingsForm.value.whitelist = settingsForm.value.whitelist.filter(p => p !== pattern)
    message.success(t('approval.settings.whitelistRemoved'))
  } catch {
    message.error(t('approval.settings.whitelistRemoveFailed'))
  } finally {
    resumePendingPoll()
  }
}

async function handleSaveSettings(): Promise<void> {
  settingsSaving.value = true
  pausePendingPoll()
  try {
    await saveSettings(settingsForm.value)
    message.success(t('approval.settings.saved'))
  } catch {
    message.error(t('approval.settings.saveFailed'))
  } finally {
    settingsSaving.value = false
    resumePendingPoll()
  }
}

// Keyboard shortcuts: A = approve first, D = deny first, R = refresh
function handleKeydown(e: KeyboardEvent) {
  const target = e.target as HTMLElement
  if (target && (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.isContentEditable)) {
    return
  }
  if (showDenyModal.value) return
  if (activeTab.value !== 'pending') return
  if (pendingApprovals.value.length === 0) return

  const key = e.key.toLowerCase()
  if (key === 'a') {
    e.preventDefault()
    const first = pendingApprovals.value[0]
    if (first && resolvingId.value !== first.id) {
      handleResolve(first.id, true)
    }
  } else if (key === 'd') {
    e.preventDefault()
    const first = pendingApprovals.value[0]
    if (first && resolvingId.value !== first.id) {
      handleResolve(first.id, false)
    }
  } else if (key === 'r') {
    e.preventDefault()
    loadAll()
  }
}

let pendingPollTimer: ReturnType<typeof setInterval> | null = null
let pendingPollPaused = false
let expiryTickTimer: ReturnType<typeof setInterval> | null = null

function pausePendingPoll() {
  pendingPollPaused = true
}

function resumePendingPoll() {
  pendingPollPaused = false
}

function handleVisibilityChange() {
  if (document.hidden) {
    // 页面隐藏时不主动拉取；定时器内部也会通过 document.hidden 跳过
    return
  }
  // 页面恢复可见时立即拉取一次
  if (!pendingPollPaused) {
    loadPending().catch(() => { /* silent */ })
  }
}

onMounted(() => {
  // 设置全局标志，让 App.vue 跳过轮询，避免双重轮询
  ;(window as any).__approvalViewActive = true

  // 请求浏览器通知权限
  if (typeof Notification !== 'undefined' && Notification.permission === 'default') {
    try {
      Notification.requestPermission().catch(() => { /* silent */ })
    } catch {
      // 某些浏览器需要用户手势触发，忽略错误
    }
  }

  loadAll()
  // 轮询间隔与 App.vue 一致（10 秒）
  pendingPollTimer = setInterval(() => {
    if (!pendingPollPaused && !document.hidden) {
      loadPending().catch(() => { /* silent */ })
    }
  }, 10000)
  // 倒计时每 5 秒更新一次，过期精度到秒级已足够
  expiryTickTimer = setInterval(() => {
    nowTick.value = Date.now()
  }, 5000)
  window.addEventListener('keydown', handleKeydown)
  document.addEventListener('visibilitychange', handleVisibilityChange)
})

onUnmounted(() => {
  // 清除全局标志
  ;(window as any).__approvalViewActive = false

  if (pendingPollTimer) {
    clearInterval(pendingPollTimer)
    pendingPollTimer = null
  }
  if (expiryTickTimer) {
    clearInterval(expiryTickTimer)
    expiryTickTimer = null
  }
  window.removeEventListener('keydown', handleKeydown)
  document.removeEventListener('visibilitychange', handleVisibilityChange)
})
</script>
