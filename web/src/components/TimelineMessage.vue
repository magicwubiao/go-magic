<template>
  <!-- 把 assistant 回合按"发生顺序"穿插渲染：文本切片（含 <think>） + 工具调用卡片。 -->
  <!-- 数据源： -->
  <!--  - 流式：chatStore.streamingSegments + chatStore.streamContent + chatStore.toolCalls -->
  <!--  - 历史：msg.streaming_timeline_snapshot + msg.content + msg.tool_calls_snapshot -->
  <!-- 缺失 segments 的旧消息自动回退到原来的"先 Reasoning 再 ToolCallGroup"展示。 -->
  <div class="message-timeline">
    <template v-if="hasTimeline">
      <template v-for="(seg, idx) in normalizedSegments" :key="seg.id">
        <ReasoningContent
          v-if="seg.kind === 'text' && textSlices[idx]"
          :content="textSlices[idx]"
          :streaming="!!streaming"
          :allow-promote="!isIntermediateSlice(idx)"
        />
        <ToolCallCard
          v-else-if="seg.kind === 'tool' && toolsById[seg.toolCallId]"
          :tool="toolsById[seg.toolCallId]"
        />
      </template>
    </template>
    <template v-else>
      <ReasoningContent
        v-if="content && content.trim()"
        :content="content"
        :streaming="!!streaming"
      />
      <ToolCallGroup v-if="tools.length > 0" :tools="tools" />
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import ReasoningContent from './ReasoningContent.vue'
import ToolCallCard from './ToolCallCard.vue'
import ToolCallGroup from './ToolCallGroup.vue'
import type { ToolCallEvent, StreamSegment } from '@/stores/chat'

const props = defineProps<{
  content: string
  // segments 来自 store 的 streamingSegments（流式）或 message.streaming_timeline_snapshot（历史）。
  // 历史侧是 unknown[]，需要规范化；缺省时回退到单一 ReasoningContent + ToolCallGroup。
  segments?: ReadonlyArray<unknown> | StreamSegment[]
  tools: ToolCallEvent[]
  streaming?: boolean
}>()

// ---- segments 规范化（容忍 unknown[] 历史数据）----
const normalizedSegments = computed<StreamSegment[]>(() => {
  const raw = props.segments
  if (!raw || raw.length === 0) return []
  const out: StreamSegment[] = []
  for (const s of raw) {
    if (!s || typeof s !== 'object') continue
    const obj = s as Record<string, unknown>
    if (obj.kind === 'text' && typeof obj.end === 'number') {
      out.push({ id: String(obj.id ?? `seg_t_${out.length}_${obj.end}`), kind: 'text', end: obj.end })
    } else if (obj.kind === 'tool' && typeof obj.toolCallId === 'string') {
      out.push({ id: String(obj.id ?? `seg_tc_${obj.toolCallId}_${out.length}`), kind: 'tool', toolCallId: obj.toolCallId })
    }
  }
  return out
})

const hasTimeline = computed(() => normalizedSegments.value.length > 0)

// 中间思考段 = 后面还有工具调用的文本段。那段文本只是"这一步在想什么"，
// 不是回合的回答（回合可能还没结束，例如被中断），因此禁止 ReasoningContent
// 把它提升为正文；否则时间线里会把模型的思考当回答展示。
// 最后一个文本段（其后不再有 tool 段）才是回答，保持默认可提升。
function isIntermediateSlice(idx: number): boolean {
  const segs = normalizedSegments.value
  for (let i = idx + 1; i < segs.length; i++) {
    if (segs[i].kind === 'tool') return true
    if (segs[i].kind === 'text') return false
  }
  return false
}

// ---- tools 按 id 索引 ----
const toolsById = computed<Record<string, ToolCallEvent>>(() => {
  const m: Record<string, ToolCallEvent> = {}
  for (const t of props.tools) {
    if (t && t.id) m[t.id] = t
  }
  return m
})

// ---- 文本切片：text 段 [start, end) ----
// 起始位置 = 向前回溯最近一个 text 段的 end（tool 段不消耗文本）。
function textStartOf(idx: number): number {
  const segs = normalizedSegments.value
  for (let i = idx - 1; i >= 0; i--) {
    const prev = segs[i]
    if (prev.kind === 'text') return prev.end
  }
  return 0
}

// 文本切片数组（与 normalizedSegments 同长度；非 text 段为 ''）
const textSlices = computed<string[]>(() => {
  const segs = normalizedSegments.value
  const out: string[] = []
  for (let i = 0; i < segs.length; i++) {
    const s = segs[i]
    if (s.kind === 'text') {
      const start = textStartOf(i)
      // 防御：start 不能超过 end，且要落在 content 范围内
      const clampedStart = Math.max(0, Math.min(start, s.end))
      out.push(props.content.substring(clampedStart, s.end))
    } else {
      out.push('')
    }
  }
  return out
})
</script>

<style scoped>
.message-timeline {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
</style>