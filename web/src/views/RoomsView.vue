<template>
  <div class="rooms-container">
    <!-- Room List -->
    <div class="room-sidebar">
      <div class="sidebar-head">
        <div style="display: flex; align-items: center; gap: 4px;">
          <n-text strong>{{ t('rooms.title') }}</n-text>
          <n-button text size="tiny" @click="handleRefresh" :loading="roomsStore.loading">
            <template #icon><n-icon><RefreshOutline /></n-icon></template>
          </n-button>
        </div>
        <n-button size="tiny" type="primary" @click="openCreate">
          <template #icon><n-icon><AddOutline /></n-icon></template>
        </n-button>
      </div>

      <div class="room-list">
        <n-empty v-if="!roomsStore.loading && roomsStore.rooms.length === 0" size="small" :description="t('rooms.empty')" style="margin-top: 40px;" />
        <div
          v-for="room in roomsStore.rooms"
          :key="room.id"
          :class="['room-item', { active: roomsStore.activeRoomId === room.id }]"
          @click="onRoomClick(room.id)"
        >
          <div class="room-item-main">
            <div class="room-item-name" :title="room.name">{{ room.name || room.id }}</div>
            <div class="room-item-meta">
              <span class="member-chips">
                <span v-for="m in room.members" :key="m" class="mini-chip">{{ m }}</span>
              </span>
            </div>
          </div>
          <n-popconfirm v-if="roomsStore.activeRoomId === room.id" @positive-click="handleDeleteRoom(room.id)">
            <template #trigger>
              <n-button size="tiny" text type="error" class="room-delete-btn" @click.stop>
                <template #icon><n-icon><CloseOutline /></n-icon></template>
              </n-button>
            </template>
            {{ t('rooms.confirmDelete') }}
          </n-popconfirm>
        </div>
      </div>
    </div>

    <!-- Chat Area -->
    <div class="chat-main">
      <template v-if="roomsStore.activeRoomId && activeRoom">
        <!-- Header -->
        <div class="chat-header">
          <div style="display: flex; align-items: center; gap: 10px; flex: 1; min-width: 0;">
            <div style="display: flex; align-items: center; gap: 6px; min-width: 0;">
              <n-text strong style="font-size: 15px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis;">
                {{ activeRoom.name || activeRoom.id }}
              </n-text>
              <n-text depth="3" style="font-size: 12px; white-space: nowrap;">
                {{ activeRoom.max_rounds }} {{ t('rooms.rounds') }} · {{ activeRoom.max_messages }} {{ t('rooms.msgsCap') }}
              </n-text>
            </div>
            <div class="member-tags">
              <n-tag v-for="m in activeRoom.members" :key="m" size="small" :bordered="false" :color="{ color: tagColor(m), textColor: '#333' }">
                {{ m }}
              </n-tag>
            </div>
          </div>
          <div style="display: flex; align-items: center; gap: 8px;">
            <n-button size="tiny" quaternary @click="openEdit">
              <template #icon><n-icon><CreateOutline /></n-icon></template>
            </n-button>
            <n-popconfirm @positive-click="handleDeleteRoom(activeRoom.id)">
              <template #trigger>
                <n-button size="tiny" quaternary type="error">
                  <template #icon><n-icon><TrashOutline /></n-icon></template>
                </n-button>
              </template>
              {{ t('rooms.confirmDelete') }}
            </n-popconfirm>
          </div>
        </div>

        <!-- Topic -->
        <div v-if="activeRoom.topic" class="room-topic">{{ activeRoom.topic }}</div>

        <!-- Messages -->
        <div class="messages" ref="messagesRef">
          <n-empty v-if="!roomsStore.loading && roomsStore.messages.length === 0 && !roomsStore.sending" size="small" :description="t('rooms.noMessages')" style="margin-top: 60px;" />
          <div
            v-for="msg in roomsStore.messages"
            :key="msg.id"
            class="message"
            :class="[isUserMsg(msg) ? 'user' : 'bot', { system: msg.from === '@system' }]"
          >
            <template v-if="msg.from === '@system'">
              <div class="system-notice">{{ msg.content }}</div>
            </template>
            <template v-else>
              <div class="avatar" :style="{ background: avatarColor(msg.from) }">
                {{ avatarText(msg.from) }}
              </div>
              <div class="message-body">
                <div class="message-header">
                  <n-text strong class="sender-name">{{ displayName(msg.from) }}</n-text>
                  <n-tag v-if="!isUserMsg(msg)" size="tiny" type="success" :bordered="false">{{ t('rooms.bot') }}</n-tag>
                  <span class="message-time">{{ formatTime(msg.timestamp) }}</span>
                  <span v-if="msg.content.startsWith('⚠️')" class="send-error">{{ t('rooms.sendFailed') }}</span>
                </div>
                <div class="message-bubble" :class="[isUserMsg(msg) ? 'bubble-user' : 'bubble-bot', { 'bubble-error': msg.content.startsWith('⚠️') }]">
                  <template v-if="!isUserMsg(msg)">
                    <ReasoningContent :content="msg.content" :streaming="false" />
                  </template>
                  <div v-else class="bubble-content" v-html="renderMarkdown(msg.content)"></div>
                </div>
              </div>
            </template>
          </div>

          <!-- Sending indicator -->
          <div v-if="roomsStore.sending" class="typing-indicator">
            <div class="typing-dots"><span></span><span></span><span></span></div>
            <span class="typing-text">{{ t('rooms.botsReplying') }}</span>
            <n-button size="tiny" text type="error" @click="roomsStore.cancelSend()">
              <template #icon><n-icon><CloseOutline /></n-icon></template>
            </n-button>
          </div>
        </div>

        <!-- Input -->
        <div class="input-area">
          <div class="input-hint">
            {{ t('rooms.inputHint') }}
            <span v-if="activeTarget" class="target-chip">→ {{ activeTarget }}</span>
          </div>
          <div class="input-wrapper">
            <n-input
              v-model:value="inputValue"
              type="textarea"
              :autosize="{ minRows: 1, maxRows: 6 }"
              :placeholder="t('rooms.typeMessage')"
              :disabled="roomsStore.sending"
              class="chat-input"
              @keydown="onKeydown"
              @input="onInput"
            />
            <div v-if="showMention && filteredMentions.length" class="mention-popup">
              <div
                v-for="(opt, idx) in filteredMentions"
                :key="opt"
                :class="['mention-item', { active: idx === mentionActiveIdx }]"
                @mousedown.prevent="selectMention(opt)"
                @mouseenter="mentionActiveIdx = idx"
              >
                <span class="mention-avatar" :style="{ background: avatarColor(opt) }">{{ avatarText(opt) }}</span>
                <span class="mention-name">{{ opt }}</span>
              </div>
            </div>
            <button
              class="send-btn-inline"
              :disabled="roomsStore.sending || !inputValue.trim()"
              @click="send()"
              @mousedown.prevent
              :title="t('rooms.send')"
            >
              <svg viewBox="0 0 24 24" width="18" height="18" fill="currentColor">
                <path d="M3.4 20.4l17.45-7.48a1 1 0 000-1.84L3.4 3.6a.993.993 0 00-1.39.91L2 9.12c0 .5.37.93.87.99L17 12 2.87 13.88c-.5.07-.87.5-.87 1l.01 4.61c0 .71.73 1.2 1.39.91z"/>
              </svg>
            </button>
          </div>
        </div>
      </template>
      <div v-else class="empty-chat">
        <n-text depth="3">{{ t('rooms.selectRoom') }}</n-text>
      </div>
    </div>

    <!-- Create/Edit Room Modal -->
    <n-modal v-model:show="showEditor" preset="card" class="modal-responsive" style="width: 560px; max-width: 96vw;" closable @close="closeEditor">
      <template #header>{{ editingId ? t('rooms.editRoom') : t('rooms.newRoom') }}</template>
      <div class="editor-body">
        <n-form label-placement="top">
          <n-form-item :label="t('rooms.roomName')">
            <n-input v-model:value="form.name" :placeholder="t('rooms.roomNamePlaceholder')" />
          </n-form-item>
          <n-form-item :label="t('rooms.topic')">
            <n-input v-model:value="form.topic" :placeholder="t('rooms.topicPlaceholder')" />
          </n-form-item>
          <n-form-item :label="t('rooms.members')">
            <div class="member-picker">
              <div v-if="botsStore.loading" class="picker-loading">
                <n-spin size="small" /> {{ t('rooms.loadingBots') }}
              </div>
              <n-empty v-else-if="botsStore.bots.length === 0" size="small" :description="t('rooms.noBots')" />
              <template v-else>
                <div
                  v-for="b in botsStore.bots"
                  :key="b.name"
                  :class="['member-option', { picked: form.members.includes(b.name) }]"
                  @click="toggleMember(b.name)"
                >
                  <span class="member-avatar" :style="{ background: avatarColor(b.name) }">{{ avatarText(b.name) }}</span>
                  <span class="member-option-name">{{ b.name }}</span>
                  <span v-if="b.title" class="member-option-title">{{ b.title }}</span>
                  <n-icon v-if="form.members.includes(b.name)" size="14" color="#18a058"><CheckmarkOutline /></n-icon>
                </div>
                <div class="member-count">{{ t('rooms.memberCount', { n: form.members.length }) }}</div>
              </template>
            </div>
          </n-form-item>
          <n-grid :cols="2" :x-gap="12">
            <n-form-item-gi :label="t('rooms.maxRounds')">
              <div style="display: flex; align-items: center; gap: 10px;">
                <n-slider v-model:value="form.max_rounds" :min="1" :max="6" :step="1" style="flex: 1;" />
                <n-text style="min-width: 16px;">{{ form.max_rounds }}</n-text>
              </div>
            </n-form-item-gi>
            <n-form-item-gi :label="t('rooms.maxMessages')">
              <div style="display: flex; align-items: center; gap: 10px;">
                <n-slider v-model:value="form.max_messages" :min="4" :max="40" :step="2" style="flex: 1;" />
                <n-text style="min-width: 24px;">{{ form.max_messages }}</n-text>
              </div>
            </n-form-item-gi>
          </n-grid>
        </n-form>
      </div>
      <template #action>
        <div style="display: flex; justify-content: flex-end; gap: 8px;">
          <n-button @click="closeEditor">{{ t('common.cancel') }}</n-button>
          <n-button type="primary" :loading="saving" :disabled="!canSave" @click="saveEditor">
            {{ editingId ? t('common.save') : t('common.create') }}
          </n-button>
        </div>
      </template>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, nextTick, watch } from 'vue'
import { useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { marked } from 'marked'
import {
  AddOutline,
  CloseOutline,
  CreateOutline,
  TrashOutline,
  RefreshOutline,
  CheckmarkOutline,
} from '@vicons/ionicons5'
import { useRoomsStore } from '@/stores/rooms'
import { useBotsStore } from '@/stores/bots'
import type { RoomMessage } from '@/api/rooms'
import ReasoningContent from '@/components/ReasoningContent.vue'

const { t } = useI18n()
const message = useMessage()
const roomsStore = useRoomsStore()
const botsStore = useBotsStore()

const inputValue = ref('')
const messagesRef = ref<HTMLElement | null>(null)
const showEditor = ref(false)
const editingId = ref<string | null>(null)
const saving = ref(false)

const form = reactive({
  name: '',
  topic: '',
  members: [] as string[],
  max_rounds: 3,
  max_messages: 10,
})

const activeRoom = computed(() => roomsStore.getActiveRoom())

// ---------- mention targeting ----------
const showMention = ref(false)
const mentionActiveIdx = ref(0)
const mentionQuery = ref('')

const filteredMentions = computed(() => {
  const members = activeRoom.value?.members || []
  const q = mentionQuery.value.toLowerCase()
  const list = q ? members.filter(m => m.toLowerCase().includes(q)) : members
  return list.slice(0, 8)
})

function onInput() {
  const val = inputValue.value
  const caret = (document.activeElement as HTMLTextAreaElement)?.selectionStart ?? val.length
  const uptoCaret = val.slice(0, caret)
  const m = uptoCaret.match(/@(\S*)$/)
  if (m && !uptoCaret.slice(0, uptoCaret.length - m[0].length).endsWith('@@')) {
    showMention.value = true
    mentionQuery.value = m[1] || ''
    if (mentionActiveIdx.value >= filteredMentions.value.length) {
      mentionActiveIdx.value = 0
    }
  } else {
    showMention.value = false
  }
}

function selectMention(name: string) {
  const el = document.activeElement as HTMLTextAreaElement
  const caret = el?.selectionStart ?? inputValue.value.length
  const uptoCaret = inputValue.value.slice(0, caret)
  const m = uptoCaret.match(/@(\S*)$/)
  if (m) {
    const start = caret - m[0].length
    inputValue.value = inputValue.value.slice(0, start) + '@' + name + ' ' + inputValue.value.slice(caret)
    nextTick(() => {
      const pos = start + name.length + 2
      el?.setSelectionRange(pos, pos)
    })
  }
  showMention.value = false
}

const activeTarget = computed(() => {
  if (!activeRoom.value) return ''
  const m = inputValue.value.match(/@(\S+)/)
  if (!m) return ''
  const name = m[1].replace(/[^\w-]/g, '')
  return activeRoom.value.members.includes(name) ? name : ''
})

function onKeydown(e: KeyboardEvent) {
  if (showMention.value && filteredMentions.value.length) {
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      mentionActiveIdx.value = (mentionActiveIdx.value + 1) % filteredMentions.value.length
      return
    }
    if (e.key === 'ArrowUp') {
      e.preventDefault()
      mentionActiveIdx.value = (mentionActiveIdx.value - 1 + filteredMentions.value.length) % filteredMentions.value.length
      return
    }
    if (e.key === 'Enter' || e.key === 'Tab') {
      e.preventDefault()
      selectMention(filteredMentions.value[mentionActiveIdx.value])
      return
    }
    if (e.key === 'Escape') {
      showMention.value = false
      return
    }
  }
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault()
    send()
  }
}

// ---------- send ----------
async function send() {
  const text = inputValue.value.trim()
  if (!text || roomsStore.sending || !activeRoom.value) return
  const target = activeTarget.value || undefined
  inputValue.value = ''
  showMention.value = false
  try {
    const res = await roomsStore.sendMessage(text, target)
    if (res?.needs_user) {
      roomsStore.messages.push({
        id: 'sys_' + Date.now(),
        from: '@system',
        content: t('rooms.needsUser'),
        timestamp: Date.now(),
      })
    }
  } catch (e) {
    message.error(t('rooms.sendFailed'))
    // restore draft so the user doesn't lose their message
    if (!inputValue.value) inputValue.value = text
  }
}

// ---------- room ops ----------
function onRoomClick(id: string) {
  roomsStore.selectRoom(id)
}

async function handleRefresh() {
  await Promise.all([roomsStore.loadRooms(), refreshActive()])
}

async function refreshActive() {
  if (roomsStore.activeRoomId) {
    await roomsStore.refreshMessages()
  }
}

function openCreate() {
  editingId.value = null
  form.name = ''
  form.topic = ''
  form.members = []
  form.max_rounds = 3
  form.max_messages = 10
  showEditor.value = true
  if (botsStore.bots.length === 0) {
    botsStore.loadBots()
  }
}

function openEdit() {
  if (!activeRoom.value) return
  editingId.value = activeRoom.value.id
  form.name = activeRoom.value.name
  form.topic = activeRoom.value.topic || ''
  form.members = [...activeRoom.value.members]
  form.max_rounds = activeRoom.value.max_rounds
  form.max_messages = activeRoom.value.max_messages
  showEditor.value = true
  if (botsStore.bots.length === 0) {
    botsStore.loadBots()
  }
}

function closeEditor() {
  showEditor.value = false
}

const canSave = computed(() => form.members.length >= 2 && form.members.length <= 6 && !!form.name.trim())

function toggleMember(name: string) {
  const idx = form.members.indexOf(name)
  if (idx >= 0) {
    form.members.splice(idx, 1)
  } else if (form.members.length < 6) {
    form.members.push(name)
  } else {
    message.warning(t('rooms.maxMembers'))
  }
}

async function saveEditor() {
  if (!canSave.value || saving.value) return
  saving.value = true
  const data = {
    name: form.name.trim(),
    topic: form.topic.trim(),
    members: [...form.members],
    max_rounds: form.max_rounds,
    max_messages: form.max_messages,
  }
  try {
    if (editingId.value) {
      await roomsStore.updateRoom(editingId.value, data)
      message.success(t('rooms.updated'))
    } else {
      const room = await roomsStore.createRoom(data)
      message.success(t('rooms.created'))
      await roomsStore.selectRoom(room.id)
    }
    showEditor.value = false
  } catch (e) {
    message.error(e instanceof Error ? e.message : String(e))
  } finally {
    saving.value = false
  }
}

async function handleDeleteRoom(id: string) {
  try {
    await roomsStore.deleteRoom(id)
    message.success(t('rooms.deleted'))
  } catch (e) {
    message.error(e instanceof Error ? e.message : String(e))
  }
}

// ---------- helpers ----------
function isUserMsg(msg: RoomMessage): boolean {
  return msg.from === '@user' || msg.from.startsWith('user:')
}

function displayName(from: string): string {
  if (from === '@user') return t('rooms.you')
  if (from === '@system') return 'System'
  return from
}

function avatarText(from: string): string {
  if (from === '@user') return '我'
  const n = from.replace(/^@/, '')
  return n ? n.slice(0, 1).toUpperCase() : 'B'
}

function avatarColor(from: string): string {
  const hue = (hashCode(from) % 360 + 360) % 360
  return `hsl(${hue}, 55%, 78%)`
}

function tagColor(name: string): string {
  const hue = (hashCode(name) % 360 + 360) % 360
  return `hsl(${hue}, 60%, 90%)`
}

function hashCode(s: string): number {
  let h = 0
  for (let i = 0; i < s.length; i++) {
    h = (h * 31 + s.charCodeAt(i)) | 0
  }
  return h
}

// ---------- markdown ----------
const mdCache = new Map<string, string>()
const MD_CACHE_LIMIT = 200
function renderMarkdown(content: string): string {
  const cached = mdCache.get(content)
  if (cached !== undefined) return cached
  const html = marked.parse(content) as string
  if (mdCache.size >= MD_CACHE_LIMIT) {
    const keys = mdCache.keys()
    for (let i = 0; i < MD_CACHE_LIMIT / 2; i++) {
      const r = keys.next()
      if (r.done) break
      mdCache.delete(r.value)
    }
  }
  mdCache.set(content, html)
  return html
}

// ---------- time ----------
function toDate(ts: string | number): Date {
  if (typeof ts === 'number') {
    return ts > 1e12 ? new Date(ts) : new Date(ts * 1000)
  }
  const d = new Date(ts)
  return isNaN(d.getTime()) ? new Date(0) : d
}

function formatTime(timestamp: string | number): string {
  if (!timestamp) return ''
  const date = toDate(timestamp)
  if (date.getTime() === 0) return ''
  const now = new Date()
  const isToday = date.toDateString() === now.toDateString()
  if (isToday) {
    return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  }
  return (
    date.toLocaleDateString([], { month: 'short', day: 'numeric' }) +
    ' ' +
    date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  )
}

// ---------- lifecycle ----------
watch(
  () => roomsStore.messages.length,
  () => {
    nextTick(() => {
      if (messagesRef.value) {
        messagesRef.value.scrollTop = messagesRef.value.scrollHeight
      }
    })
  }
)

onMounted(() => {
  roomsStore.loadRooms()
  botsStore.loadBots()
})
</script>

<style scoped>
.rooms-container {
  display: flex;
  height: calc(100vh - 48px);
  min-height: 0;
}

.room-sidebar {
  width: 240px;
  border-right: 1px solid #e8e8e8;
  display: flex;
  flex-direction: column;
  flex-shrink: 0;
  padding: 12px;
}

.sidebar-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 12px;
}

.room-list {
  flex: 1;
  overflow-y: auto;
  min-height: 0;
}

.room-item {
  padding: 8px 12px;
  border-radius: 6px;
  cursor: pointer;
  margin-bottom: 4px;
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 6px;
}

.room-item:hover {
  background: #f0f0f0;
}

.room-item.active {
  background: #e8f4ff;
}

.room-item-main {
  min-width: 0;
}

.room-item-name {
  font-size: 13px;
  font-weight: 600;
  color: #333;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.room-item-meta {
  margin-top: 2px;
}

.member-chips {
  display: flex;
  flex-wrap: wrap;
  gap: 3px;
}

.mini-chip {
  font-size: 10px;
  background: #f0f0f0;
  color: #666;
  padding: 0 6px;
  border-radius: 8px;
}

.room-delete-btn {
  opacity: 0.4;
  transition: opacity 0.2s;
}

.room-item:hover .room-delete-btn {
  opacity: 1;
}

.chat-main {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-width: 0;
}

.chat-header {
  flex-shrink: 0;
  padding: 10px 16px;
  border-bottom: 1px solid #e8e8e8;
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
}

.member-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  overflow: hidden;
  max-width: 60%;
}

.room-topic {
  flex-shrink: 0;
  padding: 6px 16px;
  font-size: 12px;
  color: #888;
  border-bottom: 1px dashed #eee;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.messages {
  flex: 1;
  overflow-y: auto;
  padding: 20px 24px;
  padding-bottom: 90px;
  min-height: 0;
}

.message {
  display: flex;
  gap: 10px;
  margin-bottom: 16px;
  animation: fadeIn 0.25s ease;
}

@keyframes fadeIn {
  from { opacity: 0; transform: translateY(6px); }
  to { opacity: 1; transform: translateY(0); }
}

.message.user {
  flex-direction: row-reverse;
}

.message.system {
  justify-content: center;
}

.system-notice {
  font-size: 12px;
  color: #b45309;
  background: #fef3c7;
  border: 1px solid #fde68a;
  border-radius: 6px;
  padding: 6px 14px;
  max-width: 80%;
  text-align: center;
}

.avatar {
  width: 32px;
  height: 32px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 14px;
  font-weight: 600;
  color: #333;
  flex-shrink: 0;
}

.message-body {
  max-width: 72%;
  min-width: 0;
}

.message.user .message-body {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
}

.message-header {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 4px;
}

.message.user .message-header {
  flex-direction: row-reverse;
}

.sender-name {
  font-size: 13px;
}

.message-time {
  font-size: 11px;
  color: #aaa;
}

.send-error {
  font-size: 11px;
  color: #d03050;
}

.message-bubble {
  padding: 8px 12px;
  border-radius: 10px;
  font-size: 13.5px;
  line-height: 1.6;
  word-break: break-word;
}

.bubble-user {
  background: #d9f0ff;
  color: #1a1a1a;
  border-top-right-radius: 2px;
}

.bubble-bot {
  background: #f5f5f5;
  color: #1a1a1a;
  border-top-left-radius: 2px;
}

.bubble-error {
  border: 1px solid #f0a0b0;
}

.bubble-content :deep(p) {
  margin: 4px 0;
}

.bubble-content :deep(pre) {
  background: #0f172a;
  color: #e2e8f0;
  padding: 10px 12px;
  border-radius: 8px;
  overflow-x: auto;
  margin: 8px 0;
  font-size: 12.5px;
}

.bubble-content :deep(code) {
  font-family: 'SFMono-Regular', Consolas, 'Liberation Mono', Menlo, monospace;
}

.bubble-content :deep(a) {
  color: #2080f0;
}

/* typing indicator */
.typing-indicator {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 6px 4px;
  margin-top: 4px;
}

.typing-dots {
  display: flex;
  gap: 4px;
  background: #f5f5f5;
  padding: 8px 12px;
  border-radius: 12px;
}

.typing-dots span {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: #999;
  animation: bounce 1.2s infinite ease-in-out;
}

.typing-dots span:nth-child(2) { animation-delay: 0.15s; }
.typing-dots span:nth-child(3) { animation-delay: 0.3s; }

@keyframes bounce {
  0%, 60%, 100% { transform: translateY(0); opacity: 0.5; }
  30% { transform: translateY(-5px); opacity: 1; }
}

.typing-text {
  font-size: 12px;
  color: #888;
}

/* input */
.input-area {
  flex-shrink: 0;
  padding: 10px 24px 16px;
  border-top: 1px solid #eee;
}

.input-hint {
  font-size: 11px;
  color: #aaa;
  margin-bottom: 6px;
  display: flex;
  align-items: center;
  gap: 8px;
}

.target-chip {
  font-size: 11px;
  background: #e8f4ff;
  color: #2080f0;
  padding: 1px 8px;
  border-radius: 8px;
}

.input-wrapper {
  position: relative;
}

.chat-input :deep(textarea) {
  padding-right: 44px !important;
}

.send-btn-inline {
  position: absolute;
  right: 6px;
  bottom: 6px;
  width: 32px;
  height: 32px;
  border: none;
  border-radius: 8px;
  background: #2080f0;
  color: #fff;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: background 0.2s;
}

.send-btn-inline:hover {
  background: #1a6fd0;
}

.send-btn-inline:disabled {
  background: #c8c8c8;
  cursor: not-allowed;
}

/* mention popup */
.mention-popup {
  position: absolute;
  bottom: 100%;
  left: 12px;
  width: 220px;
  max-height: 240px;
  overflow-y: auto;
  background: #fff;
  border: 1px solid #e0e0e0;
  border-radius: 8px;
  box-shadow: 0 6px 20px rgba(0, 0, 0, 0.12);
  z-index: 20;
  padding: 4px;
}

.mention-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 8px;
  border-radius: 6px;
  cursor: pointer;
}

.mention-item.active {
  background: #f0f0f0;
}

.mention-avatar {
  width: 22px;
  height: 22px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 11px;
  font-weight: 600;
  color: #333;
  flex-shrink: 0;
}

.mention-name {
  font-size: 13px;
  color: #333;
}

.empty-chat {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
}

/* editor */
.editor-body {
  max-height: 65vh;
  overflow-y: auto;
  padding-right: 4px;
}

.member-picker {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.picker-loading {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  color: #888;
  padding: 12px 0;
}

.member-option {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 10px;
  border: 1px solid #e5e5e5;
  border-radius: 8px;
  cursor: pointer;
  transition: all 0.15s;
}

.member-option:hover {
  border-color: #2080f0;
  background: #f6fafe;
}

.member-option.picked {
  border-color: #18a058;
  background: #f0faf2;
}

.member-avatar {
  width: 26px;
  height: 26px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 12px;
  font-weight: 600;
  color: #333;
  flex-shrink: 0;
}

.member-option-name {
  font-size: 13px;
  font-weight: 600;
  color: #333;
}

.member-option-title {
  font-size: 11px;
  color: #999;
  flex: 1;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.member-count {
  font-size: 11px;
  color: #999;
  text-align: right;
}

@media (max-width: 768px) {
  .rooms-container {
    height: calc(100vh - 80px);
  }

  .room-sidebar {
    width: 180px;
    padding: 8px;
  }

  .message-body {
    max-width: 85%;
  }

  .member-tags {
    display: none;
  }
}
</style>
