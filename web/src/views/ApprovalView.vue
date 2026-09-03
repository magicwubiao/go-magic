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
        <!-- Tab 1: Dashboard -->
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

        <!-- Tab 4: Settings -->
        <n-tab-pane name="settings" :tab="t('approval.tabs.settings')">
          <n-spin :show="settingsLoading">
            <n-card size="small" style="margin-bottom: 16px;">
              <n-space vertical>
                <n-form-item :label="t('approval.settings.strategy')">
                  <n-select v-model:value="settingsForm.strategy" :options="strategyOptions" />
                </n-form-item>
                <n-form-item :label="t('approval.settings.timeoutStrategy')">
                  <n-select v-model:value="settingsForm.timeoutStrategy" :options="timeoutStrategyOptions" />
                  <template #feedback>
                    <n-text depth="3" style="font-size: 12px;">{{ t('approval.settings.timeoutStrategyHint') }}</n-text>
                  </template>
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
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, watch, h } from 'vue'
import { useI18n } from 'vue-i18n'
import { NButton, NTag, useMessage } from 'naive-ui'
import {
  getApprovalHistory,
  getApprovalStats,
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
  type TrustedPattern,
  type DeniedPattern,
  type ApprovalSettings,
} from '@/api/approval'

const { t } = useI18n()
const message = useMessage()

// Loading & error
const loading = ref(false)
const error = ref<string | null>(null)
const activeTab = ref('dashboard')

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
const historyPagination = ref<{ pageSize: number; itemCount?: number }>({ pageSize: 20 })

// Patterns
const trustedCommands = ref<TrustedPattern[]>([])
const deniedCommands = ref<DeniedPattern[]>([])

// Settings
const settingsForm = ref<ApprovalSettings>({
  strategy: 'smart',
  timeoutStrategy: 'deny',
  trustThreshold: 3,
  enableLearning: true,
  cliConfirm: false,
  whitelist: [],
})
const newWhitelistPattern = ref('')
const whitelistAdding = ref(false)
const settingsLoading = ref(false)
const settingsSaving = ref(false)
let settingsLoaded = false

// Lazy load settings when settings tab is first opened
watch(activeTab, (newTab) => {
  if (newTab === 'settings' && !settingsLoaded) {
    settingsLoaded = true
    loadSettings()
  }
})

// Strategy options for settings dropdown
const strategyOptions = computed(() => [
  { label: t('approval.settings.strategies.manual'), value: 'manual' },
  { label: t('approval.settings.strategies.auto'), value: 'auto' },
  { label: t('approval.settings.strategies.smart'), value: 'smart' },
  { label: t('approval.settings.strategies.whitelist'), value: 'whitelist' },
])

// Timeout strategy options for settings dropdown
const timeoutStrategyOptions = computed(() => [
  { label: t('approval.settings.timeoutStrategies.deny'), value: 'deny' },
  { label: t('approval.settings.timeoutStrategies.allowLowMedium'), value: 'allow_low_medium' },
  { label: t('approval.settings.timeoutStrategies.allowAll'), value: 'allow_all' },
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

async function loadSettings(): Promise<void> {
  settingsLoading.value = true
  try {
    const [settings, whitelist] = await Promise.all([
      getSettings(),
      getWhitelist(),
    ])
    settingsForm.value = {
      strategy: settings.strategy,
      timeoutStrategy: settings.timeoutStrategy,
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

async function handleClearHistory(): Promise<void> {
  loading.value = true
  try {
    await clearHistory(168) // 7 days
    message.success(t('approval.history.cleared'))
    await Promise.all([loadHistory(), loadStats()])
  } catch {
    message.error(t('approval.history.clearFailed'))
  } finally {
    loading.value = false
  }
}

async function handleRemoveTrust(pattern: string): Promise<void> {
  loading.value = true
  try {
    await removeTrustedPattern(pattern)
    await Promise.all([loadPatterns(), loadStats()])
  } catch {
    message.error(t('approval.failedToLoad'))
  } finally {
    loading.value = false
  }
}

async function handleClearDenial(pattern: string): Promise<void> {
  loading.value = true
  try {
    await clearDeniedPattern(pattern)
    await Promise.all([loadPatterns(), loadStats()])
  } catch {
    message.error(t('approval.failedToLoad'))
  } finally {
    loading.value = false
  }
}

async function handleAddWhitelist(): Promise<void> {
  const pattern = newWhitelistPattern.value.trim()
  if (!pattern) return
  whitelistAdding.value = true
  try {
    await addWhitelist(pattern)
    settingsForm.value.whitelist = [...settingsForm.value.whitelist, pattern]
    newWhitelistPattern.value = ''
    message.success(t('approval.settings.whitelistAdded'))
  } catch {
    message.error(t('approval.settings.whitelistAddFailed'))
  } finally {
    whitelistAdding.value = false
  }
}

async function handleRemoveWhitelist(pattern: string): Promise<void> {
  try {
    await removeWhitelist(pattern)
    settingsForm.value.whitelist = settingsForm.value.whitelist.filter(p => p !== pattern)
    message.success(t('approval.settings.whitelistRemoved'))
  } catch {
    message.error(t('approval.settings.whitelistRemoveFailed'))
  }
}

async function handleSaveSettings(): Promise<void> {
  settingsSaving.value = true
  try {
    await saveSettings(settingsForm.value)
    message.success(t('approval.settings.saved'))
  } catch {
    message.error(t('approval.settings.saveFailed'))
  } finally {
    settingsSaving.value = false
  }
}

onMounted(() => {
  loadAll()
})
</script>
