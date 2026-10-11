<template>
  <!-- 执行过程：把「思考 + 工具调用」合并成一个折叠区（WorkBuddy 风格）。
       折叠态：一个头部（状态点 + 标题 + 工具次数 / 耗时），外加最后一步摘要。
       展开态：按发生顺序穿插渲染 思考段 ↔ 工具调用卡片。
       最终回答由父组件单独渲染在折叠区之外，不受这里影响。 -->
  <div v-if="hasProcess" class="process-block" :class="{ collapsed: !expanded }">
    <button class="process-toggle" type="button" :aria-expanded="expanded" @click="toggle">
      <span class="process-indicator" :class="{ pulsing: streaming, failed: hasError }"></span>
      <span class="process-title">{{ statusText }}</span>
      <span v-if="toolCount > 0" class="process-meta">{{ t('chat.processSteps', { n: toolCount }) }}</span>
      <span v-if="durationText" class="process-duration">{{ durationText }}</span>
      <span class="process-chevron" :class="{ open: expanded }">
        <svg viewBox="0 0 16 16" width="12" height="12" aria-hidden="true">
          <path d="M4 6l4 4 4-4" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" />
        </svg>
      </span>
    </button>

    <!-- 折叠态摘要：最后一句思考 / 最后一个工具摘要，点击即展开 -->
    <button
      v-if="!expanded && collapsedHint"
      class="process-hint"
      type="button"
      @click="toggle"
    >{{ collapsedHint }}</button>

    <n-collapse-transition :show="expanded">
      <div class="process-body">
        <template v-for="item in items" :key="item.key">
          <div v-if="item.kind === 'text'" class="process-thought">
            <ReasoningBody :text="item.text || ''" />
          </div>
          <ToolCallCard
            v-else-if="item.tool"
            :tool="item.tool"
          />
        </template>
      </div>
    </n-collapse-transition>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { NCollapseTransition } from 'naive-ui'
import ToolCallCard from './ToolCallCard.vue'
import ReasoningBody from './ReasoningBody.vue'
import { stripZeroWidth } from '@/utils/text'
import type { ToolCallEvent } from '@/stores/chat'

interface ProcessSegment {
  kind: 'text' | 'tool'
  end?: number
  toolCallId?: string
}

const props = defineProps<{
  // 完整文本（含 <think> 标签）。文本段通过 [start, end) 切片取用；
  // 无 segments 时整段文本作为单个思考段。
  content: string
  segments?: ReadonlyArray<unknown> | ProcessSegment[]
  tools: ToolCallEvent[]
  streaming?: boolean
}>()

const { t } = useI18n()

// 默认展开，让用户看到实时过程；用户手动 toggle 后固定，不再自动改回。
const userToggled = ref(false)
const expanded = ref(true)

watch(
  () => !!props.streaming,
  (v, old) => {
    if (userToggled.value) return
    // 流式中展开看过程，流结束后自动折叠（成品导向）
    if (v) expanded.value = true
    else if (old) expanded.value = false
  },
  { immediate: true },
)

function toggle() {
  userToggled.value = true
  expanded.value = !expanded.value
}

// ---- segments 规范化 ----
const normalizedSegments = computed<ProcessSegment[]>(() => {
  const raw = props.segments
  if (!raw || raw.length === 0) return []
  const out: ProcessSegment[] = []
  for (const s of raw) {
    if (!s || typeof s !== 'object') continue
    const obj = s as Record<string, unknown>
    if (obj.kind === 'text' && typeof obj.end === 'number') {
      out.push({ kind: 'text', end: obj.end })
    } else if (obj.kind === 'tool' && typeof obj.toolCallId === 'string') {
      out.push({ kind: 'tool', toolCallId: obj.toolCallId })
    }
  }
  return out
})

// ---- 文本切片：text 段 [start, end)，tool 段不消耗文本 ----
function textStartOf(idx: number): number {
  const segs = normalizedSegments.value
  for (let i = idx - 1; i >= 0; i--) {
    if (segs[i].kind === 'text') return segs[i].end as number
  }
  return 0
}

const toolsById = computed<Record<string, ToolCallEvent>>(() => {
  const m: Record<string, ToolCallEvent> = {}
  for (const tc of props.tools) {
    if (tc && tc.id) m[tc.id] = tc
  }
  return m
})

// ---- 解析出展示项：有 segments 时按序切片，否则单段兜底 ----
const items = computed<Array<{ key: string; kind: 'text' | 'tool'; text?: string; tool?: ToolCallEvent }>>(() => {
  const segs = normalizedSegments.value
  const out: Array<{ key: string; kind: 'text' | 'tool'; text?: string; tool?: ToolCallEvent }> = []
  if (segs.length === 0) {
    if (props.content) out.push({ key: 'text_all', kind: 'text', text: props.content })
    return out
  }
  segs.forEach((seg, idx) => {
    if (seg.kind === 'text') {
      const start = Math.max(0, Math.min(textStartOf(idx), seg.end as number))
      const text = props.content.substring(start, seg.end as number)
      out.push({ key: `seg_${idx}`, kind: 'text', text })
    } else {
      const tool = seg.toolCallId ? toolsById.value[seg.toolCallId] : undefined
      if (tool) out.push({ key: `seg_${idx}`, kind: 'tool', tool })
    }
  })
  return out
})

const hasProcess = computed(() => items.value.some((it) => (it.kind === 'text' ? !!it.text?.trim() : !!it.tool)))
const toolCount = computed(() => items.value.filter((it) => it.kind === 'tool').length)
const hasError = computed(() => props.tools.some((tc) => tc.status === 'error' || tc.success === false))

const statusText = computed(() => {
  if (props.streaming) return t('chat.processRunning')
  if (hasError.value) return t('chat.processDoneFailed')
  return t('chat.processDone')
})

// ---- 折叠态摘要：优先最后一段思考的纯文本，否则最后一个工具的参数摘要 ----
function plainText(src: string): string {
  return stripZeroWidth(src)
    .replace(/```[\s\S]*?```/g, ' ')
    .replace(/[#*`>\-]+/g, ' ')
    .replace(/\s+/g, ' ')
    .trim()
}

const collapsedHint = computed(() => {
  const list = items.value
  for (let i = list.length - 1; i >= 0; i--) {
    const it = list[i]
    if (it.kind === 'text' && it.text) {
      const p = plainText(it.text)
      if (p) return p.length > 110 ? `${p.slice(0, 110)}…` : p
    }
    if (it.kind === 'tool' && it.tool) {
      const label = it.tool.name || ''
      const arg = (it.tool.args || '').trim()
      const oneLine = arg.split('\n').map((l) => l.trim()).find((l) => l) || ''
      const s = oneLine.length > 90 ? `${oneLine.slice(0, 90)}…` : oneLine
      return s ? `${label} ${s}` : label
    }
  }
  return ''
})

// ---- 耗时：取所有工具耗时之和（字符串形如 "2.2s"） ----
const durationText = computed(() => {
  let total = 0
  let found = false
  for (const tc of props.tools) {
    const m = /([\d.]+)\s*s/.exec(tc.duration || '')
    if (m) {
      total += parseFloat(m[1])
      found = true
    }
  }
  return found ? `${total.toFixed(1)}s` : ''
})
</script>

<style scoped>
.process-block {
  border-left: 2px solid #e5e7eb;
  padding-left: 14px;
  margin-bottom: 2px;
}
.process-block.collapsed {
  border-left-color: #f3f4f6;
}

.process-toggle {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 2px 0;
  background: none;
  border: none;
  cursor: pointer;
  user-select: none;
  color: #6b7280;
  font-size: 13px;
  font-weight: 500;
  text-align: left;
}
.process-toggle:hover {
  color: #374151;
}

.process-indicator {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #9ca3af;
  flex-shrink: 0;
  transition: background 0.2s;
}
.process-indicator.pulsing {
  background: #3b82f6;
  animation: process-pulse 1.4s ease-out infinite;
}
.process-indicator.failed {
  background: #c14a4a;
}
@keyframes process-pulse {
  0% { box-shadow: 0 0 0 0 rgba(59, 130, 246, 0.4); }
  100% { box-shadow: 0 0 0 6px rgba(59, 130, 246, 0); }
}

.process-title {
  flex-shrink: 0;
}

.process-meta,
.process-duration {
  font-size: 11.5px;
  color: #9ca3af;
  font-family: 'SF Mono', 'Consolas', monospace;
  font-variant-numeric: tabular-nums;
  flex-shrink: 0;
}

.process-chevron {
  margin-left: auto;
  color: #9ca3af;
  display: inline-flex;
  transition: transform 0.2s;
}
.process-chevron.open {
  transform: rotate(180deg);
}
.process-toggle:hover .process-chevron {
  color: #6b7280;
}

.process-hint {
  display: block;
  width: 100%;
  margin-top: 2px;
  padding: 0;
  background: none;
  border: none;
  cursor: pointer;
  text-align: left;
  font-size: 12px;
  line-height: 1.5;
  color: #b0b5bd;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  user-select: none;
}
.process-hint:hover {
  color: #8b919a;
}

.process-body {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 10px 0 6px 0;
}

/* 思考段：以左侧强调线区分于工具卡片 */
.process-thought {
  border-left: 2px solid #eef0f3;
  padding-left: 10px;
  margin: 2px 0;
}

@media (prefers-color-scheme: dark) {
  .process-block {
    border-left-color: #374151;
  }
  .process-block.collapsed {
    border-left-color: #2c2c33;
  }
  .process-toggle {
    color: #9ca3af;
  }
  .process-toggle:hover {
    color: #d1d5db;
  }
  .process-indicator {
    background: #6b7280;
  }
  .process-indicator.pulsing {
    background: #60a5fa;
  }
  .process-indicator.failed {
    background: #e89292;
  }
  @keyframes process-pulse {
    0% { box-shadow: 0 0 0 0 rgba(96, 165, 250, 0.4); }
    100% { box-shadow: 0 0 0 6px rgba(96, 165, 250, 0); }
  }
  .process-meta,
  .process-duration {
    color: #6b7280;
  }
  .process-chevron {
    color: #6b7280;
  }
  .process-hint {
    color: #565b64;
  }
  .process-hint:hover {
    color: #8b919a;
  }
  .process-thought {
    border-left-color: #2c2d31;
  }
}
</style>
