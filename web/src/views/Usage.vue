<template>
  <div class="usage-view">
    <n-grid :cols="4" :x-gap="16" :y-gap="16">
      <!-- Stats Cards -->
      <n-gi :span="1">
        <n-card class="stat-card">
          <n-statistic label="Total Input Tokens" :value="stats.totalInputTokens">
            <template #prefix>
              <n-icon :component="ArrowDownCircle" />
            </template>
          </n-statistic>
        </n-card>
      </n-gi>
      <n-gi :span="1">
        <n-card class="stat-card">
          <n-statistic label="Total Output Tokens" :value="stats.totalOutputTokens">
            <template #prefix>
              <n-icon :component="ArrowUpCircle" />
            </template>
          </n-statistic>
        </n-card>
      </n-gi>
      <n-gi :span="1">
        <n-card class="stat-card">
          <n-statistic label="Estimated Cost" :value="`$${stats.estimatedCost.toFixed(4)}`">
            <template #prefix>
              <n-icon :component="Cash" />
            </template>
          </n-statistic>
        </n-card>
      </n-gi>
      <n-gi :span="1">
        <n-card class="stat-card">
          <n-statistic label="Cache Hit Rate" :value="`${(stats.cacheHitRate * 100).toFixed(1)}%`">
            <template #prefix>
              <n-icon :component="Flash" />
            </template>
          </n-statistic>
        </n-card>
      </n-gi>
    </n-grid>

    <n-grid :cols="2" :x-gap="16" :y-gap="16" style="margin-top: 16px">
      <!-- Token Distribution -->
      <n-gi>
        <n-card title="Token Distribution">
          <div ref="tokenChartRef" style="height: 300px"></div>
        </n-card>
      </n-gi>

      <!-- Model Usage -->
      <n-gi>
        <n-card title="Model Usage">
          <div ref="modelChartRef" style="height: 300px"></div>
        </n-card>
      </n-gi>
    </n-grid>

    <n-grid :cols="1" style="margin-top: 16px">
      <n-gi>
        <n-card title="Daily Usage Trend (Last 30 Days)">
          <template #header-extra>
            <n-space>
              <n-date-picker
                v-model:value="dateRange"
                type="daterange"
                clearable
                @update:value="loadUsageData"
              />
              <n-button size="small" @click="exportData">
                <template #icon>
                  <n-icon :component="Download" />
                </template>
                Export
              </n-button>
            </n-space>
          </template>
          <div ref="dailyChartRef" style="height: 300px"></div>
        </n-card>
      </n-gi>
    </n-grid>

    <n-grid :cols="1" style="margin-top: 16px">
      <n-gi>
        <n-card title="Usage Details">
          <n-data-table
            :columns="columns"
            :data="usageDetails"
            :pagination="pagination"
            :bordered="false"
          />
        </n-card>
      </n-gi>
    </n-grid>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted, h } from 'vue'
import {
  NCard,
  NGrid,
  NGi,
  NStatistic,
  NIcon,
  NButton,
  NSpace,
  NDataTable,
  NDatePicker,
  NText,
  NDivider,
} from 'naive-ui'
import {
  ArrowDownCircle,
  ArrowUpCircle,
  Cash,
  Flash,
  Download,
} from '@vicons/ionicons5'
import * as echarts from 'echarts'

interface UsageStats {
  totalInputTokens: number
  totalOutputTokens: number
  estimatedCost: number
  cacheHitRate: number
  sessionCount: number
  avgDailySessions: number
}

interface DailyUsage {
  date: string
  inputTokens: number
  outputTokens: number
  cost: number
  sessions: number
}

interface ModelUsage {
  model: string
  provider: string
  inputTokens: number
  outputTokens: number
  requests: number
  cost: number
}

const stats = ref<UsageStats>({
  totalInputTokens: 0,
  totalOutputTokens: 0,
  estimatedCost: 0,
  cacheHitRate: 0,
  sessionCount: 0,
  avgDailySessions: 0,
})

const dailyUsage = ref<DailyUsage[]>([])
const modelUsage = ref<ModelUsage[]>([])
const dateRange = ref<[number, number] | null>(null)

const tokenChartRef = ref<HTMLElement>()
const modelChartRef = ref<HTMLElement>()
const dailyChartRef = ref<HTMLElement>()

const pagination = reactive({
  page: 1,
  pageSize: 10,
  showSizePicker: true,
  pageSizes: [10, 20, 50],
})

const columns = [
  { title: 'Date', key: 'date' },
  { title: 'Model', key: 'model' },
  { title: 'Provider', key: 'provider' },
  { title: 'Input Tokens', key: 'inputTokens', render: (row: ModelUsage) => row.inputTokens.toLocaleString() },
  { title: 'Output Tokens', key: 'outputTokens', render: (row: ModelUsage) => row.outputTokens.toLocaleString() },
  { title: 'Requests', key: 'requests' },
  { title: 'Cost', key: 'cost', render: (row: ModelUsage) => `$${row.cost.toFixed(4)}` },
]

const usageDetails = ref<ModelUsage[]>([])

function initCharts() {
  if (tokenChartRef.value) {
    const chart = echarts.init(tokenChartRef.value)
    chart.setOption({
      tooltip: { trigger: 'item' },
      legend: { bottom: 0 },
      series: [
        {
          name: 'Tokens',
          type: 'pie',
          radius: ['40%', '70%'],
          data: [
            { value: stats.value.totalInputTokens, name: 'Input Tokens' },
            { value: stats.value.totalOutputTokens, name: 'Output Tokens' },
          ],
        },
      ],
    })
  }

  if (modelChartRef.value) {
    const chart = echarts.init(modelChartRef.value)
    chart.setOption({
      tooltip: { trigger: 'axis' },
      legend: { bottom: 0 },
      xAxis: { type: 'category', data: modelUsage.value.map((m) => m.model) },
      yAxis: { type: 'value' },
      series: [
        {
          name: 'Requests',
          type: 'bar',
          data: modelUsage.value.map((m) => m.requests),
        },
      ],
    })
  }

  if (dailyChartRef.value) {
    const chart = echarts.init(dailyChartRef.value)
    chart.setOption({
      tooltip: { trigger: 'axis' },
      legend: { bottom: 0 },
      xAxis: { type: 'category', data: dailyUsage.value.map((d) => d.date) },
      yAxis: [
        { type: 'value', name: 'Tokens' },
        { type: 'value', name: 'Cost ($)', axisLabel: { formatter: '${value}' } },
      ],
      series: [
        {
          name: 'Input Tokens',
          type: 'bar',
          stack: 'tokens',
          data: dailyUsage.value.map((d) => d.inputTokens),
        },
        {
          name: 'Output Tokens',
          type: 'bar',
          stack: 'tokens',
          data: dailyUsage.value.map((d) => d.outputTokens),
        },
        {
          name: 'Cost',
          type: 'line',
          yAxisIndex: 1,
          data: dailyUsage.value.map((d) => d.cost),
        },
      ],
    })
  }
}

async function loadUsageData() {
  try {
    const params = new URLSearchParams()
    if (dateRange.value) {
      params.set('start', dateRange.value[0].toString())
      params.set('end', dateRange.value[1].toString())
    }

    const res = await fetch(`/api/usage/stats?${params}`)
    if (res.ok) {
      const data = await res.json()
      stats.value = data.stats
      dailyUsage.value = data.daily
      modelUsage.value = data.models
      usageDetails.value = data.models
    }
  } catch (e) {
    console.error('Failed to load usage data:', e)
  }
}

async function exportData() {
  try {
    const params = new URLSearchParams()
    if (dateRange.value) {
      params.set('start', dateRange.value[0].toString())
      params.set('end', dateRange.value[1].toString())
    }

    const res = await fetch(`/api/usage/export?${params}`)
    if (res.ok) {
      const blob = await res.blob()
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = `usage-${new Date().toISOString().split('T')[0]}.csv`
      a.click()
      URL.revokeObjectURL(url)
    }
  } catch (e) {
    console.error('Failed to export data:', e)
  }
}

onMounted(() => {
  loadUsageData()
  // Initialize charts after data loads
  setTimeout(initCharts, 100)
})
</script>

<style lang="scss" scoped>
.usage-view {
  padding: 16px;
}

.stat-card {
  text-align: center;
}
</style>
