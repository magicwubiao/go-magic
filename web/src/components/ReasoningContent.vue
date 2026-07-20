<template>
  <div class="reasoning-wrapper">
    <div v-if="reasoningPart" class="deep-thinking-panel" :class="{ collapsed: !expanded }">
      <div class="thinking-header" @click="toggleExpand">
        <div class="thinking-left">
          <div class="thinking-icon">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <path d="M12 2a7 7 0 0 0-4 12.74V17a2 2 0 0 0 2 2h4a2 2 0 0 0 2-2v-2.26A7 7 0 0 0 12 2z"/>
              <path d="M9 22h6"/>
              <path d="M12 18v4"/>
            </svg>
          </div>
          <span class="thinking-label">{{ t('chat.thinking') }}</span>
          <span v-if="reasoningDuration" class="thinking-duration">{{ reasoningDuration }}</span>
        </div>
        <div class="thinking-toggle">
          <n-icon size="16" class="toggle-icon">
            <ChevronUp v-if="expanded" />
            <ChevronDown v-else />
          </n-icon>
        </div>
      </div>

      <n-collapse-transition :show="expanded">
        <div class="thinking-body">
          <div class="thinking-content" v-html="renderMarkdown(reasoningPart)"></div>
        </div>
      </n-collapse-transition>

      <div v-if="!expanded && reasoningPreview" class="thinking-preview">
        {{ reasoningPreview }}
      </div>
    </div>

    <div v-if="finalPart" class="final-content" v-html="renderMarkdown(finalPart)"></div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronUp, ChevronDown } from '@vicons/ionicons5'
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
}>()

const { t } = useI18n()
const expanded = ref(true)

const parsedContent = computed(() => {
  const content = props.content

  const thinkOpen = '<think>'
  const thinkClose = '</think>'
  const openIdx = content.toLowerCase().indexOf(thinkOpen)
  if (openIdx !== -1) {
    const closeIdx = content.toLowerCase().indexOf(thinkClose, openIdx + thinkOpen.length)
    if (closeIdx !== -1) {
      const reasoning = content.substring(openIdx + thinkOpen.length, closeIdx).trim()
      const before = content.substring(0, openIdx).trim()
      const after = content.substring(closeIdx + thinkClose.length).trim()
      const final = (before + '\n' + after).trim()
      return { reasoning, final }
    }
  }

  const headingRe = /(?:###|##)\s*(?:思考过程|Thinking|Reasoning|Thought|分析过程)\s*\n([\s\S]*?)(?=\n(?:###|##)\s*(?:最终结论|结论|Conclusion|Answer|回答)|$)/i
  const headingMatch = content.match(headingRe)
  if (headingMatch) {
    const fullRe = /(?:###|##)\s*(?:思考过程|Thinking|Reasoning|Thought|分析过程)[\s\S]*?(?=\n(?:###|##)\s*(?:最终结论|结论|Conclusion|Answer|回答)|$)/i
    const fullMatch = content.match(fullRe)
    if (fullMatch) {
      const reasoning = headingMatch[1].trim()
      const final = content.replace(fullMatch[0], '').replace(/\n(?:###|##)\s*(?:最终结论|结论|Conclusion|Answer|回答)\s*\n?/i, '').trim()
      if (reasoning) {
        return { reasoning, final }
      }
    }
  }

  return { reasoning: '', final: content }
})

const reasoningPart = computed(() => parsedContent.value.reasoning)
const finalPart = computed(() => parsedContent.value.final)

const reasoningDuration = computed(() => props.duration || '')

const reasoningPreview = computed(() => {
  const text = reasoningPart.value.replace(/[#*`>\-\n]/g, ' ').replace(/\s+/g, ' ').trim()
  return text.length > 100 ? text.substring(0, 100) + '...' : text
})

function toggleExpand() {
  expanded.value = !expanded.value
}

const codeRenderer = (code: string, lang?: string): string => {
  const language = lang && hljs.getLanguage(lang) ? lang : null
  const highlighted = language
    ? hljs.highlight(code, { language }).value
    : hljs.highlightAuto(code).value
  // 移除 inline onclick，改用 class + 事件委托（见 handleCodeBlockClick）
  const copyBtn = `<button class="code-copy-btn" type="button">Copy</button>`
  return `<div class="code-block">${copyBtn}<pre><code class="hljs${language ? ` language-${language}` : ''}">${highlighted}</code></pre></div>`
}

marked.use({ renderer: { code: codeRenderer } })

// 处理代码块按钮点击（事件委托替代 inline onclick，避免 v-html + inline handler XSS 风险）
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
  gap: 12px;
}

.deep-thinking-panel {
  background: #f8f9fa;
  border: 1px solid #e9ecef;
  border-radius: 12px;
  overflow: hidden;
  transition: all 0.2s ease;
}

.deep-thinking-panel.collapsed {
  background: #f1f3f5;
}

.thinking-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 16px;
  cursor: pointer;
  user-select: none;
  transition: background 0.15s;
}

.thinking-header:hover {
  background: rgba(0, 0, 0, 0.02);
}

.collapsed .thinking-header:hover {
  background: rgba(0, 0, 0, 0.04);
}

.thinking-left {
  display: flex;
  align-items: center;
  gap: 10px;
}

.thinking-icon {
  width: 20px;
  height: 20px;
  color: #868e96;
  display: flex;
  align-items: center;
  justify-content: center;
}

.thinking-icon svg {
  width: 18px;
  height: 18px;
}

.thinking-label {
  font-size: 13.5px;
  font-weight: 600;
  color: #495057;
}

.thinking-duration {
  font-size: 12px;
  color: #adb5bd;
  background: #e9ecef;
  padding: 2px 8px;
  border-radius: 10px;
  font-weight: 500;
}

.thinking-toggle {
  display: flex;
  align-items: center;
}

.toggle-icon {
  color: #adb5bd;
  transition: transform 0.2s;
}

.thinking-body {
  border-top: 1px solid #e9ecef;
  padding: 16px 20px;
  background: #fdfdfd;
}

.thinking-content {
  font-size: 13.5px;
  line-height: 1.75;
  color: #495057;
}

.thinking-content :deep(p) {
  margin: 0 0 12px 0;
}

.thinking-content :deep(p:last-child) {
  margin-bottom: 0;
}

.thinking-content :deep(ul),
.thinking-content :deep(ol) {
  margin: 10px 0;
  padding-left: 24px;
}

.thinking-content :deep(li) {
  margin: 4px 0;
}

.thinking-content :deep(h1),
.thinking-content :deep(h2),
.thinking-content :deep(h3),
.thinking-content :deep(h4) {
  margin: 16px 0 8px 0;
  font-weight: 600;
  color: #343a40;
}

.thinking-content :deep(h1) { font-size: 17px; }
.thinking-content :deep(h2) { font-size: 16px; }
.thinking-content :deep(h3) { font-size: 15px; }
.thinking-content :deep(h4) { font-size: 14px; }

.thinking-content :deep(blockquote) {
  border-left: 3px solid #ced4da;
  padding-left: 12px;
  margin: 10px 0;
  color: #6c757d;
  font-style: italic;
}

.thinking-content :deep(hr) {
  border: none;
  border-top: 1px dashed #dee2e6;
  margin: 14px 0;
}

.thinking-content :deep(pre) {
  margin: 10px 0;
}

.thinking-content :deep(code) {
  font-family: 'SF Mono', 'Fira Code', 'Consolas', monospace;
  font-size: 12.5px;
  background: #e9ecef;
  padding: 2px 6px;
  border-radius: 4px;
  color: #495057;
}

.thinking-content :deep(.code-block) {
  position: relative;
  margin: 8px 0;
  border-radius: 8px;
  overflow: hidden;
  background: #1e1e1e;
}

.thinking-content :deep(.code-block pre) {
  margin: 0;
  padding: 12px 16px;
  overflow-x: auto;
}

.thinking-content :deep(.code-block code) {
  font-family: 'SF Mono', 'Fira Code', 'Consolas', monospace;
  font-size: 12.5px;
  color: #d4d4d4;
  background: transparent;
  padding: 0;
  border-radius: 0;
}

.thinking-content :deep(.code-copy-btn) {
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

.thinking-content :deep(.code-block:hover .code-copy-btn) {
  opacity: 1;
}

.thinking-preview {
  padding: 0 16px 12px;
  font-size: 12.5px;
  color: #868e96;
  line-height: 1.5;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.final-content {
  font-size: 15px;
  line-height: 1.8;
  color: #222;
}

.final-content :deep(p) {
  margin: 0 0 14px 0;
}

.final-content :deep(p:first-child) {
  margin-top: 0;
}

.final-content :deep(p:last-child) {
  margin-bottom: 0;
}

.final-content :deep(ul),
.final-content :deep(ol) {
  margin: 12px 0;
  padding-left: 28px;
}

.final-content :deep(li) {
  margin: 6px 0;
}

.final-content :deep(h1),
.final-content :deep(h2),
.final-content :deep(h3),
.final-content :deep(h4) {
  margin: 22px 0 12px 0;
  font-weight: 600;
  color: #111;
}

.final-content :deep(h1) { font-size: 22px; }
.final-content :deep(h2) { font-size: 19px; }
.final-content :deep(h3) { font-size: 17px; }
.final-content :deep(h4) { font-size: 15px; }

.final-content :deep(blockquote) {
  border-left: 4px solid #18a058;
  padding-left: 16px;
  margin: 14px 0;
  color: #555;
  background: #f0faf0;
  padding-top: 10px;
  padding-bottom: 10px;
  border-radius: 0 8px 8px 0;
}

.final-content :deep(hr) {
  border: none;
  border-top: 1px solid #e0e0e0;
  margin: 18px 0;
}

.final-content :deep(table) {
  margin: 14px 0;
}

.final-content :deep(pre) {
  margin: 12px 0;
}

.final-content :deep(.code-block) {
  position: relative;
  margin: 10px 0;
  border-radius: 10px;
  overflow: hidden;
  background: #1e1e1e;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.08);
}

.final-content :deep(.code-block pre) {
  margin: 0;
  padding: 14px 18px;
  overflow-x: auto;
}

.final-content :deep(.code-block code) {
  font-family: 'SF Mono', 'Fira Code', 'Consolas', monospace;
  font-size: 13.5px;
  color: #d4d4d4;
  line-height: 1.65;
}

.final-content :deep(.code-copy-btn) {
  position: absolute;
  top: 8px;
  right: 8px;
  background: rgba(255, 255, 255, 0.1);
  border: 1px solid rgba(255, 255, 255, 0.2);
  color: #ccc;
  padding: 3px 12px;
  border-radius: 6px;
  font-size: 12px;
  cursor: pointer;
  opacity: 0;
  transition: opacity 0.2s, background 0.2s;
}

.final-content :deep(.code-block:hover .code-copy-btn) {
  opacity: 1;
}

.final-content :deep(.code-copy-btn:hover) {
  background: rgba(255, 255, 255, 0.2);
}

.final-content :deep(table) {
  border-collapse: collapse;
  width: 100%;
  margin: 10px 0;
  font-size: 14px;
  border-radius: 8px;
  overflow: hidden;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.06);
}

.final-content :deep(th),
.final-content :deep(td) {
  border: 1px solid #e0e0e0;
  padding: 10px 14px;
  text-align: left;
}

.final-content :deep(th) {
  background: #f0f0f0;
  font-weight: 600;
}

.final-content :deep(a) {
  color: #18a058;
  text-decoration: none;
  font-weight: 500;
}

.final-content :deep(a:hover) {
  text-decoration: underline;
}

.final-content :deep(img) {
  max-width: 100%;
  border-radius: 8px;
  margin: 10px 0;
}

@media (prefers-color-scheme: dark) {
  .deep-thinking-panel {
    background: #1e1e1e;
    border-color: #2d2d2d;
  }

  .deep-thinking-panel.collapsed {
    background: #1a1a1a;
  }

  .thinking-header:hover {
    background: rgba(255, 255, 255, 0.03);
  }

  .collapsed .thinking-header:hover {
    background: rgba(255, 255, 255, 0.05);
  }

  .thinking-label {
    color: #dee2e6;
  }

  .thinking-icon {
    color: #868e96;
  }

  .thinking-duration {
    background: #2d2d2d;
    color: #adb5bd;
  }

  .toggle-icon {
    color: #6c757d;
  }

  .thinking-body {
    border-top-color: #2d2d2d;
    background: #1a1a1a;
  }

  .thinking-content {
    color: #adb5bd;
  }

  .thinking-content :deep(h1),
  .thinking-content :deep(h2),
  .thinking-content :deep(h3),
  .thinking-content :deep(h4) {
    color: #ced4da;
  }

  .thinking-content :deep(code) {
    background: #2d2d2d;
    color: #dee2e6;
  }

  .thinking-content :deep(blockquote) {
    border-left-color: #495057;
    color: #868e96;
  }

  .thinking-preview {
    color: #6c757d;
  }

  .final-content {
    color: #ddd;
  }

  .final-content :deep(th) {
    background: #252525;
  }

  .final-content :deep(th),
  .final-content :deep(td) {
    border-color: #333;
  }

  .final-content :deep(blockquote) {
    border-left-color: #36ad6a;
    color: #aaa;
    background: #1a2a1a;
  }
}
</style>
