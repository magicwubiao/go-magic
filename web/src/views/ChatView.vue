<template>
  <div class="chat-container" :class="{ 'dark-theme': isDark }">
    <!-- Sidebar -->
    <aside class="sidebar">
      <div class="sidebar-header">
        <h2>💬 Chats</h2>
        <button class="btn-new-chat" @click="newChat" title="New Chat">➕</button>
      </div>

      <div class="search-box">
        <input v-model="searchQuery" placeholder="Search chats..." class="search-input" />
      </div>

      <div class="chat-list">
        <div
          v-for="(chat, index) in filteredChats"
          :key="index"
          class="chat-item"
          :class="{ active: currentChatIndex === index }"
          @click="selectChat(index)"
        >
          <div class="chat-item-content">
            <span class="chat-title">{{ chat.title || 'New Chat' }}</span>
            <span class="chat-time">{{ chat.time }}</span>
          </div>
          <button class="btn-delete-chat" @click.stop="deleteChat(index)" title="Delete">🗑️</button>
        </div>
      </div>

      <div class="sidebar-footer">
        <div class="session-info">
          <span>Model: {{ selectedModel }}</span>
        </div>
        <div class="theme-toggle">
          <button @click="toggleTheme" :title="isDark ? 'Light Mode' : 'Dark Mode'">
            {{ isDark ? '☀️' : '🌙' }}
          </button>
        </div>
      </div>
    </aside>

    <!-- Main Chat Area -->
    <main class="chat-main">
      <!-- Messages -->
      <div ref="messagesEl" class="messages-container">
        <!-- Welcome message when empty -->
        <div v-if="messages.length === 0" class="welcome-message">
          <h2>✨ Welcome to go-magic</h2>
          <p>Your AI assistant is ready. How can I help you today?</p>
          <div class="quick-prompts">
            <button @click="usePrompt('Help me write a Go program that...')">
              Help me write a Go program
            </button>
            <button @click="usePrompt('Explain what this code does:')">
              Explain code
            </button>
            <button @click="usePrompt('Help me debug this error:')">
              Debug an error
            </button>
          </div>
        </div>

        <!-- Messages -->
        <div
          v-for="(msg, index) in messages"
          :key="index"
          class="message"
          :class="msg.role"
        >
          <div class="message-avatar">
            {{ msg.role === 'user' ? '👤' : '🤖' }}
          </div>
          <div class="message-content">
            <div class="message-header">
              <span class="sender-name">{{ msg.role === 'user' ? 'You' : 'Assistant' }}</span>
              <span v-if="msg.model" class="model-badge">{{ msg.model }}</span>
              <span class="message-time">{{ msg.time }}</span>
            </div>
            <div class="message-body" v-if="msg.role === 'assistant' && !msg.error">
              <MarkdownRenderer :content="msg.content" :is-dark="isDark" />
            </div>
            <div class="message-body error" v-else-if="msg.error">
              <p>❌ {{ msg.content }}</p>
            </div>
            <div class="message-body" v-else>
              <p>{{ msg.content }}</p>
            </div>
            <div class="message-actions">
              <button @click="copyMessage(msg.content)" title="Copy">📋</button>
              <button @click="regenerateMessage(index)" v-if="msg.role === 'assistant'" title="Regenerate">🔄</button>
            </div>
          </div>
        </div>

        <!-- Streaming indicator -->
        <div v-if="streaming" class="message assistant">
          <div class="message-avatar">🤖</div>
          <div class="message-content">
            <div class="message-header">
              <span class="sender-name">Assistant</span>
              <span class="model-badge">{{ selectedModel }}</span>
              <span class="typing-indicator">
                <span></span><span></span><span></span>
              </span>
            </div>
            <div class="message-body streaming">
              <MarkdownRenderer :content="streamContent + '▊'" :is-dark="isDark" />
            </div>
          </div>
        </div>
      </div>

      <!-- Input Area -->
      <div class="input-area">
        <div class="input-toolbar">
          <select v-model="selectedModel" class="model-select">
            <option value="gpt-4">GPT-4</option>
            <option value="gpt-3.5-turbo">GPT-3.5 Turbo</option>
            <option value="claude-3">Claude-3</option>
            <option value="deepseek-chat">DeepSeek Chat</option>
          </select>
          <div class="input-stats">
            <span v-if="inputText.length > 0">{{ inputText.length }} chars</span>
          </div>
        </div>
        <div class="input-wrapper">
          <textarea
            ref="inputEl"
            v-model="inputText"
            @keydown.enter.exact.prevent="sendMessage"
            @keydown.enter.shift.exact="addNewLine"
            placeholder="Type your message... (Enter to send, Shift+Enter for new line)"
            class="message-input"
            rows="3"
          ></textarea>
          <button
            class="btn-send"
            @click="sendMessage"
            :disabled="!inputText.trim() || streaming"
          >
            {{ streaming ? '⏳' : '➤' }}
          </button>
        </div>
        <div class="input-footer">
          <button @click="clearChat" class="btn-clear">🗑️ Clear</button>
          <span class="connection-status" :class="{ connected }">
            {{ connected ? '🟢 Connected' : '🔴 Disconnected' }}
          </span>
        </div>
      </div>
    </main>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, nextTick, watch } from 'vue'
import MarkdownRenderer from '../components/MarkdownRenderer.vue'

const isDark = ref(false)
const messages = ref<any[]>([])
const inputText = ref('')
const inputEl = ref<HTMLTextAreaElement>()
const messagesEl = ref<HTMLDivElement>()
const streaming = ref(false)
const streamContent = ref('')
const connected = ref(false)
const selectedModel = ref('gpt-4')
const searchQuery = ref('')
const currentChatIndex = ref(0)
const chats = ref<any[]>([
  {
    title: 'Welcome Chat',
    time: 'Just now',
    messages: []
  }
])

let ws: WebSocket | null = null
let reconnectTimer: any = null

const filteredChats = computed(() => {
  if (!searchQuery.value) return chats.value
  return chats.value.filter(c =>
    c.title.toLowerCase().includes(searchQuery.value.toLowerCase())
  )
})

const formatTime = () => {
  const now = new Date()
  return now.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit' })
}

const connectWebSocket = () => {
  try {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    ws = new WebSocket(`${protocol}//${window.location.host}/ws`)

    ws.onopen = () => {
      connected.value = true
      console.log('WebSocket connected')
    }

    ws.onmessage = (event) => {
      const data = JSON.parse(event.data)

      if (data.type === 'chunk') {
        streamContent.value += data.content
        scrollToBottom()
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
        //message.error(data.content)
        streaming.value = false
        streamContent.value = ''
      }
    }

    ws.onclose = () => {
      connected.value = false
      reconnectTimer = setTimeout(connectWebSocket, 3000)
    }
  } catch (err) {
    console.error('WebSocket error:', err)
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

  // Update chat title if first message
  if (messages.value.length === 1) {
    chats.value[currentChatIndex.value].title = inputText.value.slice(0, 30) + (inputText.value.length > 30 ? '...' : '')
  }

  const question = inputText.value
  inputText.value = ''
  streaming.value = true
  streamContent.value = ''
  await nextTick()
  scrollToBottom()

  try {
    // Try HTTP first (more reliable)
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
  } catch (err) {
    console.error('Send message error:', err)
    messages.value.push({
      role: 'assistant',
      content: `❌ Error: ${err.message}. Please make sure the server is running.`,
      time: formatTime(),
      error: true
    })
  }

  streaming.value = false
  await nextTick()
  scrollToBottom()
}

const copyMessage = async (content: string) => {
  try {
    await navigator.clipboard.writeText(content)
    //message.success('Copied to clipboard!')
  } catch {
    //message.error('Failed to copy')
  }
}

const regenerateMessage = async (index: number) => {
  if (index > 0 && messages.value[index - 1].role === 'user') {
    const question = messages.value[index - 1].content
    messages.value.splice(index)
    inputText.value = question
    await sendMessage()
  }
}

const clearChat = () => {
  messages.value = []
  chats.value[currentChatIndex.value].messages = []
  //message.success('Chat cleared')
}

const newChat = () => {
  chats.value.unshift({
    title: 'New Chat',
    time: 'Just now',
    messages: []
  })
  currentChatIndex.value = 0
  messages.value = []
  inputText.value = ''
}

const selectChat = (index: number) => {
  // Save current chat
  chats.value[currentChatIndex.value].messages = [...messages.value]
  currentChatIndex.value = index
  messages.value = [...chats.value[index].messages]
}

const deleteChat = (index: number) => {
  chats.value.splice(index, 1)
  if (currentChatIndex.value >= chats.value.length) {
    currentChatIndex.value = Math.max(0, chats.value.length - 1)
  }
  if (chats.value.length === 0) {
    newChat()
  }
}

const toggleTheme = () => {
  isDark.value = !isDark.value
  localStorage.setItem('theme', isDark.value ? 'dark' : 'light')
}

const scrollToBottom = () => {
  if (messagesEl.value) {
    messagesEl.value.scrollTo({
      top: messagesEl.value.scrollHeight,
      behavior: 'smooth'
    })
  }
}

const addNewLine = () => {
  inputText.value += '\n'
}

const usePrompt = (prompt: string) => {
  inputText.value = prompt
  inputEl.value?.focus()
}

watch(isDark, (val) => {
  document.documentElement.setAttribute('data-theme', val ? 'dark' : 'light')
})

onMounted(() => {
  // Load theme
  const savedTheme = localStorage.getItem('theme')
  if (savedTheme) {
    isDark.value = savedTheme === 'dark'
  } else {
    isDark.value = window.matchMedia('(prefers-color-scheme: dark)').matches
  }

  // Try to connect WebSocket
  connectWebSocket()
})

onUnmounted(() => {
  if (reconnectTimer) clearTimeout(reconnectTimer)
  if (ws) ws.close()
})
</script>

<style scoped>
.chat-container {
  height: 100vh;
  display: flex;
  background: #f5f7fa;
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
}

.chat-container.dark-theme {
  background: #1a1a2e;
  color: #e0e0e0;
}

/* Sidebar */
.sidebar {
  width: 280px;
  background: #ffffff;
  border-right: 1px solid #e0e0e0;
  display: flex;
  flex-direction: column;
  box-shadow: 2px 0 8px rgba(0,0,0,0.05);
}

.dark-theme .sidebar {
  background: #16213e;
  border-color: #2d3748;
}

.sidebar-header {
  padding: 20px;
  border-bottom: 1px solid #e0e0e0;
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.dark-theme .sidebar-header {
  border-color: #2d3748;
}

.sidebar-header h2 {
  margin: 0;
  font-size: 18px;
  font-weight: 600;
}

.btn-new-chat {
  background: #4f46e5;
  color: white;
  border: none;
  border-radius: 8px;
  width: 32px;
  height: 32px;
  cursor: pointer;
  font-size: 16px;
  transition: all 0.2s;
}

.btn-new-chat:hover {
  background: #4338ca;
  transform: scale(1.05);
}

.search-box {
  padding: 12px 16px;
}

.search-input {
  width: 100%;
  padding: 10px 12px;
  border: 1px solid #e0e0e0;
  border-radius: 8px;
  font-size: 14px;
  background: #f9fafb;
  color: #333;
}

.dark-theme .search-input {
  background: #1f2937;
  border-color: #374151;
  color: #e0e0e0;
}

.chat-list {
  flex: 1;
  overflow-y: auto;
  padding: 8px;
}

.chat-item {
  padding: 12px;
  border-radius: 8px;
  cursor: pointer;
  margin-bottom: 4px;
  display: flex;
  justify-content: space-between;
  align-items: center;
  transition: background 0.2s;
}

.chat-item:hover {
  background: #f3f4f6;
}

.dark-theme .chat-item:hover {
  background: #1f2937;
}

.chat-item.active {
  background: #eef2ff;
}

.dark-theme .chat-item.active {
  background: #3730a3;
}

.chat-item-content {
  flex: 1;
  min-width: 0;
}

.chat-title {
  display: block;
  font-size: 14px;
  font-weight: 500;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.chat-time {
  font-size: 12px;
  color: #6b7280;
}

.btn-delete-chat {
  background: none;
  border: none;
  cursor: pointer;
  opacity: 0;
  transition: opacity 0.2s;
  font-size: 12px;
}

.chat-item:hover .btn-delete-chat {
  opacity: 1;
}

.sidebar-footer {
  padding: 16px;
  border-top: 1px solid #e0e0e0;
}

.dark-theme .sidebar-footer {
  border-color: #2d3748;
}

.session-info {
  font-size: 12px;
  color: #6b7280;
  margin-bottom: 8px;
}

.theme-toggle button {
  background: none;
  border: none;
  font-size: 20px;
  cursor: pointer;
  padding: 8px;
}

/* Main Chat Area */
.chat-main {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-width: 0;
}

.messages-container {
  flex: 1;
  overflow-y: auto;
  padding: 20px;
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.welcome-message {
  text-align: center;
  padding: 40px 20px;
  color: #6b7280;
}

.welcome-message h2 {
  font-size: 28px;
  margin-bottom: 12px;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
}

.quick-prompts {
  display: flex;
  gap: 10px;
  justify-content: center;
  margin-top: 24px;
  flex-wrap: wrap;
}

.quick-prompts button {
  padding: 10px 16px;
  border: 1px solid #e0e0e0;
  border-radius: 20px;
  background: white;
  cursor: pointer;
  font-size: 14px;
  transition: all 0.2s;
}

.dark-theme .quick-prompts button {
  background: #1f2937;
  border-color: #374151;
  color: #e0e0e0;
}

.quick-prompts button:hover {
  border-color: #667eea;
  color: #667eea;
}

/* Messages */
.message {
  display: flex;
  gap: 12px;
  max-width: 85%;
}

.message.user {
  margin-left: auto;
  flex-direction: row-reverse;
}

.message-avatar {
  width: 36px;
  height: 36px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 18px;
  flex-shrink: 0;
  background: #e5e7eb;
}

.dark-theme .message-avatar {
  background: #374151;
}

.message.user .message-avatar {
  background: #dbeafe;
}

.message-content {
  flex: 1;
  min-width: 0;
}

.message-header {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 6px;
  font-size: 13px;
}

.sender-name {
  font-weight: 600;
  color: #374151;
}

.dark-theme .sender-name {
  color: #e0e0e0;
}

.model-badge {
  padding: 2px 8px;
  border-radius: 10px;
  background: #e0e7ff;
  color: #4338ca;
  font-size: 11px;
}

.dark-theme .model-badge {
  background: #3730a3;
  color: #a5b4fc;
}

.message-time {
  color: #9ca3af;
  font-size: 12px;
}

.message-body {
  padding: 14px 18px;
  border-radius: 16px;
  background: white;
  box-shadow: 0 1px 3px rgba(0,0,0,0.08);
  line-height: 1.6;
}

.dark-theme .message-body {
  background: #1f2937;
  box-shadow: 0 1px 3px rgba(0,0,0,0.3);
}

.message.user .message-body {
  background: #4f46e5;
  color: white;
}

.dark-theme .message.user .message-body {
  background: #5b21b6;
}

.message-body.error {
  background: #fef2f2;
  border: 1px solid #fecaca;
  color: #dc2626;
}

.dark-theme .message-body.error {
  background: #450a0a;
  border-color: #7f1d1d;
}

.message-body.streaming {
  background: #f0f9ff;
}

.dark-theme .message-body.streaming {
  background: #1e3a5f;
}

.message-actions {
  display: flex;
  gap: 8px;
  margin-top: 8px;
  opacity: 0;
  transition: opacity 0.2s;
}

.message:hover .message-actions {
  opacity: 1;
}

.message-actions button {
  background: none;
  border: none;
  cursor: pointer;
  padding: 4px 8px;
  border-radius: 4px;
  font-size: 14px;
  transition: background 0.2s;
}

.message-actions button:hover {
  background: #f3f4f6;
}

.dark-theme .message-actions button:hover {
  background: #374151;
}

/* Typing Indicator */
.typing-indicator {
  display: flex;
  gap: 4px;
  padding: 0 8px;
}

.typing-indicator span {
  width: 6px;
  height: 6px;
  background: #9ca3af;
  border-radius: 50%;
  animation: bounce 1.4s infinite ease-in-out;
}

.typing-indicator span:nth-child(1) { animation-delay: 0s; }
.typing-indicator span:nth-child(2) { animation-delay: 0.2s; }
.typing-indicator span:nth-child(3) { animation-delay: 0.4s; }

@keyframes bounce {
  0%, 80%, 100% { transform: scale(0.8); opacity: 0.5; }
  40% { transform: scale(1.2); opacity: 1; }
}

/* Input Area */
.input-area {
  padding: 16px 20px;
  background: white;
  border-top: 1px solid #e0e0e0;
}

.dark-theme .input-area {
  background: #16213e;
  border-color: #2d3748;
}

.input-toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 10px;
}

.model-select {
  padding: 6px 12px;
  border: 1px solid #e0e0e0;
  border-radius: 8px;
  font-size: 13px;
  background: #f9fafb;
  cursor: pointer;
}

.dark-theme .model-select {
  background: #1f2937;
  border-color: #374151;
  color: #e0e0e0;
}

.input-stats {
  font-size: 12px;
  color: #6b7280;
}

.input-wrapper {
  display: flex;
  gap: 10px;
  align-items: flex-end;
}

.message-input {
  flex: 1;
  padding: 12px 16px;
  border: 2px solid #e0e0e0;
  border-radius: 12px;
  font-size: 14px;
  resize: none;
  font-family: inherit;
  transition: border-color 0.2s;
}

.dark-theme .message-input {
  background: #1f2937;
  border-color: #374151;
  color: #e0e0e0;
}

.message-input:focus {
  outline: none;
  border-color: #667eea;
}

.btn-send {
  width: 48px;
  height: 48px;
  border-radius: 12px;
  border: none;
  background: #4f46e5;
  color: white;
  font-size: 20px;
  cursor: pointer;
  transition: all 0.2s;
}

.btn-send:hover:not(:disabled) {
  background: #4338ca;
  transform: scale(1.05);
}

.btn-send:disabled {
  background: #d1d5db;
  cursor: not-allowed;
}

.input-footer {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-top: 10px;
}

.btn-clear {
  background: none;
  border: none;
  color: #6b7280;
  cursor: pointer;
  font-size: 13px;
  padding: 6px 12px;
  border-radius: 6px;
  transition: all 0.2s;
}

.btn-clear:hover {
  background: #f3f4f6;
  color: #dc2626;
}

.dark-theme .btn-clear:hover {
  background: #374151;
}

.connection-status {
  font-size: 12px;
  color: #9ca3af;
}

/* Responsive */
@media (max-width: 768px) {
  .sidebar {
    position: fixed;
    left: -280px;
    height: 100vh;
    z-index: 100;
    transition: left 0.3s;
  }

  .sidebar.open {
    left: 0;
  }

  .message {
    max-width: 95%;
  }
}
</style>
