<template>
  <div class="reasoning-wrapper">
    <!-- 思考过程部分 - 仅在有明确 <think> 标签时显示 -->
    <div v-if="reasoningPart" class="reasoning-section">
      <div class="reasoning-header" @click="toggleExpand">
        <n-icon size="16">
          <ChevronForward v-if="!expanded" />
          <ChevronDown v-else />
        </n-icon>
        <span class="reasoning-title">💭 {{ t('chat.thinking') }}</span>
      </div>
      <n-collapse-transition :show="expanded">
        <div class="reasoning-content" v-html="renderMarkdown(reasoningPart)"></div>
      </n-collapse-transition>
    </div>

    <!-- 最终内容 -->
    <div v-if="finalPart" class="final-content" v-html="renderMarkdown(finalPart)"></div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronForward, ChevronDown } from '@vicons/ionicons5'
import { marked } from 'marked'
import hljs from 'highlight.js'
import 'highlight.js/styles/github-dark.css'

const props = defineProps<{
  content: string
}>()

const { t } = useI18n()
const expanded = ref(false)

// Only split thinking/reasoning when there are explicit markers.
// <think>...</think> tags or specific headings like "### 思考过程".
// Everything else is rendered as normal content — no false positives.
const parsedContent = computed(() => {
  const content = props.content

  // 1. <think>...</think> tags (DeepSeek, QwQ, etc.)
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

  // 2. Explicit headings: "### 思考过程" / "### Thinking" etc.
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

  // Default: no thinking markers found, render everything as normal content
  return { reasoning: '', final: content }
})

const reasoningPart = computed(() => parsedContent.value.reasoning)
const finalPart = computed(() => parsedContent.value.final)

function toggleExpand() {
  expanded.value = !expanded.value
}

// Markdown rendering with syntax highlighting
const codeRenderer = (code: string, lang?: string): string => {
  const language = lang && hljs.getLanguage(lang) ? lang : null
  const highlighted = language
    ? hljs.highlight(code, { language }).value
    : hljs.highlightAuto(code).value
  const copyBtn = `<button class="code-copy-btn" onclick="(function(btn){var code=btn.parentElement.querySelector('code');navigator.clipboard.writeText(code.textContent);btn.textContent='✓';setTimeout(()=>btn.textContent='Copy',2000)})(this)">Copy</button>`
  return `<div class="code-block">${copyBtn}<pre><code class="hljs${language ? ` language-${language}` : ''}">${highlighted}</code></pre></div>`
}

marked.use({ renderer: { code: codeRenderer } })

function renderMarkdown(content: string): string {
  return marked.parse(content) as string
}
</script>

<style scoped>
.reasoning-wrapper {
  width: 100%;
}

.reasoning-section {
  margin-bottom: 10px;
  border: 1px solid #e8e8e8;
  border-radius: 8px;
  background: #fafafa;
  overflow: hidden;
}

.reasoning-header {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  cursor: pointer;
  background: #f5f5f5;
  transition: background 0.15s;
  font-size: 13px;
}

.reasoning-header:hover {
  background: #eeeeee;
}

.reasoning-title {
  font-weight: 500;
  color: #888;
  flex: 1;
}

.reasoning-content {
  padding: 12px;
  max-height: 400px;
  overflow-y: auto;
  font-size: 14px;
  color: #666;
  line-height: 1.6;
}

.reasoning-content :deep(p) {
  margin: 0 0 8px 0;
}

.reasoning-content :deep(p:last-child) {
  margin-bottom: 0;
}

.final-content {
  font-size: 15px;
  line-height: 1.7;
}

.final-content :deep(p:first-child) {
  margin-top: 0;
}

.final-content :deep(p:last-child) {
  margin-bottom: 0;
}

/* Code blocks */
.final-content :deep(.code-block),
.reasoning-content :deep(.code-block) {
  position: relative;
  margin: 8px 0;
  border-radius: 8px;
  overflow: hidden;
  background: #1e1e1e;
}

.final-content :deep(.code-block pre),
.reasoning-content :deep(.code-block pre) {
  margin: 0;
  padding: 12px 16px;
  overflow-x: auto;
}

.final-content :deep(.code-block code),
.reasoning-content :deep(.code-block code) {
  font-family: 'SF Mono', 'Fira Code', 'Consolas', monospace;
  font-size: 13px;
  color: #d4d4d4;
}

.final-content :deep(.code-copy-btn),
.reasoning-content :deep(.code-copy-btn) {
  position: absolute;
  top: 6px;
  right: 6px;
  background: rgba(255, 255, 255, 0.1);
  border: 1px solid rgba(255, 255, 255, 0.2);
  color: #ccc;
  padding: 2px 10px;
  border-radius: 4px;
  font-size: 12px;
  cursor: pointer;
  opacity: 0;
  transition: opacity 0.2s;
}

.final-content :deep(.code-block:hover .code-copy-btn),
.reasoning-content :deep(.code-block:hover .code-copy-btn) {
  opacity: 1;
}

/* Tables */
.final-content :deep(table) {
  border-collapse: collapse;
  width: 100%;
  margin: 8px 0;
  font-size: 14px;
}

.final-content :deep(th),
.final-content :deep(td) {
  border: 1px solid #e0e0e0;
  padding: 8px 12px;
  text-align: left;
}

.final-content :deep(th) {
  background: #f0f0f0;
  font-weight: 600;
}

/* Lists */
.final-content :deep(ul),
.final-content :deep(ol) {
  margin: 8px 0;
  padding-left: 24px;
}

.final-content :deep(li) {
  margin: 4px 0;
}

/* Blockquote */
.final-content :deep(blockquote) {
  border-left: 3px solid #d0d0d0;
  padding-left: 12px;
  margin: 8px 0;
  color: #666;
}

/* Links */
.final-content :deep(a) {
  color: #18a058;
  text-decoration: none;
}

.final-content :deep(a:hover) {
  text-decoration: underline;
}

/* Dark mode */
@media (prefers-color-scheme: dark) {
  .reasoning-section {
    background: #222;
    border-color: #444;
  }

  .reasoning-header {
    background: #2a2a2a;
  }

  .reasoning-header:hover {
    background: #333;
  }

  .reasoning-title {
    color: #999;
  }

  .reasoning-content {
    color: #aaa;
  }

  .final-content :deep(th) {
    background: #252525;
  }

  .final-content :deep(th),
  .final-content :deep(td) {
    border-color: #333;
  }

  .final-content :deep(blockquote) {
    border-left-color: #444;
    color: #999;
  }
}
</style>
