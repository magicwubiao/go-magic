<template>
  <!--
    底部执行坞（沉底，紧贴输入框上方）。
    形态参考 WorkBuddy：不再把所有工具压成一行字符串，而是展示
      · 左侧：当前正在进行的一行摘要（工具名 + 参数 + 状态 + 百分比）
      · 右侧：折叠计数「已完成 N 项」
    点击整条可展开，看到本回合逐条工具调用的时间线（每条带图标/名称/参数摘要/耗时/成败），
    正在跑的那条高亮并带 spinner，已完成/失败各有状态色。
  -->
  <div class="task-dock" role="status" aria-live="polite">
    <div class="task-dock-top">
      <button
        class="task-dock-bar"
        type="button"
        :class="barClass"
        :aria-expanded="expanded"
        @click="toggle"
      >
        <span class="td-ic">
          <span v-if="isError" class="td-ic-x">✕</span>
          <span v-else class="td-spin"></span>
        </span>
        <span class="td-label">{{ label }}</span>
        <span v-if="stepCount" class="td-count">
          {{ t('chat.dockDoneCount', { done: doneCount, total: stepCount }) }}
        </span>
        <span v-if="showPct" class="td-pct">{{ pct }}%</span>
        <span class="td-chevron" :class="{ open: expanded }">
          <svg viewBox="0 0 16 16" width="11" height="11" aria-hidden="true">
            <path
              d="M4 6l4 4 4-4"
              fill="none"
              stroke="currentColor"
              stroke-width="1.6"
              stroke-linecap="round"
              stroke-linejoin="round"
            />
          </svg>
        </span>
      </button>
    </div>

    <!-- 展开体：逐条工具调用时间线。默认收起以省空间，用户手动展开后保持状态。 -->
    <n-collapse-transition :show="expanded && stepCount > 0">
      <div class="task-dock-body">
        <div v-for="step in decoratedSteps" :key="step.key" class="td-step" :class="`s-${step.status}`">
          <span class="td-step-ic">
            <span v-if="step.status === 'running'" class="td-spin td-spin-sm"></span>
            <span v-else-if="step.status === 'failed'" class="td-step-glyph td-step-fail">✕</span>
            <span v-else-if="step.status === 'completed'" class="td-step-glyph td-step-ok">✓</span>
            <span v-else class="td-step-glyph td-step-pending">•</span>
          </span>
          <span class="td-step-emoji" aria-hidden="true">{{ step.emoji }}</span>
          <span class="td-step-name">{{ step.title }}</span>
          <span v-if="step.summary" class="td-step-summary" :title="step.summary">
            {{ step.summary }}
          </span>
          <span v-if="step.duration" class="td-step-duration">{{ step.duration }}</span>
        </div>
      </div>
    </n-collapse-transition>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { PropType } from 'vue'
import { useI18n } from 'vue-i18n'
import type { TaskProgress } from '@/stores/chat'
import { toolEmojiByTitle } from '@/utils/toolCallView'
import { NCollapseTransition } from 'naive-ui'

export interface TimelineStep {
  /** 稳定 key（工具调用 id），避免动画/复用错位 */
  key: string
  title: string
  /** 单行参数摘要，如 `src/App.vue` 或 `npm run build` */
  summary?: string
  description?: string
  status: 'pending' | 'running' | 'completed' | 'failed' | 'skipped'
  detail?: string
  duration?: string
  /** 该步是否为工具调用（合成阶段不是，不显示 emoji） */
  isTool?: boolean
}

const props = defineProps({
  steps: { type: Array as PropType<TimelineStep[]>, required: true },
  progress: { type: Object as PropType<TaskProgress | null>, default: null },
})

const { t } = useI18n()

// 默认折叠：坞的本职是「一眼看到在干什么」，展开属于按需行为。
const expanded = ref(false)
// 用户手动开合过之后，不再跟随状态自动变化（与 ToolCallGroup 同一约定）。
const userToggled = ref(false)

function toggle(): void {
  userToggled.value = true
  expanded.value = !expanded.value
}

// 出现错误时自动展开一次，让失败原因可见（用户显式收起后不再抢）。
watch(
  () => props.steps.find((s) => s.status === 'failed')?.key,
  (key, prev) => {
    if (key && key !== prev && !userToggled.value) expanded.value = true
  },
)

// 正在运行的那一步。用「最后一个」而不是「第一个」：同一个工具可能被连续调用
// 多次（读 5 个文件），此时只有最后一条才是真正在跑的那条；取第一个会让头部
// 永远停在这轮最早的一次调用上，看着像卡住了。
const runningStep = computed(() => {
  for (let i = props.steps.length - 1; i >= 0; i--) {
    if (props.steps[i].status === 'running') return props.steps[i]
  }
  return undefined
})
const failedStep = computed(() => props.steps.find((s) => s.status === 'failed'))
// 结果整合阶段：没有工具在跑，模型正在生成最终回复
const synthesizing = computed(
  () => !!runningStep.value && runningStep.value.title === t('chat.resultSynthesis'),
)

const stepCount = computed(() => props.steps.length)
const doneCount = computed(
  () => props.steps.filter((s) => s.status === 'completed' || s.status === 'skipped').length,
)

const pct = computed(() => {
  if (props.progress && Number.isFinite(props.progress.percent)) {
    return Math.max(0, Math.min(100, Math.round(props.progress.percent)))
  }
  if (props.steps.length === 0) return 0
  const done = doneCount.value
  const running = props.steps.filter((s) => s.status === 'running').length
  return Math.round(((done + running * 0.5) / props.steps.length) * 100)
})

const isError = computed(() => !!failedStep.value && !runningStep.value)

// 摘要行文案优先级：正在执行 X → 后端阶段 → 生成中 → 兜底「任务进行中」
const label = computed(() => {
  if (runningStep.value) {
    if (synthesizing.value) return t('chat.generating')
    const name = runningStep.value.title
    const sum = runningStep.value.summary
    // 有参数摘要时提升信息密度：`运行命令 · npm run build`
    return sum ? `${name} · ${sum}` : t('chat.executingTool', { name })
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

// steps → 展示项：补齐 emoji（合成/规划阶段不是工具，不给图标）
const decoratedSteps = computed(() =>
  props.steps.map((s) => ({ ...s, emoji: s.isTool === false ? '' : toolEmojiByTitle(s.title) })),
)
</script>

<style scoped>
.task-dock {
  /* 现在内联在助手消息体里，宽度由 .message-body 决定，不再自己居中定宽；
     保留 max-width 仅为防止在超宽屏上拉得过长。 */
  max-width: 900px;
  user-select: none;
}

.task-dock-top {
  display: flex;
}

.task-dock-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  height: 28px;
  padding: 0 10px;
  border-radius: 8px;
  font-size: 12px;
  line-height: 1;
  white-space: nowrap;
  text-align: left;
  cursor: pointer;
  font-family: inherit;
  transition: background 0.15s, border-color 0.15s;
}

.task-dock-bar.is-running {
  background: #f5f7fb;
  border: 1px solid #e6ebf3;
  color: #4b5563;
}
.task-dock-bar.is-running:hover {
  background: #eef2f9;
  border-color: #d8e0ec;
}

.task-dock-bar.is-error {
  background: #fdf0f1;
  border: 1px solid #f5d3d8;
  color: #c03348;
}
.task-dock-bar.is-error:hover {
  background: #fbe7e9;
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
.td-spin-sm {
  width: 10px;
  height: 10px;
  border-width: 1.5px;
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
  to {
    transform: rotate(360deg);
  }
}

.td-label {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  font-weight: 500;
  color: #2563eb;
  /* 不能沿用 bar 的 line-height:1：行盒正好等于字号时，Windows 字体回退的
     字形上伸部会超出 em 框，被自身的 overflow:hidden 削掉上半截
     （"terminal · git push … 文字被挡到一半"就是这个）。显式放宽行盒，
     align-items:center 仍保证垂直居中，ellipsis 不受影响。 */
  line-height: 18px;
}
.task-dock-bar.is-error .td-label {
  color: inherit;
}

.td-count {
  flex-shrink: 0;
  font-size: 11px;
  color: #9ca3af;
  font-variant-numeric: tabular-nums;
}

.td-pct {
  flex-shrink: 0;
  font-size: 11px;
  font-weight: 600;
  opacity: 0.75;
  font-variant-numeric: tabular-nums;
}

.td-chevron {
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  color: #9ca3af;
  transition: transform 0.18s;
}
.td-chevron.open {
  transform: rotate(180deg);
}

/* ========== 展开体：逐条工具时间线 ========== */
.task-dock-body {
  margin-top: 4px;
  padding: 6px 10px 6px 12px;
  border: 1px solid #e6ebf3;
  border-radius: 8px;
  background: #fafbfd;
  /* 6px×2 内边距 + 7×24px 整行 = 180px：滚动截断处永远停在整行边界，
     不会把最后一行文字切一半吊在那（"文字一半被遮挡"就是这么来的）。 */
  max-height: 180px;
  overflow-y: auto;
}

.td-step {
  display: flex;
  align-items: center;
  gap: 7px;
  /* min-height 而不是固定 height：万一内容比预期高（字体回退、缩放），
     行会自己长高而不是把文字溢出到相邻行上，看起来像"文字被盖住一半"。 */
  min-height: 24px;
  font-size: 12px;
  color: #6b7280;
}

.td-step-ic {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 12px;
  flex-shrink: 0;
}

.td-step-glyph {
  font-size: 10px;
  line-height: 1;
}
.td-step-ok {
  color: #4a8a5a;
}
.td-step-fail {
  color: #c14a4a;
  font-weight: 700;
}
.td-step-pending {
  color: #c0c4cc;
}

.td-step-emoji {
  flex-shrink: 0;
  font-size: 11px;
  opacity: 0.85;
}

.td-step-name {
  flex-shrink: 0;
  font-weight: 500;
  color: #4b5563;
  white-space: nowrap;
}

.td-step-summary {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  /* text-overflow: ellipsis 必须搭配 nowrap 才生效——缺了它长参数会折行，
     在固定行高里溢出压到下一行上，正是"文字一半被遮挡"的另一半来源。 */
  white-space: nowrap;
  text-overflow: ellipsis;
  color: #9ca3af;
  font-family: 'SF Mono', 'Consolas', monospace;
  font-size: 11px;
}

.td-step-duration {
  flex-shrink: 0;
  font-size: 11px;
  color: #b6bcc6;
  font-family: 'SF Mono', 'Consolas', monospace;
  font-variant-numeric: tabular-nums;
}

/* 每步的状态色：完成偏灰（不抢眼），运行中高亮，失败标红 */
.td-step.s-completed .td-step-name {
  color: #6b7280;
  font-weight: 400;
}
.td-step.s-running .td-step-name {
  color: #2563eb;
}
.td-step.s-running .td-step-summary {
  color: #7f9cd0;
}
.td-step.s-failed .td-step-name {
  color: #c03348;
}
.td-step.s-skipped {
  opacity: 0.55;
}

/* ============ 暗色 ============ */
@media (prefers-color-scheme: dark) {
  .task-dock-bar.is-running {
    background: #1b1d22;
    border-color: #2c2d31;
    color: #c8cbd1;
  }
  .task-dock-bar.is-running:hover {
    background: #232529;
    border-color: #3a3b3f;
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
  .td-label {
    color: #93c5fd;
  }
  .td-count,
  .td-chevron {
    color: #6b6e76;
  }

  .task-dock-body {
    background: #1b1c20;
    border-color: #2c2d31;
  }
  .td-step {
    color: #a0a3ab;
  }
  .td-step-name {
    color: #b9bcc4;
  }
  .td-step-summary {
    color: #6b6e76;
  }
  .td-step-duration {
    color: #5c5f66;
  }
  .td-step-ok {
    color: #63c98e;
  }
  .td-step-fail {
    color: #e89292;
  }
  .td-step-pending {
    color: #4a4d54;
  }
  .td-step.s-completed .td-step-name {
    color: #8a8d96;
  }
  .td-step.s-running .td-step-name {
    color: #93c5fd;
  }
  .td-step.s-running .td-step-summary {
    color: #6d8cba;
  }
  .td-step.s-failed .td-step-name {
    color: #f0a0ab;
  }
}
</style>
