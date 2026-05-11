<template>
  <div class="chat-view">
    <n-card class="chat-card">
      <template #header>
        <div class="chat-header">
          <div class="header-left">
            <span class="chat-title">AI Chat</span>
            <n-tag v-if="connected" type="success" size="small">Connected</n-tag>
            <n-tag v-else type="warning" size="small">Disconnected</n-tag>
          </div>
          <div class="header-right">
            <n-button size="small" @click="clearChat">Clear</n-button>
            <n-button size="small" @click="toggleTheme">
              {{ isDark ? '☀️' : '🌙' }}
            </n-button>
          </div>
        </div>
      </template>

      <div class="messages" ref="messagesEl">
        <div v-for="(msg, i) in messages" :key="i" :class="['message', msg.role]">
          <div class="message-avatar">{{ msg.role === 'user' ? '👤' : '🤖' }}</div>
          <div class="message-content">
            <div class="message-meta">
              <span class="message-time">{{ msg.time }}</span>
              <span v-if="msg.role === 'assistant' && msg.model" class="message-model">{{ msg.model }}</span>
            </div>
            <div class="message-text" v-html="renderMarkdown(msg.content)"></div>
            <div v-if="msg.error" class="message-error">{{ msg.error }}</div>
          </div>
        </div>
        
        <div v-if="streaming" class="message assistant streaming">
          <div class="message-avatar">🤖</div>
          <div class="message-content">
            <div class="message-text" v-html="renderMarkdown(streamContent)"></div>
            <span class="typing-indicator">
              <span></span><span></span><span></span>
            </span>
          </div>
        </div>
      </div>

      <div class="chat-input">
        <n-input
          v-model:value="inputText"
          type="textarea"
          placeholder="Type your message... (Shift+Enter for new line, Enter to send)"
          :autosize="{ minRows: 1, maxRows: 6 }"
          @keydown.enter.exact.prevent="sendMessage"
          :disabled="streaming"
        />
        <div class="input-actions">
          <n-select
            v-model:value="selectedModel"
            :options="modelOptions"
            size="small"
            style="width: 200px;"
            placeholder="Select model"
          />
          <n-button type="primary" @click="sendMessage" :loading="streaming" :disabled="!inputText.trim()">
            <template #icon>
              <span>➤</span>
            </template>
            Send
          </n-button>
        </div>
      </div>
    </n-card>
  </div>
</template>

<script setup>
import { ref, nextTick, onMounted, onUnmounted, computed } from 'vue'
import { marked } from 'marked'
import hljs from 'highlight.js'
import { useMessage } from 'naive-ui'

const message = useMessage()

marked.setOptions({
  highlight: (code, lang) => {
    if (lang && hljs.getLanguage(lang)) {
      return hljs.highlight(code, { language: lang }).value
    }
    return code
  },
  breaks: true,
  gfm: true
})

const messages = ref([])
const inputText = ref('')
const streaming = ref(false)
const streamContent = ref('')
const messagesEl = ref(null)
const connected = ref(false)
const selectedModel = ref('default')
const isDark = ref(true)

const modelOptions = [
  { label: 'Default Model', value: 'default' },
  { label: 'GPT-4', value: 'gpt-4' },
  { label: 'GPT-3.5', value: 'gpt-3.5-turbo' },
  { label: 'Claude 3', value: 'claude-3' },
  { label: 'DeepSeek', value: 'deepseek-chat' },
]

let ws = null
let reconnectTimer = null

const formatTime = () => {
  const now = new Date()
  return now.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit' })
}

const renderMarkdown = (text) => {
  if (!text) return ''
  let html = marked.parse(text)
  // Add copy buttons to code blocks
  html = html.replace(/<pre><code class="language-(\w+)">/g, 
    '<pre class="code-block"><button class="copy-btn" onclick="copyCode(this)">📋 Copy</button><code class="language-$1">')
  return html
}

const connectWebSocket = () => {
  try {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const wsUrl = `${protocol}//${window.location.host}/ws`
    ws = new WebSocket(wsUrl)
    
    ws.onopen = () => {
      connected.value = true
      console.log('WebSocket connected')
    }
    
    ws.onmessage = (event) => {
      const data = JSON.parse(event.data)
      if (data.type === 'chunk') {
        streamContent.value += data.content
      } else if (data.type === 'done') {
        messages.value.push({
          role: 'assistant',
          content: streamContent.value,
          time: formatTime(),
          model: selectedModel.value
        })
        streaming.value = false
        streamContent.value = ''
        scrollToBottom()
      } else if (data.type === 'error') {
        message.error(data.content)
        streaming.value = false
        streamContent.value = ''
      }
    }
    
    ws.onclose = () => {
      connected.value = false
      console.log('WebSocket disconnected')
      // Auto reconnect after 3 seconds
      reconnectTimer = setTimeout(connectWebSocket, 3000)
    }
    
    ws.onerror = (error) => {
      console.error('WebSocket error:', error)
      connected.value = false
    }
  } catch (err) {
    console.error('Failed to connect WebSocket:', err)
    connected.value = false
  }
}

const sendMessage = async () => {
  if (!inputText.value.trim() || streaming.value) return

  const userMsg = { 
    role: 'user', 
    content: inputText.value,
    time: formatTime()
  }
  messages.value.push(userMsg)
  const question = inputText.value
  inputText.value = ''
  streaming.value = true
  streamContent.value = ''

  try {
    // Try WebSocket first
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({
        type: 'chat',
        content: question,
        model: selectedModel.value
      }))
    } else {
      // Fallback to HTTP
      const response = await fetch('/api/chat', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          message: question,
          model: selectedModel.value
        })
      })
      
      if (!response.ok) throw new Error(`HTTP ${response.status}`)
      
      const reader = response.body.getReader()
      const decoder = new TextDecoder()
      
      while (true) {
        const { done, value } = await reader.read()
        if (done) break
        streamContent.value += decoder.decode(value, { stream: true })
        scrollToBottom()
      }
      
      messages.value.push({
        role: 'assistant',
        content: streamContent.value,
        time: formatTime(),
        model: selectedModel.value
      })
      streamContent.value = ''
    }
  } catch (err) {
    console.error('Send message error:', err)
    messages.value.push({
      role: 'assistant',
      content: `Sorry, I encountered an error: ${err.message}. Please make sure the server is running with 'magic server' command.`,
      time: formatTime(),
      error: true
    })
  }
  
  streaming.value = false
  await nextTick()
  scrollToBottom()
}

const clearChat = () => {
  messages.value = []
  message.success('Chat cleared')
}

const toggleTheme = () => {
  isDark.value = !isDark.value
}

const scrollToBottom = () => {
  if (messagesEl.value) {
    messagesEl.value.scrollTo({ 
      top: messagesEl.value.scrollHeight, 
      behavior: 'smooth' 
    })
  }
}

// Copy code function for code blocks
if (typeof window !== 'undefined') {
  window.copyCode = async (btn) => {
    const codeBlock = btn.nextElementSibling
    const code = codeBlock.textContent
    try {
      await navigator.clipboard.writeText(code)
      btn.textContent = '✅ Copied!'
      setTimeout(() => btn.textContent = '📋 Copy', 2000)
    } catch (err) {
      btn.textContent = '❌ Failed'
    }
  }
}

onMounted(() => {
  // Add welcome message
  messages.value.push({
    role: 'assistant',
    content: '# Welcome to go-magic Chat!\n\nI am your AI assistant. How can I help you today?\n\n**Tips:**\n- Type your message and press Enter to send\n- Use Shift+Enter for new line\n- Click the 🌙 button to toggle theme\n- Select a different model from the dropdown',
    time: formatTime()
  })
  
  // Try to connect WebSocket
  // connectWebSocket()
})

onUnmounted(() => {
  if (reconnectTimer) clearTimeout(reconnectTimer)
  if (ws) ws.close()
})
</script>

<style scoped>
.chat-view {
  height: 100%;
  display: flex;
  flex-direction: column;
}

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

.header-left {
  display: flex;
  align-items: center;
  gap: 12px;
}

.header-right {
  display: flex;
  gap: 8px;
}

.chat-title {
  font-weight: 600;
}

.messages {
  flex: 1;
  overflow-y: auto;
  padding: 16px 0;
  scroll-behavior: smooth;
}

.message {
  display: flex;
  gap: 12px;
  margin-bottom: 16px;
  animation: fadeIn 0.3s ease;
}

@keyframes fadeIn {
  from { opacity: 0; transform: translateY(10px); }
  to { opacity: 1; transform: translateY(0); }
}

.message.user {
  flex-direction: row-reverse;
}

.message-avatar {
  font-size: 28px;
  flex-shrink: 0;
}

.message-content {
  max-width: 75%;
  padding: 12px 16px;
  border-radius: 16px;
  background: #2a2a3e;
  position: relative;
}

.message.user .message-content {
  background: linear-gradient(135deg, #4f46e5, #6366f1);
}

.message.assistant .message-content {
  background: #1f1f2e;
  border: 1px solid #333;
}

.message-meta {
  display: flex;
  gap: 8px;
  margin-bottom: 4px;
  font-size: 11px;
  opacity: 0.7;
}

.message-time {
  color: #888;
}

.message-model {
  color: #10b981;
}

.message-text {
  line-height: 1.7;
  word-wrap: break-word;
}

.message-text :deep(pre) {
  background: #1a1a2e;
  border-radius: 8px;
  padding: 16px;
  margin: 12px 0;
  overflow-x: auto;
  position: relative;
}

.message-text :deep(code) {
  font-family: 'Fira Code', 'Consolas', monospace;
  font-size: 13px;
}

.message-text :deep(.copy-btn) {
  position: absolute;
  top: 8px;
  right: 8px;
  background: #333;
  border: none;
  color: #fff;
  padding: 4px 8px;
  border-radius: 4px;
  cursor: pointer;
  font-size: 12px;
  opacity: 0;
  transition: opacity 0.2s;
}

.message-text :deep(pre:hover .copy-btn) {
  opacity: 1;
}

.message-error {
  color: #ef4444;
  margin-top: 8px;
  font-size: 13px;
}

.typing-indicator {
  display: inline-flex;
  gap: 4px;
  margin-left: 8px;
}

.typing-indicator span {
  width: 8px;
  height: 8px;
  background: #888;
  border-radius: 50%;
  animation: typing 1.4s infinite;
}

.typing-indicator span:nth-child(2) { animation-delay: 0.2s; }
.typing-indicator span:nth-child(3) { animation-delay: 0.4s; }

@keyframes typing {
  0%, 60%, 100% { transform: translateY(0); opacity: 0.4; }
  30% { transform: translateY(-4px); opacity: 1; }
}

.chat-input {
  border-top: 1px solid #333;
  padding-top: 16px;
}

.input-actions {
  display: flex;
  gap: 8px;
  margin-top: 8px;
  align-items: center;
}
</style>
