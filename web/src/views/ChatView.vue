<template>
  <div class="chat-view">
    <!-- Header -->
    <div class="chat-header">
      <div class="chat-title">
        <span class="session-name">{{ currentSession?.name || 'New Chat' }}</span>
        <n-tag v-if="isStreaming" size="small" type="info">
          <template #icon>
            <span class="streaming-dot"></span>
          </template>
          Streaming
        </n-tag>
      </div>
      <div class="chat-actions">
        <n-tooltip trigger="hover">
          <template #trigger>
            <n-button quaternary circle @click="clearChat" :disabled="!currentSession">
              🗑️
            </n-button>
          </template>
          Clear Chat
        </n-tooltip>
      </div>
    </div>

    <!-- Messages -->
    <div class="messages-container" ref="messagesContainer">
      <!-- Welcome state -->
      <div v-if="!currentSession || messages.length === 0" class="welcome-state">
        <div class="welcome-icon">✨</div>
        <h2>Welcome to go-magic</h2>
        <p>Start a conversation or try one of these:</p>
        <div class="suggestions">
          <n-button
            v-for="suggestion in suggestions"
            :key="suggestion"
            size="small"
            @click="sendMessage(suggestion)"
          >
            {{ suggestion }}
          </n-button>
        </div>
      </div>

      <!-- Messages -->
      <div v-else class="messages-list">
        <div
          v-for="message in messages"
          :key="message.id"
          class="message"
          :class="[`message-${message.role}`]"
        >
          <div class="message-avatar">
            {{ message.role === 'user' ? '👤' : '🤖' }}
          </div>
          <div class="message-content">
            <div class="message-header">
              <span class="message-role">{{ message.role }}</span>
              <span class="message-time">{{ formatTime(message.timestamp) }}</span>
            </div>
            <div class="message-text">
              <MarkdownRenderer :content="message.content" />
            </div>
          </div>
        </div>

        <!-- Typing indicator -->
        <div v-if="isStreaming" class="message message-assistant">
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
    </div>

    <!-- Input -->
    <div class="chat-input-container">
      <div class="input-wrapper">
        <n-input
          v-model:value="inputMessage"
          type="textarea"
          placeholder="Type your message... (Shift+Enter for new line, Enter to send)"
          :autosize="{ minRows: 1, maxRows: 5 }"
          @keydown="handleKeydown"
          :disabled="isStreaming"
        />
        <n-button
          type="primary"
          :loading="isStreaming"
          :disabled="!inputMessage.trim()"
          @click="sendCurrentMessage"
          class="send-button"
        >
          {{ isStreaming ? '...' : '➤' }}
        </n-button>
      </div>
      <div class="input-hints">
        <span>Press Enter to send</span>
        <span>Shift+Enter for new line</span>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, nextTick } from 'vue'
import { NInput, NButton, NTooltip, NTag } from 'naive-ui'
import MarkdownRenderer from '../components/MarkdownRenderer.vue'
import { apiService, Session, Message } from '../api'

const messagesContainer = ref<HTMLElement | null>(null)
const inputMessage = ref('')
const isStreaming = ref(false)
const currentSession = ref<Session | null>(null)
const messages = ref<Message[]>([])

const suggestions = [
  'What can you help me with?',
  'Show me your features',
  'How do I configure you?',
]

// Load initial session
onMounted(async () => {
  try {
    const sessions = await apiService.sessions.list()
    if (sessions.data.length > 0) {
      // Get most recent session
      const sorted = sessions.data.sort((a, b) => 
        new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime()
      )
      await selectSession(sorted[0])
    }
  } catch (err) {
    console.error('Failed to load sessions:', err)
  }
})

async function selectSession(session: Session) {
  currentSession.value = session
  try {
    const response = await apiService.sessions.get(session.id)
    messages.value = response.data.messages || []
    scrollToBottom()
  } catch (err) {
    console.error('Failed to load session:', err)
  }
}

async function sendMessage(text: string) {
  if (!text.trim() || isStreaming.value) return

  // Create session if needed
  if (!currentSession.value) {
    try {
      const response = await apiService.sessions.create({
        name: text.slice(0, 50),
      })
      currentSession.value = response.data
    } catch (err) {
      console.error('Failed to create session:', err)
      return
    }
  }

  const userMessage: Message = {
    id: Date.now().toString(),
    role: 'user',
    content: text,
    timestamp: new Date().toISOString(),
  }
  messages.value.push(userMessage)
  inputMessage.value = ''
  scrollToBottom()

  // Stream response
  isStreaming.value = true
  
  try {
    const response = await fetch('/api/chat/stream', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        message: text,
        session_id: currentSession.value.id,
      }),
    })

    const reader = response.body?.getReader()
    const decoder = new TextDecoder()
    let assistantMessage: Message = {
      id: (Date.now() + 1).toString(),
      role: 'assistant',
      content: '',
      timestamp: new Date().toISOString(),
    }
    messages.value.push(assistantMessage)

    while (reader) {
      const { done, value } = await reader.read()
      if (done) break

      const chunk = decoder.decode(value)
      const lines = chunk.split('\n')
      
      for (const line of lines) {
        if (line.startsWith('data: ')) {
          try {
            const data = JSON.parse(line.slice(6))
            if (data.type === 'chunk') {
              assistantMessage.content += data.data.content
              scrollToBottom()
            } else if (data.type === 'done') {
              assistantMessage = data.data
            } else if (data.type === 'session') {
              currentSession.value = data.data
            }
          } catch (e) {
            // Ignore parse errors
          }
        }
      }
    }

    // Update messages with final message
    const lastIdx = messages.value.findIndex(m => m.id === assistantMessage.id)
    if (lastIdx >= 0) {
      messages.value[lastIdx] = assistantMessage
    }

  } catch (err) {
    console.error('Chat error:', err)
    const errorMsg: Message = {
      id: (Date.now() + 2).toString(),
      role: 'assistant',
      content: 'Sorry, there was an error processing your request.',
      timestamp: new Date().toISOString(),
    }
    messages.value.push(errorMsg)
  } finally {
    isStreaming.value = false
    scrollToBottom()
  }
}

function sendCurrentMessage() {
  sendMessage(inputMessage.value)
}

function handleKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault()
    sendCurrentMessage()
  }
}

async function clearChat() {
  if (!currentSession.value) return
  
  try {
    await apiService.sessions.delete(currentSession.value.id)
    messages.value = []
    currentSession.value = null
  } catch (err) {
    console.error('Failed to clear chat:', err)
  }
}

function scrollToBottom() {
  nextTick(() => {
    if (messagesContainer.value) {
      messagesContainer.value.scrollTop = messagesContainer.value.scrollHeight
    }
  })
}

function formatTime(timestamp: string): string {
  const date = new Date(timestamp)
  return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

// Watch for streaming changes to scroll
watch(messages, () => scrollToBottom(), { deep: true })
</script>

<style scoped>
.chat-view {
  display: flex;
  flex-direction: column;
  height: 100%;
  background: var(--bg-primary);
}

.chat-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 12px 20px;
  background: var(--bg-secondary);
  border-bottom: 1px solid var(--border-color);
}

.chat-title {
  display: flex;
  align-items: center;
  gap: 12px;
}

.session-name {
  font-weight: 600;
  font-size: 16px;
}

.streaming-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--info-color);
  animation: pulse 1s infinite;
}

@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.5; }
}

.messages-container {
  flex: 1;
  overflow-y: auto;
  padding: 20px;
}

.welcome-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 100%;
  text-align: center;
  color: var(--text-secondary);
}

.welcome-icon {
  font-size: 64px;
  margin-bottom: 20px;
}

.welcome-state h2 {
  font-size: 24px;
  color: var(--text-primary);
  margin-bottom: 8px;
}

.suggestions {
  display: flex;
  gap: 8px;
  margin-top: 20px;
  flex-wrap: wrap;
  justify-content: center;
}

.messages-list {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.message {
  display: flex;
  gap: 12px;
  max-width: 80%;
}

.message-user {
  margin-left: auto;
  flex-direction: row-reverse;
}

.message-avatar {
  width: 36px;
  height: 36px;
  border-radius: 50%;
  background: var(--bg-tertiary);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 18px;
  flex-shrink: 0;
}

.message-content {
  flex: 1;
  min-width: 0;
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
  color: var(--text-secondary);
}

.message-text {
  padding: 12px 16px;
  border-radius: 12px;
  background: var(--bg-secondary);
}

.message-user .message-text {
  background: var(--primary-color);
  color: white;
}

.typing-indicator {
  display: flex;
  gap: 4px;
  padding: 12px 16px;
}

.typing-indicator span {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--text-secondary);
  animation: typing 1.4s infinite;
}

.typing-indicator span:nth-child(2) {
  animation-delay: 0.2s;
}

.typing-indicator span:nth-child(3) {
  animation-delay: 0.4s;
}

@keyframes typing {
  0%, 60%, 100% { transform: translateY(0); }
  30% { transform: translateY(-4px); }
}

.chat-input-container {
  padding: 16px 20px;
  background: var(--bg-secondary);
  border-top: 1px solid var(--border-color);
}

.input-wrapper {
  display: flex;
  gap: 12px;
  align-items: flex-end;
}

.input-wrapper :deep(.n-input) {
  flex: 1;
}

.send-button {
  height: 40px;
  width: 40px;
}

.input-hints {
  display: flex;
  gap: 16px;
  margin-top: 8px;
  font-size: 12px;
  color: var(--text-secondary);
}
</style>
