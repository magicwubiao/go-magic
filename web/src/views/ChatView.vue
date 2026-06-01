<template>
  <div class="chat-container">
    <!-- Session Sidebar -->
    <div class="session-sidebar">
      <div class="sidebar-header">
        <n-button type="primary" block @click="createSession" size="small">
          + {{ t('chat.newChat') }}
        </n-button>
      </div>
      <div class="session-list">
        <template v-for="(sessions, profile) in groupedSessions" :key="profile">
          <div class="profile-group-header">{{ profile || t('chat.default') }}</div>
          <div
            v-for="session in sessions"
            :key="session.id"
            class="session-item"
            :class="{ active: chatStore.activeSessionId === session.id }"
            @click="selectSession(session.id)"
          >
            <div class="session-title">{{ session.title || t('chat.untitled') }}</div>
            <div class="session-meta">
              <n-tag v-if="session.source && session.source !== 'web'" size="tiny" :type="sourceType(session.source)" style="margin-right: 4px;">
                {{ session.source }}
              </n-tag>
              {{ session.message_count || 0 }} {{ t('chat.messages') }}
            </div>
            <n-popconfirm @positive-click="deleteSession(session.id)">
              <template #trigger>
                <n-button
                  class="session-delete"
                  size="tiny"
                  quaternary
                  circle
                  type="error"
                  @click.stop
                >
                  ×
                </n-button>
              </template>
              {{ t('common.confirmDelete') }}
            </n-popconfirm>
          </div>
        </template>
        <n-text v-if="!chatStore.sessions.length" depth="3" style="padding: 16px; display: block; text-align: center;">
          {{ t('chat.noSessions') }}
        </n-text>
      </div>
    </div>

    <!-- Chat Area -->
    <div class="chat-main">
      <CurrentGoal style="margin: 12px 12px 0 12px;" />

      <n-alert v-if="chatStore.error" type="error" closable style="margin: 12px;" @close="chatStore.error = null">
        {{ chatStore.error.message }}
      </n-alert>

      <n-alert v-if="isGatewaySession" type="info" style="margin: 12px;">
        {{ t('chat.gatewaySession', { source: activeSessionSource }) }}
      </n-alert>

      <div class="messages" ref="messagesRef">
        <div
          v-for="msg in chatStore.messages"
          :key="msg.id"
          class="message"
          :class="msg.role"
        >
          <!-- User message -->
          <template v-if="msg.role === 'user'">
            <div class="avatar user-avatar">👤</div>
            <div class="message-body">
              <div class="message-bubble user-bubble" v-html="renderMarkdown(msg.content)"></div>
              <div class="message-time">{{ formatTime(msg.timestamp) }}</div>
            </div>
          </template>

          <!-- Assistant message -->
          <template v-else-if="msg.role === 'assistant'">
            <div class="avatar bot-avatar">🤖</div>
            <div class="message-body">
              <div class="message-bubble assistant-bubble">
                <ReasoningContent :content="msg.content" />
              </div>
              <div class="message-time">{{ formatTime(msg.timestamp) }}</div>
            </div>
          </template>

          <!-- Tool message -->
          <template v-else-if="msg.role === 'tool'">
            <div class="avatar tool-avatar">🔧</div>
            <div class="message-body">
              <div class="message-bubble tool-bubble">
                <div class="tool-name-inline">{{ msg.tool_name || 'Tool' }}</div>
                <div class="tool-content-inline">{{ msg.content }}</div>
              </div>
            </div>
          </template>
        </div>

        <!-- Streaming area -->
        <template v-if="chatStore.streaming">
          <!-- Streaming message with tool calls inline -->
          <div class="message assistant">
            <div class="avatar bot-avatar">🤖</div>
            <div class="message-body">
              <!-- Tool calls inline (compact) -->
              <div v-if="chatStore.toolCalls.length > 0" class="tool-calls-inline">
                <ToolCallBlock
                  v-for="tc in chatStore.toolCalls"
                  :key="tc.id"
                  :tool-call="tc"
                />
              </div>
              <!-- Streaming text -->
              <div v-if="chatStore.streamContent" class="message-bubble assistant-bubble">
                <ReasoningContent :content="chatStore.streamContent" />
              </div>
              <!-- Waiting indicator -->
              <div v-if="!chatStore.streamContent && chatStore.activeToolCalls.length === 0" class="waiting-indicator">
                <span class="dot"></span>
                <span class="dot"></span>
                <span class="dot"></span>
              </div>
            </div>
          </div>
        </template>

        <n-text v-if="!chatStore.messages.length && !chatStore.streaming" depth="3" class="empty-hint">
          {{ t('chat.selectSession') }}
        </n-text>
      </div>

      <div class="input-area">
        <n-input
          v-model:value="inputValue"
          type="textarea"
          :autosize="{ minRows: 2, maxRows: 6 }"
          :placeholder="t('chat.placeholder')"
          @keydown.enter.exact.prevent="send"
          @keydown.enter.shift.prevent="() => {}"
        />
        <n-button v-if="!chatStore.streaming" type="primary" @click="send" :disabled="!inputValue.trim()" style="align-self: flex-end;">
          {{ t('chat.send') }}
        </n-button>
        <n-button v-else type="warning" @click="stopGeneration" style="align-self: flex-end;">
          ⏹ {{ t('chat.stop') }}
        </n-button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, nextTick, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { marked } from 'marked'
import hljs from 'highlight.js'
import 'highlight.js/styles/github.css'
import { useChatStore } from '@/stores/chat'
import ReasoningContent from '@/components/ReasoningContent.vue'
import ToolCallBlock from '@/components/ToolCallBlock.vue'
import CurrentGoal from '@/components/CurrentGoal.vue'

const { t } = useI18n()
const chatStore = useChatStore()
const inputValue = ref('')
const messagesRef = ref<HTMLDivElement>()

// Custom code renderer for highlight.js
const codeRenderer = (code: string, lang?: string): string => {
  const language = lang && hljs.getLanguage(lang) ? lang : null
  const highlighted = language
    ? hljs.highlight(code, { language }).value
    : hljs.highlightAuto(code).value
  const copyBtn = `<button class="code-copy-btn" onclick="(function(btn){var code=btn.parentElement.querySelector('code');navigator.clipboard.writeText(code.textContent);btn.textContent='✓ Copied';setTimeout(()=>btn.textContent='Copy',2000)})(this)">Copy</button>`
  return `<div class="code-block">${copyBtn}<pre><code class="hljs${language ? ` language-${language}` : ''}">${highlighted}</code></pre></div>`
}

marked.use({ renderer: { code: codeRenderer } })

function renderMarkdown(content: string): string {
  return marked.parse(content) as string
}

// Group sessions by profile
const groupedSessions = computed(() => {
  const groups: Record<string, any[]> = {}
  const sessions = chatStore.sessions || []
  for (const session of sessions) {
    let profile = session?.profile?.trim() || ''
    if (profile === '' || profile.toLowerCase() === 'default') {
      profile = t('chat.default')
    }
    if (!groups[profile]) groups[profile] = []
    groups[profile].push(session)
  }
  return groups
})

const isGatewaySession = computed(() => {
  const session = chatStore.activeSession
  return session && session.source && session.source !== 'web'
})

const activeSessionSource = computed(() => {
  return chatStore.activeSession?.source || ''
})

function sourceType(source: string) {
  const map: Record<string, string> = {
    telegram: 'info',
    discord: 'info',
    wechat: 'success',
    wecom: 'success',
    slack: 'warning',
  }
  return (map[source] || 'default') as any
}

function formatTime(timestamp: string): string {
  if (!timestamp) return ''
  const date = new Date(timestamp)
  const now = new Date()
  const isToday = date.toDateString() === now.toDateString()
  if (isToday) {
    return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  }
  return date.toLocaleDateString([], { month: 'short', day: 'numeric' }) + ' ' +
    date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

async function send() {
  const content = inputValue.value.trim()
  if (!content || chatStore.streaming) return

  inputValue.value = ''
  await chatStore.sendMessage(content)
}

function stopGeneration() {
  chatStore.stopGeneration()
}

async function createSession() {
  await chatStore.createSession()
  await chatStore.loadSessions()
}

async function deleteSession(id: string) {
  await chatStore.deleteSession(id)
  await chatStore.loadSessions()
}

async function selectSession(id: string) {
  await chatStore.selectSession(id)
}

function scrollToBottom() {
  nextTick(() => {
    messagesRef.value?.scrollTo({ top: messagesRef.value.scrollHeight, behavior: 'smooth' })
  })
}

watch(() => chatStore.messages.length, scrollToBottom)
watch(() => chatStore.streamContent, scrollToBottom)
watch(() => chatStore.toolCalls.length, scrollToBottom)

onMounted(async () => {
  await chatStore.loadSessions()
})
</script>

<style scoped>
.chat-container {
  display: flex;
  height: calc(100vh - 48px);
}

/* ========== Sidebar ========== */
.session-sidebar {
  width: 240px;
  border-right: 1px solid #e0e0e0;
  display: flex;
  flex-direction: column;
  flex-shrink: 0;
  background: #fff;
}

.sidebar-header {
  padding: 12px;
  border-bottom: 1px solid #e0e0e0;
}

.session-list {
  flex: 1;
  overflow-y: auto;
}

.profile-group-header {
  padding: 8px 12px 4px;
  font-size: 11px;
  font-weight: 600;
  color: #999;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.session-item {
  padding: 10px 12px;
  cursor: pointer;
  border-bottom: 1px solid #f0f0f0;
  position: relative;
  display: flex;
  align-items: center;
  gap: 8px;
  transition: background 0.15s;
}

.session-item:hover { background: #f0f0f0; }
.session-item.active { background: #e8f5e9; }

.session-title {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 14px;
}

.session-meta {
  font-size: 12px;
  color: #999;
  flex-shrink: 0;
}

.session-delete {
  opacity: 0;
  transition: opacity 0.2s;
  flex-shrink: 0;
}

.session-item:hover .session-delete { opacity: 1; }

/* ========== Chat Main ========== */
.chat-main {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-width: 0;
  background: #fff;
}

.messages {
  flex: 1;
  overflow-y: auto;
  padding: 20px 24px;
}

.empty-hint {
  padding: 40px;
  display: block;
  text-align: center;
}

/* ========== Message Layout ========== */
.message {
  display: flex;
  gap: 12px;
  margin-bottom: 20px;
  animation: fadeIn 0.3s ease;
}

@keyframes fadeIn {
  from { opacity: 0; transform: translateY(8px); }
  to { opacity: 1; transform: translateY(0); }
}

.message.user {
  flex-direction: row-reverse;
}

.message-body {
  max-width: 72%;
  min-width: 0;
}

/* ========== Avatars ========== */
.avatar {
  width: 34px;
  height: 34px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  font-size: 16px;
}

.user-avatar {
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
}

.bot-avatar {
  background: linear-gradient(135deg, #18a058 0%, #36ad6a 100%);
}

.tool-avatar {
  background: #f0f0f0;
}

/* ========== Message Bubbles ========== */
.message-bubble {
  padding: 12px 16px;
  border-radius: 16px;
  line-height: 1.65;
  word-break: break-word;
  overflow-wrap: break-word;
}

.user-bubble {
  background: linear-gradient(135deg, #18a058 0%, #20803a 100%);
  color: white;
  border-bottom-right-radius: 4px;
}

.assistant-bubble {
  background: #f5f5f5;
  color: #333;
  border-bottom-left-radius: 4px;
}

.tool-bubble {
  background: #f0f7ff;
  border: 1px solid #d6e4ff;
  border-bottom-left-radius: 4px;
}

.tool-name-inline {
  font-weight: 600;
  font-size: 12px;
  color: #1890ff;
  margin-bottom: 4px;
  font-family: 'SF Mono', 'Fira Code', monospace;
}

.tool-content-inline {
  font-size: 13px;
  color: #666;
  max-height: 200px;
  overflow-y: auto;
  white-space: pre-wrap;
}

/* ========== Message Time ========== */
.message-time {
  font-size: 11px;
  color: #bbb;
  margin-top: 4px;
  padding: 0 4px;
}

.message.user .message-time {
  text-align: right;
}

/* ========== Tool Call Area ========== */
.tool-calls-inline {
  margin-bottom: 8px;
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

/* ========== Waiting Indicator ========== */
.waiting-indicator {
  display: flex;
  gap: 5px;
  padding: 16px 20px;
  background: #f5f5f5;
  border-radius: 16px;
  border-bottom-left-radius: 4px;
}

.waiting-indicator .dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: #999;
  animation: waitBounce 1.4s ease-in-out infinite;
}

.waiting-indicator .dot:nth-child(2) { animation-delay: 0.2s; }
.waiting-indicator .dot:nth-child(3) { animation-delay: 0.4s; }

@keyframes waitBounce {
  0%, 80%, 100% { transform: scale(0.6); opacity: 0.4; }
  40% { transform: scale(1); opacity: 1; }
}

/* ========== Markdown Content Styles ========== */
.message-bubble :deep(p) { margin: 0 0 8px 0; }
.message-bubble :deep(p:last-child) { margin-bottom: 0; }
.message-bubble :deep(ul), .message-bubble :deep(ol) { margin: 8px 0; padding-left: 24px; }
.message-bubble :deep(li) { margin: 4px 0; }
.message-bubble :deep(blockquote) {
  border-left: 3px solid #d0d0d0;
  padding-left: 12px;
  margin: 8px 0;
  color: #666;
}
.message-bubble :deep(table) {
  border-collapse: collapse;
  width: 100%;
  margin: 8px 0;
  font-size: 14px;
}
.message-bubble :deep(th), .message-bubble :deep(td) {
  border: 1px solid #e0e0e0;
  padding: 8px 12px;
  text-align: left;
}
.message-bubble :deep(th) {
  background: #f0f0f0;
  font-weight: 600;
}
.message-bubble :deep(a) {
  color: #18a058;
  text-decoration: none;
}
.message-bubble :deep(a:hover) {
  text-decoration: underline;
}

/* ========== Code Block ========== */
.message-bubble :deep(.code-block) {
  position: relative;
  margin: 8px 0;
  border-radius: 8px;
  overflow: hidden;
  background: #1e1e1e;
}

.message-bubble :deep(.code-block pre) {
  margin: 0;
  padding: 12px 16px;
  overflow-x: auto;
}

.message-bubble :deep(.code-block code) {
  font-family: 'SF Mono', 'Fira Code', 'Consolas', monospace;
  font-size: 13px;
  color: #d4d4d4;
}

.message-bubble :deep(.code-copy-btn) {
  position: absolute;
  top: 6px;
  right: 6px;
  background: rgba(255, 255, 255, 0.1);
  border: 1px solid rgba(255, 255, 255, 0.2);
  color: #ccc;
  padding: 2px 10px;
  border-radius: 4px;
  font-size: 12px;
  cursor: pointer;
  opacity: 0;
  transition: opacity 0.2s, background 0.2s;
}

.message-bubble :deep(.code-block:hover .code-copy-btn) {
  opacity: 1;
}

.message-bubble :deep(.code-copy-btn:hover) {
  background: rgba(255, 255, 255, 0.2);
}

/* ========== Input Area ========== */
.input-area {
  display: flex;
  gap: 12px;
  padding: 16px 24px;
  border-top: 1px solid #e0e0e0;
  background: #fff;
}

.input-area .n-input {
  flex: 1;
}

/* ========== Dark Mode ========== */
@media (prefers-color-scheme: dark) {
  .session-sidebar {
    background: #fff;
    border-right-color: #333;
  }

  .session-item {
    border-bottom-color: #2a2a2a;
  }

  .session-item:hover { background: #252525; }
  .session-item.active { background: #1a3a1a; }

  .chat-main {
    background: #141414;
  }

  .assistant-bubble {
    background: #1e1e1e;
    color: #ddd;
  }

  .tool-bubble {
    background: #1a2332;
    border-color: #1a3a5c;
  }

  .waiting-indicator {
    background: #1e1e1e;
  }

  .waiting-indicator .dot {
    background: #666;
  }

  .message-time {
    color: #555;
  }

  .input-area {
    background: #fff;
    border-top-color: #333;
  }

  .message-bubble :deep(th) {
    background: #252525;
  }

  .message-bubble :deep(th), .message-bubble :deep(td) {
    border-color: #333;
  }

  .message-bubble :deep(blockquote) {
    border-left-color: #444;
    color: #999;
  }
}
</style>
