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
            <div class="session-content">
              <div v-if="editingSessionId !== session.id" class="session-title">
                {{ session.title || t('chat.untitled') }}
              </div>
              <div v-else class="session-title-edit">
                <n-input
                  v-model:value="editingName"
                  class="session-title-input"
                  size="small"
                  @keydown.enter="saveRename(session.id)"
                  @keydown.esc="cancelRename"
                  @blur="saveRename(session.id)"
                  autofocus
                />
              </div>
              <div class="session-meta">
                <n-tag v-if="session.source && session.source !== 'web'" size="tiny" :type="sourceType(session.source)" style="margin-right: 4px;">
                  {{ session.source }}
                </n-tag>
                {{ session.message_count || 0 }} {{ t('chat.messages') }}
              </div>
            </div>
            <div class="session-actions">
              <n-button
                v-if="editingSessionId !== session.id"
                class="session-rename-btn"
                size="tiny"
                quaternary
                circle
                @click.stop="startRename(session.id)"
                :title="t('chat.rename')"
              >
                <template #icon>
                  <n-icon size="14"><PencilOutline /></n-icon>
                </template>
              </n-button>
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
          </div>
        </template>
        <n-text v-if="!chatStore.sessions.length" depth="3" style="padding: 16px; display: block; text-align: center;">
          {{ t('chat.noSessions') }}
        </n-text>
      </div>
    </div>

    <!-- Chat Area -->
    <div class="chat-main">
      <CurrentGoal />

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
              <div class="message-bubble user-bubble">
                <!-- File attachments in message -->
                <div v-if="msg.files?.length" class="message-files">
                  <n-space>
                    <div
                      v-for="(file, idx) in msg.files"
                      :key="idx"
                      class="message-file-item"
                      @click="goToFilesPage"
                      :title="t('chat.viewFileManagement')"
                    >
                      <n-icon size="20"><DocumentOutline /></n-icon>
                      <span class="message-file-name">{{ file.name }}</span>
                    </div>
                  </n-space>
                </div>
                <div v-if="msg.content" v-html="renderMarkdown(msg.content)"></div>
                <div v-else-if="!msg.files?.length" class="empty-content">{{ t('chat.fileBtn') }}</div>
              </div>
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

          <!-- Tool message: hidden from chat display -->
          <template v-else-if="msg.role === 'tool'">
          </template>

          <!-- System message: command results, notifications -->
          <template v-else-if="msg.role === 'system'">
            <div class="avatar system-avatar">💡</div>
            <div class="message-body">
              <div class="message-bubble system-bubble">
                <div v-html="renderMarkdown(msg.content)"></div>
              </div>
              <div class="message-time">{{ formatTime(msg.timestamp) }}</div>
            </div>
          </template>
        </div>

        <!-- Streaming area -->
        <template v-if="chatStore.streaming">
          <!-- Task Timeline for long tasks -->
          <div v-if="taskTimelineSteps.length > 0" style="margin: 8px 12px;">
            <TaskTimeline
              :steps="taskTimelineSteps"
              :title="taskTimelineTitle"
              :overall-percent="taskTimelinePercent"
            />
          </div>
          <!-- Long task progress bar (fallback) -->
          <div v-else-if="chatStore.taskProgress" class="long-task-progress">
            <n-progress
              type="line"
              :percentage="chatStore.taskProgress.percent"
              :indicator-placement="'inside'"
              :status="chatStore.taskProgress.percent >= 100 ? 'success' : 'processing'"
              :height="20"
            >
              <template #default>
                <span style="font-size: 12px;">
                  {{ chatStore.taskProgress.phase }} — {{ chatStore.taskProgress.detail }}
                  ({{ chatStore.taskProgress.iteration }}/{{ chatStore.taskProgress.maxIterations }})
                </span>
              </template>
            </n-progress>
            <n-text depth="3" style="font-size: 11px; margin-top: 4px; display: block;">
              Tokens: {{ chatStore.taskProgress.tokensUsed }} used
              <span v-if="chatStore.taskProgress.tokensRemaining > 0">, {{ chatStore.taskProgress.tokensRemaining }} remaining</span>
            </n-text>
          </div>
          <!-- Streaming message with tool calls inline -->
          <div class="message assistant">
            <div class="avatar bot-avatar">🤖</div>
            <div class="message-body">
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

      <!-- File preview before sending -->
      <div v-if="selectedFiles.length" class="preview-bar">
        <n-space>
          <div v-for="(file, idx) in selectedFiles" :key="'file-'+idx" class="preview-item file-preview">
            <n-icon size="24"><DocumentOutline /></n-icon>
            <span class="file-name">{{ file.name }}</span>
            <n-tag v-if="file.size" size="tiny" type="info">{{ (file.size / 1024).toFixed(1) }} KB</n-tag>
            <n-button class="preview-remove" size="tiny" circle type="error" @click="removeFile(idx)">×</n-button>
          </div>
        </n-space>
      </div>

      <!-- ChatGPT-style input box -->
      <div class="input-area">
        <!-- Command suggestions -->
        <div v-if="commandSuggestions.length > 0" class="command-suggestions">
          <div
            v-for="suggestion in commandSuggestions"
            :key="suggestion"
            class="suggestion-item"
            @click="selectCommand(suggestion)"
          >
            {{ suggestion }}
          </div>
        </div>
        <div class="chat-input-box">
          <!-- Text input -->
          <n-input
            v-model:value="inputValue"
            type="textarea"
            :autosize="{ minRows: 1, maxRows: 8 }"
            :placeholder="chatStore.isCommand(inputValue) ? t('chat.commandPlaceholder') : t('chat.placeholder')"
            class="chat-textarea"
            @keydown.enter.exact.prevent="send"
            @keydown.enter.shift.prevent="() => {}"
            @input="handleInput"
          />
          <!-- Toolbar inside input box -->
          <div class="input-toolbar">
            <div class="toolbar-left">
              <!-- Model selector -->
              <n-select
                v-model:value="currentModelId"
                :options="modelOptions"
                size="tiny"
                class="toolbar-model-select"
                :placeholder="t('chat.selectModel')"
                @update:value="handleModelChange"
              />
              <!-- File upload -->
              <n-upload
                :show-file-list="false"
                :multiple="true"
                @before-upload="handleFileSelect"
              >
                <n-button size="tiny" quaternary class="toolbar-btn" :title="t('chat.uploadFile')">
                  <template #icon>
                    <n-icon><AttachOutline /></n-icon>
                  </template>
                </n-button>
              </n-upload>
            </div>
            <div class="toolbar-right">
              <n-button
                v-if="!chatStore.streaming"
                type="primary"
                size="small"
                circle
                @click="send"
                :disabled="!inputValue.trim() && !selectedFiles.length"
                class="send-circle-btn"
              >
                <template #icon>
                  <n-icon><SendOutline /></n-icon>
                </template>
              </n-button>
              <n-button
                v-else
                type="warning"
                size="small"
                circle
                @click="stopGeneration"
                class="send-circle-btn"
              >
                <template #icon>
                  <n-icon><StopCircleOutline /></n-icon>
                </template>
              </n-button>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, nextTick, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { marked } from 'marked'
import hljs from 'highlight.js'
import 'highlight.js/styles/github-dark.css'
import { useChatStore } from '@/stores/chat'
import { useModelsStore } from '@/stores/models'
import ReasoningContent from '@/components/ReasoningContent.vue'
import ToolCallBlock from '@/components/ToolCallBlock.vue'
import CurrentGoal from '@/components/CurrentGoal.vue'
import TaskTimeline from '@/components/TaskTimeline.vue'
import type { TimelineStep } from '@/components/TaskTimeline.vue'
import { AttachOutline, SendOutline, StopCircleOutline, DocumentOutline, PencilOutline } from '@vicons/ionicons5'
import type { UploadFileInfo } from 'naive-ui'
import * as sessionsApi from '@/api/sessions'
import { useRouter } from 'vue-router'

const { t } = useI18n()
const chatStore = useChatStore()
const modelsStore = useModelsStore()
const router = useRouter()
const inputValue = ref('')
const messagesRef = ref<HTMLDivElement>()

// Rename session
const editingSessionId = ref<string | null>(null)
const editingName = ref('')

// Model selector
const currentModelId = ref('')
const modelOptions = computed(() => {
  return modelsStore.models.map(m => ({
    label: `${m.provider} / ${m.name}${m.description ? ' - ' + m.description : ''}`,
    value: m.id,
  }))
})

// File upload
const selectedFiles = ref<sessionsApi.UploadedFile[]>([])
const uploadingFiles = ref<Set<string>>(new Set())

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

// Task timeline computed from tool calls and progress
const synthesisStarted = ref(false)

// Reset synthesis flag when a new message starts streaming
watch(() => chatStore.streaming, (val) => {
  if (!val) synthesisStarted.value = false
})

const taskTimelineSteps = computed((): TimelineStep[] => {
  const steps: TimelineStep[] = []
  const tcs = chatStore.toolCalls

  // Phase 1: Planning (if we have a task progress)
  if (chatStore.taskProgress) {
    steps.push({
      title: t('chat.taskPlanning'),
      description: t('chat.taskPlanningDesc'),
      status: 'completed',
    })
  }

  // Phase 2: Tool execution steps
  for (const tc of tcs) {
    const existing = steps.find(s => s.title === tc.name)
    if (existing) continue

    steps.push({
      title: tc.name,
      description: tc.args?.substring(0, 80) || '',
      status: tc.status === 'running' ? 'running' : tc.status === 'completed' ? 'completed' : tc.status === 'error' ? 'failed' : 'pending',
      detail: tc.status === 'running' ? t('chat.executing') : tc.duration ? `${t('chat.duration')} ${tc.duration}` : undefined,
      duration: tc.duration,
    })
  }

  // Phase 3: Synthesis (if streaming and no running tools)
  const showSynthesis = chatStore.streaming && chatStore.activeToolCalls.length === 0 && chatStore.streamContent
  if (showSynthesis) synthesisStarted.value = true

  if (synthesisStarted.value && chatStore.streaming) {
    steps.push({
      title: t('chat.resultSynthesis'),
      description: t('chat.resultSynthesisDesc'),
      status: 'running',
      detail: t('chat.generating'),
    })
  }

  return steps
})

const taskTimelineTitle = computed(() => {
  if (chatStore.taskProgress?.phase) {
    return `${t('chat.taskExecuting')} ${chatStore.taskProgress.phase}`
  }
  return t('chat.taskProgress')
})

const taskTimelinePercent = computed(() => {
  const steps = taskTimelineSteps.value
  if (steps.length === 0) return undefined
  const completed = steps.filter(s => s.status === 'completed').length
  const running = steps.filter(s => s.status === 'running').length
  return Math.round(((completed + running * 0.5) / steps.length) * 100)
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

// File handling - upload to server
async function handleFileSelect({ file }: { file: UploadFileInfo }) {
  const nativeFile = file.file
  if (!nativeFile) return false

  const fileKey = nativeFile.name + '-' + nativeFile.size
  if (uploadingFiles.value.has(fileKey)) return false
  uploadingFiles.value.add(fileKey)

  try {
    const uploaded = await sessionsApi.uploadFile(nativeFile)
    selectedFiles.value.push(uploaded)
  } catch (e) {
    console.error('Upload failed:', e)
    ElMessage.error(`${t('chat.fileUploadFailed')} ${(e as Error).message}`)
  } finally {
    uploadingFiles.value.delete(fileKey)
  }
  return false // Prevent default upload
}

function removeFile(index: number) {
  selectedFiles.value.splice(index, 1)
}

function goToFilesPage() {
  router.push('/files')
}

const commandSuggestions = ref<string[]>([])

function handleInput() {
  if (chatStore.isCommand(inputValue.value)) {
    commandSuggestions.value = chatStore.autocompleteCommand(inputValue.value)
  } else {
    commandSuggestions.value = []
  }
}

function selectCommand(suggestion: string) {
  inputValue.value = suggestion + ' '
  commandSuggestions.value = []
}

async function send() {
  const content = inputValue.value.trim()
  if ((!content && !selectedFiles.value.length) || chatStore.streaming) return

  // Handle commands - all logic (session create, execution, message push) handled in chatStore
  if (chatStore.isCommand(content)) {
    inputValue.value = ''
    commandSuggestions.value = []
    await chatStore.executeCommand(content)
    return
  }

  // Build content with file URL references for AI processing
  let finalContent = content
  const files = [...selectedFiles.value]

  // Append file URL references to message for AI
  // Note: file.url already contains token from uploadFile()
  for (const file of files) {
    if (finalContent) finalContent += '\n\n'
    finalContent += `[${t('chat.attachmentName')}: ${file.name}](${file.url})`
  }

  inputValue.value = ''
  selectedFiles.value = []
  commandSuggestions.value = []
  await chatStore.sendMessage(finalContent, undefined, files)
}

function stopGeneration() {
  chatStore.stopGeneration()
}

async function handleModelChange(modelId: string) {
  try {
    await modelsStore.setModel(modelId)
  } catch (e) {
    console.error('Failed to switch model:', e)
  }
}

async function createSession() {
  await chatStore.createSession()
  await chatStore.loadSessions()
}

async function deleteSession(id: string) {
  await chatStore.deleteSession(id)
  await chatStore.loadSessions()
}

function startRename(id: string) {
  const session = chatStore.sessions.find(s => s.id === id)
  if (session) {
    editingSessionId.value = id
    editingName.value = session.title || ''
  }
}

function cancelRename() {
  editingSessionId.value = null
  editingName.value = ''
}

async function saveRename(id: string) {
  if (editingName.value.trim()) {
    await chatStore.renameSession(id, editingName.value.trim())
  }
  editingSessionId.value = null
  editingName.value = ''
}

async function selectSession(id: string) {
  await chatStore.selectSession(id)
}

function scrollToBottom() {
  nextTick(() => {
    messagesRef.value?.scrollTo({ top: messagesRef.value.scrollHeight, behavior: 'smooth' })
  })
}

// Throttled scroll to bottom - only scroll on message count changes, not on every stream content update
let scrollTimer: ReturnType<typeof setTimeout> | null = null
function throttledScrollToBottom() {
  if (scrollTimer) return
  scrollTimer = setTimeout(() => {
    scrollToBottom()
    scrollTimer = null
  }, 150)
}

watch(() => chatStore.messages.length, scrollToBottom)
watch(() => chatStore.toolCalls.length, scrollToBottom)
// Do NOT watch streamContent - the throttled buffer flush in chatStore handles updates

onMounted(async () => {
  await chatStore.loadSessions()
  await chatStore.loadCommands()
  await modelsStore.loadModels()
  await modelsStore.loadCurrentModel()
  // Set default model value
  if (modelsStore.currentModel) {
    currentModelId.value = modelsStore.currentModel.id
  } else if (modelsStore.models.length > 0) {
    currentModelId.value = modelsStore.models[0].id
  }
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
  justify-content: space-between;
  gap: 8px;
  transition: background 0.15s;
}

.session-item:hover { background: #f0f0f0; }
.session-item.active { background: #e8f5e9; }

.session-content {
  flex: 1;
  min-width: 0;
}

.session-title {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 14px;
}

.session-title-edit {
  flex: 1;
}

.session-title-input {
  width: 100%;
}

.session-meta {
  display: none;
}

.session-actions {
  display: flex;
  align-items: center;
  gap: 4px;
  flex-shrink: 0;
  opacity: 0;
  transition: opacity 0.2s;
}

.session-item:hover .session-actions {
  opacity: 1;
}

.session-rename-btn {
  opacity: 0.7;
}

.session-rename-btn:hover {
  opacity: 1;
}

.session-delete {
  opacity: 0.7;
}

.session-delete:hover {
  opacity: 1;
}

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
  padding: 14px 18px;
  border-radius: 16px;
  line-height: 1.75;
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

.system-bubble {
  background: linear-gradient(135deg, #fff7ed 0%, #fef3c7 100%);
  color: #854d0e;
  border: 1px solid #fcd34d;
  border-bottom-left-radius: 4px;
  font-size: 13px;
}

.system-avatar {
  background: #fef3c7;
  border: 1px solid #fcd34d;
}

.tool-bubble {
  background: transparent;
  border: none;
  border-bottom-left-radius: 4px;
  padding: 4px 0;
}

.tool-name-inline {
  font-weight: 500;
  font-size: 11px;
  color: #aaa;
  margin-bottom: 2px;
  font-family: 'SF Mono', 'Fira Code', monospace;
}

.tool-content-inline {
  font-size: 12px;
  color: #bbb;
  max-height: 120px;
  overflow-y: auto;
  white-space: pre-wrap;
  line-height: 1.5;
}

/* ========== Message Images ========== */
.message-images {
  margin-bottom: 8px;
}

/* ========== Image Preview Bar ========== */
.image-preview-bar {
  padding: 8px 24px;
  border-top: 1px solid #e0e0e0;
  background: #fafafa;
}

.preview-item {
  position: relative;
  display: inline-block;
}

.preview-remove {
  position: absolute;
  top: -6px;
  right: -6px;
  opacity: 0.8;
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
  margin-bottom: 6px;
  display: flex;
  flex-wrap: wrap;
  gap: 2px;
  max-height: 60px;
  overflow: hidden;
}

/* ========== Waiting Indicator ========== */
.long-task-progress {
  padding: 12px 16px;
  margin: 8px 12px;
  background: #f0f7ff;
  border: 1px solid #d0e3ff;
  border-radius: 8px;
}

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
.message-bubble :deep(p) { margin: 0 0 10px 0; }
.message-bubble :deep(p:last-child) { margin-bottom: 0; }
.message-bubble :deep(ul), .message-bubble :deep(ol) { margin: 10px 0; padding-left: 28px; }
.message-bubble :deep(li) { margin: 5px 0; }
.message-bubble :deep(blockquote) {
  border-left: 3px solid #d0d0d0;
  padding-left: 14px;
  margin: 10px 0;
  color: #666;
}
.message-bubble :deep(table) {
  border-collapse: collapse;
  width: 100%;
  margin: 10px 0;
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
.message-bubble :deep(h1),
.message-bubble :deep(h2),
.message-bubble :deep(h3),
.message-bubble :deep(h4) {
  margin: 18px 0 10px 0;
  font-weight: 600;
}
.message-bubble :deep(h1) { font-size: 20px; }
.message-bubble :deep(h2) { font-size: 18px; }
.message-bubble :deep(h3) { font-size: 16px; }
.message-bubble :deep(h4) { font-size: 15px; }
.message-bubble :deep(hr) {
  border: none;
  border-top: 1px solid #e0e0e0;
  margin: 14px 0;
}
.message-bubble :deep(pre) {
  margin: 10px 0;
}
.message-bubble :deep(a) {
  color: #e0f7e0;
  text-decoration: underline;
  font-weight: 600;
  text-shadow: 0 1px 2px rgba(0,0,0,0.3);
}
.message-bubble :deep(a:hover) {
  color: #ffffff;
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

/* ========== Preview Bar ========== */
.preview-bar {
  padding: 8px 24px 0;
  background: #fff;
}

.preview-item {
  position: relative;
  display: inline-block;
}

.preview-remove {
  position: absolute;
  top: -6px;
  right: -6px;
  width: 18px;
  height: 18px;
  padding: 0;
  font-size: 12px;
  line-height: 1;
}

.file-preview {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 8px 12px;
  background: #f5f5f5;
  border-radius: 6px;
  border: 1px solid #e0e0e0;
}

.file-name {
  font-size: 12px;
  color: #333;
  max-width: 120px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* ========== ChatGPT-style Input Box ========== */
.input-area {
  padding: 12px 24px 16px;
  background: #fff;
  border-top: 1px solid #e0e0e0;
  position: relative;
}

/* Command suggestions dropdown */
.command-suggestions {
  position: absolute;
  bottom: 100%;
  left: 24px;
  right: 24px;
  background: white;
  border: 1px solid #e0e0e0;
  border-radius: 8px 8px 0 0;
  box-shadow: 0 -4px 12px rgba(0, 0, 0, 0.1);
  max-height: 200px;
  overflow-y: auto;
  margin-bottom: -1px;
}

.suggestion-item {
  padding: 8px 16px;
  cursor: pointer;
  font-size: 14px;
  color: #333;
  border-bottom: 1px solid #f0f0f0;
  transition: background 0.15s;
}

.suggestion-item:hover {
  background: #f5f5f5;
}

.suggestion-item:last-child {
  border-bottom: none;
}

.chat-input-box {
  border: 1px solid #d9d9d9;
  border-radius: 12px;
  padding: 12px 16px;
  background: #fff;
  transition: border-color 0.2s, box-shadow 0.2s;
}

.chat-input-box:focus-within {
  border-color: #18a058;
  box-shadow: 0 0 0 2px rgba(24, 160, 88, 0.15);
}

.chat-textarea {
  --n-border: none !important;
  --n-border-hover: none !important;
  --n-border-focus: none !important;
  --n-box-shadow-focus: none !important;
  --n-padding-left: 0 !important;
  --n-padding-right: 0 !important;
  background: transparent !important;
}

.chat-textarea :deep(.n-input__textarea-el) {
  resize: none;
}

.input-toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-top: 8px;
  padding-top: 8px;
  border-top: 1px solid #f0f0f0;
}

.toolbar-left {
  display: flex;
  align-items: center;
  gap: 4px;
}

.toolbar-model-select {
  width: 220px;
}

.toolbar-btn {
  padding: 4px 8px;
}

.toolbar-right {
  display: flex;
  align-items: center;
}

.send-circle-btn {
  width: 32px;
  height: 32px;
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

  .system-bubble {
    background: #2a2410;
    color: #fbbf24;
    border: 1px solid #78350f;
  }

  .system-avatar {
    background: #2a2410;
    border: 1px solid #78350f;
  }

  .tool-bubble {
    background: transparent;
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
    background: #1a1a1a;
    border-top-color: #333;
  }

  .chat-input-box {
    background: #2a2a2a;
    border-color: #444;
  }

  .chat-input-box:focus-within {
    border-color: #18a058;
    box-shadow: 0 0 0 2px rgba(24, 160, 88, 0.2);
  }

  .input-toolbar {
    border-top-color: #3a3a3a;
  }

  .file-preview {
    background: #333;
    border-color: #444;
  }

  .file-name {
    color: #ddd;
  }

  .preview-bar {
    background: #1a1a1a;
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

/* ========== Message File Attachments ========== */
.message-files {
  margin-bottom: 8px;
}
.message-file-item {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 6px 12px;
  background: rgba(255, 255, 255, 0.15);
  border: 1px solid rgba(255, 255, 255, 0.25);
  border-radius: 8px;
  cursor: pointer;
  transition: background 0.2s, border-color 0.2s;
  font-size: 13px;
  color: #fff;
}
.message-file-item:hover {
  background: rgba(255, 255, 255, 0.25);
  border-color: rgba(255, 255, 255, 0.4);
}
.message-file-name {
  max-width: 200px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>