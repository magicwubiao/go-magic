<template>
  <!-- 纯推理正文渲染：只负责把一段文本（含 Markdown / 代码块）渲染成思考样式。
       由 ReasoningContent（独立折叠块）与 ProcessBlock（合并折叠区）共用。 -->
  <div class="reasoning-body" v-html="rendered"></div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { Marked } from 'marked'
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

const props = defineProps<{ text: string }>()

const codeRenderer = (code: string, lang?: string): string => {
  const language = lang && hljs.getLanguage(lang) ? lang : null
  const highlighted = language
    ? hljs.highlight(code, { language }).value
    : hljs.highlightAuto(code).value
  const copyBtn = `<button class="code-copy-btn" type="button">Copy</button>`
  return `<div class="code-block">${copyBtn}<pre><code class="hljs${language ? ` language-${language}` : ''}">${highlighted}</code></pre></div>`
}

// 独立 marked 实例：避免与全局单例配置互相覆盖。
const marked = new Marked()
marked.use({
  renderer: { code: codeRenderer },
  breaks: true,
  gfm: true,
})

// 思考文本是模型草稿，常含未闭合围栏/残留标签，渲染前清洗。
function sanitize(src: string): string {
  if (!src) return src
  const stripped = stripZeroWidth(src).replace(/<\/?\s*think\s*>/gi, '')
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
  if (fenceCount % 2 === 1) text += '\n```'
  return text
}

const rendered = computed(() => {
  if (!props.text) return ''
  return marked.parse(sanitize(props.text)) as string
})
</script>

<style scoped>
.reasoning-body {
  font-size: 13px;
  line-height: 1.7;
  color: #6b7280;
}

.reasoning-body :deep(p) { margin: 0 0 10px 0; }
.reasoning-body :deep(p:last-child) { margin-bottom: 0; }

.reasoning-body :deep(ul),
.reasoning-body :deep(ol) { margin: 8px 0; padding-left: 22px; }
.reasoning-body :deep(li) { margin: 2px 0; }

.reasoning-body :deep(h1),
.reasoning-body :deep(h2),
.reasoning-body :deep(h3),
.reasoning-body :deep(h4) {
  margin: 12px 0 6px 0;
  font-weight: 600;
  color: #4b5563;
}
.reasoning-body :deep(h1) { font-size: 15px; }
.reasoning-body :deep(h2) { font-size: 14px; }
.reasoning-body :deep(h3) { font-size: 13.5px; }
.reasoning-body :deep(h4) { font-size: 13px; }

.reasoning-body :deep(blockquote) {
  border-left: 2px solid #e5e7eb;
  padding-left: 10px;
  margin: 8px 0;
  color: #9ca3af;
}

.reasoning-body :deep(code) {
  font-family: 'SF Mono', 'Consolas', monospace;
  font-size: 12px;
  background: #f3f4f6;
  padding: 1px 5px;
  border-radius: 3px;
  color: #4b5563;
}

.reasoning-body :deep(.code-block) {
  position: relative;
  margin: 6px 0;
  border-radius: 6px;
  overflow: hidden;
  background: #1e1e1e;
}
.reasoning-body :deep(.code-block pre) { margin: 0; padding: 10px 13px; overflow-x: auto; }
.reasoning-body :deep(.code-block code) {
  font-family: 'SF Mono', 'Consolas', monospace;
  font-size: 12px;
  color: #d4d4d4;
  background: transparent;
  padding: 0;
}
.reasoning-body :deep(.code-copy-btn) {
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
.reasoning-body :deep(.code-block:hover .code-copy-btn) { opacity: 1; }

@media (prefers-color-scheme: dark) {
  .reasoning-body { color: #9ca3af; }
  .reasoning-body :deep(h1),
  .reasoning-body :deep(h2),
  .reasoning-body :deep(h3),
  .reasoning-body :deep(h4) { color: #d1d5db; }
  .reasoning-body :deep(code) { background: #374151; color: #d1d5db; }
  .reasoning-body :deep(blockquote) { border-left-color: #4b5563; color: #6b7280; }
}
</style>
