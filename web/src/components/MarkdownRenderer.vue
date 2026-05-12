<template>
  <div class="markdown-body" v-html="renderedContent"></div>
</template>

<script setup lang="ts">
import { computed, onMounted, watch } from 'vue'
import hljs from 'highlight.js'
import 'highlight.js/styles/github.css'

const props = defineProps<{
  content: string
  isDark?: boolean
}>()

const renderedContent = computed(() => {
  let content = escapeHtml(props.content || '')

  // Code blocks with syntax highlighting
  content = content.replace(/```(\w+)?\n([\s\S]*?)```/g, (match, lang, code) => {
    const language = lang || 'plaintext'
    let highlighted = code
    try {
      if (hljs.getLanguage(language)) {
        highlighted = hljs.highlight(code.trim(), { language, ignoreIllegals: true }).value
      } else {
        highlighted = hljs.highlightAuto(code.trim()).value
      }
    } catch (e) {
      // fallback
    }
    return `
      <div class="code-block">
        <div class="code-header">
          <span class="code-lang">${language}</span>
          <button class="copy-btn" onclick="copyCode(this)">📋 Copy</button>
        </div>
        <pre><code class="hljs language-${language}">${highlighted}</code></pre>
      </div>
    `
  })

  // Inline code
  content = content.replace(/`([^`]+)`/g, '<code class="inline-code">$1</code>')

  // Headers
  content = content.replace(/^### (.*$)/gm, '<h3>$1</h3>')
  content = content.replace(/^## (.*$)/gm, '<h2>$1</h2>')
  content = content.replace(/^# (.*$)/gm, '<h1>$1</h1>')

  // Bold and italic
  content = content.replace(/\*\*\*(.*?)\*\*\*/g, '<strong><em>$1</em></strong>')
  content = content.replace(/\*\*(.*?)\*\*/g, '<strong>$1</strong>')
  content = content.replace(/\*(.*?)\*/g, '<em>$1</em>')
  content = content.replace(/__(.*?)__/g, '<u>$1</u>')

  // Links
  content = content.replace(/\[([^\]]+)\]\(([^)]+)\)/g, '<a href="$2" target="_blank" rel="noopener">$1</a>')

  // Images
  content = content.replace(/!\[([^\]]*)\]\(([^)]+)\)/g, '<img src="$2" alt="$1" loading="lazy" />')

  // Blockquotes
  content = content.replace(/^> (.*$)/gm, '<blockquote>$1</blockquote>')

  // Tables
  content = content.replace(/\|(.+)\|/g, (match) => {
    const cells = match.split('|').filter(c => c.trim())
    if (cells.some(c => /^-+$/.test(c.trim()))) {
      return ''
    }
    const row = cells.map(c => `<td>${c.trim()}</td>`).join('')
    return `<tr>${row}</tr>`
  })

  // Lists
  content = content.replace(/^\d+\. (.*$)/gm, '<li>$1</li>')
  content = content.replace(/^- (.*$)/gm, '<li>$1</li>')
  content = content.replace(/(<li>.*<\/li>\n?)+/g, '<ul>$&</ul>')

  // Horizontal rules
  content = content.replace(/^---$/gm, '<hr />')
  content = content.replace(/^\*\*\*$/gm, '<hr />')

  // Line breaks
  content = content.replace(/\n\n/g, '</p><p>')
  content = content.replace(/\n/g, '<br />')

  // Wrap in paragraphs
  if (!content.startsWith('<')) {
    content = '<p>' + content + '</p>'
  }

  return content
})

const escapeHtml = (text: string): string => {
  const map: Record<string, string> = {
    '&': '&amp;',
    '<': '&lt;',
    '>': '&gt;',
    '"': '&quot;',
    "'": '&#039;'
  }
  return text.replace(/[&<>"']/g, m => map[m])
}

// Global copy function for code blocks
if (typeof window !== 'undefined') {
  (window as any).copyCode = async (btn: HTMLButtonElement) => {
    const codeBlock = btn.parentElement?.nextElementSibling
    const code = codeBlock?.textContent || ''
    try {
      await navigator.clipboard.writeText(code)
      btn.textContent = '✅ Copied!'
      setTimeout(() => btn.textContent = '📋 Copy', 2000)
    } catch (err) {
      btn.textContent = '❌ Failed'
    }
  }
}
</script>

<style>
.markdown-body {
  font-size: 15px;
  line-height: 1.7;
  word-wrap: break-word;
}

.markdown-body h1 {
  font-size: 1.8em;
  margin: 0.8em 0;
  padding-bottom: 0.3em;
  border-bottom: 1px solid #eaecef;
}

.markdown-body h2 {
  font-size: 1.5em;
  margin: 0.8em 0;
  padding-bottom: 0.3em;
  border-bottom: 1px solid #eaecef;
}

.markdown-body h3 {
  font-size: 1.25em;
  margin: 0.6em 0;
}

.markdown-body p {
  margin: 0.8em 0;
}

.markdown-body a {
  color: #4f46e5;
  text-decoration: none;
}

.markdown-body a:hover {
  text-decoration: underline;
}

.markdown-body blockquote {
  margin: 1em 0;
  padding: 0.5em 1em;
  border-left: 4px solid #4f46e5;
  background: #f6f8fa;
  color: #24292e;
}

.markdown-body ul,
.markdown-body ol {
  margin: 1em 0;
  padding-left: 2em;
}

.markdown-body li {
  margin: 0.3em 0;
}

.markdown-body table {
  border-collapse: collapse;
  margin: 1em 0;
  width: 100%;
}

.markdown-body td,
.markdown-body th {
  border: 1px solid #dfe2e5;
  padding: 0.5em 1em;
}

.markdown-body th {
  background: #f6f8fa;
  font-weight: 600;
}

.markdown-body hr {
  border: none;
  border-top: 1px solid #eaecef;
  margin: 1.5em 0;
}

.markdown-body img {
  max-width: 100%;
  height: auto;
  border-radius: 8px;
  margin: 1em 0;
}

/* Inline code */
.markdown-body .inline-code {
  padding: 0.2em 0.4em;
  background: #f1f3f4;
  border-radius: 4px;
  font-family: 'Fira Code', 'Consolas', monospace;
  font-size: 0.9em;
  color: #e83e8c;
}

/* Code blocks */
.markdown-body .code-block {
  margin: 1em 0;
  border-radius: 8px;
  overflow: hidden;
  background: #f6f8fa;
  border: 1px solid #eaecef;
}

.markdown-body .code-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 8px 16px;
  background: #eaecef;
  border-bottom: 1px solid #dfe2e5;
}

.markdown-body .code-lang {
  font-size: 12px;
  color: #6a737d;
  font-weight: 500;
}

.markdown-body .copy-btn {
  background: white;
  border: 1px solid #dfe2e5;
  border-radius: 4px;
  padding: 4px 8px;
  font-size: 12px;
  cursor: pointer;
  transition: all 0.2s;
}

.markdown-body .copy-btn:hover {
  background: #f6f8fa;
}

.markdown-body pre {
  margin: 0;
  padding: 16px;
  overflow-x: auto;
  font-family: 'Fira Code', 'Consolas', monospace;
  font-size: 14px;
  line-height: 1.5;
}

.markdown-body code {
  font-family: 'Fira Code', 'Consolas', monospace;
}

/* Dark theme */
.dark-theme .markdown-body {
  color: #c9d1d9;
}

.dark-theme .markdown-body h1,
.dark-theme .markdown-body h2 {
  border-color: #30363d;
}

.dark-theme .markdown-body a {
  color: #8b5cf6;
}

.dark-theme .markdown-body blockquote {
  background: #161b22;
  border-color: #8b5cf6;
  color: #c9d1d9;
}

.dark-theme .markdown-body td,
.dark-theme .markdown-body th {
  border-color: #30363d;
}

.dark-theme .markdown-body th {
  background: #161b22;
}

.dark-theme .markdown-body hr {
  border-color: #30363d;
}

.dark-theme .markdown-body .code-block {
  background: #0d1117;
  border-color: #30363d;
}

.dark-theme .markdown-body .code-header {
  background: #161b22;
  border-color: #30363d;
}

.dark-theme .markdown-body .inline-code {
  background: #161b22;
  color: #f97583;
}

.dark-theme .markdown-body pre {
  background: #0d1117;
}
</style>
