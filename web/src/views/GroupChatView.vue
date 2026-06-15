<template>
  <div style="height: calc(100vh - 48px); display: flex;">
    <!-- Room List -->
    <div style="width: 240px; border-right: 1px solid #e8e8e8; padding: 12px; overflow-y: auto;">
      <n-space justify="space-between" style="margin-bottom: 12px;">
        <n-text strong>{{ t('groupchat.rooms') }}</n-text>
        <n-button size="small" type="primary" @click="showCreateRoom = true">+</n-button>
      </n-space>
      <div
        v-for="room in groupchatStore.rooms"
        :key="room.id"
        :class="['room-item', { active: groupchatStore.activeRoomId === room.id }]"
        @click="groupchatStore.selectRoom(room.id)"
      >
        <n-space justify="space-between" align="start">
          <div>
            <n-text strong>{{ room.name }}</n-text>
            <br />
            <n-text depth="3" style="font-size: 12px;">{{ room.agent_ids?.length || 0 }} {{ t('groupchat.agents') }}</n-text>
          </div>
          <n-button
            size="tiny"
            text
            type="error"
            @click.stop="handleDeleteRoom(room.id)"
            style="opacity: 0; transition: opacity 0.2s;"
            class="room-delete-btn"
          >
            <template #icon>
              <n-icon><close-outline /></n-icon>
            </template>
          </n-button>
        </n-space>
      </div>
    </div>

    <!-- Chat Area -->
    <div style="flex: 1; display: flex; flex-direction: column; min-width: 0;">
      <template v-if="groupchatStore.activeRoomId">
        <!-- Room Header -->
        <div style="padding: 12px 16px; border-bottom: 1px solid #e8e8e8; display: flex; justify-content: space-between; align-items: center;">
          <n-text strong style="font-size: 16px;">{{ activeRoom?.name }}</n-text>
          <n-button size="small" @click="showAgents = true">{{ t('groupchat.agents') }} ({{ groupchatStore.agents.length }})</n-button>
        </div>

        <!-- Messages -->
        <div class="messages" ref="messagesRef">
          <div v-for="msg in groupchatStore.messages" :key="msg.id" class="message" :class="msg.role">
            <div class="msg-bubble" :class="{ streaming: msg._streaming }">
              <div class="msg-bubble-header">
                <strong>{{ msg.sender }}</strong>
                <n-tag v-if="msg.role === 'agent'" size="tiny" type="success">AI</n-tag>
                <n-spin v-if="msg._streaming" size="small" />
                <span class="msg-time">{{ new Date(msg.timestamp).toLocaleTimeString() }}</span>
              </div>
              <div class="msg-bubble-content" v-html="msg.content ? renderMarkdown(msg.content) : '<span style=\'color:#999\'>...</span>'"></div>
            </div>
          </div>
        </div>

        <!-- Input -->
        <div class="input-area">
          <n-popover
            v-if="groupchatStore.agents.length > 0"
            trigger="click"
            placement="top-start"
          >
            <template #trigger>
              <n-button size="small" quaternary>@</n-button>
            </template>
            <n-list style="max-height: 200px; overflow-y: auto;">
              <n-list-item
                v-for="opt in agentMentionOptions"
                :key="opt.key"
                style="cursor: pointer;"
                @click="insertMention(opt.key)"
              >
                {{ opt.label }}
              </n-list-item>
            </n-list>
          </n-popover>
          <n-input
            v-model:value="inputValue"
            :placeholder="t('groupchat.typeMessage')"
            @keydown.enter="send"
            style="flex: 1;"
          />
          <n-button v-if="!replying" type="primary" @click="send">{{ t('groupchat.send') }}</n-button>
          <n-button v-else type="warning" @click="stopGeneration">⏹ {{ t('groupchat.stop') }}</n-button>
        </div>
      </template>
      <div v-else style="flex: 1; display: flex; align-items: center; justify-content: center;">
        <n-text depth="3">{{ t('groupchat.selectRoom') }}</n-text>
      </div>
    </div>

    <!-- Create Room Modal -->
    <n-modal v-model:show="showCreateRoom" :title="t('groupchat.newRoom')" preset="dialog" closable @close="showCreateRoom = false" style="width: 480px;">
      <n-form>
        <n-form-item :label="t('groupchat.roomName')">
          <n-input v-model:value="newRoom.name" />
        </n-form-item>
        <n-form-item :label="t('groupchat.description')">
          <n-input v-model:value="newRoom.description" />
        </n-form-item>
      </n-form>
      <template #action>
        <n-space justify="end">
          <n-button @click="showCreateRoom = false">{{ t('common.cancel') }}</n-button>
          <n-button type="primary" @click="createRoom">{{ t('common.create') }}</n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- Agents Modal -->
    <n-modal v-model:show="showAgents" :title="t('groupchat.agents')" style="width: 640px;" preset="card" closable @close="showAgents = false">
      <n-card>
        <n-list>
          <n-list-item v-for="a in groupchatStore.agents" :key="a.id">
            <n-space justify="space-between" align="start" style="width: 100%;">
              <n-space vertical style="flex: 1; min-width: 0;">
                <n-space align="center" :wrap="false">
                  <n-text strong style="font-size: 15px;">{{ a.name }}</n-text>
                  <n-tag v-if="a.profile" size="tiny" type="default">{{ a.profile }}</n-tag>
                  <n-tag size="tiny" type="info">{{ a.temperature !== undefined ? a.temperature.toFixed(1) : '0.7' }}</n-tag>
                </n-space>
                <n-text v-if="a.description" depth="3" style="font-size: 12px;">{{ a.description }}</n-text>
                <n-text v-if="a.system_prompt" depth="3" style="font-size: 11px; word-break: break-all;" :ellipsis="{ rows: 2 }">
                  {{ a.system_prompt }}
                </n-text>
              </n-space>
              <n-space :wrap="false">
                <n-button size="tiny" @click="editAgent(a)">{{ t('common.edit') }}</n-button>
                <n-button size="tiny" type="error" @click="handleRemoveAgent(a)">{{ t('common.remove') }}</n-button>
              </n-space>
            </n-space>
          </n-list-item>
        </n-list>
        <n-divider />
        <n-text strong>{{ editingAgentId ? t('groupchat.editAgent') : t('groupchat.addAgent') }}</n-text>
        <n-form style="margin-top: 8px;">
          <n-grid :cols="2" :x-gap="12">
            <n-form-item-gi :label="t('groupchat.agentName')">
              <n-input v-model:value="newAgent.name" :placeholder="t('groupchat.agentNamePlaceholder')" />
            </n-form-item-gi>
            <n-form-item-gi :label="t('groupchat.agentProfile')">
              <n-input v-model:value="newAgent.profile" :placeholder="t('groupchat.profilePlaceholder')" />
            </n-form-item-gi>
          </n-grid>
          <n-form-item :label="t('groupchat.agentDescription')">
            <n-input v-model:value="newAgent.description" :placeholder="t('groupchat.agentDescriptionPlaceholder')" />
          </n-form-item>
          <n-form-item :label="t('groupchat.systemPrompt')">
            <n-input v-model:value="newAgent.system_prompt" type="textarea" :rows="3" :placeholder="t('groupchat.systemPromptPlaceholder')" />
          </n-form-item>
          <n-grid :cols="2" :x-gap="12">
            <n-form-item-gi :label="t('groupchat.temperature')">
              <n-space align="center">
                <n-slider v-model:value="newAgent.temperature" :min="0" :max="2" :step="0.1" style="width: 120px;" />
                <n-text>{{ newAgent.temperature.toFixed(1) }}</n-text>
              </n-space>
            </n-form-item-gi>
            <n-form-item-gi :label="t('groupchat.tools')">
              <n-input v-model:value="newAgent.tools" :placeholder="t('groupchat.toolsPlaceholder')" />
            </n-form-item-gi>
          </n-grid>
          <n-space justify="end" style="margin-top: 8px;">
            <n-button v-if="editingAgentId" @click="cancelEdit">{{ t('common.cancel') }}</n-button>
            <n-button type="primary" @click="addAgent">{{ editingAgentId ? t('common.save') : t('groupchat.addAgent') }}</n-button>
          </n-space>
        </n-form>
      </n-card>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted, computed, nextTick } from 'vue'
import { useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { useGroupChatStore } from '@/stores/groupchat'
import { marked } from 'marked'
import hljs from 'highlight.js'
import 'highlight.js/styles/github-dark.css'
import { CloseOutline } from '@vicons/ionicons5'

const { t } = useI18n()
const message = useMessage()
const groupchatStore = useGroupChatStore()
const inputValue = ref('')
const showCreateRoom = ref(false)
const showAgents = ref(false)
const inviteCode = ref('')
const newRoom = reactive({ name: '', description: '' })
const editingAgentId = ref('')
const replying = ref(false)
const replyingAgent = ref('')
const messagesRef = ref<HTMLElement | null>(null)
let streamAbortController: AbortController | null = null
let streamBuffer = ''
let streamFlushTimer: ReturnType<typeof setTimeout> | null = null
const STREAM_FLUSH_INTERVAL = 80 // ms

const newAgent = reactive({
  name: '',
  profile: '',
  description: '',
  system_prompt: '',
  temperature: 0.7,
  tools: '',
})

const activeRoom = computed(() =>
  groupchatStore.rooms.find(r => r.id === groupchatStore.activeRoomId)
)

const agentMentionOptions = computed(() =>
  groupchatStore.agents.map(a => ({ label: a.name, key: a.name }))
)

// Configure marked with highlight.js
marked.setOptions({
  highlight: (code: string, lang: string) => {
    if (lang && hljs.getLanguage(lang)) {
      return hljs.highlight(code, { language: lang }).value
    }
    return hljs.highlightAuto(code).value
  },
})

function renderMarkdown(content: string): string {
  return marked.parse(content) as string
}

function flushStreamToMessage(msgId: string) {
  if (!streamBuffer) return
  const msg = groupchatStore.messages.find(m => m.id === msgId)
  if (msg) {
    msg.content += streamBuffer
  }
  streamBuffer = ''
  scrollToBottom()
}

function stopGeneration() {
  if (streamAbortController) {
    streamAbortController.abort()
    streamAbortController = null
  }
  if (streamFlushTimer) {
    clearTimeout(streamFlushTimer)
    streamFlushTimer = null
  }
  streamBuffer = ''
  replying.value = false
  // Mark remaining streaming messages
  for (const msg of groupchatStore.messages) {
    if (msg._streaming) {
      msg._streaming = false
      if (!msg.content) {
        msg.content = '*[Stopped by user]*'
      } else {
        msg.content += '\n\n*[Stopped by user]*'
      }
    }
  }
}

function scrollToBottom() {
  nextTick(() => {
    if (messagesRef.value) {
      messagesRef.value.scrollTop = messagesRef.value.scrollHeight
    }
  })
}

// Watch messages for auto-scroll
import { watch } from 'vue'
watch(() => groupchatStore.messages.length, () => scrollToBottom())

async function send() {
  if (!inputValue.value.trim()) return
  const content = inputValue.value
  inputValue.value = ''

  // Check if mentioning agents
  const mentions = groupchatStore.agents
    .filter(a => content.includes(`@${a.name}`))
    .map(a => ({ id: a.id, name: a.name }))

  await groupchatStore.sendMessage(content)
  scrollToBottom()

  // Stream agent replies via SSE
  if (mentions.length > 0) {
    streamAgentReplies(content, mentions)
  }
}

async function streamAgentReplies(content: string, mentions: { id: string, name: string }[]) {
  const roomId = groupchatStore.activeRoomId
  if (!roomId) return

  replying.value = true
  replyingAgent.value = mentions.map(m => m.name).join(', ')

  // Create abort controller for stop button
  streamAbortController = new AbortController()
  const { signal } = streamAbortController

  // Add streaming message placeholders
  const streamMsgs: Record<string, { id: string; content: string }> = {}
  for (const m of mentions) {
    const msgId = 'stream_' + Date.now() + '_' + m.id
    streamMsgs[m.id] = { id: msgId, content: '' }
    // Add a temporary message to the store
    groupchatStore.messages.push({
      id: msgId,
      room_id: roomId,
      sender: m.name,
      role: 'agent',
      content: '',
      timestamp: Date.now(),
      _streaming: true,
    })
    scrollToBottom()
  }

  try {
    const token = localStorage.getItem('auth_token')
    const headers: Record<string, string> = { 'Content-Type': 'application/json' }
    if (token) {
      headers['Authorization'] = `Bearer ${token}`
    }
    const response = await fetch(`/api/groupchat/rooms/${roomId}/stream`, {
      method: 'POST',
      headers,
      body: JSON.stringify({ content }),
      signal,
    })

    if (!response.ok) {
      // If SSE fails, fall back to polling
      response.body?.cancel()
      // Remove streaming placeholders
      groupchatStore.messages = groupchatStore.messages.filter(m => !m._streaming)
      pollForReplies(mentions.map(m => m.name))
      return
    }

    const reader = response.body?.getReader()
    if (!reader) {
      groupchatStore.messages = groupchatStore.messages.filter(m => !m._streaming)
      replying.value = false
      return
    }

    const decoder = new TextDecoder()
    let buffer = ''

    while (true) {
      const { done, value } = await reader.read()
      if (done) break

      buffer += decoder.decode(value, { stream: true })
      const lines = buffer.split('\n')
      buffer = lines.pop() || ''

      for (const line of lines) {
        if (!line.startsWith('data: ')) continue
        const data = line.slice(6).trim()
        if (data === '[DONE]') continue

        try {
          const event = JSON.parse(data)
          if (event.type === 'content' && event.agentId && streamMsgs[event.agentId]) {
            // Throttle: accumulate in buffer, flush every 80ms
            streamBuffer += event.content
            if (!streamFlushTimer) {
              streamFlushTimer = setTimeout(() => {
                flushStreamToMessage(streamMsgs[event.agentId].id)
                streamFlushTimer = null
              }, STREAM_FLUSH_INTERVAL)
            }
          } else if (event.type === 'done' && event.agentId && streamMsgs[event.agentId]) {
            // Flush remaining buffer
            if (streamFlushTimer) {
              clearTimeout(streamFlushTimer)
              streamFlushTimer = null
            }
            flushStreamToMessage(streamMsgs[event.agentId].id)
            // Mark streaming message as done
            const msg = groupchatStore.messages.find(m => m.id === streamMsgs[event.agentId].id)
            if (msg) {
              msg._streaming = false
              msg.id = event.messageId || msg.id
            }
            delete streamMsgs[event.agentId]
          } else if (event.type === 'error') {
            const msg = groupchatStore.messages.find(m => m._streaming && m.sender === event.agent)
            if (msg) {
              msg.content = `[Error: ${event.error}]`
              msg._streaming = false
            }
          }
        } catch (e) {
          // Ignore parse errors for partial data
        }
      }
    }
  } catch (e) {
    // Network error - remove streaming placeholders
    groupchatStore.messages = groupchatStore.messages.filter(m => !m._streaming)
  }

  // Clean up any remaining streaming messages
  groupchatStore.messages = groupchatStore.messages.filter(m => !m._streaming)
  replying.value = false

  // Refresh messages from server to get final state
  await groupchatStore.selectRoom(roomId)
}

async function pollForReplies(agentNames: string[]) {
  const roomId = groupchatStore.activeRoomId
  if (!roomId) return

  const maxPolls = 60 // 60 * 2s = 120s timeout
  for (let i = 0; i < maxPolls; i++) {
    await new Promise(resolve => setTimeout(resolve, 2000))
    await groupchatStore.selectRoom(roomId)
    const allMsgs = groupchatStore.messages
    const allReplied = agentNames.every(name =>
      allMsgs.some(m => m.sender === name && m.role === 'agent')
    )
    if (allReplied) break
  }
  replying.value = false
}

async function createRoom() {
  if (!newRoom.name) return
  await groupchatStore.createRoom({ ...newRoom })
  newRoom.name = ''
  newRoom.description = ''
  showCreateRoom.value = false
  message.success(t('groupchat.created'))
}

function editAgent(a: any) {
  editingAgentId.value = a.id
  newAgent.name = a.name
  newAgent.profile = a.profile
  newAgent.description = a.description
  newAgent.system_prompt = a.system_prompt || ''
  newAgent.temperature = a.temperature || 0.7
  newAgent.tools = a.tools || ''
}

function cancelEdit() {
  editingAgentId.value = ''
  newAgent.name = ''
  newAgent.profile = ''
  newAgent.description = ''
  newAgent.system_prompt = ''
  newAgent.temperature = 0.7
  newAgent.tools = ''
}

async function addAgent() {
  if (!newAgent.name) return
  if (editingAgentId.value) {
    // Update existing agent
    await groupchatStore.updateAgent(editingAgentId.value, { ...newAgent })
    cancelEdit()
    message.success(t('groupchat.agentUpdated'))
  } else {
    // Create new agent
    await groupchatStore.addAgent({
      agent_id: 'agent_' + Date.now(),
      name: newAgent.name,
      profile: newAgent.profile,
      description: newAgent.description,
      system_prompt: newAgent.system_prompt,
      temperature: newAgent.temperature,
      tools: newAgent.tools,
    })
    newAgent.name = ''
    newAgent.profile = ''
    newAgent.description = ''
    newAgent.system_prompt = ''
    newAgent.temperature = 0.7
    newAgent.tools = ''
    message.success(t('groupchat.agentAdded'))
  }
  // Refresh agents list
  if (groupchatStore.activeRoomId) {
    await groupchatStore.selectRoom(groupchatStore.activeRoomId)
  }
}

async function handleDeleteRoom(roomId: string) {
  if (!confirm(t('groupchat.confirmDeleteRoom'))) return
  await groupchatStore.deleteRoom(roomId)
  message.success(t('groupchat.roomDeleted'))
}

async function handleRemoveAgent(agent: any) {
  if (!confirm(t('groupchat.confirmRemoveAgent', { name: agent.name }))) return
  await groupchatStore.removeAgent(agent.id)
  message.success(t('groupchat.agentRemoved'))
}

function insertMention(name: string) {
  inputValue.value += `@${name} `
}

onMounted(() => groupchatStore.loadRooms())
</script>

<style scoped>
.room-item {
  padding: 8px 12px;
  border-radius: 6px;
  cursor: pointer;
  margin-bottom: 4px;
}

.room-item:hover {
  background: #f0f0f0;
}

.room-item.active {
  background: #e8f4ff;
}

.room-item:hover .room-delete-btn {
  opacity: 1 !important;
}

.messages {
  flex: 1;
  overflow-y: auto;
  padding: 16px;
}

.message {
  margin-bottom: 12px;
  display: flex;
  flex-direction: column;
}

/* User messages - right aligned */
.message:not(.agent):not(.system) {
  align-items: flex-end;
}

/* Agent messages - left aligned */
.message.agent {
  align-items: flex-start;
}

/* System messages - center */
.message.system {
  align-items: center;
}

.msg-bubble {
  max-width: 85%;
  border-radius: 12px;
  padding: 10px 14px;
  box-shadow: 0 1px 2px rgba(0,0,0,0.06);
}

/* User bubble - white/gray */
.message:not(.agent):not(.system) .msg-bubble {
  background: #ffffff;
  border: 1px solid #e8e8e8;
}

/* Agent bubble - green tint */
.message.agent .msg-bubble {
  background: #e8f5e9;
  border: 1px solid #c8e6c9;
}

/* Streaming bubble - pulsing border */
.msg-bubble.streaming {
  border-style: dashed;
  animation: pulse-border 1.5s ease-in-out infinite;
}

@keyframes pulse-border {
  0%, 100% { border-color: #c8e6c9; }
  50% { border-color: #66bb6a; }
}

/* System bubble - orange tint */
.message.system .msg-bubble {
  background: #fff3e0;
  border: 1px solid #ffe0b2;
  font-style: italic;
}

.msg-bubble-header {
  display: flex;
  gap: 8px;
  align-items: center;
  margin-bottom: 6px;
  font-size: 13px;
}

.message:not(.agent):not(.system) .msg-bubble-header {
  justify-content: flex-end;
}

.message.agent .msg-bubble-header {
  justify-content: flex-start;
}

.msg-bubble-header strong {
  color: #333;
}

.msg-time {
  font-size: 11px;
  color: #999;
}

.msg-bubble-content {
  word-break: break-word;
  overflow-wrap: break-word;
  line-height: 1.6;
}

.msg-bubble-content :deep(pre) {
  background: #1e1e1e;
  color: #d4d4d4;
  padding: 12px;
  border-radius: 6px;
  overflow-x: auto;
  max-width: 100%;
  margin: 8px 0;
}

.msg-bubble-content :deep(code) {
  font-family: 'Fira Code', monospace;
  font-size: 13px;
}

.msg-bubble-content :deep(p) {
  margin: 4px 0;
}

.msg-bubble-content :deep(ul), .msg-bubble-content :deep(ol) {
  margin: 4px 0;
  padding-left: 20px;
}

.msg-bubble-content :deep(li) {
  margin: 2px 0;
}

.input-area {
  display: flex;
  gap: 8px;
  padding: 12px;
  border-top: 1px solid #e8e8e8;
}
</style>
