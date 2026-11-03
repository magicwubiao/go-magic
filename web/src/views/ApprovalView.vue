<template>
  <div class="approval-page">
    <!-- 页头：标题 + 当前生效策略（一眼看到设置的结果） -->
    <div class="page-head">
      <div class="page-head-main">
        <div class="page-head-icon">
          <n-icon :component="ShieldCheckmarkOutline" :size="22" />
        </div>
        <div>
          <h2 class="page-head-title">{{ t('approval.title') }}</h2>
          <p class="page-head-sub">{{ t('approval.subtitle') }}</p>
        </div>
      </div>
      <div class="page-head-actions">
        <div class="live-pill">
          <span class="live-dot" :class="strategyTone" />
          <span class="live-label">{{ t('approval.settings.summaryStrategy') }}</span>
          <strong>{{ currentStrategyLabel }}</strong>
          <span class="live-sep" />
          <span class="live-label">{{ t('approval.settings.summaryLearning') }}</span>
          <strong>{{ settingsForm.enableLearning ? t('approval.settings.on') : t('approval.settings.off') }}</strong>
        </div>
        <n-button secondary :loading="refreshing" @click="handleRefresh">
          <template #icon><n-icon :component="RefreshOutline" /></template>
          {{ t('common.refresh') }}
        </n-button>
      </div>
    </div>

    <n-alert v-if="error" type="error" closable style="margin-bottom: 16px;" @close="error = null">
      {{ error }}
    </n-alert>

    <n-spin :show="loading">
      <n-tabs v-model:value="activeTab" type="line" animated>
        <!-- ========== 设置：主视图 ========== -->
        <n-tab-pane name="settings" :tab="t('approval.tabs.settings')">
          <n-spin :show="settingsLoading">
            <div class="settings-grid">
              <!-- 左列：策略 / 超时 / 学习 -->
              <div class="settings-main">
                <!-- 审批策略 -->
                <n-card size="small" class="setting-card">
                  <template #header>
                    <div class="section-head">
                      <n-icon :component="OptionsOutline" :size="18" class="section-icon" />
                      <div>
                        <div class="section-title">{{ t('approval.settings.sections.strategy.title') }}</div>
                        <div class="hint">{{ t('approval.settings.sections.strategy.desc') }}</div>
                      </div>
                    </div>
                  </template>
                  <div class="choice-grid">
                    <button
                      v-for="c in strategyCards"
                      :key="c.value"
                      type="button"
                      class="choice"
                      :class="{ active: settingsForm.strategy === c.value }"
                      :aria-pressed="settingsForm.strategy === c.value"
                      @click="settingsForm.strategy = c.value"
                    >
                      <div class="choice-top">
                        <n-icon :component="c.icon" :size="18" class="choice-icon" />
                        <span class="choice-name">{{ c.title }}</span>
                        <n-tag v-if="c.recommended" size="tiny" type="primary" :bordered="false">
                          {{ t('approval.settings.recommended') }}
                        </n-tag>
                        <n-icon
                          v-if="settingsForm.strategy === c.value"
                          :component="CheckmarkCircleOutline"
                          :size="16"
                          class="choice-check"
                        />
                      </div>
                      <p class="choice-desc">{{ c.desc }}</p>
                    </button>
                  </div>
                </n-card>

                <!-- 超时与兜底 -->
                <n-card size="small" class="setting-card">
                  <template #header>
                    <div class="section-head">
                      <n-icon :component="TimerOutline" :size="18" class="section-icon" />
                      <div>
                        <div class="section-title">{{ t('approval.settings.sections.timeout.title') }}</div>
                        <div class="hint">{{ t('approval.settings.sections.timeout.desc') }}</div>
                      </div>
                    </div>
                  </template>
                  <div class="choice-list">
                    <button
                      v-for="c in timeoutCards"
                      :key="c.value"
                      type="button"
                      class="choice choice-row"
                      :class="{ active: settingsForm.timeoutStrategy === c.value }"
                      :aria-pressed="settingsForm.timeoutStrategy === c.value"
                      @click="settingsForm.timeoutStrategy = c.value"
                    >
                      <div class="choice-row-body">
                        <div class="choice-row-title">
                          <span>{{ c.title }}</span>
                          <n-tag v-if="c.danger" size="tiny" type="error" :bordered="false">
                            {{ t('approval.settings.timeoutDangerTag') }}
                          </n-tag>
                        </div>
                        <p class="choice-desc">{{ c.desc }}</p>
                      </div>
                      <n-icon
                        v-if="settingsForm.timeoutStrategy === c.value"
                        :component="CheckmarkCircleOutline"
                        :size="16"
                        class="choice-check"
                      />
                    </button>
                  </div>
                  <n-alert
                    v-if="settingsForm.timeoutStrategy === 'allow_all'"
                    type="warning"
                    :bordered="false"
                    class="inline-alert"
                  >
                    {{ t('approval.settings.allowAllWarning') }}
                  </n-alert>

                  <div class="row">
                    <div class="row-label">
                      <span class="row-title">{{ t('approval.settings.approvalTimeout') }}</span>
                      <span class="hint">{{ t('approval.settings.approvalTimeoutHint') }}</span>
                    </div>
                    <div class="control">
                      <n-input-number
                        v-model:value="settingsForm.approvalTimeout"
                        :min="5"
                        :max="3600"
                        :step="5"
                        style="width: 140px;"
                      />
                      <span class="unit">{{ t('approval.settings.seconds') }}</span>
                    </div>
                  </div>
                </n-card>

                <!-- 学习与确认 -->
                <n-card size="small" class="setting-card">
                  <template #header>
                    <div class="section-head">
                      <n-icon :component="SparklesOutline" :size="18" class="section-icon" />
                      <div>
                        <div class="section-title">{{ t('approval.settings.sections.behavior.title') }}</div>
                        <div class="hint">{{ t('approval.settings.sections.behavior.desc') }}</div>
                      </div>
                    </div>
                  </template>

                  <div class="row">
                    <div class="row-label">
                      <span class="row-title">{{ t('approval.settings.enableLearning') }}</span>
                      <span class="hint">{{ t('approval.settings.enableLearningHint') }}</span>
                    </div>
                    <div class="control">
                      <n-switch v-model:value="settingsForm.enableLearning" />
                    </div>
                  </div>

                  <div class="row">
                    <div class="row-label">
                      <span class="row-title">{{ t('approval.settings.trustThreshold') }}</span>
                      <span class="hint">{{ t('approval.settings.trustThresholdHint') }}</span>
                    </div>
                    <div class="control" :class="{ dimmed: !settingsForm.enableLearning }">
                      <n-slider
                        v-model:value="settingsForm.trustThreshold"
                        :min="1"
                        :max="10"
                        :step="1"
                        :disabled="!settingsForm.enableLearning"
                        style="width: 150px;"
                      />
                      <n-input-number
                        v-model:value="settingsForm.trustThreshold"
                        :min="1"
                        :max="10"
                        size="small"
                        :disabled="!settingsForm.enableLearning"
                        style="width: 88px;"
                      />
                    </div>
                  </div>

                  <div class="row">
                    <div class="row-label">
                      <span class="row-title">{{ t('approval.settings.cliConfirm') }}</span>
                      <span class="hint">{{ t('approval.settings.cliConfirmHint') }}</span>
                    </div>
                    <div class="control">
                      <n-switch v-model:value="settingsForm.cliConfirm" />
                    </div>
                  </div>

                  <div class="row">
                    <div class="row-label">
                      <span class="row-title">{{ t('approval.settings.requirePaidConfirm') }}</span>
                      <span class="hint">{{ t('approval.settings.requirePaidConfirmHint') }}</span>
                    </div>
                    <div class="control">
                      <n-switch v-model:value="settingsForm.requirePaidConfirm" />
                    </div>
                  </div>
                </n-card>
              </div>

              <!-- 右列：保存面板 + 白名单（随滚动吸附） -->
              <aside class="settings-side">
                <n-card size="small" class="save-card" :class="{ dirty: isDirty }">
                  <div class="save-status">
                    <n-icon
                      :component="isDirty ? AlertCircleOutline : CheckmarkCircleOutline"
                      :size="20"
                      class="status-icon"
                    />
                    <div>
                      <div class="save-title">
                        {{ isDirty ? t('approval.settings.unsavedTitle', { count: dirtyCount }) : t('approval.settings.savedTitle') }}
                      </div>
                      <div class="hint">
                        {{ isDirty ? t('approval.settings.unsavedHint') : t('approval.settings.savedHint') }}
                      </div>
                    </div>
                  </div>

                  <div class="save-actions">
                    <n-button
                      type="primary"
                      block
                      :loading="settingsSaving"
                      :disabled="!isDirty"
                      @click="handleSaveSettings"
                    >
                      <template #icon><n-icon :component="SaveOutline" /></template>
                      {{ t('approval.settings.save') }}
                    </n-button>
                    <n-button block quaternary :disabled="!isDirty || settingsSaving" @click="handleResetSettings">
                      {{ t('approval.settings.reset') }}
                    </n-button>
                  </div>

                  <n-divider class="save-divider" />

                  <div class="summary-title">{{ t('approval.settings.summary') }}</div>
                  <div class="summary-row">
                    <span>{{ t('approval.settings.summaryStrategy') }}</span>
                    <b>{{ currentStrategyLabel }}</b>
                  </div>
                  <div class="summary-row">
                    <span>{{ t('approval.settings.summaryTimeout') }}</span>
                    <b>{{ currentTimeoutLabel }}</b>
                  </div>
                  <div class="summary-row">
                    <span>{{ t('approval.settings.summaryTimeoutLabel') }}</span>
                    <b>{{ settingsForm.approvalTimeout }}{{ t('approval.settings.seconds') }}</b>
                  </div>
                  <div class="summary-row">
                    <span>{{ t('approval.settings.summaryThreshold') }}</span>
                    <b>{{ settingsForm.trustThreshold }} {{ t('approval.settings.times') }}</b>
                  </div>
                </n-card>

                <n-card size="small" class="setting-card">
                  <template #header>
                    <div class="section-head">
                      <n-icon :component="LockClosedOutline" :size="18" class="section-icon" />
                      <div>
                        <div class="section-title">
                          {{ t('approval.settings.sections.whitelist.title') }}
                          <n-tag size="tiny" :bordered="false">{{ whitelist.length }}</n-tag>
                        </div>
                        <div class="hint">{{ t('approval.settings.sections.whitelist.desc') }}</div>
                      </div>
                    </div>
                  </template>

                  <div class="wl-add">
                    <n-input
                      v-model:value="newWhitelistPattern"
                      size="small"
                      :placeholder="t('approval.settings.whitelistPlaceholder')"
                      @keyup.enter="handleAddWhitelist"
                    />
                    <n-button
                      size="small"
                      type="primary"
                      :loading="whitelistAdding"
                      :disabled="!newWhitelistPattern.trim()"
                      @click="handleAddWhitelist"
                    >
                      <template #icon><n-icon :component="AddOutline" /></template>
                    </n-button>
                  </div>

                  <div v-if="whitelist.length" class="wl-list">
                    <n-tag
                      v-for="p in whitelist"
                      :key="p"
                      size="small"
                      closable
                      class="wl-tag"
                      @close="handleRemoveWhitelist(p)"
                    >
                      {{ p }}
                    </n-tag>
                  </div>
                  <n-empty v-else size="small" class="wl-empty" :description="t('approval.settings.whitelistEmpty')" />

                  <n-alert
                    v-if="settingsForm.strategy === 'whitelist' && !whitelist.length"
                    type="warning"
                    :bordered="false"
                    class="inline-alert"
                  >
                    {{ t('approval.settings.whitelistEmptyWarning') }}
                  </n-alert>
                </n-card>
              </aside>
            </div>
          </n-spin>
        </n-tab-pane>

        <!-- ========== 数据概览 ========== -->
        <n-tab-pane name="dashboard" :tab="t('approval.tabs.dashboard')">
          <div class="stat-grid">
            <n-card v-for="card in statCards" :key="card.key" size="small" class="stat-card">
              <div class="stat-label">{{ card.label }}</div>
              <div class="stat-value" :style="{ color: card.color }">{{ card.value }}</div>
              <div class="hint" :title="card.hint">{{ card.sub }}</div>
            </n-card>
          </div>

          <n-empty v-if="stats.totalRequests === 0" :description="t('approval.stats.noData')" style="margin: 24px 0;" />

          <template v-else>
            <n-card size="small" :title="t('approval.stats.riskDistribution')" class="setting-card card-gap">
              <n-space vertical>
                <div v-for="level in riskLevels" :key="level.key">
                  <div class="risk-row">
                    <n-tag :type="riskTagType(level.key)" size="small">{{ t(`approval.riskLevels.${level.key}`) }}</n-tag>
                    <span class="hint">{{ getRiskCount(level.key) }}</span>
                  </div>
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

            <n-card v-if="stats.topCommands?.length" size="small" :title="t('approval.stats.topCommands')" class="setting-card">
              <n-data-table :columns="topCommandColumns" :data="stats.topCommands || []" :bordered="false" size="small" />
            </n-card>
          </template>
        </n-tab-pane>

        <!-- ========== 审批历史 ========== -->
        <n-tab-pane name="history" :tab="t('approval.tabs.history')">
          <n-card size="small" class="setting-card card-gap">
            <div class="card-toolbar">
              <span class="hint">
                {{ historyRecords.length ? `${historyRecords.length} ${t('approval.history.records')}` : t('approval.history.noRecords') }}
              </span>
              <n-popconfirm @positive-click="handleClearHistory">
                <template #trigger>
                  <n-button type="error" size="small" secondary :disabled="!historyRecords.length">
                    {{ t('approval.history.clearHistory') }}
                  </n-button>
                </template>
                {{ t('approval.history.clearConfirm') }}
              </n-popconfirm>
            </div>
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
          </n-card>
        </n-tab-pane>

        <!-- ========== 命令模式 ========== -->
        <n-tab-pane name="patterns" :tab="t('approval.tabs.patterns')">
          <n-card size="small" class="setting-card card-gap">
            <template #header>
              <div class="section-head">
                <div>
                  <div class="section-title">
                    {{ t('approval.patterns.trusted') }}
                    <n-tag size="tiny" :bordered="false">{{ trustedCommands.length }}</n-tag>
                  </div>
                  <div class="hint">{{ t('approval.patterns.trustedHint') }}</div>
                </div>
              </div>
            </template>
            <n-data-table
              v-if="trustedCommands.length"
              :columns="trustedColumns"
              :data="trustedCommands"
              :bordered="false"
              size="small"
            />
            <n-empty v-else :description="t('approval.patterns.noTrusted')" />
          </n-card>

          <n-card size="small" class="setting-card">
            <template #header>
              <div class="section-head">
                <div>
                  <div class="section-title">
                    {{ t('approval.patterns.denied') }}
                    <n-tag size="tiny" :bordered="false">{{ deniedCommands.length }}</n-tag>
                  </div>
                  <div class="hint">{{ t('approval.patterns.deniedHint') }}</div>
                </div>
              </div>
            </template>
            <n-data-table
              v-if="deniedCommands.length"
              :columns="deniedColumns"
              :data="deniedCommands"
              :bordered="false"
              size="small"
            />
            <n-empty v-else :description="t('approval.patterns.noDenied')" />
          </n-card>
        </n-tab-pane>
      </n-tabs>
    </n-spin>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, h } from 'vue'
import { useI18n } from 'vue-i18n'
import { NButton, NTag, useMessage } from 'naive-ui'
import {
  NAlert,
  NCard,
  NDataTable,
  NDivider,
  NEmpty,
  NIcon,
  NInput,
  NInputNumber,
  NPopconfirm,
  NProgress,
  NSlider,
  NSpace,
  NSpin,
  NSwitch,
  NTabPane,
  NTabs,
} from 'naive-ui'
import {
  AddOutline,
  AlertCircleOutline,
  CheckmarkCircleOutline,
  FlashOutline,
  ListOutline,
  LockClosedOutline,
  OptionsOutline,
  RefreshOutline,
  SaveOutline,
  ShieldCheckmarkOutline,
  SparklesOutline,
  TimerOutline,
} from '@vicons/ionicons5'
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
const refreshing = ref(false)
const error = ref<string | null>(null)
// 设置是主视图，默认落在设置页
const activeTab = ref('settings')

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
function defaultSettings(): ApprovalSettings {
  return {
    strategy: 'smart',
    timeoutStrategy: 'deny',
    trustThreshold: 3,
    enableLearning: true,
    cliConfirm: false,
    requirePaidConfirm: true,
    approvalTimeout: 60,
    whitelist: [],
  }
}

const settingsForm = ref<ApprovalSettings>(defaultSettings())
// 服务端已生效的配置快照，用于判断"有未保存的修改"与还原
const savedSettings = ref<ApprovalSettings>(defaultSettings())
const newWhitelistPattern = ref('')
const whitelistAdding = ref(false)
const settingsLoading = ref(false)
const settingsSaving = ref(false)

const whitelist = computed(() => settingsForm.value.whitelist)

// 变更检测：只比对后端 PUT 支持的字段（白名单走独立接口、即时生效，不计入）
const dirtyFields = computed(() => {
  const form = settingsForm.value
  const saved = savedSettings.value
  const keys: (keyof ApprovalSettings)[] = [
    'strategy',
    'timeoutStrategy',
    'approvalTimeout',
    'trustThreshold',
    'enableLearning',
    'cliConfirm',
    'requirePaidConfirm',
  ]
  return keys.filter(k => form[k] !== saved[k])
})
const isDirty = computed(() => dirtyFields.value.length > 0)
const dirtyCount = computed(() => dirtyFields.value.length)

// 策略卡片
const strategyCards = computed(() => [
  {
    value: 'manual',
    icon: ShieldCheckmarkOutline,
    title: t('approval.settings.cards.manual.title'),
    desc: t('approval.settings.cards.manual.desc'),
    recommended: false,
  },
  {
    value: 'smart',
    icon: SparklesOutline,
    title: t('approval.settings.cards.smart.title'),
    desc: t('approval.settings.cards.smart.desc'),
    recommended: true,
  },
  {
    value: 'auto',
    icon: FlashOutline,
    title: t('approval.settings.cards.auto.title'),
    desc: t('approval.settings.cards.auto.desc'),
    recommended: false,
  },
  {
    value: 'whitelist',
    icon: ListOutline,
    title: t('approval.settings.cards.whitelistOnly.title'),
    desc: t('approval.settings.cards.whitelistOnly.desc'),
    recommended: false,
  },
])

// 超时策略卡片
const timeoutCards = computed(() => [
  {
    value: 'deny',
    title: t('approval.settings.timeoutCards.deny.title'),
    desc: t('approval.settings.timeoutCards.deny.desc'),
    danger: false,
  },
  {
    value: 'allow_low_medium',
    title: t('approval.settings.timeoutCards.allowLowMedium.title'),
    desc: t('approval.settings.timeoutCards.allowLowMedium.desc'),
    danger: false,
  },
  {
    value: 'allow_all',
    title: t('approval.settings.timeoutCards.allowAll.title'),
    desc: t('approval.settings.timeoutCards.allowAll.desc'),
    danger: true,
  },
])

// 中文/英文短标签，用于页头与摘要
function strategyShortLabel(value: string): string {
  const key = `approval.settings.strategyShort.${value}`
  const label = t(key)
  return label === key ? value : label
}

const currentStrategyLabel = computed(() => strategyShortLabel(settingsForm.value.strategy))

const currentTimeoutLabel = computed(() => {
  const map: Record<string, string> = {
    deny: 'approval.settings.timeoutCards.deny.title',
    allow_low_medium: 'approval.settings.timeoutCards.allowLowMedium.title',
    allow_all: 'approval.settings.timeoutCards.allowAll.title',
  }
  const key = map[settingsForm.value.timeoutStrategy]
  return key ? t(key) : settingsForm.value.timeoutStrategy
})

// 页头状态点：越宽松越"热"
const strategyTone = computed(() => {
  switch (settingsForm.value.strategy) {
    case 'auto':
      return 'tone-warn'
    case 'whitelist':
      return 'tone-info'
    case 'manual':
      return 'tone-neutral'
    default:
      return 'tone-ok'
  }
})

// 风险等级
const riskLevels = [{ key: 'low' }, { key: 'medium' }, { key: 'high' }, { key: 'critical' }]

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

function riskLevelKey(level: string | number): string {
  const map: Record<string, string> = {
    low: 'low',
    medium: 'medium',
    high: 'high',
    critical: 'critical',
    '0': 'low',
    '1': 'low',
    '2': 'medium',
    '3': 'high',
    '4': 'critical',
  }
  return map[String(level)] || 'low'
}

function getRiskCount(level: string): number {
  return stats.value?.riskDistribution?.[level] || 0
}

function getRiskPercent(level: string): number {
  const total = stats.value?.totalRequests || 1
  return Math.round((getRiskCount(level) / total) * 100)
}

function formatPercent(value: number, total: number): string {
  if (!total) return '0%'
  return Math.round((value / total) * 100) + '%'
}

function formatTime(ts: string): string {
  if (!ts) return '-'
  try {
    return new Date(ts).toLocaleString()
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
  result = result.replace(/Bearer\s+[\w.\-]+/gi, 'Bearer ****')
  result = result.replace(/(api[_-]?key|token|secret|password|passwd)(\s*[=:]\s*)["']?[\w.\-]+["']?/gi, '$1$2****')
  result = result.replace(/AKIA[0-9A-Z]{16}/g, 'AKIA****')
  result = result.replace(/-----BEGIN[\s\S]*?-----END[A-Z\s]+KEY-----/g, '[REDACTED KEY]')
  return result
}

// 概览卡片
const statCards = computed(() => {
  const total = stats.value.totalRequests
  return [
    {
      key: 'total',
      label: t('approval.stats.totalRequests'),
      value: String(total),
      color: '#111827',
      sub: t('approval.stats.totalHint'),
    },
    {
      key: 'auto',
      label: t('approval.stats.autoApproved'),
      value: formatPercent(stats.value.autoApproved, total),
      color: '#18a058',
      sub: `${stats.value.autoApproved} / ${total}`,
      hint: t('approval.stats.autoApprovedHint'),
    },
    {
      key: 'user',
      label: t('approval.stats.userApproved'),
      value: formatPercent(stats.value.userApproved, total),
      color: '#2080f0',
      sub: `${stats.value.userApproved} / ${total}`,
      hint: t('approval.stats.userApprovedHint'),
    },
    {
      key: 'denied',
      label: t('approval.stats.userDenied'),
      value: formatPercent(stats.value.userDenied, total),
      color: '#d03050',
      sub: `${stats.value.userDenied} / ${total}`,
      hint: t('approval.stats.userDeniedHint'),
    },
  ]
})

// Top commands columns
const topCommandColumns = computed(() => [
  { title: '#', key: 'index', width: 40, render: (_row: any, index: number) => index + 1 },
  {
    title: t('approval.history.command'),
    key: 'command',
    ellipsis: { tooltip: true },
    render: (row: any) => sanitizeCommand(row.command || ''),
  },
  { title: t('approval.patterns.count'), key: 'count', width: 80 },
  {
    title: t('approval.history.riskLevel'),
    key: 'riskLevel',
    width: 100,
    render: (row: any) =>
      h(
        NTag,
        { type: riskTagType(row.riskLevel || 'low'), size: 'small' },
        { default: () => t(`approval.riskLevels.${riskLevelKey(row.riskLevel || 'low')}`) },
      ),
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
    render: (row: ApprovalHistoryRecord) =>
      h('span', { style: 'font-family: monospace; font-size: 12px;' }, truncate(sanitizeCommand(row.command), 60)),
  },
  {
    title: t('approval.history.riskLevel'),
    key: 'riskLevel',
    width: 80,
    render: (row: ApprovalHistoryRecord) =>
      h(
        NTag,
        { type: riskTagType(row.riskLevel || 'low'), size: 'small' },
        { default: () => t(`approval.riskLevels.${riskLevelKey(row.riskLevel || 'low')}`) },
      ),
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
      // 历史表用短标签（手动审批/自动放行/智能审批/白名单），
      // 不复用设置页选项卡片的长文案，避免窄列里断行难读
      const key = `approval.history.strategies.${row.strategy}`
      const translated = t(key)
      const text = translated === key ? row.strategy : translated
      // 悬停显示后端记录的判定原因，便于追溯
      return h('span', { title: sanitizeCommand(row.reason || '') }, text)
    },
  },
  {
    title: t('approval.history.duration'),
    key: 'duration',
    width: 80,
    render: (row: ApprovalHistoryRecord) => (row.durationMs ? `${row.durationMs}ms` : '-'),
  },
])

// Trusted columns
const trustedColumns = computed(() => [
  { title: t('approval.patterns.pattern'), key: 'pattern', ellipsis: { tooltip: true } },
  { title: t('approval.patterns.count'), key: 'count', width: 80 },
  {
    title: t('approval.history.riskLevel'),
    key: 'riskLevel',
    width: 80,
    render: (row: TrustedPattern) =>
      h(
        NTag,
        { type: riskTagType(row.riskLevel || 'low'), size: 'small' },
        { default: () => t(`approval.riskLevels.${riskLevelKey(row.riskLevel || 'low')}`) },
      ),
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
    render: (row: TrustedPattern) =>
      h(
        NButton,
        { size: 'small', type: 'warning', secondary: true, onClick: () => handleRemoveTrust(row.pattern) },
        { default: () => t('approval.patterns.removeTrust') },
      ),
  },
])

// Denied columns
const deniedColumns = computed(() => [
  { title: t('approval.patterns.pattern'), key: 'pattern', ellipsis: { tooltip: true } },
  { title: t('approval.patterns.count'), key: 'count', width: 80 },
  {
    title: t('approval.history.riskLevel'),
    key: 'riskLevel',
    width: 80,
    render: (row: DeniedPattern) =>
      h(
        NTag,
        { type: riskTagType(row.riskLevel || 'low'), size: 'small' },
        { default: () => t(`approval.riskLevels.${riskLevelKey(row.riskLevel || 'low')}`) },
      ),
  },
  {
    title: t('approval.patterns.actions'),
    key: 'actions',
    width: 120,
    render: (row: DeniedPattern) =>
      h(
        NButton,
        { size: 'small', type: 'warning', secondary: true, onClick: () => handleClearDenial(row.pattern) },
        { default: () => t('approval.patterns.clearDenial') },
      ),
  },
])

// Loaders
async function loadStats(): Promise<void> {
  stats.value = await getApprovalStats()
}

async function loadHistory(): Promise<void> {
  const result = await getApprovalHistory(100, 0)
  historyRecords.value = result.records || []
  historyPagination.value = { ...historyPagination.value, itemCount: result.total || 0 }
}

async function loadPatterns(): Promise<void> {
  const [trusted, denied] = await Promise.all([getTrustedPatterns(), getDeniedPatterns()])
  trustedCommands.value = trusted || []
  deniedCommands.value = denied || []
}

async function loadOverview(): Promise<void> {
  loading.value = true
  error.value = null
  // 使用 allSettled 让各数据源独立失败，互不影响
  const results = await Promise.allSettled([loadStats(), loadHistory(), loadPatterns()])
  if (results.some(r => r.status === 'rejected')) {
    error.value = t('approval.failedToLoad')
  }
  loading.value = false
}

async function loadSettings(): Promise<void> {
  settingsLoading.value = true
  try {
    const [settings, patterns] = await Promise.all([getSettings(), getWhitelist()])
    const next: ApprovalSettings = { ...settings, whitelist: [...patterns] }
    settingsForm.value = { ...next, whitelist: [...next.whitelist] }
    savedSettings.value = { ...next, whitelist: [...next.whitelist] }
  } catch {
    message.error(t('approval.settings.loadSettingsFailed'))
  } finally {
    settingsLoading.value = false
  }
}

// Handlers
async function handleRefresh(): Promise<void> {
  refreshing.value = true
  try {
    await loadOverview()
    // 有未保存修改时跳过设置刷新，避免把用户正在编辑的内容冲掉
    if (isDirty.value) {
      message.warning(t('approval.settings.unsavedRefreshBlocked'))
    } else {
      await loadSettings()
    }
  } finally {
    refreshing.value = false
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
  try {
    await removeTrustedPattern(pattern)
    await Promise.all([loadPatterns(), loadStats()])
  } catch {
    message.error(t('approval.failedToLoad'))
  }
}

async function handleClearDenial(pattern: string): Promise<void> {
  try {
    await clearDeniedPattern(pattern)
    await Promise.all([loadPatterns(), loadStats()])
  } catch {
    message.error(t('approval.failedToLoad'))
  }
}

// 白名单：增删即时生效，不参与"保存设置"
async function handleAddWhitelist(): Promise<void> {
  const pattern = newWhitelistPattern.value.trim()
  if (!pattern) return
  if (settingsForm.value.whitelist.includes(pattern)) {
    message.warning(t('approval.settings.whitelistDuplicated'))
    return
  }
  whitelistAdding.value = true
  try {
    await addWhitelist(pattern)
    settingsForm.value.whitelist = [...settingsForm.value.whitelist, pattern]
    savedSettings.value.whitelist = [...savedSettings.value.whitelist, pattern]
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
    savedSettings.value.whitelist = savedSettings.value.whitelist.filter(p => p !== pattern)
    message.success(t('approval.settings.whitelistRemoved'))
  } catch {
    message.error(t('approval.settings.whitelistRemoveFailed'))
  }
}

async function handleSaveSettings(): Promise<void> {
  settingsSaving.value = true
  try {
    await saveSettings(settingsForm.value)
    // 保存成功后以当前表单为准刷新快照（白名单只保留本地已生效的列表）
    savedSettings.value = { ...settingsForm.value, whitelist: [...settingsForm.value.whitelist] }
    message.success(t('approval.settings.saved'))
  } catch {
    message.error(t('approval.settings.saveFailed'))
  } finally {
    settingsSaving.value = false
  }
}

function handleResetSettings(): void {
  const saved = savedSettings.value
  settingsForm.value = {
    ...saved,
    whitelist: [...settingsForm.value.whitelist],
  }
  message.info(t('approval.settings.resetDone'))
}

onMounted(() => {
  loadOverview()
  loadSettings()
})
</script>

<style scoped>
.approval-page {
  max-width: 1240px;
  margin: 0 auto;
}

/* ---------- 页头 ---------- */
.page-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
  margin-bottom: 20px;
}

.page-head-main {
  display: flex;
  align-items: center;
  gap: 12px;
  min-width: 0;
}

.page-head-icon {
  width: 42px;
  height: 42px;
  border-radius: 12px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex: none;
  color: #2080f0;
  background: linear-gradient(135deg, #eaf4ff, #dbeafe);
}

.page-head-title {
  margin: 0;
  font-size: 20px;
  font-weight: 600;
  line-height: 1.3;
  color: #111827;
}

.page-head-sub {
  margin: 3px 0 0;
  font-size: 12.5px;
  line-height: 1.5;
  color: #8a8f99;
  max-width: 620px;
}

.page-head-actions {
  display: flex;
  align-items: center;
  gap: 10px;
}

.live-pill {
  display: flex;
  align-items: center;
  gap: 7px;
  padding: 6px 12px;
  border-radius: 999px;
  background: #f7f9fb;
  border: 1px solid #eceff3;
  font-size: 12px;
  color: #6b7280;
  white-space: nowrap;
}

.live-pill strong {
  color: #111827;
  font-weight: 600;
}

.live-label {
  color: #98a2b3;
}

.live-sep {
  width: 1px;
  height: 12px;
  background: #e5e7eb;
  margin: 0 2px;
}

.live-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  flex: none;
  background: #18a058;
  box-shadow: 0 0 0 3px rgba(24, 160, 88, 0.14);
}

.live-dot.tone-warn {
  background: #f0a020;
  box-shadow: 0 0 0 3px rgba(240, 160, 32, 0.16);
}

.live-dot.tone-info {
  background: #7b2ff2;
  box-shadow: 0 0 0 3px rgba(123, 47, 242, 0.14);
}

.live-dot.tone-neutral {
  background: #2080f0;
  box-shadow: 0 0 0 3px rgba(32, 128, 240, 0.14);
}

/* ---------- 设置布局 ---------- */
.settings-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 330px;
  gap: 16px;
  align-items: start;
}

.settings-main,
.settings-side {
  display: flex;
  flex-direction: column;
  gap: 16px;
  min-width: 0;
}

.settings-side {
  position: sticky;
  top: 8px;
}

@media (max-width: 1080px) {
  .settings-grid {
    grid-template-columns: minmax(0, 1fr);
  }

  .settings-side {
    position: static;
  }
}

.setting-card + .setting-card {
  margin-top: 0;
}

/* 非网格布局（概览/历史/模式）里的卡片间距 */
.card-gap {
  margin-bottom: 16px;
}

/* ---------- 卡片内标题 ---------- */
.section-head {
  display: flex;
  align-items: flex-start;
  gap: 8px;
}

.section-icon {
  color: #7c8899;
  margin-top: 2px;
  flex: none;
}

.section-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
  font-weight: 600;
  color: #111827;
  line-height: 1.4;
}

.hint {
  font-size: 12px;
  line-height: 1.5;
  color: #98a2b3;
}

/* ---------- 选项卡片 ---------- */
.choice-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
}

@media (max-width: 720px) {
  .choice-grid {
    grid-template-columns: minmax(0, 1fr);
  }
}

.choice-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.choice {
  -webkit-appearance: none;
  appearance: none;
  display: flex;
  flex-direction: column;
  gap: 6px;
  width: 100%;
  padding: 11px 12px;
  border: 1px solid #eaecf0;
  border-radius: 10px;
  background: #fff;
  font: inherit;
  text-align: left;
  cursor: pointer;
  transition: border-color 0.15s ease, background 0.15s ease, box-shadow 0.15s ease;
}

.choice:hover {
  border-color: #cdd8e8;
  background: #fbfcfe;
}

.choice:focus-visible {
  outline: none;
  box-shadow: 0 0 0 3px rgba(32, 128, 240, 0.16);
}

.choice.active {
  border-color: #2080f0;
  background: #f4f9ff;
  box-shadow: 0 0 0 3px rgba(32, 128, 240, 0.09);
}

.choice-top {
  display: flex;
  align-items: center;
  gap: 8px;
}

.choice-icon {
  color: #7c8899;
  flex: none;
}

.choice.active .choice-icon {
  color: #2080f0;
}

.choice-name {
  font-size: 13.5px;
  font-weight: 600;
  color: #111827;
}

.choice-check {
  margin-left: auto;
  color: #2080f0;
  flex: none;
}

.choice-desc {
  margin: 0;
  font-size: 12.5px;
  line-height: 1.55;
  color: #8a8f99;
}

.choice-row {
  flex-direction: row;
  align-items: center;
  gap: 12px;
}

.choice-row-body {
  min-width: 0;
  flex: 1;
}

.choice-row-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13.5px;
  font-weight: 600;
  color: #111827;
  margin-bottom: 3px;
}

.inline-alert {
  margin-top: 12px;
  border-radius: 8px;
}

/* ---------- 开关/数值行 ---------- */
.row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 8px 16px;
  padding: 12px 0;
  border-top: 1px solid #f2f4f7;
}

.row:first-of-type {
  border-top: none;
}

/* 标签保住最小可读宽度；控件放不下时换行，避免窄屏下标签被挤成竖排字 */
.row-label {
  display: flex;
  flex-direction: column;
  gap: 3px;
  flex: 1 1 220px;
  min-width: 0;
}

.row-title {
  font-size: 13.5px;
  font-weight: 500;
  color: #111827;
}

.control {
  display: flex;
  align-items: center;
  gap: 10px;
  flex: none;
}

.control.dimmed {
  opacity: 0.55;
}

.unit {
  font-size: 12px;
  color: #98a2b3;
}

/* ---------- 保存面板 ---------- */
.save-card {
  border-radius: 12px;
  border-color: #eaecf0;
  transition: border-color 0.2s ease, box-shadow 0.2s ease;
}

.save-card.dirty {
  border-color: #ffd591;
  box-shadow: 0 2px 10px rgba(240, 160, 32, 0.1);
}

.save-status {
  display: flex;
  align-items: flex-start;
  gap: 10px;
}

.status-icon {
  color: #18a058;
  margin-top: 1px;
  flex: none;
}

.save-card.dirty .status-icon {
  color: #f0a020;
}

.save-title {
  font-size: 14px;
  font-weight: 600;
  color: #111827;
  margin-bottom: 2px;
}

.save-actions {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-top: 14px;
}

.save-divider {
  margin: 14px 0 10px !important;
}

.summary-title {
  font-size: 12px;
  font-weight: 600;
  color: #98a2b3;
  letter-spacing: 0.04em;
  margin-bottom: 6px;
}

.summary-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 4px 0;
  font-size: 12.5px;
  color: #8a8f99;
}

.summary-row b {
  color: #111827;
  font-weight: 500;
  text-align: right;
}

/* ---------- 白名单 ---------- */
.wl-add {
  display: flex;
  align-items: center;
  gap: 8px;
}

.wl-list {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 12px;
  max-height: 210px;
  overflow-y: auto;
}

.wl-tag {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  max-width: 100%;
}

.wl-empty {
  margin-top: 12px;
}

/* ---------- 概览 ---------- */
.stat-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
  margin-bottom: 16px;
}

@media (max-width: 900px) {
  .stat-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 520px) {
  .stat-grid {
    grid-template-columns: minmax(0, 1fr);
  }
}

.stat-label {
  font-size: 12px;
  color: #98a2b3;
  margin-bottom: 6px;
}

.stat-value {
  font-size: 24px;
  font-weight: 600;
  line-height: 1.2;
  font-variant-numeric: tabular-nums;
}

.risk-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 5px;
}

/* ---------- 通用工具条 ---------- */
.card-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 12px;
}

/* 窄屏：配置行改为上下堆叠，控件占满整行。
   注意必须放在基础 .row/.row-label 规则之后，否则 flex-basis 覆盖不到，
   纵向布局下标签会被撑出 220px 高度 */
@media (max-width: 720px) {
  .row {
    flex-direction: column;
    align-items: stretch;
  }

  .row-label {
    flex-basis: auto;
  }

  .control {
    width: 100%;
    flex-wrap: wrap;
  }
}
</style>
