<template>
  <!-- 通用 Markdown 正文渲染：用于展示 agent / 脚本产出这类"内容型"文本
       （定时任务执行输出、任务结果等）。与 ReasoningBody 的区别是这里渲染的是
       成品正文而不是模型思考草稿：字号按正文给，标题有层级，代码块带复制按钮。 -->
  <div class="md-block" v-html="rendered" @click="handleClick"></div>
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
import sql from 'highlight.js/lib/languages/sql'

hljs.registerLanguage('javascript', javascript)
hljs.registerLanguage('typescript', typescript)
hljs.registerLanguage('python', python)
hljs.registerLanguage('go', go)
hljs.registerLanguage('bash', bash)
hljs.registerLanguage('json', json)
hljs.registerLanguage('xml', xml)
hljs.registerLanguage('css', css)
hljs.registerLanguage('markdown', markdown)
hljs.registerLanguage('sql', sql)

const props = defineProps<{ text: string }>()

const codeRenderer = (code: string, lang?: string): string => {
  const language = lang && hljs.getLanguage(lang) ? lang : null
  const highlighted = language
    ? hljs.highlight(code, { language }).value
    : hljs.highlightAuto(code).value
  // 语言名放在左上角，复制按钮右上角：输出里常有整段 HTML/JSON，
  // 一眼看出是什么语言比按钮更重要。
  const label = language ? `<span class="md-code-lang">${language}</span>` : ''
  const copyBtn = `<button class="md-code-copy" type="button">Copy</button>`
  return `<div class="md-code">${label}${copyBtn}<pre><code class="hljs${language ? ` language-${language}` : ''}">${highlighted}</code></pre></div>`
}

// 独立 marked 实例：不与 ChatView / ReasoningBody 的全局单例配置互相覆盖。
const marked = new Marked()
marked.use({
  renderer: { code: codeRenderer },
  breaks: true,
  gfm: true,
})

// 输出里可能夹带零宽占位符（模型回显）和未闭合围栏（执行中断）。
// 渲染前清洗，避免大段空白和整段被吞进代码块。
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
    // 围栏外的裸 < 会破坏 HTML 渲染（输出里常见 "a < b"），转义掉
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

// 代码块复制走事件委托：v-html 里的按钮没法绑 Vue 事件。
async function handleClick(e: MouseEvent) {
  const target = e.target as HTMLElement | null
  if (!target || !target.classList.contains('md-code-copy')) return
  const pre = target.parentElement?.querySelector('pre')
  const code = pre?.textContent ?? ''
  try {
    await navigator.clipboard.writeText(code)
    const prev = target.textContent
    target.textContent = 'Copied'
    window.setTimeout(() => { target.textContent = prev || 'Copy' }, 1200)
  } catch {
    // 非安全上下文（http + 非 localhost）没有 clipboard API，静默失败
  }
}
</script>

<style scoped>
.md-block {
  font-size: 13.5px;
  line-height: 1.75;
  color: #303133;
  word-break: break-word;
}

.md-block :deep(p) { margin: 0 0 10px 0; }
.md-block :deep(p:last-child) { margin-bottom: 0; }

.md-block :deep(h1),
.md-block :deep(h2),
.md-block :deep(h3),
.md-block :deep(h4),
.md-block :deep(h5) {
  margin: 18px 0 8px 0;
  font-weight: 600;
  line-height: 1.4;
  color: #1f2329;
}
.md-block :deep(h1:first-child),
.md-block :deep(h2:first-child),
.md-block :deep(h3:first-child) { margin-top: 0; }
.md-block :deep(h1) { font-size: 19px; }
.md-block :deep(h2) { font-size: 17px; }
.md-block :deep(h3) { font-size: 15px; }
.md-block :deep(h4) { font-size: 14px; }

.md-block :deep(ul),
.md-block :deep(ol) { margin: 8px 0; padding-left: 22px; }
.md-block :deep(li) { margin: 3px 0; }
.md-block :deep(li > p) { margin: 0; }

.md-block :deep(blockquote) {
  margin: 10px 0;
  padding: 6px 12px;
  border-left: 3px solid #dcdfe6;
  background: #f8f9fb;
  color: #5c6169;
}

.md-block :deep(a) { color: #2563eb; text-decoration: none; }
.md-block :deep(a:hover) { text-decoration: underline; }

.md-block :deep(hr) {
  border: none;
  border-top: 1px solid #ebeef5;
  margin: 16px 0;
}

.md-block :deep(code) {
  font-family: 'SF Mono', 'Consolas', 'Menlo', monospace;
  font-size: 12.5px;
  background: #f2f4f7;
  padding: 1px 5px;
  border-radius: 4px;
  color: #c7254e;
}

.md-block :deep(table) {
  border-collapse: collapse;
  margin: 10px 0;
  font-size: 13px;
  display: block;
  overflow-x: auto;
  max-width: 100%;
}
.md-block :deep(th),
.md-block :deep(td) {
  border: 1px solid #e5e7eb;
  padding: 6px 10px;
  text-align: left;
}
.md-block :deep(th) { background: #f8f9fb; font-weight: 600; }

.md-block :deep(.md-code) {
  position: relative;
  margin: 10px 0;
  border-radius: 8px;
  overflow: hidden;
  background: #1e1e1e;
}
.md-block :deep(.md-code pre) { margin: 0; padding: 30px 14px 12px; overflow-x: auto; }
.md-block :deep(.md-code code) {
  font-family: 'SF Mono', 'Consolas', 'Menlo', monospace;
  font-size: 12.5px;
  line-height: 1.6;
  color: #d4d4d4;
  background: transparent;
  padding: 0;
}
.md-block :deep(.md-code-lang) {
  position: absolute;
  top: 6px;
  left: 12px;
  font-size: 11px;
  color: rgba(255, 255, 255, 0.45);
  text-transform: lowercase;
}
.md-block :deep(.md-code-copy) {
  position: absolute;
  top: 5px;
  right: 8px;
  background: rgba(255, 255, 255, 0.1);
  border: 1px solid rgba(255, 255, 255, 0.18);
  color: #d0d0d0;
  padding: 1px 8px;
  border-radius: 4px;
  font-size: 11px;
  cursor: pointer;
  opacity: 0;
  transition: opacity 0.15s;
}
.md-block :deep(.md-code:hover .md-code-copy) { opacity: 1; }

@media (max-width: 768px) {
  .md-block { font-size: 13px; line-height: 1.7; }
  .md-block :deep(h1) { font-size: 17px; }
  .md-block :deep(h2) { font-size: 15.5px; }
  .md-block :deep(h3) { font-size: 14px; }
  .md-block :deep(.md-code pre) { padding: 28px 11px 10px; }
  .md-block :deep(.md-code code) { font-size: 11.5px; }
  .md-block :deep(th),
  .md-block :deep(td) { padding: 5px 8px; }
}

@media (prefers-color-scheme: dark) {
  .md-block { color: #d8dbe0; }
  .md-block :deep(h1),
  .md-block :deep(h2),
  .md-block :deep(h3),
  .md-block :deep(h4),
  .md-block :deep(h5) { color: #f0f1f3; }
  .md-block :deep(blockquote) { background: #26282c; border-left-color: #3a3d42; color: #a8adb5; }
  .md-block :deep(code) { background: #2b2d31; color: #f08a8a; }
  .md-block :deep(th) { background: #26282c; }
  .md-block :deep(th),
  .md-block :deep(td) { border-color: #3a3d42; }
  .md-block :deep(hr) { border-top-color: #3a3d42; }
}
</style>
