<template>
  <div class="markdown-renderer" v-html="renderedContent" ref="contentRef"></div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted } from 'vue'
import hljs from 'highlight.js'

const props = defineProps<{
  content: string
  sanitize?: boolean
}>()

const contentRef = ref<HTMLElement | null>(null)

// Simple markdown parser
const renderedContent = computed(() => {
  if (!props.content) return ''
  
  let html = props.content
  
  // Escape HTML (unless disabled)
  if (props.sanitize !== false) {
    html = html
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
  }
  
  // Code blocks (```language\ncode\n```)
  html = html.replace(/```(\w*)\n([\s\S]*?)```/g, (match, lang, code) => {
    const langClass = lang ? `language-${lang}` : ''
    const highlighted = lang && hljs.getLanguage(lang) 
      ? hljs.highlight(code.trim(), { language: lang }).value 
      : code.trim().replace(/</g, '&lt;').replace(/>/g, '&gt;')
    return `<pre class="code-block"><code class="${langClass}">${highlighted}</code><button class="copy-btn" onclick="navigator.clipboard.writeText(this.parentElement.querySelector('code').textContent)">📋 Copy</button></pre>`
  })
  
  // Inline code (`code`)
  html = html.replace(/`([^`]+)`/g, '<code class="inline-code">$1</code>')
  
  // Headers
  html = html.replace(/^###### (.+)$/gm, '<h6>$1</h6>')
  html = html.replace(/^##### (.+)$/gm, '<h5>$1</h5>')
  html = html.replace(/^#### (.+)$/gm, '<h4>$1</h4>')
  html = html.replace(/^### (.+)$/gm, '<h3>$1</h3>')
  html = html.replace(/^## (.+)$/gm, '<h2>$1</h2>')
  html = html.replace(/^# (.+)$/gm, '<h1>$1</h1>')
  
  // Bold and Italic
  html = html.replace(/\*\*\*(.+?)\*\*\*/g, '<strong><em>$1</em></strong>')
  html = html.replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>')
  html = html.replace(/\*(.+?)\*/g, '<em>$1</em>')
  html = html.replace(/__(.+?)__/g, '<strong>$1</strong>')
  html = html.replace(/_(.+?)_/g, '<em>$1</em>')
  
  // Strikethrough
  html = html.replace(/~~(.+?)~~/g, '<del>$1</del>')
  
  // Links
  html = html.replace(/\[([^\]]+)\]\(([^)]+)\)/g, '<a href="$2" target="_blank" rel="noopener">$1</a>')
  
  // Images
  html = html.replace(/!\[([^\]]*)\]\(([^)]+)\)/g, '<img src="$2" alt="$1" loading="lazy" />')
  
  // Blockquotes
  html = html.replace(/^&gt; (.+)$/gm, '<blockquote>$1</blockquote>')
  
  // Unordered lists
  html = html.replace(/^[\-\*] (.+)$/gm, '<li>$1</li>')
  html = html.replace(/(<li>.*<\/li>\n?)+/g, '<ul>$&</ul>')
  
  // Ordered lists
  html = html.replace(/^\d+\. (.+)$/gm, '<li>$1</li>')
  
  // Horizontal rules
  html = html.replace(/^---$/gm, '<hr />')
  html = html.replace(/^\*\*\*$/gm, '<hr />')
  
  // Tables (basic support)
  const tableRegex = /^\|(.+)\|$/gm
  const tables = html.match(tableRegex)
  if (tables) {
    let inTable = false
    let tableRows: string[] = []
    html = html.replace(tableRegex, (match, content) => {
      if (!inTable) {
        inTable = true
        tableRows = []
      }
      const cells = content.split('|').filter(c => c.trim())
      if (cells.some(c => /^[-:]+$/.test(c.trim()))) {
        // Header row separator, skip
        return ''
      }
      tableRows.push(`<tr>${cells.map(c => `<td>${c.trim()}</td>`).join('')}</tr>`)
      if (tableRows.length === 1) {
        const header = tableRows[0].replace(/<td>/g, '<th>').replace(/<\/td>/g, '</th>')
        tableRows[0] = header
      }
      return ''
    })
    if (tableRows.length > 0) {
      html += `<table><thead>${tableRows[0]}</thead><tbody>${tableRows.slice(1).join('')}</tbody></table>`
    }
  }
  
  // Line breaks
  html = html.replace(/\n\n/g, '</p><p>')
  html = '<p>' + html + '</p>'
  html = html.replace(/<p><\/p>/g, '')
  html = html.replace(/<p>(<h[1-6]|<ul|<ol|<blockquote|<pre|<table)/g, '$1')
  html = html.replace(/(<\/h[1-6]>|<\/ul>|<\/ol>|<\/blockquote>|<\/pre>|<\/table>)<\/p>/g, '$1')
  
  return html
})

// Auto-apply syntax highlighting after render
watch(() => props.content, () => {
  setTimeout(highlightCode, 0)
})

onMounted(() => {
  highlightCode()
})

function highlightCode() {
  if (!contentRef.value) return
  const codeBlocks = contentRef.value.querySelectorAll('pre code:not(.hljs)')
  codeBlocks.forEach((block) => {
    hljs.highlightElement(block as HTMLElement)
  })
}
</script>

<style>
.markdown-renderer {
  font-size: 14px;
  line-height: 1.6;
  color: var(--text-primary);
}

.markdown-renderer h1,
.markdown-renderer h2,
.markdown-renderer h3,
.markdown-renderer h4,
.markdown-renderer h5,
.markdown-renderer h6 {
  margin: 16px 0 8px;
  font-weight: 600;
}

.markdown-renderer h1 { font-size: 24px; }
.markdown-renderer h2 { font-size: 20px; }
.markdown-renderer h3 { font-size: 18px; }
.markdown-renderer h4 { font-size: 16px; }

.markdown-renderer p {
  margin: 0 0 12px;
}

.markdown-renderer a {
  color: var(--primary-color);
  text-decoration: none;
}

.markdown-renderer a:hover {
  text-decoration: underline;
}

.markdown-renderer img {
  max-width: 100%;
  border-radius: 8px;
  margin: 12px 0;
}

.markdown-renderer blockquote {
  margin: 12px 0;
  padding: 12px 16px;
  border-left: 4px solid var(--primary-color);
  background: var(--bg-secondary);
  border-radius: 0 8px 8px 0;
}

.markdown-renderer ul,
.markdown-renderer ol {
  margin: 12px 0;
  padding-left: 24px;
}

.markdown-renderer li {
  margin: 4px 0;
}

.markdown-renderer hr {
  border: none;
  border-top: 1px solid var(--border-color);
  margin: 20px 0;
}

.markdown-renderer table {
  width: 100%;
  border-collapse: collapse;
  margin: 16px 0;
}

.markdown-renderer th,
.markdown-renderer td {
  padding: 8px 12px;
  border: 1px solid var(--border-color);
  text-align: left;
}

.markdown-renderer th {
  background: var(--bg-secondary);
  font-weight: 600;
}

/* Inline code */
.markdown-renderer .inline-code {
  padding: 2px 6px;
  background: var(--bg-secondary);
  border-radius: 4px;
  font-family: 'Fira Code', monospace;
  font-size: 13px;
}

/* Code blocks */
.markdown-renderer .code-block {
  position: relative;
  margin: 16px 0;
  border-radius: 12px;
  overflow: hidden;
  background: #1e1e1e;
}

.markdown-renderer .code-block code {
  display: block;
  padding: 16px;
  padding-top: 40px;
  overflow-x: auto;
  font-family: 'Fira Code', 'Consolas', monospace;
  font-size: 13px;
  line-height: 1.5;
}

.markdown-renderer .code-block .copy-btn {
  position: absolute;
  top: 8px;
  right: 8px;
  padding: 4px 8px;
  background: rgba(255, 255, 255, 0.1);
  border: none;
  border-radius: 4px;
  color: #fff;
  font-size: 12px;
  cursor: pointer;
  opacity: 0;
  transition: opacity 0.2s;
}

.markdown-renderer .code-block:hover .copy-btn {
  opacity: 1;
}

.markdown-renderer .code-block .copy-btn:hover {
  background: rgba(255, 255, 255, 0.2);
}

/* Highlight.js theme (One Dark inspired) */
.hljs {
  color: #abb2bf;
}
.hljs-comment,
.hljs-quote {
  color: #5c6370;
  font-style: italic;
}
.hljs-doctag,
.hljs-keyword,
.hljs-formula {
  color: #c678dd;
}
.hljs-section,
.hljs-name,
.hljs-selector-tag,
.hljs-deletion,
.hljs-subst {
  color: #e06c75;
}
.hljs-literal {
  color: #56b6c2;
}
.hljs-string,
.hljs-regexp,
.hljs-addition,
.hljs-attribute,
.hljs-meta .hljs-string {
  color: #98c379;
}
.hljs-attr,
.hljs-variable,
.hljs-template-variable,
.hljs-type,
.hljs-selector-class,
.hljs-selector-attr,
.hljs-selector-pseudo,
.hljs-number {
  color: #d19a66;
}
.hljs-symbol,
.hljs-bullet,
.hljs-link,
.hljs-meta,
.hljs-selector-id,
.hljs-title {
  color: #61afef;
}
.hljs-built_in,
.hljs-title.class_,
.hljs-class .hljs-title {
  color: #e6c07b;
}
.hljs-emphasis {
  font-style: italic;
}
.hljs-strong {
  font-weight: bold;
}
.hljs-link {
  text-decoration: underline;
}
</style>
