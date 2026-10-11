<template>
  <!-- 执行过程 + 最终回答。
       过程（思考 + 工具调用）合并进一个 ProcessBlock 折叠区；
       最终回答独立渲染在过程下方。 -->
  <div class="message-timeline">
    <ProcessBlock
      v-if="hasProcess"
      :content="content"
      :segments="segments"
      :tools="tools"
      :streaming="!!streaming"
    />
    <ReasoningContent
      v-if="finalContent && finalContent.trim()"
      :content="finalContent"
      :streaming="!!streaming"
    />
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import ProcessBlock from './ProcessBlock.vue'
import ReasoningContent from './ReasoningContent.vue'
import type { ToolCallEvent, StreamSegment } from '@/stores/chat'

const props = defineProps<{
  content: string
  segments?: ReadonlyArray<unknown> | StreamSegment[]
  tools: ToolCallEvent[]
  streaming?: boolean
}>()

// ---- segments 规范化 ----
const normalizedSegments = computed<Array<{ id: string; kind: 'text' | 'tool'; end?: number; toolCallId?: string }>>(() => {
  const raw = props.segments
  if (!raw || raw.length === 0) return []
  const out: Array<{ id: string; kind: 'text' | 'tool'; end?: number; toolCallId?: string }> = []
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

const hasProcess = computed(() => {
  if (props.tools.length > 0) return true
  return normalizedSegments.value.length > 0
})

// ---- 正文：最后一个文本段 [其 start, end) 的内容 ----
// 若最后一个 text 段之后还有 tool 段，则该 text 是"中间思考"，正文为空；
// 否则最后一段文本即为最终回答（可能为纯思考，交由 ReasoningContent 提升逻辑处理）。
const finalContent = computed(() => {
  const segs = normalizedSegments.value
  if (segs.length === 0) return props.content

  let lastTextIdx = -1
  for (let i = segs.length - 1; i >= 0; i--) {
    if (segs[i].kind === 'text') {
      lastTextIdx = i
      break
    }
  }
  if (lastTextIdx === -1) return ''

  // 其后是否还有 tool 段
  for (let i = lastTextIdx + 1; i < segs.length; i++) {
    if (segs[i].kind === 'tool') return ''
  }

  // 起始位置 = 向前回溯最近一个 text 段的 end
  const end = segs[lastTextIdx].end as number
  let start = 0
  for (let i = lastTextIdx - 1; i >= 0; i--) {
    if (segs[i].kind === 'text') {
      start = segs[i].end as number
      break
    }
  }
  return props.content.substring(Math.max(0, Math.min(start, end)), end)
})
</script>

<style scoped>
.message-timeline {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
</style>
