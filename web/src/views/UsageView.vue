<template>
  <div class="usage-view">
    <h2>{{ t('usage.title') }}</h2>

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
          <n-statistic :value="formatNumber(insights.total_input_tokens + insights.total_output_tokens)">
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
        <n-card :title="t('usage.mostUsedModel')" size="small">
          <n-tag type="info">{{ insights.most_used_model || '-' }}</n-tag>
        </n-card>
      </n-gi>
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
    </n-grid>

    <!-- 月度统计表格 -->
    <n-card :title="t('usage.monthlyUsage')" style="margin-top: 16px">
      <n-data-table
        :columns="monthlyColumns"
        :data="monthlyData"
        :pagination="false"
        :bordered="false"
      />
    </n-card>

    <!-- 每日趋势表格 -->
    <n-card :title="t('usage.dailyTrend')" style="margin-top: 16px">
      <template #header_extra>
        <n-select
          v-model:value="selectedDays"
          :options="daysOptions"
          style="width: 120px"
          @update:value="loadDailyStats"
        />
      </template>
      <n-data-table
        :columns="dailyColumns"
        :data="dailyData"
        :pagination="{ pageSize: 10 }"
        :bordered="false"
      />
    </n-card>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  NGrid, NGi, NCard, NStatistic, NTag, NDataTable, NSelect,
  NButton, NPopconfirm
} from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import {
  getUsageToday,
  getUsageDaily,
  getUsageMonthly,
  getUsageInsights,
  type TodayStats,
  type DailyUsage,
  type MonthlyUsage,
  type UsageInsight
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

const daysOptions = [
  { label: '7 ' + t('usage.days'), value: 7 },
  { label: '14 ' + t('usage.days'), value: 14 },
  { label: '30 ' + t('usage.days'), value: 30 },
  { label: '90 ' + t('usage.days'), value: 90 }
]

const monthlyColumns: DataTableColumns<MonthlyUsage> = [
  {
    title: t('usage.month'),
    key: 'month'
  },
  {
    title: t('usage.sessions'),
    key: 'total_sessions'
  },
  {
    title: t('usage.messages'),
    key: 'total_messages'
  },
  {
    title: 'Input Tokens',
    key: 'input_tokens',
    render: (row) => formatNumber(row.total_tokens)
  },
  {
    title: 'Output Tokens',
    key: 'output_tokens',
    render: (row) => formatNumber(row.total_tokens)
  },
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
]

const dailyColumns: DataTableColumns<DailyUsage> = [
  {
    title: t('usage.date'),
    key: 'date'
  },
  {
    title: t('usage.sessions'),
    key: 'sessions'
  },
  {
    title: t('usage.messages'),
    key: 'messages'
  },
  {
    title: 'Input Tokens',
    key: 'input_tokens',
    render: (row) => formatNumber(row.input_tokens)
  },
  {
    title: 'Output Tokens',
    key: 'output_tokens',
    render: (row) => formatNumber(row.output_tokens)
  },
  {
    title: 'Total Tokens',
    key: 'total_tokens',
    render: (row) => formatNumber(row.total_tokens)
  },
  {
    title: t('usage.cost'),
    key: 'cost',
    render: (row) => `$${formatCost(row.cost)}`
  }
]

function formatNumber(num: number): string {
  if (num >= 1000000) {
    return (num / 1000000).toFixed(2) + 'M'
  }
  if (num >= 1000) {
    return (num / 1000).toFixed(2) + 'K'
  }
  return num.toString()
}

function formatCost(cost: number): string {
  return cost.toFixed(4)
}

async function loadStats() {
  try {
    const [today, daily, monthly, insight] = await Promise.all([
      getUsageToday(),
      getUsageDaily(selectedDays.value),
      getUsageMonthly(),
      getUsageInsights()
    ])
    todayStats.value = today
    dailyData.value = daily
    monthlyData.value = monthly
    insights.value = insight
  } catch (error) {
    console.error('Failed to load usage stats:', error)
  }
}

async function loadDailyStats() {
  try {
    dailyData.value = await getUsageDaily(selectedDays.value)
  } catch (error) {
    console.error('Failed to load daily stats:', error)
  }
}

onMounted(() => {
  loadStats()
})
</script>

<style scoped>
.usage-view {
  padding: 16px;
}

.stats-grid {
  margin-bottom: 0;
}
</style>
