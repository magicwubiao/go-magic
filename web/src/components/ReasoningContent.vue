<template>
  <div class="reasoning-wrapper">
    <!-- 思考过程：单个可折叠区域（WorkBuddy 风格，极简） -->
    <div v-if="reasoningPart" class="thinking-block" :class="{ collapsed: !expanded }">
      <button class="thinking-toggle" type="button" @click="expanded = !expanded">
        <span class="thinking-indicator" :class="{ pulsing: isStreaming }"></span>
        <span class="thinking-title">{{ t('chat.thinking') }}</span>
        <n-icon size="14" class="thinking-chevron">
          <ChevronDownOutline v-if="!expanded" />
          <ChevronUpOutline v-else />
        </n-icon>
      </button>
      <n-collapse-transition :show="expanded">
        <div class="thinking-body">
          <div class="thinking-content" v-html="renderedReasoning"></div>
        </div>
      </n-collapse-transition>
    </div>

    <!-- 最终回答：全部直接展示，不再做步骤拆分折叠 -->
    <div
      v-if="finalPart"
      class="final-content"
      v-html="renderedFinal"
    ></div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronUpOutline, ChevronDownOutline } from '@vicons/ionicons5'
import { Marked } from 'marked'
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
import 'highlight.js/styles/github-dark.css'

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
  // 分段渲染（timeline 切片）时，工具调用之间的中间思考段必须关闭此能力，
  // 否则纯思考段会被当作正文展开显示，而不是折叠的思考块。
  allowPromote?: boolean
}>()

const { t } = useI18n()
const expanded = ref(false) // 思考过程默认折叠

const isStreaming = computed(() => props.streaming === true)

watch(isStreaming, (v) => {
  if (v) {
    // 流式时默认展开，让用户看到实时思考
    expanded.value = true
  } else {
    // 流式结束后自动折叠（成品导向）
    expanded.value = false
  }
}, { immediate: true })

// ---- 解析 <think>...</think> 分离思考与最终回答 ----
//
// 这里需要处理三种情况：
// 1) 多轮（含工具调用）场景：每一轮 LLM 调用都会产生一个 <think>...</think>
//    块，全文拼接后存在多个 think 块。需要把所有 think 块都归入思考，块外
//    的所有文本归入最终回答，避免后续 <think> 标签被当成正文泄漏到回答里。
// 2) 流式窗口：<think> 已到但 </think> 尚未到达，把 <think> 之后的内容立刻
//    放进 reasoning 应用思考样式，避免"先出现一大段文字再变成折叠样式"的闪烁。
// 3) 模型把全部内容（含操作叙述与结果）都放进 reasoning_content、Content 为
//    空：此时 </think> 已闭合但最终回答为空。若仍把整段塞进折叠区，流式结束
//    后自动折叠，用户将看不到任何回答。因此闭合且 final 为空时，把思考内容
//    提升为最终回答直接展示（流式窗口除外，仍实时展示在折叠区）。
const parsedContent = computed(() => {
  const content = props.content || ''
  const thinkOpen = '<think>'
  const thinkClose = '</think>'
  const low = content.toLowerCase()

  const reasoningParts: string[] = []
  const finalParts: string[] = []
  let cursor = 0
  let searchFrom = 0
  let hasOpenThink = false // 是否存在未闭合的 <think>（流式窗口）

  while (true) {
    const openIdx = low.indexOf(thinkOpen, searchFrom)
    if (openIdx === -1) break
    const closeIdx = low.indexOf(thinkClose, openIdx + thinkOpen.length)
    if (closeIdx === -1) {
      // <think> 已到但 </think> 未到。
      // - 流式中（streaming=true）：之前归 final，之后归 reasoning 实时展示在折叠区。
      // - 非流式（streaming=false）：流已结束，未闭合的 think 是模型漏了闭合标签。
      //   按"已闭合"处理，think 内容归 reasoning；若最终 final 为空，下方兜底逻辑
      //   会把 reasoning 提升为 final 直接展示，避免整段被折叠隐藏。
      finalParts.push(content.substring(cursor, openIdx))
      reasoningParts.push(content.substring(openIdx + thinkOpen.length))
      hasOpenThink = true
      cursor = content.length
      break
    }
    // 已闭合块：块内归思考，块前文本归回答
    finalParts.push(content.substring(cursor, openIdx))
    reasoningParts.push(content.substring(openIdx + thinkOpen.length, closeIdx))
    cursor = closeIdx + thinkClose.length
    searchFrom = cursor
  }
  // 最后一个 think 块之后的尾部文本
  if (cursor < content.length) {
    finalParts.push(content.substring(cursor))
  }

  let reasoning = reasoningParts.map((s) => s.trim()).filter((s) => s).join('\n\n')
  let final = finalParts.map((s) => s.trim()).filter((s) => s).join('\n\n')

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

  // 关键修复：非流式状态下最终回答为空（think 块已全部闭合，或模型漏了闭合标签）——
  // 说明模型把全部内容都放进了 reasoning_content。把思考内容提升为最终回答直接展示，
  // 避免整段被折叠隐藏导致用户看不到任何回答。
  // 流式中（streaming=true）不提升，仍实时展示在展开的折叠区，保留"实时看思考"体验。
  // 但 hasOpenThink（流结束时 <think> 仍未闭合）是模型"思考退化中断"的典型特征：
  // 此时整段很可能是重复思考，直接提升会把垃圾文本当正文展示。只有内容较短
  //（模型只是漏了闭合标签的正常回答）才提升；退化的长思考保留在折叠区，
  // 其中的重复部分由 collapseRepetitiveThinking 折叠。
  const promoteable = !hasOpenThink || reasoning.length < 2000
  if (!props.streaming && props.allowPromote !== false && reasoning && !final && promoteable) {
    final = reasoning
    reasoning = ''
  }

  return { reasoning, final }
})

// ---- 重复思考检测与折叠 ----
//
// 思考型模型偶发"重复退化"：同一段短语（甚至带标点/感叹号变化的变体）在思考
// 中反复出现几十次。这种内容展示价值为零，还会把折叠区撑得巨大。这里按行做
// 归一化去重统计，连续重复超过阈值时把重复主体替换为占位提示，只保留首次
// 出现的片段供追溯。
const REP_MIN_LINE_LEN = 8 // 参与统计的最短行长度（字符）
const REP_MAX_TAIL = 400 // 超过阈值时保留的重复开头行数
const REP_TRIGGER_LINES = 8 // 触发折叠的连续相同行数

const normalizeRepLine = (line: string): string =>
  line
    .toLowerCase()
    .replace(/[\p{P}\p{S}]/gu, '') // 去掉标点/符号，让 "GO!" 与 "GO!!" 视为相同
    .replace(/\s+/g, ' ')
    .trim()

const collapseRepetitiveThinking = (text: string): string => {
  if (!text) return text
  const lines = text.split('\n')
  const normalized = lines.map(normalizeRepLine)

  const result: string[] = []
  let i = 0
  let collapsedAny = false
  while (i < lines.length) {
    const cur = normalized[i]
    if (!cur || cur.length < REP_MIN_LINE_LEN) {
      result.push(lines[i])
      i++
      continue
    }
    // 统计从 i 开始连续相同（归一化后）的行数
    let j = i + 1
    while (j < lines.length && normalized[j] === cur) j++
    const runLen = j - i
    if (runLen >= REP_TRIGGER_LINES) {
      result.push(...lines.slice(i, i + REP_MAX_TAIL))
      result.push(`… [重复思考内容已折叠，共 ${runLen} 行相似内容]`)
      collapsedAny = true
    } else {
      result.push(...lines.slice(i, j))
    }
    i = j
  }
  return collapsedAny ? result.join('\n') : text
}

const reasoningPart = computed(() => collapseRepetitiveThinking(parsedContent.value.reasoning))
const finalPart = computed(() => parsedContent.value.final)

// ---- Markdown 渲染 ----
const codeRenderer = (code: string, lang?: string): string => {
  const language = lang && hljs.getLanguage(lang) ? lang : null
  const highlighted = language
    ? hljs.highlight(code, { language }).value
    : hljs.highlightAuto(code).value
  const copyBtn = `<button class="code-copy-btn" type="button">Copy</button>`
  return `<div class="code-block">${copyBtn}<pre><code class="hljs${language ? ` language-${language}` : ''}">${highlighted}</code></pre></div>`
}

// 独立 marked 实例：marked 默认导出是全局单例，ChatView 等模块也会对它
// marked.use()，配置互相覆盖导致渲染行为不可预期。组件内使用独立实例。
const thinkingMarked = new Marked()
thinkingMarked.use({
  renderer: { code: codeRenderer },
  breaks: true, // 单换行转 <br>
  gfm: true,
})

// ---- 思考内容清洗 ----
// 思考文本是模型的内部草稿，常包含未闭合的代码围栏、残留的 <think> 标签、
// 裸 HTML 等，直接按 Markdown 渲染会出现"后半段被吞进代码块、布局错乱"等
// 混乱效果。渲染前做三步清洗：
//  1) 去掉残留的 think 标签（嵌套/解析残留）
//  2) 代码围栏外的 < 转义为 &lt;，防止未闭合标签破坏布局
//     （围栏内不转义：marked 会自己转义，预先转义会二次转义成 &amp;lt;）
//  3) 围栏出现奇数次时补一个闭合围栏，防止未闭合代码块吞掉后续内容
function sanitizeThinking(src: string): string {
  if (!src) return src
  const stripped = src.replace(/<\/?\s*think\s*>/gi, '')
  const lines = stripped.split('\n')
  const out: string[] = []
  let fenceCount = 0
  for (const line of lines) {
    if (/^\s{0,3}(```|~~~)/.test(line)) {
      fenceCount++
      out.push(line)
      continue
    }
    out.push(fenceCount % 2 === 1 ? line : line.replace(/</g, '&lt;'))
  }
  let text = out.join('\n')
  if (fenceCount % 2 === 1) {
    text += '\n```'
  }
  return text
}

function handleCodeBlockClick(e: MouseEvent) {
  const target = e.target as HTMLElement
  const btn = target.closest('.code-copy-btn') as HTMLElement | null
  if (!btn) return
  const codeEl = btn.parentElement?.querySelector('code')
  const code = codeEl?.textContent || ''
  navigator.clipboard.writeText(code).catch(() => { /* ignore */ })
  const original = btn.textContent
  btn.textContent = '✓'
  setTimeout(() => { btn.textContent = original }, 2000)
}

onMounted(() => {
  document.addEventListener('click', handleCodeBlockClick)
})

onUnmounted(() => {
  document.removeEventListener('click', handleCodeBlockClick)
})

// 缓存 Markdown 渲染结果，避免每次重渲染都重新解析全文
const renderedReasoning = computed(() => {
  const content = reasoningPart.value
  if (!content) return ''
  return thinkingMarked.parse(sanitizeThinking(content)) as string
})

const renderedFinal = computed(() => {
  const content = finalPart.value
  if (!content) return ''
  return thinkingMarked.parse(content) as string
})
</script>

<style scoped>
.reasoning-wrapper {
  width: 100%;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

/* ============ 思考过程折叠区 ============ */
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

.thinking-toggle:hover {
  color: #374151;
}

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
  animation: pulse 1.4s ease-out infinite;
}

@keyframes pulse {
  0% { box-shadow: 0 0 0 0 rgba(59, 130, 246, 0.4); }
  100% { box-shadow: 0 0 0 6px rgba(59, 130, 246, 0); }
}

.thinking-title {
  flex: 1;
  text-align: left;
}

.thinking-duration {
  font-size: 11.5px;
  color: #9ca3af;
  font-family: 'SF Mono', 'Consolas', monospace;
  font-variant-numeric: tabular-nums;
}

.thinking-chevron {
  color: #9ca3af;
  transition: transform 0.2s;
}

.thinking-toggle:hover .thinking-chevron {
  color: #6b7280;
}

/* 思考内容 */
.thinking-body {
  padding: 10px 0 6px 0;
}

.thinking-content {
  font-size: 13px;
  line-height: 1.7;
  color: #6b7280;
}

.thinking-content :deep(p) {
  margin: 0 0 10px 0;
}
.thinking-content :deep(p:last-child) { margin-bottom: 0; }

.thinking-content :deep(ul),
.thinking-content :deep(ol) {
  margin: 8px 0;
  padding-left: 22px;
}

.thinking-content :deep(li) {
  margin: 2px 0;
}

.thinking-content :deep(h1),
.thinking-content :deep(h2),
.thinking-content :deep(h3),
.thinking-content :deep(h4) {
  margin: 12px 0 6px 0;
  font-weight: 600;
  color: #4b5563;
}
.thinking-content :deep(h1) { font-size: 15px; }
.thinking-content :deep(h2) { font-size: 14px; }
.thinking-content :deep(h3) { font-size: 13.5px; }
.thinking-content :deep(h4) { font-size: 13px; }

.thinking-content :deep(blockquote) {
  border-left: 2px solid #e5e7eb;
  padding-left: 10px;
  margin: 8px 0;
  color: #9ca3af;
}

.thinking-content :deep(code) {
  font-family: 'SF Mono', 'Consolas', monospace;
  font-size: 12px;
  background: #f3f4f6;
  padding: 1px 5px;
  border-radius: 3px;
  color: #4b5563;
}

.thinking-content :deep(.code-block) {
  position: relative;
  margin: 6px 0;
  border-radius: 6px;
  overflow: hidden;
  background: #1e1e1e;
}

.thinking-content :deep(.code-block pre) {
  margin: 0;
  padding: 10px 13px;
  overflow-x: auto;
}

.thinking-content :deep(.code-block code) {
  font-family: 'SF Mono', 'Consolas', monospace;
  font-size: 12px;
  color: #d4d4d4;
  background: transparent;
  padding: 0;
}

.thinking-content :deep(.code-copy-btn) {
  position: absolute;
  top: 4px;
  right: 4px;
  background: rgba(255, 255, 255, 0.1);
  border: 1px solid rgba(255, 255, 255, 0.2);
  color: #ccc;
  padding: 1px 8px;
  border-radius: 3px;
  font-size: 10px;
  cursor: pointer;
  opacity: 0;
  transition: opacity 0.2s;
}

.thinking-content :deep(.code-block:hover .code-copy-btn) {
  opacity: 1;
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
  .thinking-block {
    border-left-color: #374151;
  }
  .thinking-block.collapsed {
    border-left-color: #2c2c33;
  }

  .thinking-toggle {
    color: #9ca3af;
  }
  .thinking-toggle:hover {
    color: #d1d5db;
  }
  .thinking-indicator {
    background: #6b7280;
  }
  .thinking-indicator.pulsing {
    background: #60a5fa;
  }
  @keyframes pulse {
    0% { box-shadow: 0 0 0 0 rgba(96, 165, 250, 0.4); }
    100% { box-shadow: 0 0 0 6px rgba(96, 165, 250, 0); }
  }
  .thinking-duration {
    color: #6b7280;
  }
  .thinking-chevron {
    color: #6b7280;
  }

  .thinking-content {
    color: #9ca3af;
  }
  .thinking-content :deep(h1),
  .thinking-content :deep(h2),
  .thinking-content :deep(h3),
  .thinking-content :deep(h4) {
    color: #d1d5db;
  }
  .thinking-content :deep(code) {
    background: #374151;
    color: #d1d5db;
  }
  .thinking-content :deep(blockquote) {
    border-left-color: #4b5563;
    color: #6b7280;
  }

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