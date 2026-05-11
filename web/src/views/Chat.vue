<script setup lang="ts">
import { ref, onMounted, onUnmounted, nextTick, computed, watch } from 'vue'
import { NInput, NButton, NIcon, NSelect, NTag, NSpin, NScrollbar } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { marked } from 'marked'
import hljs from 'highlight.js'

const { t } = useI18n()

interface Message {
  id: string
  role: 'user' | 'assistant' | 'system'
  content: string
  timestamp: number
  tool_calls?: ToolCall[]
  tool_results?: ToolResult[]
}

interface ToolCall {
  id: string
  name: string
  arguments: string
}

interface ToolResult {
  tool_call_id: string
  content: string
}

interface Session {
  id: string
  title: string
  updatedAt: number
  source: string
}

const messagesRef = ref<HTMLElement>()
const inputMessage = ref('')
const messages = ref<Message[]>([])
const isLoading = ref(false)
const streaming = ref(false)
const expandedTools = ref<Set<string>>(new Set())
const showSidebar = ref(true)

const sessions = ref<Session[]>([])
const activeSessionId = ref<string | null>(null)
const selectedModel = ref('deepseek/deepseek-chat')

const models = [
  { label: 'DeepSeek Chat', value: 'deepseek/deepseek-chat' },
  { label: 'GPT-4', value: 'openai/gpt-4' },
  { label: 'Claude 3', value: 'anthropic/claude-3' },
]

const activeSession = computed(() => 
  sessions.value.find(s => s.id === activeSessionId.value)
)

function formatTime(timestamp: number): string {
  return new Date(timestamp).toLocaleTimeString()
}

function renderMarkdown(content: string): string {
  try {
    const html = marked.parse(content) as string
    return html
  } catch {
    return content
  }
}

function highlightCode(content: string): string {
  const container = document.createElement('div')
  container.innerHTML = content
  container.querySelectorAll('pre code').forEach(block => {
    hljs.highlightElement(block as HTMLElement)
  })
  return container.innerHTML
}

function toggleToolResult(id: string) {
  if (expandedTools.value.has(id)) {
    expandedTools.value.delete(id)
  } else {
    expandedTools.value.add(id)
  }
  expandedTools.value = new Set(expandedTools.value)
}

async function sendMessage() {
  if (!inputMessage.value.trim() || isLoading.value) return
  
  const userMessage: Message = {
    id: crypto.randomUUID(),
    role: 'user',
    content: inputMessage.value,
    timestamp: Date.now()
  }
  
  messages.value.push(userMessage)
  inputMessage.value = ''
  isLoading.value = true
  streaming.value = true
  
  // Scroll to bottom
  await nextTick()
  scrollToBottom()
  
  // Simulate streaming response
  const assistantMessage: Message = {
    id: crypto.randomUUID(),
    role: 'assistant',
    content: '',
    timestamp: Date.now()
  }
  messages.value.push(assistantMessage)
  
  // Simulate stream (replace with real SSE in production)
  const responseText = `这是对 "${userMessage.content}" 的回复。\n\n我可以帮助你：\n- 回答问题\n- 编写代码\n- 分析数据\n- 写作和翻译\n\n有什么我可以帮你的吗？`
  
  for (let i = 0; i < responseText.length; i++) {
    await new Promise(resolve => setTimeout(resolve, 20))
    assistantMessage.content += responseText[i]
    await nextTick()
    scrollToBottom()
  }
  
  streaming.value = false
  isLoading.value = false
}

function scrollToBottom() {
  if (messagesRef.value) {
    messagesRef.value.scrollTop = messagesRef.value.scrollHeight
  }
}

function clearChat() {
  messages.value = []
}

function newChat() {
  const session: Session = {
    id: crypto.randomUUID(),
    title: 'New Chat',
    updatedAt: Date.now(),
    source: 'cli'
  }
  sessions.value.unshift(session)
  activeSessionId.value = session.id
  messages.value = []
}

function selectSession(id: string) {
  activeSessionId.value = id
  // Load session messages
  messages.value = []
}

function handleKeyDown(e: KeyboardEvent) {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault()
    sendMessage()
  }
}

function getRoleIcon(role: string) {
  switch (role) {
    case 'user': return '👤'
    case 'assistant': return '🤖'
    default: return '⚙️'
  }
}

onMounted(async () => {
  // Load sessions
  try {
    const res = await fetch('/api/sessions')
    if (res.ok) {
      const data = await res.json()
      sessions.value = data.sessions || []
    }
  } catch (e) {
    console.error('Failed to load sessions:', e)
  }
  
  // Load last session
  if (sessions.value.length > 0) {
    activeSessionId.value = sessions.value[0].id
  } else {
    newChat()
  }
})
</script>

<template>
  <div class="chat-view">
    <!-- Sidebar -->
    <div class="chat-sidebar" :class="{ collapsed: !showSidebar }">
      <div class="sidebar-header">
        <NButton type="primary" block @click="newChat">
          {{ t('chat.newChat') }}
        </NButton>
      </div>
      <NScrollbar>
        <div class="session-list">
          <div
            v-for="session in sessions"
            :key="session.id"
            class="session-item"
            :class="{ active: session.id === activeSessionId }"
            @click="selectSession(session.id)"
          >
            <span class="session-title">{{ session.title }}</span>
            <span class="session-time">{{ formatTime(session.updatedAt) }}</span>
          </div>
        </div>
      </NScrollbar>
    </div>

    <!-- Main Chat Area -->
    <div class="chat-main">
      <!-- Chat Header -->
      <div class="chat-header">
        <NButton
          text
          @click="showSidebar = !showSidebar"
          class="toggle-sidebar"
        >
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <line x1="3" y1="12" x2="21" y2="12"/>
            <line x1="3" y1="6" x2="21" y2="6"/>
            <line x1="3" y1="18" x2="21" y2="18"/>
          </svg>
        </NButton>
        <div class="chat-model">
          <NSelect
            v-model:value="selectedModel"
            :options="models"
            size="small"
            style="width: 180px"
          />
        </div>
        <NTag v-if="activeSession" type="info" size="small">
          {{ activeSession.source }}
        </NTag>
      </div>

      <!-- Messages -->
      <NScrollbar ref="messagesRef" class="messages-container">
        <div class="messages">
          <div
            v-for="message in messages"
            :key="message.id"
            :class="['message', `message-${message.role}`]"
          >
            <div class="message-avatar">{{ getRoleIcon(message.role) }}</div>
            <div class="message-content">
              <div class="message-header">
                <span class="message-role">{{ message.role }}</span>
                <span class="message-time">{{ formatTime(message.timestamp) }}</span>
              </div>
              <div 
                class="message-text" 
                v-html="highlightCode(renderMarkdown(message.content))"
              />
              <div v-if="message.tool_calls?.length" class="tool-calls">
                <div
                  v-for="call in message.tool_calls"
                  :key="call.id"
                  class="tool-call"
                  @click="toggleToolResult(call.id)"
                >
                  <NTag size="small" type="warning">⚡ {{ call.name }}</NTag>
                </div>
              </div>
              <div
                v-if="expandedTools.has(message.id) && message.tool_results?.length"
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

          <div v-if="isLoading && !streaming" class="message message-assistant">
            <div class="message-avatar">🤖</div>
            <div class="message-content">
              <NSpin size="small" />
            </div>
          </div>

          <div v-if="messages.length === 0" class="empty-messages">
            <div class="empty-icon">💬</div>
            <p>{{ t('chat.startConversation') }}</p>
          </div>
        </div>
      </NScrollbar>

      <!-- Input Area -->
      <div class="input-area">
        <div class="input-container">
          <NInput
            v-model:value="inputMessage"
            type="textarea"
            :placeholder="t('chat.placeholder')"
            :autosize="{ minRows: 1, maxRows: 4 }"
            @keydown="handleKeyDown"
          />
          <div class="input-actions">
            <NButton
              size="small"
              quaternary
              @click="clearChat"
              :disabled="messages.length === 0"
            >
              {{ t('chat.clear') }}
            </NButton>
            <NButton
              type="primary"
              @click="sendMessage"
              :loading="isLoading"
              :disabled="!inputMessage.trim()"
            >
              {{ t('chat.send') }}
            </NButton>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped lang="scss">
.chat-view {
  height: calc(100 * var(--vh));
  display: flex;
  overflow: hidden;
}

.chat-sidebar {
  width: 260px;
  background: #1a1a2e;
  border-right: 1px solid #303133;
  display: flex;
  flex-direction: column;
  transition: width 0.3s;

  &.collapsed {
    width: 0;
    border-right: none;
    overflow: hidden;
  }
}

.sidebar-header {
  padding: 12px;
  border-bottom: 1px solid #303133;
}

.session-list {
  padding: 8px;
}

.session-item {
  display: flex;
  flex-direction: column;
  padding: 10px 12px;
  border-radius: 6px;
  cursor: pointer;
  margin-bottom: 4px;
  transition: background 0.2s;

  &:hover {
    background: rgba(255, 255, 255, 0.05);
  }

  &.active {
    background: rgba(255, 215, 0, 0.1);
    border: 1px solid rgba(255, 215, 0, 0.3);
  }
}

.session-title {
  font-size: 13px;
  color: #e6e6e6;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.session-time {
  font-size: 11px;
  color: #909399;
  margin-top: 2px;
}

.chat-main {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.chat-header {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 16px;
  border-bottom: 1px solid #303133;
  background: #1a1a2e;
}

.toggle-sidebar {
  color: #909399;
}

.chat-model {
  flex: 1;
}

.messages-container {
  flex: 1;
  overflow: hidden;
}

.messages {
  padding: 20px;
  max-width: 900px;
  margin: 0 auto;
}

.message {
  display: flex;
  gap: 12px;
  margin-bottom: 20px;
}

.message-avatar {
  font-size: 24px;
  flex-shrink: 0;
}

.message-content {
  flex: 1;
  min-width: 0;
}

.message-header {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 4px;
}

.message-role {
  font-size: 12px;
  font-weight: 600;
  text-transform: capitalize;
}

.message-user .message-role {
  color: #3fb950;
}

.message-assistant .message-role {
  color: #ffd700;
}

.message-time {
  font-size: 11px;
  color: #909399;
}

.message-text {
  font-size: 14px;
  line-height: 1.6;
  color: #c0c4cc;
  word-break: break-word;

  :deep(pre) {
    background: #161b22;
    border-radius: 6px;
    padding: 12px;
    overflow-x: auto;
    margin: 8px 0;
  }

  :deep(code) {
    font-family: 'Fira Code', monospace;
    font-size: 13px;
  }

  :deep(p) {
    margin: 8px 0;
  }
}

.tool-calls {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 8px;
}

.tool-call {
  cursor: pointer;
}

.tool-results {
  margin-top: 8px;
  background: #161b22;
  border-radius: 6px;
  padding: 12px;

  pre {
    margin: 0;
    font-size: 12px;
    white-space: pre-wrap;
    word-break: break-all;
  }
}

.empty-messages {
  text-align: center;
  padding: 60px 20px;
  color: #909399;
}

.empty-icon {
  font-size: 48px;
  margin-bottom: 16px;
}

.input-area {
  padding: 12px 16px;
  border-top: 1px solid #303133;
  background: #1a1a2e;
}

.input-container {
  max-width: 900px;
  margin: 0 auto;
}

.input-actions {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-top: 8px;
}
</style>
