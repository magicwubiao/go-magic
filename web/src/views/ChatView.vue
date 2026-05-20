<template>
  <div class="chat-container">
    <!-- Session Sidebar -->
    <div class="session-sidebar">
      <div class="sidebar-header">
        <n-button type="primary" block @click="createSession" size="small">
          + New Chat
        </n-button>
      </div>
      <div class="session-list">
        <template v-for="(sessions, profile) in groupedSessions" :key="profile">
          <div class="profile-group-header">{{ profile || 'Default' }}</div>
          <div
            v-for="session in sessions"
            :key="session.id"
            class="session-item"
            :class="{ active: chatStore.activeSessionId === session.id }"
            @click="selectSession(session.id)"
          >
            <div class="session-title">{{ session.title || 'Untitled' }}</div>
            <div class="session-meta">
              <n-tag v-if="session.source && session.source !== 'web'" size="tiny" :type="sourceType(session.source)" style="margin-right: 4px;">
                {{ session.source }}
              </n-tag>
              {{ session.message_count || 0 }} msgs
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
              Delete this session?
            </n-popconfirm>
          </div>
        </template>
        <n-text v-if="!chatStore.sessions.length && !chatStore.loading" depth="3" style="padding: 16px; display: block; text-align: center;">
          No sessions yet
        </n-text>
        <n-spin v-if="chatStore.loading" size="small" style="padding: 16px; display: block; text-align: center;" />
      </div>
    </div>

    <!-- Chat Area -->
    <div class="chat-main">
      <n-alert v-if="chatStore.error" type="error" closable style="margin: 12px;" @close="chatStore.error = null">
        {{ chatStore.error.message }}
      </n-alert>

      <n-alert v-if="isGatewaySession" type="info" style="margin: 12px;">
        This session is from {{ activeSessionSource }}. Messages may not be available in web view.
      </n-alert>

      <div class="messages" ref="messagesRef">
        <div
          v-for="msg in chatStore.messages"
          :key="msg.id"
          class="message"
          :class="msg.role"
        >
          <div class="avatar">{{ msg.role === 'user' ? '👤' : '🤖' }}</div>
          <div class="content" v-html="renderMarkdown(msg.content)"></div>
        </div>
        <div v-if="chatStore.streaming" class="message assistant">
          <div class="avatar">🤖</div>
          <div class="content" v-html="renderMarkdown(chatStore.streamContent)"></div>
          <n-spin size="small" />
        </div>
        <n-text v-if="!chatStore.messages.length && !chatStore.streaming" depth="3" style="padding: 40px; display: block; text-align: center;">
          Select a session or start a new chat
        </n-text>
      </div>
      <div class="input-area">
        <n-input
          v-model:value="inputValue"
          type="textarea"
          :autosize="{ minRows: 2, maxRows: 6 }"
          placeholder="Type a message..."
          @keydown.enter.prevent="send"
        />
        <n-button type="primary" @click="send" :loading="chatStore.streaming" :disabled="!inputValue.trim()">
          Send
        </n-button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, nextTick, watch } from 'vue'
import { marked } from 'marked'
import hljs from 'highlight.js'
import 'highlight.js/styles/github.css'
import { useChatStore } from '@/stores/chat'

const chatStore = useChatStore()
const inputValue = ref('')
const messagesRef = ref<HTMLDivElement>()

marked.setOptions({
  highlight: (code, lang) => {
    if (lang && hljs.getLanguage(lang)) {
      return hljs.highlight(code, { language: lang }).value
    }
    return hljs.highlightAuto(code).value
  },
})

function renderMarkdown(content: string): string {
  return marked.parse(content) as string
}

// Group sessions by profile
const groupedSessions = computed(() => {
  const groups: Record<string, any[]> = {}
  const sessions = chatStore.sessions || []
  for (const session of sessions) {
    // Normalize profile: empty, 'default', 'Default' all become 'Default'
    let profile = session?.profile?.trim() || ''
    if (profile === '' || profile.toLowerCase() === 'default') {
      profile = 'Default'
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

async function send() {
  const content = inputValue.value.trim()
  if (!content || chatStore.streaming) return

  inputValue.value = ''
  await chatStore.sendMessage(content)
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
    messagesRef.value?.scrollTo(0, messagesRef.value.scrollHeight)
  })
}

watch(() => chatStore.messages.length, scrollToBottom)
watch(() => chatStore.streamContent, scrollToBottom)

onMounted(async () => {
  await chatStore.loadSessions()
})
</script>

<style scoped>
.chat-container {
  display: flex;
  height: calc(100vh - 48px);
}

.session-sidebar {
  width: 240px;
  border-right: 1px solid #e0e0e0;
  display: flex;
  flex-direction: column;
  flex-shrink: 0;
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
}

.session-item:hover { background: #f5f5f5; }
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

.chat-main {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-width: 0;
}

.messages { flex: 1; overflow-y: auto; padding: 16px; }

.message { display: flex; gap: 12px; margin-bottom: 16px; }
.message.user { flex-direction: row-reverse; }
.message.user .content { background: #18a058; color: white; }

.avatar {
  width: 32px; height: 32px; border-radius: 50%;
  display: flex; align-items: center; justify-content: center;
  background: #f0f0f0; flex-shrink: 0;
}

.content {
  max-width: 70%; padding: 12px 16px; border-radius: 12px;
  background: #f5f5f5; line-height: 1.6;
  word-break: break-word; overflow-wrap: break-word;
}

.content :deep(p) { margin: 0 0 8px 0; }
.content :deep(p:last-child) { margin-bottom: 0; }
.content :deep(ul), .content :deep(ol) { margin: 8px 0; padding-left: 24px; }
.content :deep(li) { margin: 4px 0; }
.content :deep(pre) { background: #1e1e1e; padding: 12px; border-radius: 8px; overflow-x: auto; }
.content :deep(code) { font-family: 'Fira Code', monospace; font-size: 14px; }

.input-area { display: flex; gap: 12px; padding: 16px; border-top: 1px solid #e0e0e0; }
.input-area .n-input { flex: 1; }
</style>
