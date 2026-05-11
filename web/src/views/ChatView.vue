<template>
  <div class="chat-view">
    <n-card class="chat-card">
      <template #header>
        <div class="chat-header">
          <span>AI Chat</span>
          <n-button size="small" @click="clearChat">Clear</n-button>
        </div>
      </template>

      <div class="messages" ref="messagesEl">
        <div v-for="(msg, i) in messages" :key="i" :class="['message', msg.role]">
          <div class="message-avatar">{{ msg.role === 'user' ? '👤' : '🤖' }}</div>
          <div class="message-content">
            <div class="message-text" v-html="renderMarkdown(msg.content)"></div>
          </div>
        </div>
        <div v-if="streaming" class="message assistant streaming">
          <div class="message-avatar">🤖</div>
          <div class="message-content">
            <div class="message-text">{{ streamContent }}</div>
          </div>
        </div>
      </div>

      <div class="chat-input">
        <n-input
          v-model:value="inputText"
          type="textarea"
          placeholder="Type your message... (Shift+Enter for new line)"
          :autosize="{ minRows: 1, maxRows: 4 }"
          @keydown.enter.exact.prevent="sendMessage"
        />
        <n-button type="primary" @click="sendMessage" :loading="streaming">
          Send
        </n-button>
      </div>
    </n-card>
  </div>
</template>

<script setup>
import { ref, nextTick } from 'vue'
import { marked } from 'marked'
import hljs from 'highlight.js'

marked.setOptions({
  highlight: (code, lang) => {
    if (lang && hljs.getLanguage(lang)) {
      return hljs.highlight(code, { language: lang }).value
    }
    return code
  }
})

const messages = ref([
  { role: 'assistant', content: 'Hello! I am go-magic, your AI assistant. How can I help you today?' }
])
const inputText = ref('')
const streaming = ref(false)
const streamContent = ref('')
const messagesEl = ref(null)

const renderMarkdown = (text) => {
  return marked.parse(text)
}

const sendMessage = async () => {
  if (!inputText.value.trim() || streaming.value) return

  const userMsg = { role: 'user', content: inputText.value }
  messages.value.push(userMsg)
  inputText.value = ''
  streaming.value = true
  streamContent.value = ''

  // Simulate streaming response
  const response = 'This is a simulated response. In production, this would connect to the go-magic backend API.'
  for (const char of response) {
    streamContent.value += char
    await new Promise(r => setTimeout(r, 20))
  }

  messages.value.push({ role: 'assistant', content: streamContent.value })
  streaming.value = false
  streamContent.value = ''

  await nextTick()
  messagesEl.value?.scrollTo({ top: messagesEl.value.scrollHeight, behavior: 'smooth' })
}

const clearChat = () => {
  messages.value = [{ role: 'assistant', content: 'Chat cleared. How can I help you?' }]
}
</script>

<style scoped>
.chat-card {
  height: 100%;
  display: flex;
  flex-direction: column;
}

.chat-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.messages {
  flex: 1;
  overflow-y: auto;
  padding: 16px 0;
}

.message {
  display: flex;
  gap: 12px;
  margin-bottom: 16px;
}

.message.user {
  flex-direction: row-reverse;
}

.message-avatar {
  font-size: 24px;
}

.message-content {
  max-width: 70%;
  padding: 12px 16px;
  border-radius: 12px;
  background: #2a2a3e;
}

.message.user .message-content {
  background: #4f46e5;
}

.message-text {
  line-height: 1.6;
}

.streaming .message-text::after {
  content: '|';
  animation: blink 1s step-end infinite;
}

@keyframes blink {
  50% { opacity: 0; }
}

.chat-input {
  display: flex;
  gap: 12px;
  padding-top: 16px;
  border-top: 1px solid #333;
}

.chat-input .n-input {
  flex: 1;
}
</style>
