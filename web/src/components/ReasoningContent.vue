<template>
  <div class="reasoning-wrapper">
    <!-- 思考过程部分 - 可折叠 -->
    <div v-if="reasoningPart" class="reasoning-section">
      <div class="reasoning-header" @click="toggleExpand">
        <n-icon size="16">
          <ChevronForward v-if="!expanded" />
          <ChevronDown v-else />
        </n-icon>
        <span class="reasoning-title">💭 {{ t('chat.thinking') }}</span>
        <n-tag v-if="steps.length > 0" size="tiny" type="info">{{ steps.length }} {{ t('chat.steps') }}</n-tag>
      </div>
      <n-collapse-transition :show="expanded">
        <div class="reasoning-content">
          <div v-for="(step, index) in steps" :key="index" class="reasoning-step">
            <div class="step-number">{{ index + 1 }}</div>
            <div class="step-content" v-html="renderMarkdown(step)"></div>
          </div>
          <div v-if="steps.length === 0" class="reasoning-text" v-html="renderMarkdown(reasoningPart)"></div>
        </div>
      </n-collapse-transition>
    </div>
    
    <!-- 分割线 -->
    <div v-if="reasoningPart && finalPart" class="reasoning-divider">
      <span class="divider-line"></span>
      <span class="divider-text">✨ {{ t('chat.conclusion') }}</span>
      <span class="divider-line"></span>
    </div>
    
    <!-- 最终结论部分 -->
    <div v-if="finalPart" class="final-content" v-html="renderMarkdown(finalPart)"></div>
    
    <!-- 没有思考过程的普通内容 -->
    <div v-if="!reasoningPart && !finalPart" v-html="renderMarkdown(content)"></div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronForward, ChevronDown } from '@vicons/ionicons5'
import { marked } from 'marked'
import hljs from 'highlight.js'

const props = defineProps<{
  content: string
}>()

const { t } = useI18n()
const expanded = ref(false)

// 解析思考过程和最终结论
const parsedContent = computed(() => {
  const content = props.content
  
  // 尝试匹配 <think>...</think> 或 <reasoning>...</reasoning> 标签
  const thinkMatch = content.match(/<think>([\s\S]*?)<\/think>/i)
  if (thinkMatch) {
    const reasoning = thinkMatch[1].trim()
    const final = content.replace(/<think>[\s\S]*?<\/think>/i, '').trim()
    return { reasoning, final }
  }
  
  // 尝试匹配 ### 思考过程 / ### Thinking 等标题
  const reasoningMatch = content.match(/(?:###|##)\s*(?:思考过程|Thinking|Reasoning|Thought|分析过程)[\s\S]*?(?=\n(?:###|##)\s*(?:最终结论|结论|Conclusion|Answer|回答)|$)/i)
  if (reasoningMatch) {
    const reasoning = reasoningMatch[0].trim()
    const final = content.replace(reasoningMatch[0], '').replace(/\n(?:###|##)\s*(?:最终结论|结论|Conclusion|Answer|回答)\s*\n?/i, '').trim()
    return { reasoning, final }
  }
  
  // 尝试按特定分隔符分割（如 "---" 或 "===")
  const separatorMatch = content.match(/([\s\S]*?)(?:\n-{3,}|={3,}|\n#{3,}\n)([\s\S]*)/)
  if (separatorMatch && separatorMatch[1].length > 50 && separatorMatch[2].length > 20) {
    return { 
      reasoning: separatorMatch[1].trim(), 
      final: separatorMatch[2].trim() 
    }
  }
  
  // 默认：没有明显的思考过程标记
  return { reasoning: '', final: content }
})

const reasoningPart = computed(() => parsedContent.value.reasoning)
const finalPart = computed(() => parsedContent.value.final)

// 解析思考步骤
const steps = computed(() => {
  if (!reasoningPart.value) return []
  
  // 尝试按步骤分割（Step 1, Step 2... 或 1., 2., 或 - 等）
  const stepPatterns = [
    /(?:^|\n)(?:Step|步骤)\s*\d+[.:\s]/gi,
    /(?:^|\n)\d+[.:\s]\s+/g,
    /(?:^|\n)[-\*]\s+/g
  ]
  
  for (const pattern of stepPatterns) {
    const matches = reasoningPart.value.split(pattern).filter(s => s.trim())
    if (matches.length >= 2) {
      return matches.map(s => s.trim()).filter(s => s.length > 5)
    }
  }
  
  return []
})

function toggleExpand() {
  expanded.value = !expanded.value
}

// Markdown 渲染
const codeRenderer = (code: string, lang?: string): string => {
  const language = lang && hljs.getLanguage(lang) ? lang : null
  const highlighted = language
    ? hljs.highlight(code, { language }).value
    : hljs.highlightAuto(code).value
  return `<pre><code class="hljs${language ? ` language-${language}` : ''}">${highlighted}</code></pre>`
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
  margin-bottom: 12px;
  border: 1px solid #e0e0e0;
  border-radius: 8px;
  background: #fafafa;
  overflow: hidden;
}

.reasoning-header {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 12px;
  cursor: pointer;
  background: #f0f0f0;
  transition: background 0.2s;
}

.reasoning-header:hover {
  background: #e8e8e8;
}

.reasoning-title {
  font-weight: 500;
  flex: 1;
}

.reasoning-content {
  padding: 12px;
  max-height: 400px;
  overflow-y: auto;
}

.reasoning-step {
  display: flex;
  gap: 12px;
  margin-bottom: 12px;
  padding: 8px;
  background: white;
  border-radius: 6px;
  border-left: 3px solid #18a058;
}

.step-number {
  width: 24px;
  height: 24px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: #18a058;
  color: white;
  border-radius: 50%;
  font-size: 12px;
  font-weight: bold;
  flex-shrink: 0;
}

.step-content {
  flex: 1;
  font-size: 14px;
}

.step-content :deep(p) {
  margin: 0;
}

.reasoning-text {
  font-size: 14px;
  color: #666;
}

.reasoning-divider {
  display: flex;
  align-items: center;
  gap: 12px;
  margin: 16px 0;
  padding: 8px 0;
}

.divider-line {
  flex: 1;
  height: 1px;
  background: linear-gradient(to right, transparent, #d0d0d0, transparent);
}

.divider-text {
  font-size: 13px;
  color: #888;
  white-space: nowrap;
}

.final-content {
  font-size: 15px;
  line-height: 1.7;
}

.final-content :deep(p:first-child) {
  margin-top: 0;
}

/* 深色模式适配 */
@media (prefers-color-scheme: dark) {
  .reasoning-section {
    background: #2a2a2a;
    border-color: #444;
  }
  
  .reasoning-header {
    background: #333;
  }
  
  .reasoning-header:hover {
    background: #3a3a3a;
  }
  
  .reasoning-step {
    background: #1a1a1a;
  }
  
  .divider-line {
    background: linear-gradient(to right, transparent, #555, transparent);
  }
}
</style>
