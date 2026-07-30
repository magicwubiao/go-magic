<template>
  <div class="reasoning-wrapper">
    <!-- 思考过程：单个可折叠区域（WorkBuddy 风格，极简） -->
    <div v-if="reasoningPart" class="thinking-block" :class="{ collapsed: !expanded }">
      <button class="thinking-toggle" type="button" @click="expanded = !expanded">
        <span class="thinking-indicator" :class="{ pulsing: isStreaming }"></span>
        <span class="thinking-title">{{ isStreaming ? t('chat.thinking') : t('chat.thoughtFor') }}</span>
        <span v-if="durationText" class="thinking-duration">{{ durationText }}</span>
        <n-icon size="14" class="thinking-chevron">
          <ChevronDownOutline v-if="!expanded" />
          <ChevronUpOutline v-else />
        </n-icon>
      </button>
      <n-collapse-transition :show="expanded">
        <div class="thinking-body">
          <div class="thinking-content" v-html="renderMarkdown(reasoningPart)"></div>
        </div>
      </n-collapse-transition>
    </div>

    <!-- 执行步骤折叠区：仅当检测到多个过程步骤 + 独立结论时才拆分 -->
    <div v-if="stepsSplit.steps" class="steps-block" :class="{ collapsed: !stepsExpanded }">
      <button class="steps-toggle" type="button" @click="stepsExpanded = !stepsExpanded">
        <n-icon size="14" class="steps-chevron">
          <ChevronDownOutline v-if="!stepsExpanded" />
          <ChevronUpOutline v-else />
        </n-icon>
        <span class="steps-title">{{ stepsTitle }}</span>
        <span class="steps-count">{{ stepsSplit.stepCount }} 步</span>
      </button>
      <n-collapse-transition :show="stepsExpanded">
        <div class="steps-body">
          <div class="steps-content" v-html="renderMarkdown(stepsSplit.steps)"></div>
        </div>
      </n-collapse-transition>
    </div>

    <!-- 最终结果：拆分时直接展示结论；不拆分时退化为全部内容 -->
    <div
      v-if="stepsSplit.result || (!stepsSplit.steps && finalPart)"
      class="final-content"
      v-html="renderMarkdown(stepsSplit.result || finalPart)"
    ></div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronUpOutline, ChevronDownOutline } from '@vicons/ionicons5'
import { marked } from 'marked'
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
  duration?: string
  streaming?: boolean
}>()

const { t } = useI18n()
const expanded = ref(false) // 思考过程默认折叠
const stepsExpanded = ref(false) // 执行步骤默认折叠

const stepsTitle = computed(() => {
  try {
    const v = t('chat.executionSteps')
    if (typeof v === 'string' && v && v !== 'chat.executionSteps') return v
  } catch (_) { /* ignore missing key */ }
  return '执行步骤'
})

// 流式耗时计时
const startTime = ref<number | null>(null)
const endTime = ref<number | null>(null)
const liveTick = ref(0)
let tickTimer: ReturnType<typeof setInterval> | null = null

const isStreaming = computed(() => props.streaming === true)

watch(isStreaming, (v) => {
  if (v) {
    if (startTime.value === null) startTime.value = Date.now()
    if (!tickTimer) {
      tickTimer = setInterval(() => { liveTick.value++ }, 500)
    }
    // 流式时默认展开，让用户看到实时思考
    expanded.value = true
  } else {
    if (tickTimer) {
      clearInterval(tickTimer)
      tickTimer = null
    }
    endTime.value = Date.now()
    // 流式结束后自动折叠（成品导向）
    expanded.value = false
  }
}, { immediate: true })

onUnmounted(() => {
  if (tickTimer) clearInterval(tickTimer)
})

const durationText = computed(() => {
  // 优先用外部传入的 duration
  if (props.duration) return props.duration
  liveTick.value // 触发实时刷新
  if (startTime.value === null) return ''
  const end = endTime.value ?? Date.now()
  const ms = end - startTime.value
  if (ms < 1000) return `${Math.round(ms / 100) / 10}s`
  if (ms < 60000) return `${Math.round(ms / 1000)}s`
  const m = Math.floor(ms / 60000)
  const s = Math.round((ms % 60000) / 1000)
  return `${m}m${s}s`
})

// ---- 解析 <think>...</think> 分离思考与最终回答 ----
//
// **流式期间的闪烁问题修复**：
// 后端以 SSE 逐字推流，<think> 打开后到 </think> 闭合前的窗口内，
// 旧逻辑因缺少 </think> 会走到 fallback，把"尚未闭合的思考内容"
// 整段塞进 final → 以 final-content 直接显示（纯文本+大字号），
// 等 </think> 到达后才切换为 thinking-block 折叠样式，造成用户感知的
// "先出现一大段文字，过一会才变成思考折叠样式"。
//
// 修复：只要检测到 <think> 开头（流式中</think>可以还没到），就立即
// 把 <think> 之后的内容放到 reasoning 部分，先应用思考样式，避免闪烁。
const parsedContent = computed(() => {
  const content = props.content
  const thinkOpen = '<think>'
  const thinkClose = '</think>'
  const low = content.toLowerCase()
  const openIdx = low.indexOf(thinkOpen)

  if (openIdx !== -1) {
    const closeIdx = low.indexOf(thinkClose, openIdx + thinkOpen.length)

    // 1) 完整闭合：<think>...</think> 均存在 → 正常拆分
    if (closeIdx !== -1) {
      const reasoning = content.substring(openIdx + thinkOpen.length, closeIdx).trim()
      const before = content.substring(0, openIdx).trim()
      const after = content.substring(closeIdx + thinkClose.length).trim()
      return { reasoning, final: (before + '\n' + after).trim() }
    }

    // 2) 流式窗口：<think> 已到但 </think> 尚未到达 → 立刻放进 reasoning，
    //    避免被塞进 final 导致样式闪烁。before 放 final（空）不影响显示。
    const reasoning = content.substring(openIdx + thinkOpen.length).trim()
    const before = content.substring(0, openIdx).trim()
    return { reasoning, final: before }
  }

  // 兜底：Markdown 标题
  const headingRe = /(?:###|##)\s*(?:思考过程|Thinking|Reasoning|Thought|分析过程)\s*\n([\s\S]*?)(?=\n(?:###|##)\s*(?:最终结论|结论|Conclusion|Answer|回答)|$)/i
  const m = content.match(headingRe)
  if (m) {
    const fullRe = /(?:###|##)\s*(?:思考过程|Thinking|Reasoning|Thought|分析过程)[\s\S]*?(?=\n(?:###|##)\s*(?:最终结论|结论|Conclusion|Answer|回答)|$)/i
    const fullMatch = content.match(fullRe)
    if (fullMatch) {
      const reasoning = m[1].trim()
      const final = content.replace(fullMatch[0], '').replace(/\n(?:###|##)\s*(?:最终结论|结论|Conclusion|Answer|回答)\s*\n?/i, '').trim()
      if (reasoning) return { reasoning, final }
    }
  }
  return { reasoning: '', final: content }
})

const reasoningPart = computed(() => parsedContent.value.reasoning)
const finalPart = computed(() => parsedContent.value.final)

// ---- 把 finalPart 拆成"过程步骤区（折叠）" + "最终结果区（直接展示）" ----
//
// 匹配步骤特征：
//   1. 行首是 emoji 图标（✅ ✔ ☑ ⚙️ 📝 🔧 🚀 🔄 📋 🎯 🏗️ 🔨 🧪 🔍 💡 📦 🎨 💾 📤 📥 🗂️ 📊 🧹 ⚡ 🔥 🌐 🔌 🛠️ 🔩 🧩 🛡️ 等）
//      后面跟文字（可能包含 顿号、冒号、文件名）
//   2. 或者是 ⓘ ⊙ ⊚ ⦿ ◎ ○ ● ➜ ➤ ❯ › 这类特殊符号开头 + 文字
const STEP_ICON_RE =
  /^[\s\u3000]*[\u{1F300}-\u{1FAFF}\u{2600}-\u{27BF}\u{2B00}-\u{2BFF}\u{1F000}-\u{1F02F}✅✔☑✓⚙️📝🔧🚀🔄📋🎯🏗️🔨🧪🔍💡📦🎨💾📤📥🗂️📊🧹⚡🔥🌐🔌🛠️🔩🧩🛡️⊙⊚⦿◎●➜➤❯›→⇒⇢⁍⊳▸▻▶▶️🔘⚫⚪]+\s*/u

interface StepsSplit {
  steps: string     // 过程步骤（折叠展示），空串表示不拆分
  result: string    // 最终结果（直接展示），空串表示无独立结论
  stepCount: number // 识别到的步骤数量
}

const stepsSplit = computed<StepsSplit>((): StepsSplit => {
  const text = finalPart.value
  if (!text || text.length < 60) return { steps: '', result: '', stepCount: 0 }

  const lines = text.split('\n')
  // 收集每行是否为步骤行，以及其在原始字符串中的字符位置
  let charIdx = 0
  const stepEndings: { idx: number; end: number }[] = []
  for (const ln of lines) {
    const lineStart = charIdx
    const lineEnd = charIdx + ln.length
    const trimmed = ln.replace(/^\s+/, '')
    if (STEP_ICON_RE.test(trimmed) && trimmed.length > 6) {
      stepEndings.push({ idx: stepEndings.length, end: lineEnd })
    }
    charIdx = lineEnd + 1 // +1 for \n
  }

  // 至少检测到 2 个步骤行才认为有可拆分的过程
  if (stepEndings.length < 2) return { steps: '', result: '', stepCount: 0 }

  const last = stepEndings[stepEndings.length - 1]
  // 最后一个步骤结束位置之后的内容 = tail
  const tailStart = Math.min(last.end + 1, text.length)
  let tail = text.substring(tailStart).trim()

  // 如果 tail 太短（< 60 字），认为结论不独立，不拆分
  // 例外：tail 包含多个段落（有 \n\n 分隔），即使字数少也算独立结论
  if (tail.length < 60 && !tail.includes('\n\n')) {
    return { steps: '', result: '', stepCount: stepEndings.length }
  }

  // 找到第一个步骤出现之前的引导文字（比如开场白），需要保留
  const firstStepLineStartIdx = (() => {
    let ci = 0
    for (const ln of lines) {
      const trimmed = ln.replace(/^\s+/, '')
      if (STEP_ICON_RE.test(trimmed) && trimmed.length > 6) return ci
      ci += ln.length + 1
    }
    return 0
  })()
  const intro = text.substring(0, firstStepLineStartIdx).trimEnd()

  // steps 区域 = 引导文字 + 最后一个步骤行结束之前（含步骤行）的内容
  const stepsPart = (intro ? intro + '\n' : '') + text.substring(firstStepLineStartIdx, last.end + 1).trimEnd()

  return {
    steps: stepsPart.trim(),
    result: tail,
    stepCount: stepEndings.length,
  }
})

// ---- Markdown 渲染 ----
const codeRenderer = (code: string, lang?: string): string => {
  const language = lang && hljs.getLanguage(lang) ? lang : null
  const highlighted = language
    ? hljs.highlight(code, { language }).value
    : hljs.highlightAuto(code).value
  const copyBtn = `<button class="code-copy-btn" type="button">Copy</button>`
  return `<div class="code-block">${copyBtn}<pre><code class="hljs${language ? ` language-${language}` : ''}">${highlighted}</code></pre></div>`
}

marked.use({
  renderer: { code: codeRenderer },
  breaks: true, // 单换行转 <br>
  gfm: true,
})

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

function renderMarkdown(content: string): string {
  return marked.parse(content) as string
}
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

/* ============ 执行步骤折叠区 ============ */
.steps-block {
  border-left: 2px solid #e5e7eb;
  padding-left: 14px;
  margin-bottom: 6px;
}
.steps-block.collapsed { border-left-color: #f3f4f6; }

.steps-toggle {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 0;
  background: none;
  border: none;
  cursor: pointer;
  user-select: none;
  color: #6b7280;
  font-size: 13px;
  font-weight: 500;
}
.steps-toggle:hover { color: #374151; }

.steps-title { flex: 1; text-align: left; }
.steps-count {
  font-size: 11.5px;
  color: #9ca3af;
  font-family: 'SF Mono', 'Consolas', monospace;
  font-variant-numeric: tabular-nums;
}
.steps-chevron {
  color: #9ca3af;
  transition: transform 0.2s;
}
.steps-toggle:hover .steps-chevron { color: #6b7280; }

.steps-body { padding: 8px 0 2px 0; }
.steps-content {
  font-size: 13.5px;
  line-height: 1.7;
  color: #4b5563;
}
.steps-content :deep(p) { margin: 0 0 8px 0; }
.steps-content :deep(p:last-child) { margin-bottom: 0; }
.steps-content :deep(ul),
.steps-content :deep(ol) {
  margin: 6px 0;
  padding-left: 22px;
}
.steps-content :deep(li) { margin: 2px 0; }
.steps-content :deep(h1),
.steps-content :deep(h2),
.steps-content :deep(h3),
.steps-content :deep(h4) {
  margin: 10px 0 4px 0;
  font-weight: 600;
  color: #374151;
}
.steps-content :deep(h1) { font-size: 15px; }
.steps-content :deep(h2) { font-size: 14px; }
.steps-content :deep(h3) { font-size: 13.5px; }
.steps-content :deep(h4) { font-size: 13px; }
.steps-content :deep(blockquote) {
  padding-left: 10px;
  margin: 6px 0;
  color: #6b7280;
  border-left: 2px solid #d1d5db;
}
.steps-content :deep(code) {
  font-family: 'SF Mono', 'Consolas', monospace;
  font-size: 12px;
  background: #f3f4f6;
  padding: 1px 5px;
  border-radius: 3px;
  color: #4b5563;
}
.steps-content :deep(.code-block) {
  position: relative;
  margin: 6px 0;
  border-radius: 6px;
  overflow: hidden;
  background: #1e1e1e;
}
.steps-content :deep(.code-block pre) {
  margin: 0;
  padding: 10px 13px;
  overflow-x: auto;
}
.steps-content :deep(.code-block code) {
  font-family: 'SF Mono', 'Consolas', monospace;
  font-size: 12px;
  color: #d4d4d4;
  background: transparent;
  padding: 0;
}
.steps-content :deep(.code-copy-btn) {
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
.steps-content :deep(.code-block:hover .code-copy-btn) { opacity: 1; }

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

  .steps-block { border-left-color: #374151; }
  .steps-block.collapsed { border-left-color: #2c2c33; }
  .steps-toggle { color: #9ca3af; }
  .steps-toggle:hover { color: #d1d5db; }
  .steps-count { color: #6b7280; }
  .steps-chevron { color: #6b7280; }
  .steps-content { color: #9ca3af; }
  .steps-content :deep(h1),
  .steps-content :deep(h2),
  .steps-content :deep(h3),
  .steps-content :deep(h4) { color: #d1d5db; }
  .steps-content :deep(code) { background: #374151; color: #d1d5db; }
  .steps-content :deep(blockquote) { border-left-color: #4b5563; color: #6b7280; }
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
