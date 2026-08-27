<template>
  <div>
    <n-space justify="space-between" style="margin-bottom: 24px;" align="center">
      <h2>{{ t('usage.title') }}</h2>
      <n-space>
        <n-select
          v-model:value="selectedDays"
          :options="daysOptions"
          style="width: 150px"
          @update:value="loadStats"
        />
        <n-button @click="loadStats">
          <template #icon>
            <n-icon><RefreshCircleOutline /></n-icon>
          </template>
          {{ t('usage.refresh') }}
        </n-button>
        <n-button type="primary" @click="showBudgetDialog = true">
          <template #icon>
            <n-icon><WalletOutline /></n-icon>
          </template>
          {{ t('usage.editBudget') }}
        </n-button>
      </n-space>
    </n-space>

    <!-- 预算状态 -->
    <n-card :title="t('usage.budget')" style="margin-bottom: 24px" v-if="budget.limit > 0">
      <div class="budget-card">
        <div class="budget-info">
          <n-statistic :value="formatCost(budget.current)">
            <template #prefix>$</template>
            <template #suffix>
              <n-tag :type="budgetStatusType" size="small">
                {{ budgetStatusText }}
              </n-tag>
            </template>
          </n-statistic>
          <div class="budget-limit">
            {{ t('usage.budgetLimit') }}: ${{ formatCost(budget.limit) }} ({{ budgetPercent }}%)
          </div>
        </div>
        <div class="budget-bar">
          <n-progress
            type="line"
            :percentage="Math.min(budgetPercent, 100)"
            :status="budgetStatusType"
            :indicator-placement="'inside'"
          />
        </div>
      </div>
    </n-card>

    <!-- 今日统计 -->
    <n-grid :cols="4" :x-gap="16" :y-gap="16" class="stats-grid">
      <n-gi>
        <n-card :title="t('usage.todaySessions')" size="small">
          <n-statistic :value="todayStats.sessions || 0">
            <template #suffix>{{ t('usage.sessions') }}</template>
          </n-statistic>
        </n-card>
      </n-gi>
      <n-gi>
        <n-card :title="t('usage.todayMessages')" size="small">
          <n-statistic :value="todayStats.messages || 0">
            <template #suffix>{{ t('usage.messages') }}</template>
          </n-statistic>
        </n-card>
      </n-gi>
      <n-gi>
        <n-card :title="t('usage.todayTokens')" size="small">
          <n-statistic :value="formatNumber(todayStats.total_tokens || 0)">
            <template #suffix>Tokens</template>
          </n-statistic>
        </n-card>
      </n-gi>
      <n-gi>
        <n-card :title="t('usage.todayCost')" size="small">
          <n-statistic :value="formatCost(todayStats.cost || 0)">
            <template #prefix>$</template>
          </n-statistic>
        </n-card>
      </n-gi>
    </n-grid>

    <!-- 概览统计 -->
    <n-grid :cols="4" :x-gap="16" :y-gap="16" class="stats-grid" style="margin-top: 16px">
      <n-gi>
        <n-card :title="t('usage.totalSessions')" size="small">
          <n-statistic :value="insights.total_sessions || 0">
            <template #suffix>{{ t('usage.sessions') }}</template>
          </n-statistic>
        </n-card>
      </n-gi>
      <n-gi>
        <n-card :title="t('usage.totalMessages')" size="small">
          <n-statistic :value="formatNumber(insights.total_messages || 0)">
            <template #suffix>{{ t('usage.messages') }}</template>
          </n-statistic>
        </n-card>
      </n-gi>
      <n-gi>
        <n-card :title="t('usage.totalTokens')" size="small">
          <n-statistic :value="formatNumber(safeTotalTokens)">
            <template #suffix>Tokens</template>
          </n-statistic>
        </n-card>
      </n-gi>
      <n-gi>
        <n-card :title="t('usage.totalCost')" size="small">
          <n-statistic :value="formatCost(insights.total_cost || 0)">
            <template #prefix>$</template>
          </n-statistic>
        </n-card>
      </n-gi>
    </n-grid>

    <!-- 详细洞察 -->
    <n-grid :cols="3" :x-gap="16" :y-gap="16" class="stats-grid" style="margin-top: 16px">
      <n-gi>
        <n-card :title="t('usage.avgCostPerSession')" size="small">
          <n-statistic :value="formatCost(insights.avg_cost_per_session || 0)">
            <template #prefix>$</template>
          </n-statistic>
        </n-card>
      </n-gi>
      <n-gi>
        <n-card :title="t('usage.avgTokensPerMessage')" size="small">
          <n-statistic :value="Math.round(insights.avg_tokens_per_message || 0)">
          </n-statistic>
        </n-card>
      </n-gi>
      <n-gi>
        <n-card :title="t('usage.mostUsedModel')" size="small">
          <n-statistic :value="insights.most_used_model || '-'">
          </n-statistic>
        </n-card>
      </n-gi>
    </n-grid>

    <!-- 模型用量表格 -->
    <n-card :title="t('usage.modelUsage')" style="margin-top: 16px">
      <n-data-table
        :columns="modelColumns"
        :data="insights.top_models || []"
        :pagination="false"
        :bordered="false"
      />
    </n-card>

    <!-- 每日趋势图表 -->
    <n-card :title="t('usage.dailyTrend')" style="margin-top: 16px">
      <n-data-table
        :columns="dailyColumns"
        :data="dailyData"
        :pagination="{ pageSize: 10 }"
        :bordered="false"
      />
    </n-card>

    <!-- 月度统计表格 -->
    <n-card :title="t('usage.monthlyUsage')" style="margin-top: 16px">
      <n-data-table
        :columns="monthlyColumns"
        :data="monthlyData"
        :pagination="false"
        :bordered="false"
      />
    </n-card>

    <!-- Budget Edit Modal -->
    <n-modal
      v-model:show="showBudgetDialog"
      preset="card"
      class="modal-responsive"
      :title="t('usage.editBudget')"
      style="width: 420px; max-width: 96vw;"
    >
      <div class="budget-dialog">
        <n-form-item :label="t('usage.budgetLimit')">
          <n-input-number
            v-model:value="budgetForm.limit"
            :min="0"
            :step="10"
            style="width: 100%"
          />
        </n-form-item>
        <n-form-item :label="t('usage.alertThreshold') + ' (%)'">
          <n-slider
            v-model:value="budgetForm.alert_threshold"
            :min="10"
            :max="100"
            :step="5"
          />
          <div class="threshold-display">{{ budgetForm.alert_threshold }}%</div>
        </n-form-item>
        <div class="dialog-actions">
          <n-button @click="showBudgetDialog = false">{{ t('usage.cancel') }}</n-button>
          <n-button type="primary" @click="handleSaveBudget">
            {{ t('usage.save') }}
          </n-button>
        </div>
      </div>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  NGrid, NGi, NCard, NStatistic, NTag, NDataTable, NSelect,
  NButton, NIcon, NModal, NFormItem, NInputNumber, NSlider,
  NProgress, NEmpty
} from 'naive-ui'
import { RefreshCircleOutline, WalletOutline } from '@vicons/ionicons5'
import type { DataTableColumns } from 'naive-ui'
import {
  getUsageToday,
  getUsageDaily,
  getUsageMonthly,
  getUsageInsights,
  getUsageBudget,
  updateUsageBudget,
  type TodayStats,
  type DailyUsage,
  type MonthlyUsage,
  type UsageInsight,
  type UsageBudget
} from '@/api/usage'

const { t } = useI18n()

const todayStats = ref<TodayStats>({
  sessions: 0,
  messages: 0,
  input_tokens: 0,
  output_tokens: 0,
  total_tokens: 0,
  cost: 0,
  avg_response_time: 0,
  top_models: []
})

const insights = ref<UsageInsight>({
  total_sessions: 0,
  total_messages: 0,
  total_input_tokens: 0,
  total_output_tokens: 0,
  total_cost: 0,
  avg_cost_per_session: 0,
  avg_cost_per_message: 0,
  avg_tokens_per_message: 0,
  most_used_model: '',
  most_active_hour: 0,
  most_active_day: ''
})

const monthlyData = ref<MonthlyUsage[]>([])
const dailyData = ref<DailyUsage[]>([])
const selectedDays = ref(30)
const showBudgetDialog = ref(false)

const budget = ref<UsageBudget>({
  limit: 0,
  current: 0,
  alert_threshold: 80
})

const budgetForm = ref({
  limit: 0,
  alert_threshold: 80
})

const daysOptions = [
  { label: '7 ' + t('usage.days'), value: 7 },
  { label: '14 ' + t('usage.days'), value: 14 },
  { label: '30 ' + t('usage.days'), value: 30 },
  { label: '90 ' + t('usage.days'), value: 90 }
]

// Safe computed for total tokens (fixes NaN)
const safeTotalTokens = computed(() => {
  const input = insights.value.total_input_tokens || 0
  const output = insights.value.total_output_tokens || 0
  const total = input + output
  return isNaN(total) ? 0 : total
})

const budgetPercent = computed(() => {
  if (!budget.value.limit || budget.value.limit === 0 || budget.value.limit === undefined) return 0
  const percent = (budget.value.current / budget.value.limit) * 100
  return Math.round(percent * 100) / 100
})

const budgetStatusType = computed(() => {
  if (!budget.value.limit || budget.value.limit === 0 || budget.value.limit === undefined) return 'success'
  if (budgetPercent.value >= 100) return 'error'
  const threshold = budget.value.alert_threshold || 80
  if (budgetPercent.value >= threshold) return 'warning'
  return 'success'
})

const budgetStatusText = computed(() => {
  if (!budget.value.limit || budget.value.limit === 0 || budget.value.limit === undefined) return t('usage.budgetOK')
  if (budgetPercent.value >= 100) return t('usage.budgetExceeded')
  const threshold = budget.value.alert_threshold || 80
  if (budgetPercent.value >= threshold) return t('usage.budgetWarning')
  return t('usage.budgetOK')
})

const monthlyColumns = computed<DataTableColumns<MonthlyUsage>>(() => [
  { title: t('usage.month'), key: 'month' },
  { title: t('usage.sessions'), key: 'total_sessions' },
  { title: t('usage.messages'), key: 'total_messages' },
  {
    title: t('usage.totalTokens'),
    key: 'total_tokens',
    render: (row) => formatNumber(row.total_tokens)
  },
  {
    title: t('usage.cost'),
    key: 'total_cost',
    render: (row) => `$${formatCost(row.total_cost)}`
  }
])

const dailyColumns = computed<DataTableColumns<DailyUsage>>(() => [
  { title: t('usage.date'), key: 'date' },
  { title: t('usage.sessions'), key: 'sessions' },
  { title: t('usage.messages'), key: 'messages' },
  {
    title: t('usage.inputTokens'),
    key: 'input_tokens',
    render: (row) => formatNumber(row.input_tokens)
  },
  {
    title: t('usage.outputTokens'),
    key: 'output_tokens',
    render: (row) => formatNumber(row.output_tokens)
  },
  {
    title: t('usage.totalTokens'),
    key: 'total_tokens',
    render: (row) => formatNumber(row.total_tokens)
  },
  {
    title: t('usage.cost'),
    key: 'cost',
    render: (row) => `$${formatCost(row.cost)}`
  }
])

const modelColumns = computed<DataTableColumns<{
  model: string
  requests: number
  tokens: number
  cost: number
  percentage: number
}>>(() => [
  { title: t('usage.model'), key: 'model' },
  { title: t('usage.requests'), key: 'requests' },
  {
    title: t('usage.tokens'),
    key: 'tokens',
    render: (row) => formatNumber(row.tokens)
  },
  {
    title: t('usage.cost'),
    key: 'cost',
    render: (row) => `$${formatCost(row.cost)}`
  },
  {
    title: t('usage.percentage'),
    key: 'percentage',
    render: (row) => `${row.percentage.toFixed(1)}%`
  }
])

function formatNumber(num: number): string {
  if (num === undefined || num === null || isNaN(num)) return '0'
  if (num >= 1000000) {
    return (num / 1000000).toFixed(2) + 'M'
  }
  if (num >= 1000) {
    return (num / 1000).toFixed(2) + 'K'
  }
  return Math.round(num).toLocaleString()
}

function formatCost(cost: number): string {
  if (cost === undefined || cost === null || isNaN(cost)) return '0.0000'
  return cost.toFixed(4)
}

async function loadStats() {
  try {
    const [today, daily, monthly, insight, budgetData] = await Promise.all([
      getUsageToday(),
      getUsageDaily(selectedDays.value),
      getUsageMonthly(),
      getUsageInsights(),
      getUsageBudget()
    ])
    todayStats.value = today
    dailyData.value = daily
    monthlyData.value = monthly
    insights.value = insight
    if (budgetData) {
      budget.value = budgetData
    }
  } catch (error) {
    console.error('Failed to load usage stats:', error)
  }
}

async function handleSaveBudget() {
  try {
    // Convert percentage (80) to decimal (0.8) for backend
    const thresholdDecimal = budgetForm.value.alert_threshold / 100
    await updateUsageBudget(budgetForm.value.limit, thresholdDecimal)
    budget.value.limit = budgetForm.value.limit
    budget.value.alert_threshold = budgetForm.value.alert_threshold
    showBudgetDialog.value = false
  } catch (error) {
    console.error('Failed to save budget:', error)
  }
}

onMounted(async () => {
  try {
    const budgetData = await getUsageBudget()
    // Convert decimal (0.8) to percentage (80) for UI
    budget.value = {
      limit: budgetData.limit || 0,
      current: budgetData.current || 0,
      alert_threshold: budgetData.alert_threshold ? budgetData.alert_threshold * 100 : 80
    }
    budgetForm.value = { ...budget.value }
  } catch (e) {
    console.error('Budget load error:', e)
  }
  loadStats()
})
</script>

<style scoped>
.stats-grid {
  margin-bottom: 24px;
}

.budget-card {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.budget-info {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.budget-limit {
  font-size: 13px;
  color: #999;
}

.budget-dialog {
  padding: 16px 0;
}

.threshold-display {
  margin-top: 8px;
  text-align: center;
  font-weight: 500;
}

.dialog-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 16px;
}
</style>
