<template>
  <!-- 极简执行状态条：单行、不可交互、无下拉；只负责告知“正在执行” -->
  <div class="task-dock" role="status" aria-live="polite">
    <div class="task-dock-bar" :class="barClass">
      <span class="td-ic">
        <span v-if="isError" class="td-ic-x">✕</span>
        <span v-else class="td-spin"></span>
      </span>
      <span class="td-label">{{ label }}</span>
      <span v-if="showPct" class="td-pct">{{ pct }}%</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { PropType } from 'vue'
import { useI18n } from 'vue-i18n'
import type { TaskProgress } from '@/stores/chat'

export interface TimelineStep {
  title: string
  description?: string
  status: 'pending' | 'running' | 'completed' | 'failed' | 'skipped'
  detail?: string
  duration?: string
}

const props = defineProps({
  steps: { type: Array as PropType<TimelineStep[]>, required: true },
  progress: { type: Object as PropType<TaskProgress | null>, default: null },
})

const { t } = useI18n()

const runningStep = computed(() => props.steps.find((s) => s.status === 'running'))
const failedStep = computed(() => props.steps.find((s) => s.status === 'failed'))
// 结果整合阶段：没有工具在跑，模型正在生成最终回复
const synthesizing = computed(
  () => !!runningStep.value && runningStep.value.title === t('chat.resultSynthesis'),
)

const pct = computed(() => {
  if (props.progress && Number.isFinite(props.progress.percent)) {
    return Math.max(0, Math.min(100, Math.round(props.progress.percent)))
  }
  if (props.steps.length === 0) return 0
  const done = props.steps.filter((s) => s.status === 'completed' || s.status === 'skipped').length
  const running = props.steps.filter((s) => s.status === 'running').length
  return Math.round(((done + running * 0.5) / props.steps.length) * 100)
})

const isError = computed(() => !!failedStep.value && !runningStep.value)

// 单行文案：正在执行 X → 后端阶段 → 生成中 → 兜底“任务进行中”
const label = computed(() => {
  if (runningStep.value) {
    if (synthesizing.value) return t('chat.generating')
    return t('chat.executingTool', { name: runningStep.value.title })
  }
  if (isError.value) return `${t('chat.taskFailed')} · ${failedStep.value!.title}`
  if (props.progress && pct.value < 100) {
    const parts = [props.progress.phase, props.progress.detail].filter(Boolean)
    if (parts.length) return parts.join(' · ')
  }
  return t('chat.taskInProgress')
})

const showPct = computed(() => !isError.value && pct.value > 0 && pct.value < 100)

const barClass = computed(() => (isError.value ? 'is-error' : 'is-running'))
</script>

<style scoped>
.task-dock {
  max-width: 900px;
  margin: 0 auto;
  user-select: none;
}

.task-dock-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  height: 26px;
  padding: 0 12px;
  border-radius: 8px;
  font-size: 12px;
  line-height: 1;
  white-space: nowrap;
}

.task-dock-bar.is-running {
  background: #eef4ff;
  border: 1px solid #dbe7fd;
  color: #2563eb;
}

.task-dock-bar.is-error {
  background: #fdf0f1;
  border: 1px solid #f5d3d8;
  color: #c03348;
}

.td-ic {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  width: 14px;
  height: 14px;
}

.td-spin {
  width: 12px;
  height: 12px;
  border: 2px solid rgba(32, 128, 240, 0.25);
  border-top-color: #2080f0;
  border-radius: 50%;
  animation: td-spin 0.8s linear infinite;
  display: inline-block;
  box-sizing: border-box;
}
.task-dock-bar.is-error .td-spin {
  display: none;
}

.td-ic-x {
  font-size: 11px;
  font-weight: 700;
  line-height: 1;
}

@keyframes td-spin {
  to { transform: rotate(360deg); }
}

.td-label {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  font-weight: 500;
}

.td-pct {
  flex-shrink: 0;
  font-size: 11px;
  font-weight: 600;
  opacity: 0.75;
  font-variant-numeric: tabular-nums;
}

/* ============ 暗色 ============ */
@media (prefers-color-scheme: dark) {
  .task-dock-bar.is-running {
    background: #15253d;
    border-color: #2c4a78;
    color: #93c5fd;
  }
  .task-dock-bar.is-error {
    background: #2c1318;
    border-color: #5c232b;
    color: #f0a0ab;
  }
  .td-spin {
    border-color: rgba(110, 168, 255, 0.25);
    border-top-color: #6ea8ff;
  }
}
</style>
