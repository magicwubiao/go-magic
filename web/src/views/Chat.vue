<template>
  <div class="chat-view">
    <!-- Chat Messages -->
    <div class="messages" ref="messagesRef">
      <div
        v-for="message in messages"
        :key="message.id"
        :class="['message', `message-${message.role}`]"
      >
        <div class="message-avatar">
          <n-icon
            :component="message.role === 'user' ? Person : Construct"
            size="24"
          />
        </div>
        <div class="message-content">
          <div class="message-header">
            <span class="message-role">{{ message.role }}</span>
            <span class="message-time">{{ formatTime(message.timestamp) }}</span>
          </div>
          <div class="message-text" v-html="renderMarkdown(message.content)" />
          <div v-if="message.tool_calls?.length" class="tool-calls">
            <div
              v-for="call in message.tool_calls"
              :key="call.id"
              class="tool-call"
              @click="toggleToolResult(call.id)"
            >
              <n-icon :component="Construct" size="16" />
              <span>{{ call.name }}</span>
            </div>
          </div>
          <div
            v-if="expandedTools.has(message.id)"
            class="tool-results"
          >
            <div
              v-for="result in message.tool_results"
              :key="result.tool_call_id"
              class="tool-result"
            >
              <pre>{{ result.content }}</pre>
            </div>
          </div>
        </div>
      </div>

      <div v-if="isLoading" class="message message-assistant">
        <div class="message-avatar">
          <n-icon :component="Construct" size="24" />
        </div>
        <div class="message-content">
          <n-spin size="small" />
        </div>
      </div>
    </div>

    <!-- Input Area -->
    <div class="input-area">
      <n-input
        v-model:value="inputMessage"
        type="textarea"
        :placeholder="$t('chat.placeholder')"
        :autosize="{ minRows: 1, maxRows: 4 }"
        @keydown.enter.exact.prevent="sendMessage"
      />
      <div class="input-actions">
        <n-button-group>
          <n-button size="small" @click="clearChat">
            <template #icon>
              <n-icon :component="Trash" />
            </template>
          </n-button>
          <n-button size="small" @click="showAttach = !showAttach">
            <template #icon>
              <n-icon :component="Attach" />
            </template>
          </n-button>
        </n-button-group>
        <n-button type="primary" @click="sendMessage" :loading="isLoading">
          {{ $t('chat.send') }}
        </n-button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, nextTick, onMounted } from 'vue'
import { marked } from 'marked'
import hljs from 'highlight.js'
import { NIcon } from 'naive-ui'
import { Person, Boat, Construct, Trash, Attach } from '@vicons/ionicons5'
import type { Message } from '@/types'

const messagesRef = ref<HTMLElement>()
const inputMessage = ref('')
const messages = ref<Message[]>([])
const isLoading = ref(false)
const expandedTools = ref(new Set<string>())
const showAttach = ref(false)

// Configure marked with highlight.js
marked.use({
  renderer: {
    code({ text, lang }: { text: string; lang?: string }) {
      const validLanguage = lang && hljs.getLanguage(lang)
      const highlighted = validLanguage
        ? hljs.highlight(text, { language: lang }).value
        : hljs.highlightAuto(text).value
      return `<pre><code class="hljs ${lang || ''}">${highlighted}</code></pre>`
    }
  }
})

function renderMarkdown(content: string): string {
  return marked.parse(content) as string
}

function formatTime(timestamp: string): string {
  return new Date(timestamp).toLocaleTimeString()
}

function toggleToolResult(id: string) {
  if (expandedTools.value.has(id)) {
    expandedTools.value.delete(id)
  } else {
    expandedTools.value.add(id)
  }
}

async function sendMessage() {
  if (!inputMessage.value.trim() || isLoading.value) return

  const userMessage: Message = {
    id: crypto.randomUUID(),
    role: 'user',
    content: inputMessage.value,
    timestamp: new Date().toISOString(),
  }

  messages.value.push(userMessage)
  inputMessage.value = ''
  isLoading.value = true

  await nextTick()
  scrollToBottom()

  try {
    const response = await fetch('/api/chat', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ message: userMessage.content }),
    })

    const data = await response.json()

    const assistantMessage: Message = {
      id: crypto.randomUUID(),
      role: 'assistant',
      content: data.content || 'No response',
      timestamp: new Date().toISOString(),
      tool_calls: data.tool_calls,
      tool_results: data.tool_results,
    }

    messages.value.push(assistantMessage)
  } catch (e) {
    console.error('Failed to send message:', e)
  } finally {
    isLoading.value = false
    await nextTick()
    scrollToBottom()
  }
}

function scrollToBottom() {
  if (messagesRef.value) {
    messagesRef.value.scrollTop = messagesRef.value.scrollHeight
  }
}

function clearChat() {
  messages.value = []
}

onMounted(() => {
  // Load previous messages if session exists
})
</script>

<style lang="scss" scoped>
.chat-view {
  display: flex;
  flex-direction: column;
  height: 100%;
  background: var(--bg-color);
}

.messages {
  flex: 1;
  overflow-y: auto;
  padding: 16px;
}

.message {
  display: flex;
  gap: 12px;
  margin-bottom: 16px;
}

.message-user {
  flex-direction: row-reverse;
}

.message-avatar {
  width: 36px;
  height: 36px;
  border-radius: 50%;
  background: var(--primary-color);
  color: white;
  display: flex;
  align-items: center;
  justify-content: center;
}

.message-content {
  max-width: 70%;
}

.message-header {
  display: flex;
  gap: 8px;
  margin-bottom: 4px;
  font-size: 12px;
}

.message-role {
  font-weight: 600;
  text-transform: capitalize;
}

.message-time {
  color: var(--text-color-3);
}

.message-text {
  padding: 12px 16px;
  border-radius: 12px;
  background: var(--card-color);
  white-space: pre-wrap;
}

.message-user .message-text {
  background: var(--primary-color);
  color: white;
}

.tool-calls {
  margin-top: 8px;
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.tool-call {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 4px 8px;
  border-radius: 4px;
  background: var(--info-color);
  color: white;
  font-size: 12px;
  cursor: pointer;
}

.tool-results {
  margin-top: 8px;
}

.tool-result pre {
  padding: 8px;
  border-radius: 4px;
  background: var(--bg-color);
  font-size: 12px;
  overflow-x: auto;
}

.input-area {
  padding: 16px;
  border-top: 1px solid var(--border-color);
  background: var(--card-color);
}

.input-actions {
  display: flex;
  justify-content: space-between;
  margin-top: 8px;
}
</style>
