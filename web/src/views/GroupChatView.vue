<template>
  <div class="groupchat-container">
    <!-- Room List -->
    <div class="room-sidebar" :class="{ 'mobile-expanded': mobileRoomsExpanded }">
      <!-- Mobile drag handle -->
      <div class="mobile-room-handle" @click="mobileRoomsExpanded = !mobileRoomsExpanded">
        <div class="handle-bar"></div>
      </div>
      <div v-show="!isMobile || mobileRoomsExpanded" style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px;">
        <div style="display: flex; align-items: center; gap: 4px;">
          <n-text strong>{{ t('groupchat.rooms') }}</n-text>
          <n-button text size="tiny" @click="handleRefresh" :loading="groupchatStore.loading">
            <template #icon><n-icon><RefreshOutline /></n-icon></template>
          </n-button>
        </div>
        <n-button size="tiny" type="primary" @click="showCreateRoom = true">+</n-button>
      </div>
      <div class="room-list" v-show="!isMobile || mobileRoomsExpanded">
        <div
          v-for="room in groupchatStore.rooms"
          :key="room.id"
          :class="['room-item', { active: groupchatStore.activeRoomId === room.id }]"
          @click="onRoomClick(room.id)"
        >
          <div style="display: flex; justify-content: space-between; align-items: flex-start;">
            <div>
              <n-text strong>{{ room.name }}</n-text>
              <br />
              <n-text depth="3" style="font-size: 12px;">{{ room.agent_ids?.length || 0 }} {{ t('groupchat.agents') }}</n-text>
            </div>
            <div style="display: flex; gap: 4px; align-items: center;">
              <n-button
                v-if="groupchatStore.activeRoomId === room.id"
                size="tiny"
                text
                type="info"
                @click.stop="groupchatStore.selectRoom(room.id); startEditRoomName()">
                <template #icon><n-icon size="14"><create-outline /></n-icon></template>
              </n-button>
            <n-popconfirm @positive-click="handleDeleteRoom(room.id)">
              <template #trigger>
                <n-button
                  size="tiny"
                  text
                  type="error"
                  @click.stop
                  style="transition: opacity 0.2s;"
                  class="room-delete-btn"
                >
                  <template #icon>
                    <n-icon><close-outline /></n-icon>
                  </template>
                </n-button>
              </template>
              {{ t('groupchat.confirmDeleteRoom') }}
            </n-popconfirm>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- Chat Area -->
    <div class="chat-main">
      <template v-if="groupchatStore.activeRoomId">
        <!-- Room Header -->
        <div class="chat-header">
          <div style="display: flex; align-items: center; flex: 1; min-width: 0;">
            <template v-if="editingRoomName">
            <div style="display: flex; align-items: center; gap: 8px;">
              <n-input
                v-model:value="editRoomNameValue"
                size="small"
                style="width: 200px;"
                @keyup.enter="saveRoomName"
                @keyup.escape="editingRoomName = false"
                ref="editNameInput"
              />
              <n-button size="tiny" type="primary" @click="saveRoomName">{{ t('common.save') }}</n-button>
              <n-button size="tiny" @click="editingRoomName = false">{{ t('common.cancel') }}</n-button>
            </div>
          </template>
          <template v-else>
            <div style="display: flex; align-items: center; gap: 4px; cursor: pointer;" @click="startEditRoomName">
              <n-text strong style="font-size: 16px;" :title="activeRoom?.name">{{ activeRoom?.name }}</n-text>
              <n-icon size="14" style="color: #999;"><create-outline /></n-icon>
            </div>
          </template>
          </div>
          <div style="display: flex; align-items: center; gap: 14px;">
            <n-button size="tiny" quaternary @click="showInviteCode = true">
              <template #icon><n-icon><documents-outline /></n-icon></template>
            </n-button>
            <n-button size="tiny" @click="showAgents = true">{{ t('groupchat.agents') }} ({{ groupchatStore.agents.length }})</n-button>
            <n-popover trigger="click" placement="bottom-end" :content-style="{ padding: '4px', width: 'auto', minWidth: 'auto' }">
              <template #trigger>
                <n-button size="tiny" quaternary>
                  <template #icon><n-icon><ellipsis-horizontal-outline /></n-icon></template>
                </n-button>
              </template>
              <div style="display: flex; flex-direction: column; gap: 2px;">
                <n-button text size="tiny" @click="showInviteCode = true" style="justify-content: flex-start;"><template #icon><n-icon size="14"><documents-outline /></n-icon></template> {{ t('groupchat.inviteCode') }}</n-button>
                <n-button text size="tiny" @click="showRoomInfo = !showRoomInfo" style="justify-content: flex-start;"><template #icon><n-icon size="14"><information-circle-outline /></n-icon></template> {{ t('groupchat.info') }}</n-button>
              </div>
            </n-popover>
          </div>
        </div>

        <!-- Room Info -->
        <div v-if="showRoomInfo && activeRoom" class="room-info-panel">
          <div style="display: flex; align-items: center; justify-content: space-between;">
            <span style="font-weight: 500; font-size: 13px; color: #666;">{{ activeRoom.name }}</span>
            <button class="info-close-btn" @click="showRoomInfo = false">&times;</button>
          </div>
          <div v-if="activeRoom?.description" style="font-size: 12px; color: #999; margin-top: 4px;">{{ activeRoom.description }}</div>
          <div style="margin-top: 4px; display: flex; gap: 8px;">
            <span v-if="groupchatStore.members.length > 0" style="font-size: 11px; background: #f0f0f0; padding: 1px 8px; border-radius: 8px;">{{ t('groupchat.members') }}: {{ groupchatStore.members.length }}</span>
            <span v-if="groupchatStore.agents.length > 0" style="font-size: 11px; background: #f0f0f0; padding: 1px 8px; border-radius: 8px;">{{ t('groupchat.agents') }}: {{ groupchatStore.agents.length }}</span>
          </div>
        </div>

        <!-- Messages -->
        <div class="messages" ref="messagesRef" @click="handleCodeClick">
          <!-- 加载骨架：切换房间或初次加载时显示，避免白屏 -->
          <div v-if="groupchatStore.loading && groupchatStore.messages.length === 0" class="msg-skeleton">
            <n-skeleton text :repeat="4" />
          </div>
          <template v-else>
            <div v-for="(msgs, dateKey) in groupedMessages" :key="dateKey">
              <div v-if="dateKey" class="date-separator"><span>{{ dateKey }}</span></div>
              <div
                v-for="msg in msgs"
                :key="msg.id"
                class="message"
                :class="msg.role"
              >
              <!-- Avatar -->
              <div class="avatar" :class="avatarClass(msg)">
                {{ avatarText(msg) }}
              </div>
              <!-- Body -->
              <div class="message-body" :class="{ 'agent-body': msg.role === 'agent' }">
                <!-- Header: sender + time -->
                <div class="message-header">
                  <n-text strong class="sender-name">{{ msg.sender }}</n-text>
                  <n-tag v-if="msg.role === 'agent'" size="tiny" type="success">{{ t('groupchat.ai') }}</n-tag>
                  <span v-if="formatTime(msg.timestamp)" class="message-time">{{ formatTime(msg.timestamp) }}</span>
                </div>
                <!-- Bubble / content -->
                <div class="message-bubble" :class="[bubbleClass(msg), { streaming: msg._streaming }]">
                  <n-spin v-if="msg._streaming && !msg.content" size="small" class="stream-spin" />
                  <template v-if="msg.role === 'agent' && msg.content">
                    <ReasoningContent :content="msg.content" :streaming="msg._streaming" />
                  </template>
                  <div
                    v-else
                    class="bubble-content"
                    v-html="msg.content ? renderMarkdownWithCopy(msg.content) : '<span class=\'placeholder\'>...</span>'"
                  ></div>
                </div>
              </div>
            </div>
            </div>
          </template>
        </div>

        <!-- Input -->
        <div class="input-area">
          <div class="input-wrapper" ref="inputWrapperRef">
            <n-input
              v-model:value="inputValue"
              type="textarea"
              :autosize="{ minRows: 1, maxRows: 6 }"
              :placeholder="inputPlaceholder"
              :disabled="replying"
              class="chat-input"
              @keydown="onKeydown"
              @input="onInput"
              @blur="onBlur"
            />
            <!-- @ 提及浮层：输入 @ 自动触发，支持搜索过滤与键盘导航 -->
            <div
              v-if="showMention && filteredMentions.length"
              class="mention-popup"
            >
              <div
                v-for="(opt, idx) in filteredMentions"
                :key="opt.id"
                :class="['mention-item', { active: idx === mentionActiveIdx }]"
                @mousedown.prevent="selectMention(opt)"
                @mouseenter="mentionActiveIdx = idx"
              >
                <n-tag size="tiny" type="info">{{ opt.profile || 'AI' }}</n-tag>
                <span class="mention-name">{{ opt.name }}</span>
              </div>
            </div>
            <!-- 发送/停止按钮内置在输入框右下角 -->
            <!-- @mousedown.prevent 阻止按钮点击时获取焦点，避免触发 input-wrapper 的 :focus-within 导致边框闪现 -->
            <button
              class="send-btn-inline"
              :class="{ stopping: replying }"
              :disabled="!replying && !inputValue.trim()"
              @click="replying ? stopGeneration() : send()"
              @mousedown.prevent
              :title="replying ? t('groupchat.stop') : t('groupchat.send')"
            >
              <svg v-if="!replying" viewBox="0 0 24 24" width="18" height="18" fill="currentColor">
                <path d="M3.4 20.4l17.45-7.48a1 1 0 000-1.84L3.4 3.6a.993.993 0 00-1.39.91L2 9.12c0 .5.37.93.87.99L17 12 2.87 13.88c-.5.07-.87.5-.87 1l.01 4.61c0 .71.73 1.2 1.39.91z"/>
              </svg>
              <svg v-else viewBox="0 0 24 24" width="16" height="16" fill="currentColor">
                <rect x="6" y="6" width="12" height="12" rx="2"/>
              </svg>
            </button>
          </div>
        </div>
      </template>
      <div v-else style="flex: 1; display: flex; align-items: center; justify-content: center;">
        <n-text depth="3">{{ t('groupchat.selectRoom') }}</n-text>
      </div>
    </div>

    <!-- Invite Code Modal -->
    <n-modal v-model:show="showInviteCode" :title="t('groupchat.inviteCode')" preset="card" class="modal-responsive" style="width: 420px; max-width: 96vw;" closable @close="showInviteCode = false">
      <div style="display: flex; flex-direction: column; gap: 12px;">
        <n-button @click="generateInvite" :loading="generatingInvite" style="width: 100%;">{{ t('groupchat.generate') }}</n-button>
        <n-input v-if="inviteCodeText" v-model:value="inviteCodeText" readonly>
          <template #suffix>
            <n-button text @click="copyToClipboard(inviteCodeText)"><template #icon><n-icon><CopyOutline /></n-icon></template></n-button>
          </template>
        </n-input>
      </div>
    </n-modal>

    <!-- Create Room Modal -->
    <n-modal v-model:show="showCreateRoom" :title="t('groupchat.newRoom')" preset="dialog" class="modal-responsive" closable @close="showCreateRoom = false" style="width: 480px; max-width: 96vw;">
      <n-form>
        <n-form-item :label="t('groupchat.roomName')">
          <n-input v-model:value="newRoom.name" />
        </n-form-item>
        <n-form-item :label="t('groupchat.description')">
          <n-input v-model:value="newRoom.description" />
        </n-form-item>
      </n-form>
      <template #action>
        <div style="display: flex; justify-content: flex-end;">
          <n-button @click="showCreateRoom = false">{{ t('common.cancel') }}</n-button>
          <n-button type="primary" @click="createRoom">{{ t('common.create') }}</n-button>
        </div>
      </template>
    </n-modal>

    <!-- Agents Modal -->
    <n-modal v-model:show="showAgents" :title="t('groupchat.agents')" style="width: 640px; max-width: 96vw;" preset="card" closable @close="showAgents = false">
      <n-list>
        <n-list-item v-for="a in groupchatStore.agents" :key="a.id">
          <div style="display: flex; justify-content: space-between; align-items: flex-start; width: 100%;">
            <div style="display: flex; flex-direction: column; flex: 1; min-width: 0;">
              <div style="display: flex; align-items: center;">
                <n-text strong style="font-size: 15px;">{{ a.name }}</n-text>
                <n-tag v-if="a.profile" size="tiny" type="default">{{ a.profile }}</n-tag>
                <n-tag size="tiny" type="info">{{ a.temperature !== undefined ? a.temperature.toFixed(1) : '0.7' }}</n-tag>
              </div>
              <n-text v-if="a.description" depth="3" style="font-size: 12px;">{{ a.description }}</n-text>
              <n-text v-if="a.system_prompt" depth="3" style="font-size: 11px; word-break: break-all;" :ellipsis="{ rows: 2 }">
                {{ a.system_prompt }}
              </n-text>
            </div>
            <div style="display: flex;">
              <n-button size="tiny" @click="editAgent(a)">{{ t('common.edit') }}</n-button>
              <n-popconfirm @positive-click="handleRemoveAgent(a)">
                <template #trigger>
                  <n-button size="tiny" type="error">{{ t('common.remove') }}</n-button>
                </template>
                {{ t('groupchat.confirmRemoveAgent', { name: a.name }) }}
              </n-popconfirm>
            </div>
          </div>
        </n-list-item>
      </n-list>
      <n-divider style="margin: 12px 0;" />
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
            <div style="display: flex; align-items: center; gap: 10px;">
              <n-slider v-model:value="newAgent.temperature" :min="0" :max="2" :step="0.1" style="width: 120px;" />
              <n-text>{{ newAgent.temperature.toFixed(1) }}</n-text>
            </div>
          </n-form-item-gi>
          <n-form-item-gi :label="t('groupchat.tools')">
            <n-input v-model:value="newAgent.tools" :placeholder="t('groupchat.toolsPlaceholder')" />
          </n-form-item-gi>
        </n-grid>
        <div style="display: flex; justify-content: flex-end; gap: 8px; margin-top: 8px;">
          <n-button v-if="editingAgentId" @click="cancelEdit">{{ t('common.cancel') }}</n-button>
          <n-button type="primary" @click="addAgent">{{ editingAgentId ? t('common.save') : t('groupchat.addAgent') }}</n-button>
        </div>
      </n-form>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted, onUnmounted, computed, nextTick } from 'vue'
import { useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { useGroupChatStore } from '@/stores/groupchat'
import { stripZeroWidth } from '@/utils/text'
import { marked } from 'marked'
import hljs from 'highlight.js'
import 'highlight.js/styles/github-dark.css'
import { CloseOutline, RefreshOutline, CopyOutline, CreateOutline, InformationCircleOutline, DocumentsOutline, EllipsisHorizontalOutline } from '@vicons/ionicons5'
import ReasoningContent from '@/components/ReasoningContent.vue'

const { t } = useI18n()
const message = useMessage()
const groupchatStore = useGroupChatStore()
const inputValue = ref('')

// 移动端房间侧栏下拉状态
const mobileRoomsExpanded = ref(false)
const isMobile = ref(window.innerWidth <= 768)

function handleResize() {
  isMobile.value = window.innerWidth <= 768
}

onMounted(() => {
  window.addEventListener('resize', handleResize)
})

onUnmounted(() => {
  window.removeEventListener('resize', handleResize)
})

function onRoomClick(id: string) {
  groupchatStore.selectRoom(id)
  // 移动端选中房间后自动收起抽屉
  if (isMobile.value) {
    mobileRoomsExpanded.value = false
  }
}
const showCreateRoom = ref(false)
const showAgents = ref(false)
const showInviteCode = ref(false)
const inviteCodeText = ref('')
const showRoomInfo = ref(false)
const editingRoomName = ref(false)
const editRoomNameValue = ref('')
const newRoom = reactive({ name: '', description: '' })
const editingAgentId = ref('')
const replying = ref(false)
const replyingAgent = ref('')
const generatingInvite = ref(false)
const messagesRef = ref<HTMLElement | null>(null)
let streamAbortController: AbortController | null = null
let streamBuffer = ''
let streamFlushTimer: ReturnType<typeof setTimeout> | null = null
const STREAM_FLUSH_INTERVAL = 80 // ms

// @ 提及：输入 @ 自动触发浮层，支持搜索过滤与键盘导航
const showMention = ref(false)
const mentionQuery = ref('')
const mentionStart = ref(-1) // @ 符号在 inputValue 中的起始下标
const mentionActiveIdx = ref(0)
const inputWrapperRef = ref<HTMLElement | null>(null)

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

// @ 提及候选：按当前 query 过滤
const filteredMentions = computed(() => {
  const q = mentionQuery.value.toLowerCase()
  const list = groupchatStore.agents.filter(a => a.invited !== false)
  if (!q) return list
  return list.filter(a =>
    a.name.toLowerCase().includes(q) || (a.profile || '').toLowerCase().includes(q)
  )
})

// placeholder:有 agent 时提示 @ 提及与快捷键;移动端空间有限用短文案
const inputPlaceholder = computed(() => {
  if (groupchatStore.agents.length === 0) return t('groupchat.typeMessage')
  if (isMobile.value) return t('groupchat.inputHintShort')
  return t('groupchat.inputHint')
})

// Configure marked with highlight.js.
// Note: marked v12 removed the `highlight` option; use the renderer override
// (same approach as ChatView / BotsView) so code blocks actually get highlighted.
const codeRenderer = (code: string, lang?: string): string => {
  const language = lang && hljs.getLanguage(lang) ? lang : null
  const highlighted = language
    ? hljs.highlight(code, { language }).value
    : hljs.highlightAuto(code).value
  return `<pre><code class="hljs${language ? ` language-${language}` : ''}">${highlighted}</code></pre>`
}

marked.use({ renderer: { code: codeRenderer }, breaks: true, gfm: true })

// Markdown 渲染缓存：避免 v-for 重渲染时重复解析同一内容
const mdCache = new Map<string, string>()
const MD_CACHE_LIMIT = 200

function renderMarkdown(content: string): string {
  content = stripZeroWidth(content)
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

function formatTime(timestamp: string | number): string {
  if (!timestamp) return ''
  const date = toDate(timestamp)
  if (isNaN(date.getTime())) return ''
  const now = new Date()
  const isToday = date.toDateString() === now.toDateString()
  if (isToday) {
    return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  }
  return date.toLocaleDateString([], { month: 'short', day: 'numeric' }) + ' ' +
    date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

// 兼容三种 timestamp 格式（与 ChatView 对齐）：ISO8601 字符串 / 毫秒数字 / 秒数字
function toDate(ts: string | number): Date {
  if (typeof ts === 'number') {
    const n = ts as number
    if (n < 1e12) return new Date(n * 1000)
    return new Date(n)
  }
  const s = String(ts).trim()
  if (!s) return new Date(NaN)
  if (/^-?\d+$/.test(s)) {
    const n = Number(s)
    if (isFinite(n)) {
      if (n < 1e12) return new Date(n * 1000)
      return new Date(n)
    }
  }
  const d = new Date(s)
  if (!isNaN(d.getTime())) return d
  return new Date(s + 'Z')
}

// 头像样式：user 紫色渐变，agent 绿色渐变，system 橙色
function avatarClass(msg: any): string {
  if (msg.role === 'system') return 'system-avatar'
  if (msg.role === 'agent') return 'bot-avatar'
  return 'user-avatar'
}

// 头像文字：取 sender 首字符（支持中英文），system 用 'S'
function avatarText(msg: any): string {
  if (msg.role === 'system') return 'S'
  const name = (msg.sender || '').trim()
  return name ? name.charAt(0).toUpperCase() : '?'
}

// 气泡样式：user 绿色渐变白字，agent 白色，system 橙色渐变
function bubbleClass(msg: any): string {
  if (msg.role === 'system') return 'system-bubble'
  if (msg.role === 'agent') return 'agent-bubble'
  return 'user-bubble'
}

// 解析输入中的 @ 提及：用正则精确匹配 @name，避免 includes 误匹配与重名错配
function parseMentions(content: string): { id: string, name: string }[] {
  const regex = /@([^\s@]+)/g
  const seen = new Set<string>()
  const result: { id: string, name: string }[] = []
  let m: RegExpExecArray | null
  while ((m = regex.exec(content)) !== null) {
    const name = m[1]
    const agent = groupchatStore.agents.find(a => a.name === name)
    if (agent && !seen.has(agent.id)) {
      seen.add(agent.id)
      result.push({ id: agent.id, name: agent.name })
    }
  }
  return result
}

// 获取 textarea 光标位置（n-input 内部原生元素）
function getCursorPos(): number {
  const wrapper = inputWrapperRef.value
  if (!wrapper) return inputValue.value.length
  const el = wrapper.querySelector('textarea') as HTMLTextAreaElement | null
  return el ? el.selectionStart : inputValue.value.length
}

// 输入回调：检测光标前的 @ 触发提及浮层
function onInput() {
  const pos = getCursorPos()
  const before = inputValue.value.slice(0, pos)
  // 匹配行内最后一个未闭合的 @xxx（xxx 不含空格和 @）
  const match = before.match(/@([^\s@]*)$/)
  if (match && groupchatStore.agents.length > 0) {
    mentionStart.value = match.index!
    mentionQuery.value = match[1]
    showMention.value = true
    mentionActiveIdx.value = 0
  } else {
    closeMention()
  }
}

function closeMention() {
  showMention.value = false
  mentionQuery.value = ''
  mentionStart.value = -1
  mentionActiveIdx.value = 0
}

// 选中某个 agent，将输入框中的 @query 替换为 @name + 空格
function selectMention(agent: { name: string }) {
  const pos = getCursorPos()
  const start = mentionStart.value
  if (start < 0) return
  const before = inputValue.value.slice(0, start)
  const after = inputValue.value.slice(pos)
  inputValue.value = `${before}@${agent.name} ${after}`
  closeMention()
  // 重新聚焦并将光标移到插入内容之后
  nextTick(() => {
    const wrapper = inputWrapperRef.value
    const el = wrapper?.querySelector('textarea') as HTMLTextAreaElement | null
    if (el) {
      const newPos = before.length + agent.name.length + 2
      el.focus()
      el.setSelectionRange(newPos, newPos)
    }
  })
}

function onBlur() {
  // 延迟关闭，让 mousedown 选择能先触发
  setTimeout(closeMention, 150)
}

// 键盘处理：Enter 发送 / Shift+Enter 换行 / 浮层开启时方向键导航
function onKeydown(e: KeyboardEvent) {
  // 提及浮层开启时的键盘导航
  if (showMention.value && filteredMentions.value.length > 0) {
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
      const opt = filteredMentions.value[mentionActiveIdx.value]
      if (opt) selectMention(opt)
      return
    }
    if (e.key === 'Escape') {
      e.preventDefault()
      closeMention()
      return
    }
    // 空格关闭浮层（不选）
    if (e.key === ' ') {
      closeMention()
    }
  }
  // Enter 发送（无 Shift），Shift+Enter 换行（默认行为，不阻止）
  if (e.key === 'Enter' && !e.shiftKey && !showMention.value) {
    e.preventDefault()
    send()
  }
}

async function send() {
  if (!inputValue.value.trim() || replying.value) return
  closeMention()
  const content = inputValue.value
  inputValue.value = ''

  const mentions = parseMentions(content)

  // 本地占位消息已在 store.sendMessage 中先 push，立即滚动可见
  scrollToBottom()
  try {
    await groupchatStore.sendMessage(content)
  } catch {
    message.error(t('groupchat.sendFailed') || '发送失败')
    return
  }

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

            // 所有 agent 回复完毕后立即恢复按钮，不等 HTTP 流关闭
            if (Object.keys(streamMsgs).length === 0) {
              replying.value = false
            }
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

  // 异步刷新最终状态，不阻塞 UI
  groupchatStore.selectRoom(roomId).catch(() => {})
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
  if (!newRoom.name.trim()) {
    message.warning(t('groupchat.nameRequired'))
    return
  }
  try {
    await groupchatStore.createRoom({ ...newRoom })
    newRoom.name = ''
    newRoom.description = ''
    showCreateRoom.value = false
    message.success(t('groupchat.created'))
  } catch (e) {
    message.error(e instanceof Error ? e.message : String(e))
  }
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
  await groupchatStore.deleteRoom(roomId)
  message.success(t('groupchat.roomDeleted'))
}

async function handleRemoveAgent(agent: any) {
  await groupchatStore.removeAgent(agent.id)
  message.success(t('groupchat.agentRemoved'))
}

function handleRefresh() {
  groupchatStore.loadRooms()
  if (groupchatStore.activeRoomId) {
    groupchatStore.selectRoom(groupchatStore.activeRoomId)
  }
}

async function generateInvite() {
  generatingInvite.value = true
  try {
    const code = await groupchatStore.generateInviteCode()
    inviteCodeText.value = code
    message.success(t('groupchat.inviteCodeGenerated'))
  } catch {
    message.error(t('groupchat.inviteCodeFailed'))
  } finally {
    generatingInvite.value = false
  }
}

function copyToClipboard(text: string) {
  navigator.clipboard.writeText(text).then(() => {
    message.success(t('groupchat.inviteCodeCopied'))
  }).catch(() => {
    // Fallback
    const el = document.createElement('textarea')
    el.value = text
    document.body.appendChild(el)
    el.select()
    document.execCommand('copy')
    document.body.removeChild(el)
    message.success(t('groupchat.inviteCodeCopied'))
  })
}

// Group messages by date for display
const groupedMessages = computed(() => {
  const groups: Record<string, any[]> = {}
  for (const msg of groupchatStore.messages) {
    const key = formatDate(msg.timestamp)
    if (!groups[key]) groups[key] = []
    groups[key].push(msg)
  }
  return groups
})

function formatDate(ts: string | number): string {
  if (!ts) return ''
  const d = toDate(ts)
  if (isNaN(d.getTime())) return ''
  const now = new Date()
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  const yesterday = new Date(today.getTime() - 86400000)
  const msgDate = new Date(d.getFullYear(), d.getMonth(), d.getDate())
  if (msgDate.getTime() === today.getTime()) return ''
  if (msgDate.getTime() === yesterday.getTime()) return 'Yesterday'
  return d.toLocaleDateString([], { month: 'short', day: 'numeric', year: 'numeric' })
}

// Markdown render with code block copy buttons
function startEditRoomName() {
  if (!activeRoom.value) return
  editRoomNameValue.value = activeRoom.value.name
  editingRoomName.value = true
}

async function saveRoomName() {
  if (!editRoomNameValue.value.trim()) return
  await groupchatStore.updateRoomName(editRoomNameValue.value.trim())
  editingRoomName.value = false
  message.success(t('groupchat.roomNameUpdated'))
}

function handleCodeClick(e: MouseEvent) {
  const target = e.target as HTMLElement
  // Check if click is on a code block area
  const pre = target.closest('pre')
  if (pre) {
    const code = pre.querySelector('code')
    if (code) {
      navigator.clipboard.writeText(code.textContent || '').then(() => {
        message.success(t('groupchat.copied'))
      }).catch(() => {})
    }
  }
}

function renderMarkdownWithCopy(content: string): string {
  return renderMarkdown(content)
}




onMounted(() => groupchatStore.loadRooms())
</script>

<style scoped>
.groupchat-container {
  display: flex;
  height: 100vh;
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

/* Mobile drag handle - hidden on desktop */
.mobile-room-handle {
  display: none;
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
}

.room-item:hover {
  background: #f0f0f0;
}

.room-item.active {
  background: #e8f4ff;
}

.room-item .room-delete-btn {
  opacity: 0.5 !important;
}
.room-item:hover .room-delete-btn {
  opacity: 1 !important;
}

.chat-main {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-width: 0;
}

.chat-header {
  flex-shrink: 0;
  padding: 12px 16px;
  border-bottom: 1px solid #e8e8e8;
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.messages {
  flex: 1;
  overflow-y: auto;
  padding: 20px 24px;
  padding-bottom: 80px;
  min-height: 0;
}

/* 消息内容区限制最大宽度并居中，宽屏下避免拉满 */
.messages > * {
  max-width: 960px;
  margin-left: auto;
  margin-right: auto;
}

/* 加载骨架 */
.msg-skeleton {
  padding: 12px 0;
}

/* ========== Message Layout (对齐 ChatView) ========== */
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

/* User messages - 右对齐 */
.message.user {
  flex-direction: row-reverse;
}

/* System messages - 居中 */
.message.system {
  justify-content: center;
}

.message-body {
  max-width: 72%;
  min-width: 0;
  display: flex;
  flex-direction: column;
}

/* Agent 回复限制最大宽度，避免长回答占满整屏 */
.message-body.agent-body {
  max-width: 80%;
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
  color: #fff;
  font-weight: 600;
  user-select: none;
}

.user-avatar {
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
}

.bot-avatar {
  background: linear-gradient(135deg, #18a058 0%, #36ad6a 100%);
}

.system-avatar {
  background: #fef3c7;
  border: 1px solid #fcd34d;
  color: #854d0e;
}

/* ========== Message Header ========== */
.message-header {
  display: flex;
  gap: 8px;
  align-items: center;
  margin-bottom: 4px;
  font-size: 13px;
}

.message.user .message-header {
  justify-content: flex-end;
  flex-direction: row-reverse;
}

/* 用户消息整体右对齐：body 内的 header 和 bubble 都靠右 */
.message.user .message-body {
  align-items: flex-end;
}

.sender-name {
  color: #333;
}

.message-time {
  font-size: 11px;
  color: #bbb;
}

.stream-spin {
  margin-right: 4px;
}

/* ========== Message Bubbles ========== */
.message-bubble {
  padding: 14px 18px;
  border-radius: 16px;
  line-height: 1.75;
  word-break: break-word;
  overflow-wrap: break-word;
  display: flex;
  align-items: flex-start;
  gap: 8px;
}

/* User bubble - 绿色渐变白字 */
.user-bubble {
  background: linear-gradient(135deg, #18a058 0%, #20803a 100%);
  color: #fff;
  border-bottom-right-radius: 4px;
}

/* Agent bubble - 白色卡片 */
.agent-bubble {
  background: #fff;
  color: #1f2937;
  border: 1px solid #e8e8e8;
  border-bottom-left-radius: 4px;
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.06);
}

/* System bubble - 橙色渐变 */
.system-bubble {
  background: linear-gradient(135deg, #fff7ed 0%, #fef3c7 100%);
  color: #854d0e;
  border: 1px solid #fcd34d;
  border-radius: 12px;
  font-size: 13px;
  font-style: italic;
  max-width: 80%;
}

/* Streaming bubble - 脉冲边框 */
.message-bubble.streaming {
  border-style: dashed;
  animation: pulse-border 1.5s ease-in-out infinite;
}

@keyframes pulse-border {
  0%, 100% { border-color: #c8e6c9; }
  50% { border-color: #66bb6a; }
}

.bubble-content {
  word-break: break-word;
  overflow-wrap: break-word;
  flex: 1;
  min-width: 0;
}

.bubble-content :deep(.placeholder) {
  color: #999;
}

/* ========== Markdown Content (对齐 ChatView) ========== */
.message-bubble :deep(p) { margin: 0 0 10px 0; }
.message-bubble :deep(p:last-child) { margin-bottom: 0; }
.message-bubble :deep(ul), .message-bubble :deep(ol) { margin: 10px 0; padding-left: 28px; }
.message-bubble :deep(li) { margin: 5px 0; }

.message-bubble :deep(blockquote) {
  margin: 10px 0;
  padding: 8px 16px;
  border-left: 4px solid #d0d0d0;
  background: rgba(0, 0, 0, 0.03);
  color: inherit;
}

.message-bubble :deep(table) {
  border-collapse: collapse;
  margin: 10px 0;
  width: 100%;
}

.message-bubble :deep(th), .message-bubble :deep(td) {
  border: 1px solid #d0d0d0;
  padding: 6px 12px;
}

.message-bubble :deep(th) {
  background: rgba(0, 0, 0, 0.04);
}

.message-bubble :deep(h1),
.message-bubble :deep(h2),
.message-bubble :deep(h3),
.message-bubble :deep(h4) {
  margin: 14px 0 8px;
  font-weight: 600;
}

.message-bubble :deep(h1) { font-size: 20px; }
.message-bubble :deep(h2) { font-size: 18px; }
.message-bubble :deep(h3) { font-size: 16px; }
.message-bubble :deep(h4) { font-size: 15px; }

.message-bubble :deep(hr) {
  border: none;
  border-top: 1px solid #d0d0d0;
  margin: 14px 0;
}

.message-bubble :deep(pre) {
  background: #1e1e1e;
  color: #d4d4d4;
  padding: 12px 16px;
  border-radius: 8px;
  overflow-x: auto;
  max-width: 100%;
  margin: 10px 0;
}

.message-bubble :deep(code) {
  font-family: 'SF Mono', 'Fira Code', 'Consolas', monospace;
  font-size: 13px;
}

.message-bubble :deep(a) {
  color: inherit;
  text-decoration: underline;
  font-weight: 600;
}

/* User 气泡内链接用浅色，保证在绿色背景上可读 */
.user-bubble :deep(a) {
  color: #e0f7e0;
  text-shadow: 0 1px 2px rgba(0, 0, 0, 0.3);
}

.user-bubble :deep(a:hover) {
  color: #fff;
}

.input-area {
  display: flex;
  gap: 8px;
  padding: 12px 24px 16px;
  border-top: 1px solid #e0e0e0;
  background: #fff;
  align-items: flex-end;
}

/* 输入框内容限制最大宽度并居中，与消息区对齐 */
.input-area > * {
  max-width: 960px;
  margin-left: auto;
  margin-right: auto;
}

.input-wrapper {
  position: relative;
  flex: 1;
  min-width: 0;
  width: 100%;
  border: 1px solid #d9d9d9;
  border-radius: 12px;
  padding: 12px 16px;
  padding-right: 48px; /* 为内置发送按钮留出空间 */
  background: #fff;
  transition: border-color 0.2s, box-shadow 0.2s;
}

.input-wrapper:focus-within {
  border-color: #18a058;
  box-shadow: 0 0 0 2px rgba(24, 160, 88, 0.15);
}

/* 内置发送按钮：固定在输入框右下角 */
.send-btn-inline {
  position: absolute;
  right: 8px;
  bottom: 8px;
  width: 32px;
  height: 32px;
  border: none;
  border-radius: 8px;
  background: #18a058;
  color: #fff;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: background 0.2s, opacity 0.2s;
  flex-shrink: 0;
}

.send-btn-inline:hover:not(:disabled) {
  background: #20803a;
}

.send-btn-inline:disabled {
  background: #c5c5c5;
  cursor: not-allowed;
}

.send-btn-inline.stopping {
  background: #f0a020;
}

.send-btn-inline.stopping:hover {
  background: #d98610;
}

.chat-input {
  --n-border: none !important;
  --n-border-hover: none !important;
  --n-border-focus: none !important;
  --n-box-shadow-focus: none !important;
  --n-padding-left: 0 !important;
  --n-padding-right: 0 !important;
  background: transparent !important;
}

/* 彻底移除 n-input 的边框/焦点环，避免点击发送按钮后边框闪现 */
.chat-input :deep(.n-input) {
  background: transparent !important;
  box-shadow: none !important;
}
.chat-input :deep(.n-input__border),
.chat-input :deep(.n-input__border-focus),
.chat-input :deep(.n-input__state-border) {
  border: none !important;
  box-shadow: none !important;
  display: none !important;
}
/* n-input--focus 是与 .chat-input 同级的修饰类，需用组合选择器覆盖 */
.chat-input.n-input :deep(.n-input__state-border),
.chat-input:deep(.n-input--focus .n-input__state-border) {
  border: none !important;
  box-shadow: none !important;
  display: none !important;
}

.chat-input :deep(.n-input__textarea-el) {
  resize: none;
}

/* @ 提及浮层 */
.mention-popup {
  position: absolute;
  bottom: 100%;
  left: 16px;
  right: 16px;
  margin-bottom: 4px;
  background: #fff;
  border: 1px solid #e0e0e0;
  border-radius: 8px 8px 0 0;
  box-shadow: 0 -4px 12px rgba(0, 0, 0, 0.1);
  max-height: 220px;
  overflow-y: auto;
  z-index: 10;
}

.mention-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 12px;
  cursor: pointer;
  font-size: 13px;
}

.mention-item:hover,
.mention-item.active {
  background: #f0f7ff;
}

.mention-name {
  color: #333;
}
/* Code block clickable hint */
.messages pre:hover {
  outline: 2px solid #1890ff;
  outline-offset: 2px;
  cursor: pointer;
  transition: outline 0.2s;
}
.messages pre {
  position: relative;
  border-radius: 6px;
}
.messages pre:hover::after {
  content: 'Click to copy';
  position: absolute;
  top: 4px;
  right: 8px;
  font-size: 11px;
  color: #1890ff;
  background: rgba(255,255,255,0.9);
  padding: 0 6px;
  border-radius: 3px;
  pointer-events: none;
}

/* Date separator */
.date-separator {
  text-align: center;
  padding: 16px 0 8px;
  font-size: 12px;
  color: #999;
}
.date-separator span {
  background: #f5f5f5;
  padding: 2px 12px;
  border-radius: 10px;
}

/* Room info panel */
.room-info-panel {
  padding: 6px 14px;
  border-bottom: 1px solid #e8e8e8;
  background: #fafafa;
  font-size: 13px;
}

/* Invite code modal */
.invite-code-input {
  cursor: pointer;
}

/* Copy button */
.copy-btn {
  cursor: pointer;
}

.info-close-btn { border: none; background: none; cursor: pointer; font-size: 16px; color: #999; padding: 0; line-height: 1; }
.info-close-btn:hover { color: #333; }

/* 移动端:房间侧栏改为 ChatView 同款顶部下拉抽屉 */
@media (max-width: 768px) {
  .groupchat-container {
    flex-direction: column;
    /* 锁定视口高度,dvh 随浏览器地址栏动态伸缩,保证输入框始终贴底 */
    height: 100vh;
    height: 100dvh;
    overflow: hidden;
  }
  .room-sidebar {
    width: 100%;
    max-height: 32px;
    padding: 0;
    border-right: none;
    border-bottom: 1px solid #e8e8e8;
    overflow: hidden;
    transition: max-height 0.3s ease;
    flex-shrink: 0;
  }
  .room-sidebar.mobile-expanded {
    max-height: 60vh;
    padding: 12px;
    overflow-y: auto;
  }

  /* 聊天主区纵向弹性:消息内部滚动,输入区固定底部 */
  .chat-main {
    min-height: 0;
  }
  .messages {
    padding: 10px 12px;
    padding-bottom: 16px;
  }

  /* 输入区收紧留白并适配手势条安全区 */
  .input-area {
    padding: 8px 12px;
    padding-bottom: calc(8px + env(safe-area-inset-bottom));
  }

  .mobile-room-handle {
    display: flex;
    justify-content: center;
    align-items: center;
    padding: 11px 0;
    cursor: pointer;
  }
  .handle-bar {
    width: 56px;
    height: 6px;
    border-radius: 3px;
    background: #bbb;
    transition: background 0.2s;
  }
  .mobile-room-handle:hover .handle-bar {
    background: #888;
  }
}
</style>