<template>
  <div class="analytics-view">
    <n-card title="Usage Analytics">
      <n-grid :cols="4" :x-gap="16" :y-gap="16">
        <n-gi>
          <n-statistic label="Total Sessions" :value="stats.totalSessions">
            <template #prefix>
              <n-icon><Chatbubbles /></n-icon>
            </template>
          </n-statistic>
        </n-gi>
        <n-gi>
          <n-statistic label="Total Tokens" :value="stats.totalTokens">
            <template #prefix>
              <n-icon><Sparkles /></n-icon>
            </template>
          </n-statistic>
        </n-gi>
        <n-gi>
          <n-statistic label="Estimated Cost" :value="`$${stats.estimatedCost.toFixed(2)}`">
            <template #prefix>
              <n-icon><Wallet /></n-icon>
            </template>
          </n-statistic>
        </n-gi>
        <n-gi>
          <n-statistic label="Cache Hit Rate" :value="`${(stats.cacheHitRate * 100).toFixed(1)}%`">
            <template #prefix>
              <n-icon><Flash /></n-icon>
            </template>
          </n-statistic>
        </n-gi>
      </n-grid>

      <n-divider />

      <n-grid :cols="2" :x-gap="16">
        <n-gi>
          <n-card title="Token Usage Breakdown" size="small">
            <div class="chart-placeholder">
              <n-progress type="circle" :percentage="inputPercentage" :stroke-width="12">
                <div class="progress-content">
                  <div>Input</div>
                  <div class="value">{{ stats.inputTokens }}</div>
                </div>
              </n-progress>
              <n-progress type="circle" :percentage="outputPercentage" :stroke-width="12" :color="outputColor">
                <div class="progress-content">
                  <div>Output</div>
                  <div class="value">{{ stats.outputTokens }}</div>
                </div>
              </n-progress>
            </div>
          </n-card>
        </n-gi>
        <n-gi>
          <n-card title="Model Usage" size="small">
            <n-list hoverable>
              <n-list-item v-for="model in modelUsage" :key="model.name">
                <n-thing :title="model.name" :description="`${model.requests} requests`">
                  <template #header-extra>
                    <n-text>{{ model.tokens }} tokens</n-text>
                  </template>
                </n-thing>
              </n-list-item>
            </n-list>
          </n-card>
        </n-gi>
      </n-grid>

      <n-divider />

      <n-card title="Daily Trend (Last 30 Days)" size="small">
        <n-data-table :columns="columns" :data="dailyData" :pagination="false" />
      </n-card>
    </n-card>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import {
  NCard, NGrid, NGi, NStatistic, NIcon, NDivider,
  NProgress, NList, NListItem, NThing, NText, NDataTable, NDatePicker
} from 'naive-ui'
import { Chatbubbles, Sparkles, Wallet, Flash } from '@vicons/ionicons5'

interface Stats {
  totalSessions: number
  totalTokens: number
  inputTokens: number
  outputTokens: number
  estimatedCost: number
  cacheHitRate: number
}

interface ModelUsage {
  name: string
  requests: number
  tokens: number
}

interface DailyData {
  date: string
  sessions: number
  tokens: number
}

const stats = ref<Stats>({
  totalSessions: 0,
  totalTokens: 0,
  inputTokens: 0,
  outputTokens: 0,
  estimatedCost: 0,
  cacheHitRate: 0
})

const modelUsage = ref<ModelUsage[]>([])

const dailyData = ref<DailyData[]>([])

const columns = [
  { title: 'Date', key: 'date' },
  { title: 'Sessions', key: 'sessions', sorter: (a: DailyData, b: DailyData) => a.sessions - b.sessions },
  { title: 'Tokens', key: 'tokens', sorter: (a: DailyData, b: DailyData) => a.tokens - b.tokens }
]

const inputPercentage = computed(() => {
  if (stats.value.totalTokens === 0) return 0
  return Math.round((stats.value.inputTokens / stats.value.totalTokens) * 100)
})

const outputPercentage = computed(() => {
  if (stats.value.totalTokens === 0) return 0
  return Math.round((stats.value.outputTokens / stats.value.totalTokens) * 100)
})

const outputColor = '#18a058'

const loadStats = async () => {
  // TODO: Call API
}

onMounted(() => {
  loadStats()
})
</script>

<style scoped>
.chart-placeholder {
  display: flex;
  justify-content: center;
  gap: 40px;
  padding: 20px;
}

.progress-content {
  text-align: center;
}

.progress-content .value {
  font-size: 1.2em;
  font-weight: bold;
}
</style>
