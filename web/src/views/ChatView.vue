<template>
  <div class="chat-view">
    <!-- Chat Header -->
    <div class="chat-header">
      <div class="header-left">
        <button @click="showSidebar = !showSidebar" class="icon-btn" title="Toggle Sidebar">
          ☰
        </button>
        <h2>{{ currentSession?.title || 'New Chat' }}</h2>
        <span v-if="isStreaming" class="streaming-badge">● Streaming</span>
      </div>
      <div class="header-right">
        <select v-model="selectedModel" class="model-select">
          <option value="gpt-4o">GPT-4o</option>
          <option value="gpt-4o-mini">GPT-4o Mini</option>
          <option value="claude-3-5-sonnet">Claude 3.5 Sonnet</option>
          <option value="deepseek-chat">DeepSeek Chat</option>
        </select>
        <button @click="showSettings = true" class="icon-btn" title="Settings">⚙️</button>
        <button @click="toggleTheme" class="icon-btn" title="Toggle Theme">
          {{ isDark ? '☀️' : '🌙' }}
        </button>
      </div>
    </div>

    <!-- Messages Container -->
    <div class="messages-container" ref="messagesContainer">
      <!-- Welcome Message -->
      <div v-if="messages.length === 0" class="welcome-message">
        <div class="welcome-icon">🤖</div>
        <h2>Welcome to Go Magic</h2>
        <p>Your AI assistant is ready. Start a conversation!</p>
        <div class="suggestions">
          <button
            v-for="suggestion in suggestions"
            :key="suggestion"
            @click="sendMessage(suggestion)"
            class="suggestion-btn"
          >
            {{ suggestion }}
          </button>
        </div>
      </div>

      <!-- Messages -->
      <div v-for="(msg, index) in messages" :key="index" class="message" :class="msg.role">
        <div class="message-avatar">
          {{ msg.role === 'user' ? '👤' : '🤖' }}
        </div>
        <div class="message-content">
          <div class="message-header">
            <span class="sender-name">{{ msg.role === 'user' ? 'You' : 'Magic' }}</span>
            <span class="message-time">{{ formatTime(msg.timestamp) }}</span>
            <button @click="copyMessage(msg)" class="copy-btn" title="Copy">📋</button>
          </div>
          <div class="message-body">
            <MarkdownRenderer :content="msg.content" />
          </div>
          <div v-if="msg.role === 'assistant' && msg.toolCalls" class="tool-calls">
            <div v-for="(tool, tIdx) in msg.toolCalls" :key="tIdx" class="tool-call">
              <span class="tool-icon">{{ getToolIcon(tool.name) }}</span>
              <span class="tool-name">{{ tool.name }}</span>
            </div>
          </div>
        </div>
      </div>

      <!-- Typing Indicator -->
      <div v-if="isTyping" class="message assistant">
        <div class="message-avatar">🤖</div>
        <div class="message-content">
          <div class="typing-indicator">
            <span></span>
            <span></span>
            <span></span>
          </div>
        </div>
      </div>
    </div>

    <!-- Input Area -->
    <div class="input-area">
      <div v-if="attachedFiles.length > 0" class="attached-files">
        <div v-for="(file, idx) in attachedFiles" :key="idx" class="attached-file">
          <span>{{ file.name }}</span>
          <button @click="removeFile(idx)">×</button>
        </div>
      </div>
      <div class="input-container">
        <button @click="showFileUpload = !showFileUpload" class="attach-btn" title="Attach File">
          📎
        </button>
        <textarea
          ref="inputRef"
          v-model="inputMessage"
          @keydown.enter.exact.prevent="sendMessage()"
          @keydown.enter.shift.exact="inputMessage += '\n'"
          placeholder="Type your message... (Enter to send, Shift+Enter for new line)"
          class="message-input"
          rows="1"
        ></textarea>
        <button @click="sendMessage()" class="send-btn" :disabled="!inputMessage.trim() && attachedFiles.length === 0">
          ➤
        </button>
      </div>
      <div class="input-hints">
        <span>Press <kbd>/</kbd> for commands</span>
        <span>Press <kbd>Ctrl</kbd>+<kbd>K</kbd> for shortcuts</span>
      </div>
    </div>

    <!-- File Upload Modal -->
    <div v-if="showFileUpload" class="modal-overlay" @click="showFileUpload = false">
      <div class="modal-content" @click.stop>
        <FileUpload @files-selected="handleFilesSelected" />
        <button @click="showFileUpload = false" class="modal-close">Close</button>
      </div>
    </div>

    <!-- Settings Modal -->
    <div v-if="showSettings" class="modal-overlay" @click="showSettings = false">
      <SettingsView @close="showSettings = false" />
    </div>

    <!-- Command Palette -->
    <CommandPalette :show="showCommandPalette" @close="showCommandPalette = false" @command="handleCommand" />

    <!-- Notifications -->
    <div class="notifications">
      <div
        v-for="(notification, idx) in notifications"
        :key="idx"
        class="notification"
        :class="notification.type"
      >
        {{ notification.message }}
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted, onUnmounted, nextTick, watch } from 'vue'
import MarkdownRenderer from '../components/MarkdownRenderer.vue'
import FileUpload from '../components/FileUpload.vue'
import CommandPalette from '../components/CommandPalette.vue'
import SettingsView from '../views/SettingsView.vue'

interface Message {
  role: 'user' | 'assistant'
  content: string
  timestamp: Date
  toolCalls?: Array<{ name: string; arguments: string }>
}

interface Session {
  id: string
  title: string
}

interface Notification {
  message: string
  type: 'success' | 'error' | 'info'
}

const messages = ref<Message[]>([])
const currentSession = ref<Session | null>(null)
const inputMessage = ref('')
const isTyping = ref(false)
const isStreaming = ref(false)
const isDark = ref(true)
const showSidebar = ref(true)
const showSettings = ref(false)
const showFileUpload = ref(false)
const showCommandPalette = ref(false)
const selectedModel = ref('gpt-4o-mini')
const attachedFiles = ref<Array<{ name: string; data: string }>>([])
const notifications = ref<Notification[]>([])
const messagesContainer = ref<HTMLElement | null>(null)
const inputRef = ref<HTMLTextAreaElement | null>(null)

const suggestions = [
  'Help me write a Python script',
  'Explain quantum computing',
  'Debug my code',
  'Write a haiku about programming',
]

onMounted(() => {
  // Load from localStorage
  const saved = localStorage.getItem('go-magic-messages')
  if (saved) {
    messages.value = JSON.parse(saved)
  }
  
  const theme = localStorage.getItem('theme')
  isDark.value = theme !== 'light'
  
  // Keyboard shortcuts
  document.addEventListener('keydown', handleKeydown)
})

onUnmounted(() => {
  document.removeEventListener('keydown', handleKeydown)
})

// Save messages to localStorage
watch(messages, (newMessages) => {
  localStorage.setItem('go-magic-messages', JSON.stringify(newMessages))
}, { deep: true })

function handleKeydown(e: KeyboardEvent) {
  // Ctrl+K for command palette
  if ((e.ctrlKey || e.metaKey) && e.key === 'k') {
    e.preventDefault()
    showCommandPalette.value = true
  }
  // / for focus input
  if (e.key === '/' && document.activeElement?.tagName !== 'TEXTAREA') {
    e.preventDefault()
    inputRef.value?.focus()
  }
  // Ctrl+N for new chat
  if ((e.ctrlKey || e.metaKey) && e.key === 'n') {
    e.preventDefault()
    newChat()
  }
  // Escape to close modals
  if (e.key === 'Escape') {
    showSettings.value = false
    showFileUpload.value = false
    showCommandPalette.value = false
  }
}

async function sendMessage(content?: string) {
  const message = content || inputMessage.value.trim()
  if (!message && attachedFiles.value.length === 0) return
  
  // Add user message
  messages.value.push({
    role: 'user',
    content: message,
    timestamp: new Date(),
  })
  
  inputMessage.value = ''
  isTyping.value = true
  
  // Scroll to bottom
  await nextTick()
  scrollToBottom()
  
  try {
    // Send to backend
    const response = await fetch('/api/chat', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        message,
        model: selectedModel.value,
        files: attachedFiles.value,
      }),
    })
    
    if (!response.ok) throw new Error('Failed to send message')
    
    const data = await response.json()
    
    // Add assistant message
    messages.value.push({
      role: 'assistant',
      content: data.content || data.message || 'I received your message.',
      timestamp: new Date(),
      toolCalls: data.toolCalls,
    })
    
    attachedFiles.value = []
  } catch (error) {
    showNotification('Failed to send message', 'error')
    messages.value.push({
      role: 'assistant',
      content: 'Sorry, I encountered an error. Please try again.',
      timestamp: new Date(),
    })
  } finally {
    isTyping.value = false
    scrollToBottom()
  }
}

function newChat() {
  messages.value = []
  currentSession.value = null
  localStorage.removeItem('go-magic-messages')
  showNotification('New chat started', 'success')
}

function copyMessage(msg: Message) {
  navigator.clipboard.writeText(msg.content)
  showNotification('Message copied!', 'success')
}

function formatTime(date: Date): string {
  return new Date(date).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

function scrollToBottom() {
  if (messagesContainer.value) {
    messagesContainer.value.scrollTop = messagesContainer.value.scrollHeight
  }
}

function toggleTheme() {
  isDark.value = !isDark.value
  localStorage.setItem('theme', isDark.value ? 'dark' : 'light')
  document.documentElement.setAttribute('data-theme', isDark.value ? 'dark' : 'light')
}

function getToolIcon(toolName: string): string {
  const icons: Record<string, string> = {
    web_search: '🌐',
    read_file: '📄',
    write_file: '✏️',
    terminal: '💻',
    execute_code: '⚡',
    memory_store: '💾',
  }
  return icons[toolName] || '🛠️'
}

function handleFilesSelected(files: Array<{ name: string; data: string }>) {
  attachedFiles.value = files
  showFileUpload.value = false
  showNotification(`${files.length} file(s) attached`, 'success')
}

function removeFile(idx: number) {
  attachedFiles.value.splice(idx, 1)
}

function handleCommand(command: string) {
  switch (command) {
    case 'new-chat':
      newChat()
      break
    case 'toggle-theme':
      toggleTheme()
      break
    case 'settings':
      showSettings.value = true
      break
    case 'focus-input':
      inputRef.value?.focus()
      break
  }
}

function showNotification(message: string, type: 'success' | 'error' | 'info') {
  notifications.value.push({ message, type })
  setTimeout(() => {
    notifications.value.shift()
  }, 3000)
}
</script>

<style scoped>
.chat-view {
  display: flex;
  flex-direction: column;
  height: 100vh;
  background: var(--bg-primary);
}

/* Header */
.chat-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 12px 20px;
  background: var(--bg-secondary);
  border-bottom: 1px solid var(--border-color);
}
.header-left, .header-right {
  display: flex;
  align-items: center;
  gap: 12px;
}
.header-left h2 {
  margin: 0;
  font-size: 16px;
  font-weight: 500;
}
.streaming-badge {
  font-size: 12px;
  color: #10b981;
  animation: pulse 1.5s infinite;
}
@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.5; }
}
.icon-btn {
  background: none;
  border: none;
  font-size: 18px;
  cursor: pointer;
  padding: 8px;
  border-radius: 8px;
  transition: background 0.2s;
}
.icon-btn:hover {
  background: var(--hover-bg);
}
.model-select {
  padding: 6px 12px;
  border: 1px solid var(--border-color);
  border-radius: 8px;
  background: var(--bg-tertiary);
  color: var(--text-primary);
  font-size: 13px;
}

/* Messages */
.messages-container {
  flex: 1;
  overflow-y: auto;
  padding: 20px;
}
.welcome-message {
  text-align: center;
  padding: 60px 20px;
}
.welcome-icon {
  font-size: 64px;
  margin-bottom: 20px;
}
.welcome-message h2 {
  font-size: 28px;
  margin-bottom: 12px;
}
.welcome-message p {
  color: var(--text-secondary);
  margin-bottom: 24px;
}
.suggestions {
  display: flex;
  flex-wrap: wrap;
  justify-content: center;
  gap: 12px;
}
.suggestion-btn {
  padding: 10px 20px;
  border: 1px solid var(--border-color);
  border-radius: 20px;
  background: var(--bg-secondary);
  color: var(--text-primary);
  cursor: pointer;
  transition: all 0.2s;
}
.suggestion-btn:hover {
  background: var(--primary-color);
  color: white;
  border-color: var(--primary-color);
}
.message {
  display: flex;
  gap: 16px;
  margin-bottom: 24px;
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
  max-width: 80%;
}
.message-header {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 8px;
}
.sender-name {
  font-weight: 500;
  font-size: 14px;
}
.message-time {
  font-size: 12px;
  color: var(--text-secondary);
}
.copy-btn {
  background: none;
  border: none;
  cursor: pointer;
  opacity: 0;
  transition: opacity 0.2s;
  font-size: 14px;
}
.message:hover .copy-btn {
  opacity: 1;
}
.message-body {
  padding: 16px;
  border-radius: 12px;
  background: var(--bg-secondary);
  font-size: 14px;
  line-height: 1.6;
}
.message.user .message-body {
  background: var(--primary-color);
  color: white;
}
.tool-calls {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 12px;
}
.tool-call {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 12px;
  background: var(--bg-tertiary);
  border-radius: 16px;
  font-size: 12px;
}

/* Typing Indicator */
.typing-indicator {
  display: flex;
  gap: 4px;
  padding: 16px;
}
.typing-indicator span {
  width: 8px;
  height: 8px;
  background: var(--text-secondary);
  border-radius: 50%;
  animation: bounce 1.4s infinite ease-in-out;
}
.typing-indicator span:nth-child(1) { animation-delay: 0s; }
.typing-indicator span:nth-child(2) { animation-delay: 0.2s; }
.typing-indicator span:nth-child(3) { animation-delay: 0.4s; }
@keyframes bounce {
  0%, 80%, 100% { transform: scale(0.8); opacity: 0.5; }
  40% { transform: scale(1); opacity: 1; }
}

/* Input Area */
.input-area {
  padding: 16px 20px;
  background: var(--bg-secondary);
  border-top: 1px solid var(--border-color);
}
.attached-files {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 12px;
}
.attached-file {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 12px;
  background: var(--bg-tertiary);
  border-radius: 16px;
  font-size: 13px;
}
.attached-file button {
  background: none;
  border: none;
  cursor: pointer;
  font-size: 16px;
}
.input-container {
  display: flex;
  align-items: flex-end;
  gap: 12px;
  background: var(--bg-primary);
  border: 1px solid var(--border-color);
  border-radius: 12px;
  padding: 8px 12px;
}
.attach-btn, .send-btn {
  background: none;
  border: none;
  font-size: 20px;
  cursor: pointer;
  padding: 8px;
  border-radius: 8px;
  transition: background 0.2s;
}
.attach-btn:hover, .send-btn:hover {
  background: var(--hover-bg);
}
.send-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.message-input {
  flex: 1;
  border: none;
  background: transparent;
  color: var(--text-primary);
  font-size: 14px;
  resize: none;
  outline: none;
  min-height: 24px;
  max-height: 120px;
}
.message-input::placeholder {
  color: var(--text-secondary);
}
.input-hints {
  display: flex;
  gap: 24px;
  margin-top: 8px;
  font-size: 12px;
  color: var(--text-secondary);
}
kbd {
  padding: 2px 6px;
  background: var(--bg-tertiary);
  border-radius: 4px;
  font-size: 11px;
}

/* Modal */
.modal-overlay {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  justify-content: center;
  align-items: center;
  z-index: 1000;
}
.modal-content {
  background: var(--bg-primary);
  border-radius: 16px;
  padding: 24px;
  max-width: 500px;
  width: 90%;
}
.modal-close {
  margin-top: 16px;
  padding: 10px 20px;
  background: var(--primary-color);
  color: white;
  border: none;
  border-radius: 8px;
  cursor: pointer;
  width: 100%;
}

/* Notifications */
.notifications {
  position: fixed;
  bottom: 100px;
  right: 20px;
  display: flex;
  flex-direction: column;
  gap: 8px;
  z-index: 2000;
}
.notification {
  padding: 12px 20px;
  border-radius: 8px;
  font-size: 14px;
  animation: slideIn 0.3s ease;
}
@keyframes slideIn {
  from { opacity: 0; transform: translateX(20px); }
  to { opacity: 1; transform: translateX(0); }
}
.notification.success { background: #10b981; color: white; }
.notification.error { background: #ef4444; color: white; }
.notification.info { background: #3b82f6; color: white; }
</style>
