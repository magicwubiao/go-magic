<template>
  <div class="reasoning-wrapper">
    <!-- 可选：独立展示思考折叠区（聊天主线已把思考并入 ProcessBlock，不再使用；
         BotsView 等场景仍需要单独展示思考）。 -->
    <div v-if="showThinking && reasoningPart" class="thinking-block" :class="{ collapsed: !thinkingOpen }">
      <button class="thinking-toggle" type="button" @click="thinkingOpen = !thinkingOpen">
        <span class="thinking-indicator" :class="{ pulsing: isStreaming }"></span>
        <span class="thinking-title">{{ t('chat.thinking') }}</span>
        <n-icon size="14" class="thinking-chevron">
          <ChevronDownOutline v-if="!thinkingOpen" />
          <ChevronUpOutline v-else />
        </n-icon>
      </button>
      <button
        v-if="!thinkingOpen && !isStreaming && thinkingPreview"
        class="thinking-preview"
        type="button"
        @click="thinkingOpen = true"
      >{{ thinkingPreview }}</button>
      <n-collapse-transition :show="thinkingOpen">
        <div class="thinking-body">
          <ReasoningBody :text="reasoningPart" />
        </div>
      </n-collapse-transition>
    </div>

    <!-- 最终回答：直接展示（执行过程由 ProcessBlock 独立承载）。 -->
    <div
      v-if="finalPart"
      class="final-content"
      v-html="renderedFinal"
    ></div>

    <!-- 无正文提示：思考存在、正文为空且不允许提升时，明确告诉用户本回合没
         有回答（例如回合被中断，模型停在工具调用那一步）。缺省不显示。 -->
    <div v-if="showEmptyHint" class="empty-answer-hint">{{ emptyHint }}</div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronUpOutline, ChevronDownOutline } from '@vicons/ionicons5'
import { NCollapseTransition, NIcon } from 'naive-ui'
import { Marked } from 'marked'
import ReasoningBody from './ReasoningBody.vue'
import { stripZeroWidth } from '@/utils/text'
import hljs from 'highlight.js/lib/core'
import javascript from 'highlight.js/lib/languages/javascript'
import typescript from 'highlight.js/lib/languages/typescript'
import python from 'highlight.js/lib/languages/python'
import go from 'highlight.js/lib/languages/go'
import bash from 'highlight.js/lib/languages/bash'
import json from 'highlight.js/lib/languages/json'
import xml from 'highlight.js/lib/languages/xml'
import css from 'highlight.js/lib/languages/css'
import markdown from 'highlight.js/lib/languages/markdown'

hljs.registerLanguage('javascript', javascript)
hljs.registerLanguage('typescript', typescript)
hljs.registerLanguage('python', python)
hljs.registerLanguage('go', go)
hljs.registerLanguage('bash', bash)
hljs.registerLanguage('json', json)
hljs.registerLanguage('xml', xml)
hljs.registerLanguage('css', css)
hljs.registerLanguage('markdown', markdown)

const props = defineProps<{
  content: string
  streaming?: boolean
  // 是否允许把"纯思考内容"提升为正文直接展示（默认允许）。
  allowPromote?: boolean
  // 正文为空的提示文案（例如"本回合未产生回答"）。
  emptyHint?: string
  // 是否单独展示"思考过程"折叠区（默认关闭；聊天主线把思考并入 ProcessBlock）。
  showThinking?: boolean
}>()

const { t } = useI18n()
const thinkingOpen = ref(false)

const isStreaming = computed(() => props.streaming === true)

watch(isStreaming, (v) => {
  if (v) thinkingOpen.value = true
  else thinkingOpen.value = false
}, { immediate: true })

// ---- 解析 <think>...</think> 分离思考与最终回答 ----
const parsedContent = computed(() => {
  const content = props.content || ''
  const thinkOpen = '<think>'
  const thinkClose = '</think>'
  const low = content.toLowerCase()

  const reasoningParts: string[] = []
  const finalParts: string[] = []
  let cursor = 0
  let searchFrom = 0
  let hasOpenThink = false

  while (true) {
    const openIdx = low.indexOf(thinkOpen, searchFrom)
    if (openIdx === -1) break
    const closeIdx = low.indexOf(thinkClose, openIdx + thinkOpen.length)
    if (closeIdx === -1) {
      finalParts.push(content.substring(cursor, openIdx))
      reasoningParts.push(content.substring(openIdx + thinkOpen.length))
      hasOpenThink = true
      cursor = content.length
      break
    }
    finalParts.push(content.substring(cursor, openIdx))
    reasoningParts.push(content.substring(openIdx + thinkOpen.length, closeIdx))
    cursor = closeIdx + thinkClose.length
    searchFrom = cursor
  }
  if (cursor < content.length) {
    finalParts.push(content.substring(cursor))
  }

  const hasVisibleText = (s: string): boolean => stripZeroWidth(s).trim() !== ''
  let reasoning = reasoningParts.filter(hasVisibleText).map((s) => s.trim()).join('\n\n')
  let final = finalParts.filter(hasVisibleText).map((s) => s.trim()).join('\n\n')

  // 兜底：无 <think> 标签时尝试 Markdown 标题式思考
  if (!low.includes(thinkOpen) && !reasoning && !final) {
    const headingRe = /(?:###|##)\s*(?:思考过程|Thinking|Reasoning|Thought|分析过程)\s*\n([\s\S]*?)(?=\n(?:###|##)\s*(?:最终结论|结论|Conclusion|Answer|回答)|$)/i
    const m = content.match(headingRe)
    if (m) {
      const fullRe = /(?:###|##)\s*(?:思考过程|Thinking|Reasoning|Thought|分析过程)[\s\S]*?(?=\n(?:###|##)\s*(?:最终结论|结论|Conclusion|Answer|回答)|$)/i
      const fullMatch = content.match(fullRe)
      if (fullMatch) {
        const r = m[1].trim()
        const f = content.replace(fullMatch[0], '').replace(/\n(?:###|##)\s*(?:最终结论|结论|Conclusion|Answer|回答)\s*\n?/i, '').trim()
        if (r) {
          reasoning = r
          final = f
        }
      }
    }
    if (!reasoning && !final) {
      return { reasoning: '', final: content }
    }
  }

  // 非流式且正文为空时，把思考提升为正文（避免整段被折叠隐藏）。
  const promoteable = !hasOpenThink || reasoning.length < 2000
  if (!props.streaming && props.allowPromote !== false && reasoning && !final && promoteable) {
    final = reasoning
    reasoning = ''
  }

  return { reasoning, final }
})

const finalPart = computed(() => parsedContent.value.final)
const reasoningPart = computed(() => parsedContent.value.reasoning)

// 折叠态预览：剥掉 Markdown 记号取首行纯文本，截 100 字符。
const thinkingPreview = computed(() => {
  const text = reasoningPart.value
  if (!text) return ''
  const plain = text
    .replace(/```[\s\S]*?```/g, ' ')
    .replace(/[#*`>\-]+/g, ' ')
    .replace(/\s+/g, ' ')
    .trim()
  return plain.length > 100 ? `${plain.slice(0, 100)}…` : plain
})

// 正文为空提示：流式结束、思考非空、正文为空时显示。
const showEmptyHint = computed(
  () => !isStreaming.value && !!props.emptyHint && !!parsedContent.value.reasoning && !finalPart.value,
)

// ---- Markdown 渲染 ----
const codeRenderer = (code: string, lang?: string): string => {
  const language = lang && hljs.getLanguage(lang) ? lang : null
  const highlighted = language
    ? hljs.highlight(code, { language }).value
    : hljs.highlightAuto(code).value
  const copyBtn = `<button class="code-copy-btn" type="button">Copy</button>`
  return `<div class="code-block">${copyBtn}<pre><code class="hljs${language ? ` language-${language}` : ''}">${highlighted}</code></pre></div>`
}

const marked = new Marked()
marked.use({
  renderer: { code: codeRenderer },
  breaks: true,
  gfm: true,
})

const renderedFinal = computed(() => {
  const content = finalPart.value
  if (!content) return ''
  return marked.parse(stripZeroWidth(content)) as string
})
</script>

<style scoped>
.reasoning-wrapper {
  width: 100%;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.empty-answer-hint {
  color: #9ca3af;
  font-size: 13px;
  line-height: 1.5;
}

/* ============ 可选：独立思考折叠区 ============ */
.thinking-block {
  border-left: 2px solid #e5e7eb;
  padding-left: 14px;
  margin-bottom: 2px;
}
.thinking-block.collapsed {
  border-left-color: #f3f4f6;
}

.thinking-toggle {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 2px 0;
  background: none;
  border: none;
  cursor: pointer;
  user-select: none;
  color: #6b7280;
  font-size: 13px;
  font-weight: 500;
}
.thinking-toggle:hover { color: #374151; }

.thinking-indicator {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #9ca3af;
  flex-shrink: 0;
  transition: background 0.2s;
}
.thinking-indicator.pulsing {
  background: #3b82f6;
  animation: thinking-pulse 1.4s ease-out infinite;
}
@keyframes thinking-pulse {
  0% { box-shadow: 0 0 0 0 rgba(59, 130, 246, 0.4); }
  100% { box-shadow: 0 0 0 6px rgba(59, 130, 246, 0); }
}

.thinking-title { flex: 1; text-align: left; }

.thinking-chevron {
  color: #9ca3af;
  transition: transform 0.2s;
}
.thinking-toggle:hover .thinking-chevron { color: #6b7280; }

.thinking-preview {
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
.thinking-preview:hover { color: #8b919a; }

.thinking-body {
  padding: 10px 0 6px 0;
  max-height: 50vh;
  overflow-y: auto;
  overscroll-behavior: contain;
  padding-right: 6px;
}

/* ============ 最终回答 ============ */
.final-content {
  font-size: 14.5px;
  line-height: 1.75;
  color: #1f2937;
}

.final-content :deep(p) {
  margin: 0 0 12px 0;
}
.final-content :deep(p:first-child) { margin-top: 0; }
.final-content :deep(p:last-child) { margin-bottom: 0; }

.final-content :deep(ul),
.final-content :deep(ol) {
  margin: 10px 0;
  padding-left: 24px;
}

.final-content :deep(li) {
  margin: 4px 0;
}

.final-content :deep(h1),
.final-content :deep(h2),
.final-content :deep(h3),
.final-content :deep(h4) {
  margin: 18px 0 10px 0;
  font-weight: 600;
  color: #111;
}
.final-content :deep(h1) { font-size: 20px; }
.final-content :deep(h2) { font-size: 17px; }
.final-content :deep(h3) { font-size: 15px; }
.final-content :deep(h4) { font-size: 14px; }

.final-content :deep(blockquote) {
  border-left: 3px solid #3b82f6;
  padding: 8px 14px;
  margin: 12px 0;
  color: #555;
  background: #f8fafc;
  border-radius: 0 6px 6px 0;
}

.final-content :deep(table) {
  border-collapse: collapse;
  width: 100%;
  margin: 10px 0;
  font-size: 13.5px;
  border-radius: 6px;
  overflow: hidden;
  border: 1px solid #e5e7eb;
}

.final-content :deep(th),
.final-content :deep(td) {
  border: 1px solid #e5e7eb;
  padding: 8px 12px;
  text-align: left;
}

.final-content :deep(th) {
  background: #f9fafb;
  font-weight: 600;
}

.final-content :deep(a) {
  color: #3b82f6;
  text-decoration: none;
}
.final-content :deep(a:hover) { text-decoration: underline; }

.final-content :deep(img) {
  max-width: 100%;
  border-radius: 6px;
  margin: 8px 0;
}

.final-content :deep(.code-block) {
  position: relative;
  margin: 10px 0;
  border-radius: 8px;
  overflow: hidden;
  background: #1e1e1e;
}

.final-content :deep(.code-block pre) {
  margin: 0;
  padding: 12px 15px;
  overflow-x: auto;
}

.final-content :deep(.code-block code) {
  font-family: 'SF Mono', 'Consolas', monospace;
  font-size: 13px;
  color: #d4d4d4;
  line-height: 1.6;
}

.final-content :deep(.code-copy-btn) {
  position: absolute;
  top: 6px;
  right: 6px;
  background: rgba(255, 255, 255, 0.1);
  border: 1px solid rgba(255, 255, 255, 0.2);
  color: #ccc;
  padding: 2px 10px;
  border-radius: 4px;
  font-size: 11px;
  cursor: pointer;
  opacity: 0;
  transition: opacity 0.2s;
}

.final-content :deep(.code-block:hover .code-copy-btn) {
  opacity: 1;
}

/* ============ 暗色模式 ============ */
@media (prefers-color-scheme: dark) {
  .thinking-block { border-left-color: #374151; }
  .thinking-block.collapsed { border-left-color: #2c2c33; }
  .thinking-toggle { color: #9ca3af; }
  .thinking-toggle:hover { color: #d1d5db; }
  .thinking-indicator { background: #6b7280; }
  .thinking-indicator.pulsing { background: #60a5fa; }
  @keyframes thinking-pulse {
    0% { box-shadow: 0 0 0 0 rgba(96, 165, 250, 0.4); }
    100% { box-shadow: 0 0 0 6px rgba(96, 165, 250, 0); }
  }
  .thinking-chevron { color: #6b7280; }
  .thinking-preview { color: #565b64; }
  .thinking-preview:hover { color: #8b919a; }

  .final-content {
    color: #e5e7eb;
  }
  .final-content :deep(h1),
  .final-content :deep(h2),
  .final-content :deep(h3),
  .final-content :deep(h4) {
    color: #f3f4f6;
  }
  .final-content :deep(blockquote) {
    border-left-color: #60a5fa;
    color: #d1d5db;
    background: #1e293b;
  }
  .final-content :deep(th) {
    background: #1f1f23;
  }
  .final-content :deep(th),
  .final-content :deep(td) {
    border-color: #374151;
  }
  .final-content :deep(a) {
    color: #60a5fa;
  }
}
</style>
