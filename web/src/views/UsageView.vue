<template>
  <div>
    <h2 style="margin-bottom: 24px;">{{ t('usage.title') }}</h2>
    <n-spin :show="loading">

      <!-- Overview Cards -->
      <n-grid :cols="4" :x-gap="16" :y-gap="16" style="margin-bottom: 24px;">
        <n-gi>
          <n-card size="small">
            <n-statistic :label="t('usage.totalRequests')" :value="todayStats?.total_requests ?? 0" />
            <div style="font-size: 12px; color: #999; margin-top: 4px;">{{ t('usage.today') }}</div>
          </n-card>
        </n-gi>
        <n-gi>
          <n-card size="small">
            <n-statistic :label="t('usage.inputTokens')" :value="formatNumber(todayStats?.total_input_tokens ?? 0)" />
            <div style="font-size: 12px; color: #999; margin-top: 4px;">{{ t('usage.today') }}</div>
          </n-card>
        </n-gi>
        <n-gi>
          <n-card size="small">
            <n-statistic :label="t('usage.outputTokens')" :value="formatNumber(todayStats?.total_output_tokens ?? 0)" />
            <div style="font-size: 12px; color: #999; margin-top: 4px;">{{ t('usage.today') }}</div>
          </n-card>
        </n-gi>
        <n-gi>
          <n-card size="small">
            <n-statistic :label="t('usage.totalCost')">
              <template #default>
                {{ (todayStats?.total_cost ?? 0).toFixed(4) }} {{ t('usage.costUnit') }}
              </template>
            </n-statistic>
            <div style="font-size: 12px; color: #999; margin-top: 4px;">{{ t('usage.today') }}</div>
          </n-card>
        </n-gi>
      </n-grid>

      <!-- Budget -->
      <n-card :title="t('usage.budget')" size="small" style="margin-bottom: 24px;">
        <template v-if="budget && budget.limit > 0">
          <n-space vertical>
            <n-progress
              type="line"
              :percentage="budgetPercentage"
              :status="budgetStatus"
              :height="16"
            />
            <n-space>
              <n-text>{{ t('usage.budgetUsed') }}: {{ budget.current.toFixed(4) }} {{ t('usage.costUnit') }}</n-text>
              <n-text>/</n-text>
              <n-text>{{ t('usage.budgetLimit') }}: {{ budget.limit.toFixed(2) }} {{ t('usage.costUnit') }}</n-text>
              <n-text>/</n-text>
              <n-text>{{ t('usage.budgetRemaining') }}: {{ (budget.limit - budget.current).toFixed(4) }} {{ t('usage.costUnit') }}</n-text>
            </n-space>
          </n-space>
        </template>
        <n-text v-else depth="3">{{ t('usage.noBudget') }}</n-text>
      </n-card>

      <!-- Insights -->
      <n-grid :cols="2" :x-gap="16" :y-gap="16" style="margin-bottom: 24px;">
        <n-gi>
          <n-card :title="t('usage.topModels')" size="small">
            <n-data-table
              v-if="insights?.top_models?.length"
              :columns="modelColumns"
              :data="insights.top_models"
              size="small"
              :bordered="false"
            />
            <n-text v-else depth="3">{{ t('usage.noData') }}</n-text>
          </n-card>
        </n-gi>
        <n-gi>
          <n-card :title="t('usage.topProviders')" size="small">
            <n-data-table
              v-if="insights?.top_providers?.length"
              :columns="providerColumns"
              :data="insights.top_providers"
              size="small"
              :bordered="false"
            />
            <n-text v-else depth="3">{{ t('usage.noData') }}</n-text>
          </n-card>
        </n-gi>
      </n-grid>

      <!-- Daily Trend -->
      <n-card :title="t('usage.dailyTrend')" size="small" style="margin-bottom: 24px;">
        <n-data-table
          v-if="dailyData.length"
          :columns="dailyColumns"
          :data="dailyData"
          size="small"
          :bordered="false"
          :max-height="400"
        />
        <n-text v-else depth="3">{{ t('usage.noData') }}</n-text>
      </n-card>

      <!-- Recommendations -->
      <n-card v-if="insights?.recommendations?.length" :title="t('usage.recommendations')" size="small">
        <n-list bordered size="small">
          <n-list-item v-for="(rec, i) in insights.recommendations" :key="i">
            <n-text>{{ rec }}</n-text>
          </n-list-item>
        </n-list>
      </n-card>

    </n-spin>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, h } from 'vue'
import { useI18n } from 'vue-i18n'
import { NTag } from 'naive-ui'
import { getUsageToday, getUsageDaily, getUsageInsights, getUsageBudget } from '@/api/usage'
import type { DailyStats, Insights, MonthlyBudget } from '@/api/usage'

const { t } = useI18n()

const loading = ref(true)
const todayStats = ref<DailyStats | null>(null)
const dailyData = ref<DailyStats[]>([])
const insights = ref<Insights | null>(null)
const budget = ref<MonthlyBudget | null>(null)

const budgetPercentage = computed(() => {
  if (!budget.value || budget.value.limit <= 0) return 0
  return Math.min(100, (budget.value.current / budget.value.limit) * 100)
})

const budgetStatus = computed(() => {
  const pct = budgetPercentage.value
  if (pct >= 100) return 'error'
  if (pct >= 80) return 'warning'
  return 'success'
})

const modelColumns = computed(() => [
  { title: t('usage.model'), key: 'name' },
  { title: t('usage.requests'), key: 'requests' },
  {
    title: t('usage.cost'),
    key: 'cost',
    render: (row: any) => `${row.cost.toFixed(4)} ${t('usage.costUnit')}`,
  },
])

const providerColumns = computed(() => [
  { title: t('usage.provider'), key: 'name' },
  { title: t('usage.requests'), key: 'requests' },
  {
    title: t('usage.cost'),
    key: 'cost',
    render: (row: any) => `${row.cost.toFixed(4)} ${t('usage.costUnit')}`,
  },
])

const dailyColumns = computed(() => [
  { title: t('system.date', '日期'), key: 'date' },
  {
    title: t('usage.requests'),
    key: 'total_requests',
    render: (row: any) => formatNumber(row.total_requests),
  },
  {
    title: t('usage.inputTokens'),
    key: 'total_input_tokens',
    render: (row: any) => formatNumber(row.total_input_tokens),
  },
  {
    title: t('usage.outputTokens'),
    key: 'total_output_tokens',
    render: (row: any) => formatNumber(row.total_output_tokens),
  },
  {
    title: t('usage.totalCost'),
    key: 'total_cost',
    render: (row: any) => `${row.total_cost.toFixed(4)} ${t('usage.costUnit')}`,
  },
])

function formatNumber(n: number): string {
  if (n >= 1000000) return (n / 1000000).toFixed(1) + 'M'
  if (n >= 1000) return (n / 1000).toFixed(1) + 'K'
  return String(n)
}

async function loadData() {
  loading.value = true
  try {
    const [today, daily, ins, bgt] = await Promise.all([
      getUsageToday().catch(() => null),
      getUsageDaily(30).catch(() => []),
      getUsageInsights().catch(() => null),
      getUsageBudget().catch(() => null),
    ])
    todayStats.value = today
    dailyData.value = daily
    insights.value = ins
    budget.value = bgt
  } finally {
    loading.value = false
  }
}

onMounted(loadData)
</script>
