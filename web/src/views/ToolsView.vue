<template>
  <div>
    <h2 style="margin-bottom: 24px;">{{ t('tools.title') }}</h2>
    <n-spin v-if="toolsStore.loading" />
    <template v-else>
      <!-- Tool Statistics Overview -->
      <n-card :title="t('tools.statistics')" style="margin-bottom: 24px;" v-if="toolStats.length > 0">
        <n-grid :cols="4" :x-gap="12">
          <n-gi>
            <n-statistic :label="t('tools.totalCalls')" :value="totalCalls" />
          </n-gi>
          <n-gi>
            <n-statistic :label="t('tools.avgSuccessRate')" :value="avgSuccessRate.toFixed(1)" suffix="%" />
          </n-gi>
          <n-gi>
            <n-statistic :label="t('tools.topTool')" :value="topToolName" />
          </n-gi>
          <n-gi>
            <n-statistic :label="t('tools.failingTools')" :value="failingToolsCount" />
          </n-gi>
        </n-grid>
      </n-card>

      <!-- Top Tools -->
      <n-card :title="t('tools.topTools')" style="margin-bottom: 24px;" v-if="topTools.length > 0">
        <n-list>
          <n-list-item v-for="(tool, index) in topTools" :key="tool.tool_name">
            <n-thing :title="`${index + 1}. ${tool.tool_name}`">
              <template #description>
                <n-space>
                  <n-tag :type="getSuccessRateType(tool.success_rate)" size="small">
                    {{ (tool.success_rate * 100).toFixed(0) }}% {{ t('tools.successRate') }}
                  </n-tag>
                  <n-tag size="small">{{ tool.total_calls }} {{ t('tools.calls') }}</n-tag>
                  <n-tag v-if="tool.trend !== 'stable'" :type="getTrendType(tool.trend)" size="small">
                    {{ t(`tools.trends.${tool.trend}`) }}
                  </n-tag>
                </n-space>
              </template>
            </n-thing>
          </n-list-item>
        </n-list>
      </n-card>

      <!-- Toolsets -->
      <n-card :title="t('tools.toolsets')">
        <n-grid :cols="3" :x-gap="12" :y-gap="12">
          <n-gi v-for="toolset in toolsStore.toolsets" :key="toolset.id">
            <n-card size="small" hoverable>
              <template #header>
                <n-space align="center" justify="space-between" style="width: 100%;">
                  <n-space align="center">
                    <span style="font-weight: 500;">{{ toolset.name }}</span>
                    <n-tag :type="toolset.enabled ? 'success' : 'default'" size="small">
                      {{ toolset.enabled ? t('tools.enabled') : t('tools.disabled') }}
                    </n-tag>
                  </n-space>
                  <n-tag v-if="getToolsetStat(toolset.name)" size="tiny" type="info">
                    {{ getToolsetStat(toolset.name)!.total_calls }} {{ t('tools.calls') }}
                  </n-tag>
                </n-space>
              </template>
              <template #header-extra>
                <n-switch :value="toolset.enabled" size="small" @update:value="toggleToolset(toolset.id, $event)" />
              </template>
              <n-text depth="3">{{ toolset.description || t('tools.noDescription') }}</n-text>
              <template #footer>
                <n-space justify="space-between">
                  <n-text depth="3" style="font-size: 12px;">
                    {{ t('tools.toolsCount', { count: toolset.tools?.length || 0 }) }}
                  </n-text>
                  <n-button text size="tiny" @click="showToolsetDetail(toolset, $event)">
                    {{ t('tools.viewDetails') }}
                  </n-button>
                </n-space>
              </template>
            </n-card>
          </n-gi>
        </n-grid>
        <n-empty v-if="!toolsStore.toolsets.length" :description="t('tools.noToolsets')" />
      </n-card>
    </template>

    <!-- Toolset Detail Modal -->
    <n-modal 
      v-model:show="showDetailModal" 
      :title="selectedToolset?.name" 
      preset="card" 
      style="width: 600px;"
      @update:show="handleModalShowChange"
    >
      <n-space vertical v-if="selectedToolset">
        <n-descriptions bordered>
          <n-descriptions-item :label="t('tools.description')">
            {{ selectedToolset.description || t('tools.noDescription') }}
          </n-descriptions-item>
          <n-descriptions-item :label="t('tools.toolsCount', { count: selectedToolset.tools?.length || 0 })">
            {{ selectedToolset.tools?.length || 0 }}
          </n-descriptions-item>
        </n-descriptions>
        
        <!-- Toolset Statistics -->
        <n-card v-if="getToolsetStat(selectedToolset.name)" :title="t('tools.statistics')" size="small">
          <n-grid :cols="2" :x-gap="12">
            <n-gi>
              <n-statistic :label="t('tools.totalCalls')" :value="getToolsetStat(selectedToolset.name)!.total_calls" />
            </n-gi>
            <n-gi>
              <n-statistic :label="t('tools.lastUsed')" :value="formatTime(getToolsetStat(selectedToolset.name)!.last_used)" />
            </n-gi>
          </n-grid>
        </n-card>

        <!-- Tools in Toolset -->
        <n-card :title="t('tools.toolsInToolset')" size="small">
          <n-list v-if="selectedToolset.tools && selectedToolset.tools.length > 0">
            <n-list-item v-for="toolName in selectedToolset.tools" :key="toolName">
              <n-thing :title="toolName">
                <template #description>
                  <n-space v-if="getToolStat(toolName)">
                    <n-tag :type="getSuccessRateType(getToolStat(toolName)!.success_rate)" size="small">
                      {{ (getToolStat(toolName)!.success_rate * 100).toFixed(0) }}%
                    </n-tag>
                    <n-tag size="small">{{ getToolStat(toolName)!.total_calls }} {{ t('tools.calls') }}</n-tag>
                  </n-space>
                  <n-text v-else depth="3" style="font-size: 12px;">{{ t('tools.noStats') }}</n-text>
                </template>
              </n-thing>
            </n-list-item>
          </n-list>
          <n-empty v-else :description="t('tools.noToolsInToolset')" />
        </n-card>
      </n-space>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import { useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { useToolsStore } from '@/stores/tools'
import { getToolStatistics, getToolsetStatistics } from '@/api/tools'

const { t } = useI18n()
const message = useMessage()
const toolsStore = useToolsStore()

// UI State
const showDetailModal = ref(false)
const selectedToolset = ref<any>(null)

// Data
const toolStats = ref<ToolStatistics[]>([])
const toolsetStats = ref<ToolsetStatistics[]>([])

// Types
interface ToolStatistics {
  tool_name: string
  total_calls: number
  success_calls: number
  failed_calls: number
  success_rate: number
  avg_duration: number
  last_used: string
  trend: string
}

interface ToolsetStatistics {
  toolset_name: string
  total_calls: number
  tool_stats: Record<string, number>
  last_used: string
}

// Computed
const totalCalls = computed(() => {
  return toolStats.value.reduce((sum, t) => sum + t.total_calls, 0)
})

const avgSuccessRate = computed(() => {
  if (toolStats.value.length === 0) return 0
  const total = toolStats.value.reduce((sum, t) => sum + t.success_rate, 0)
  return (total / toolStats.value.length) * 100
})

const topToolName = computed(() => {
  if (toolStats.value.length === 0) return '-'
  const top = toolStats.value.reduce((max, t) => t.total_calls > max.total_calls ? t : max, toolStats.value[0])
  return top.tool_name
})

const failingToolsCount = computed(() => {
  return toolStats.value.filter(t => t.success_rate < 0.5 && t.total_calls > 5).length
})

const topTools = computed(() => {
  return [...toolStats.value]
    .sort((a, b) => b.total_calls - a.total_calls)
    .slice(0, 5)
})

// Methods
function getSuccessRateType(rate: number): 'success' | 'warning' | 'error' {
  if (rate >= 0.8) return 'success'
  if (rate >= 0.5) return 'warning'
  return 'error'
}

function getTrendType(trend: string): 'success' | 'warning' | 'error' | 'default' {
  switch (trend) {
    case 'improving': return 'success'
    case 'declining': return 'error'
    case 'stable': return 'default'
    default: return 'default'
  }
}

function getToolStat(toolName: string): ToolStatistics | undefined {
  return toolStats.value.find(t => t.tool_name === toolName)
}

function getToolsetStat(toolsetName: string): ToolsetStatistics | undefined {
  return toolsetStats.value.find(t => t.toolset_name === toolsetName)
}

function formatTime(timeStr: string): string {
  if (!timeStr) return '-'
  return new Date(timeStr).toLocaleString()
}

function showToolsetDetail(toolset: any, event?: Event) {
  // Prevent focus from staying on the button
  if (event && event.target instanceof HTMLElement) {
    event.target.blur()
  }
  selectedToolset.value = toolset
  showDetailModal.value = true
}

function handleModalShowChange(visible: boolean) {
  if (visible) {
    // When modal opens, focus should move inside the modal
    // Naive UI's Modal should handle this automatically, but we can ensure it
    setTimeout(() => {
      const modalContent = document.querySelector('.n-modal-content')
      if (modalContent) {
        const focusable = modalContent.querySelector('button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])') as HTMLElement
        if (focusable) {
          focusable.focus()
        }
      }
    }, 100)
  }
}

async function toggleToolset(id: string, enabled: boolean): Promise<void> {
  try {
    await toolsStore.toggleToolset(id, enabled)
    await toolsStore.loadToolsets()
    message.success(enabled ? t('tools.toolsetEnabled') : t('tools.toolsetDisabled'))
  } catch (e) {
    message.error(t('tools.failedToToggle'))
  }
}

onMounted(async () => {
  await toolsStore.loadToolsets()
  
  // Load statistics
  try {
    const [tools, toolsets] = await Promise.all([
      getToolStatistics(),
      getToolsetStatistics()
    ])
    toolStats.value = tools
    toolsetStats.value = toolsets
  } catch (e) {
    console.error('Failed to load tool statistics:', e)
  }
})
</script>