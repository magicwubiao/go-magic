<template>
  <div class="chat-container">
    <!-- Session Sidebar -->
    <div class="session-sidebar" :class="{ 'mobile-expanded': mobileSessionExpanded }">
      <!-- Mobile drag handle -->
      <div class="mobile-session-handle" @click="mobileSessionExpanded = !mobileSessionExpanded">
        <div class="handle-bar"></div>
      </div>
      <div class="sidebar-header" v-show="!isMobile || mobileSessionExpanded">
        <n-button type="primary" block @click="createSession" size="small">
          + {{ t('chat.newChat') }}
        </n-button>
      </div>
      <div class="session-list" ref="sessionListRef" v-show="!isMobile || mobileSessionExpanded">
        <template v-for="(sessions, profile) in groupedSessions" :key="profile">
          <div class="profile-group-header">{{ profile || t('chat.default') }}</div>
          <div
            v-for="session in sessions"
            :key="session.id"
            class="session-item"
            :class="{ active: chatStore.activeSessionId === session.id }"
            @click="selectSession(session.id)"
            @mouseenter="loadSessionGoals(session.id)"
          >
            <div class="session-content">
              <div v-if="editingSessionId !== session.id" class="session-title">
                <div style="display: flex; align-items: center; gap: 4px;">
                  <!-- Goal indicator - icon only, hover shows details -->
                  <n-popover 
                    v-if="getSessionGoals(session.id).length" 
                    trigger="hover"
                    placement="right"
                    :show-arrow="true"
                    :duration="100"
                    :show="popoverShow[session.id]"
                    @mouseenter="popoverShow[session.id] = true"
                    @mouseleave="popoverShow[session.id] = false"
                  >
                    <template #trigger>
                      <n-icon :component="FlagOutline" :size="14" color="#2080f0" style="cursor: pointer; flex-shrink: 0;" />
                    </template>
                    <template #default>
                      <div style="min-width: 240px; padding: 8px;">
                        <n-text strong style="font-size: 13px; display: block; margin-bottom: 8px;">
                          {{ t('goals.linkedGoals') }}
                        </n-text>
                        <div class="session-goal-list">
                          <div 
                            v-for="goal in getSessionGoals(session.id)" 
                            :key="goal.id" 
                            class="session-goal-item"
                          >
                            <div style="display: flex; align-items: center; justify-content: space-between; width: 100%;">
                              <span style="font-size: 13px; flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">{{ goal.title }}</span>
                              <n-progress :percentage="goal.progress" :show-indicator="false" :height="6" style="width: 60px; margin-left: 8px;" />
                            </div>
                            <n-text depth="3" style="font-size: 11px;">{{ t('goals.statusOptions.' + goal.status) }}</n-text>
                            <n-space :size="4" style="margin-top: 4px;">
                              <n-button size="tiny" text @click="(e: Event) => { e.stopPropagation(); goToGoal(goal.id); }">{{ t('goals.details') }}</n-button>
                              <n-button size="tiny" text type="error" @click="(e: Event) => { e.stopPropagation(); unlinkSessionGoal(goal.id, session.id, session.id); }">{{ t('goals.unlinkGoal') }}</n-button>
                            </n-space>
                          </div>
                        </div>
                      </div>
                    </template>
                  </n-popover>
                  <span class="session-title-text">{{ session.title || t('chat.untitled') }}</span>
                </div>
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
                    <template #icon>
                      <n-icon size="14"><TrashOutline /></n-icon>
                    </template>
                  </n-button>
                </template>
                {{ t('chat.deleteSession') }}
              </n-popconfirm>
            </div>
          </div>
        </template>
        <div v-if="chatStore.sessionsLoading" style="padding: 16px; text-align: center;">
          <n-spin size="small" />
        </div>
        <n-text v-if="!chatStore.sessions.length && !chatStore.sessionsLoading" depth="3" style="padding: 16px; display: block; text-align: center;">
          {{ t('chat.noSessions') }}
        </n-text>
      </div>
    </div>

    <!-- Chat Area -->
    <div class="chat-main">
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
              <div v-if="formatTime(msg.timestamp)" class="message-time">{{ formatTime(msg.timestamp) }}</div>
            </div>
          </template>

          <!-- Assistant message -->
          <template v-else-if="msg.role === 'assistant'">
            <div class="avatar bot-avatar">🤖</div>
            <div class="message-body assistant-body">
              <div class="assistant-content">
                <ReasoningContent :content="msg.content" :streaming="false" />
                <!-- 工具调用统计（置于思考过程下方，含各工具成功/失败明细） -->
                <div v-if="toolCallStats(msg).total > 0" class="tool-stats-block">
                  <div class="tool-stats-line">
                    <n-icon size="13"><BuildOutline /></n-icon>
                    <span>{{ t('chat.toolStats', toolCallStats(msg)) }}</span>
                  </div>
                  <div class="tool-stats-detail">
                    <span v-for="ts in toolCallStats(msg).tools" :key="ts.name" class="tool-chip" :class="{ 'has-failed': ts.failed > 0, 'is-running': ts.running > 0 }">
                      <span class="tool-chip-name">{{ ts.name }}</span>
                      <span class="tool-chip-count">×{{ ts.total }}</span>
                      <span v-if="ts.success > 0" class="tool-chip-ok">✓{{ ts.success }}</span>
                      <span v-if="ts.failed > 0" class="tool-chip-fail">✗{{ ts.failed }}</span>
                      <span v-if="ts.running > 0" class="tool-chip-run">⟳</span>
                    </span>
                  </div>
                </div>
              </div>
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
              <div v-if="formatTime(msg.timestamp)" class="message-time">{{ formatTime(msg.timestamp) }}</div>
            </div>
          </template>
        </div>

        <!-- Streaming area -->
        <template v-if="chatStore.streaming">
          <!-- Task Timeline for long tasks -->
          <div v-if="taskTimelineSteps.length > 0" class="task-timeline-wrap">
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
            <div class="message-body assistant-body">
              <!-- Status panel when no content yet & no running tools -->
              <div v-if="!streamContentOnly && chatStore.activeToolCalls.length === 0 && chatStore.pendingApprovals.length === 0" class="agent-status-panel">
                <div class="status-header">
                  <div class="status-spinner"></div>
                  <span class="status-phase">{{ agentPhase }}</span>
                  <span class="status-elapsed">{{ elapsedDisplay }}</span>
                </div>
                <div class="status-hint">{{ t(thinkingHints[hintIndex]) }}</div>
              </div>

              <!-- 思考过程 + 最终回答，工具调用统计（思考过程下方） -->
              <div v-if="streamContentOnly" class="assistant-content">
                <ReasoningContent :content="streamContentOnly" :streaming="chatStore.streaming" />
              </div>
              <div v-if="toolCallStats().total > 0" class="tool-stats-block">
                <div class="tool-stats-line">
                  <n-icon size="13"><BuildOutline /></n-icon>
                  <span>{{ t('chat.toolStats', toolCallStats()) }}</span>
                </div>
                <div class="tool-stats-detail">
                  <span v-for="ts in toolCallStats().tools" :key="ts.name" class="tool-chip" :class="{ 'has-failed': ts.failed > 0, 'is-running': ts.running > 0 }">
                    <span class="tool-chip-name">{{ ts.name }}</span>
                    <span class="tool-chip-count">×{{ ts.total }}</span>
                    <span v-if="ts.success > 0" class="tool-chip-ok">✓{{ ts.success }}</span>
                    <span v-if="ts.failed > 0" class="tool-chip-fail">✗{{ ts.failed }}</span>
                    <span v-if="ts.running > 0" class="tool-chip-run">⟳</span>
                  </span>
                </div>
              </div>
            </div>
          </div>
        </template>

        <n-text v-if="!chatStore.messages.length && !chatStore.streaming" depth="3" class="empty-hint">
          {{ t('chat.selectSession') }}
        </n-text>
      </div>

      <!-- 底部固定审批栏：待审批命令始终可见可操作，不随对话滚动。 -->
      <div v-if="chatStore.pendingApprovals.length > 0" class="approval-bar">
        <div class="approval-bar-list">
          <ChatApprovalCard
            v-for="approval in chatStore.pendingApprovals"
            :key="approval.id"
            :approval="approval"
            :session-id="chatStore.activeSessionId || ''"
          />
        </div>
      </div>

      <!-- File preview before sending -->
      <div v-if="selectedFiles.length" class="preview-bar">
        <n-space>
          <div v-for="(file, idx) in selectedFiles" :key="'file-'+idx" class="preview-item file-preview">
            <n-icon size="24"><DocumentOutline /></n-icon>
            <span class="file-name">{{ file.name }}</span>
            <n-tag v-if="file.size" size="tiny" type="info">{{ (file.size / 1024).toFixed(1) }} KB</n-tag>
            <n-button class="preview-remove" size="tiny" circle type="error" @click="removeFile(idx)">
              <template #icon><n-icon size="12"><CloseCircleOutline /></n-icon></template>
            </n-button>
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
              <!-- File upload -->
              <n-upload
                :show-file-list="false"
                :multiple="true"
                :custom-request="handleFileSelect"
              >
                <n-button size="tiny" quaternary class="toolbar-btn" :title="t('chat.uploadFile')">
                  <template #icon>
                    <n-icon><AttachOutline /></n-icon>
                  </template>
                </n-button>
              </n-upload>
            </div>
            <div class="toolbar-right">
              <n-select
                v-if="modelOptions.length > 0"
                v-model:value="currentModelId"
                :options="modelOptions"
                size="small"
                style="min-width: 160px; max-width: 260px; margin-right: 8px"
                :placeholder="t('chat.selectModel')"
                :consistent-menu-width="false"
                :render-label="renderModelLabel"
                @update:value="handleModelChange"
              />
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

      <!-- 工作目录栏（最下层） -->
      <div class="workdir-bar">
        <!-- 已锁定：用户已设置，只读不可改（每个会话仅允许设置一次） -->
        <div
          v-if="chatStore.currentWorkDirUserSet"
          class="workdir-bar-inner workdir-bar-locked"
          :title="t('chat.workDirLocked') + ' — ' + chatStore.currentWorkDir"
        >
          <n-icon size="13"><LockClosedOutline /></n-icon>
          <span class="workdir-bar-path">{{ chatStore.currentWorkDir }}</span>
          <span class="workdir-bar-hint">{{ t('chat.workDirLocked') }}</span>
        </div>
        <!-- 系统默认目录（尚未由用户设置）：可点击浏览，可清除 -->
        <div
          v-else-if="chatStore.currentWorkDir"
          class="workdir-bar-inner"
          @click="handleWorkDirMenu('browse')"
          :title="t('chat.workDir') + ' — ' + chatStore.currentWorkDir"
        >
          <n-icon size="13"><FolderOpenOutline /></n-icon>
          <span class="workdir-bar-path">{{ chatStore.currentWorkDir }}</span>
          <n-icon size="13" class="workdir-bar-clear" @click.stop="clearWorkDir">
            <CloseCircleOutline />
          </n-icon>
        </div>
        <!-- 未设置：按钮触发目录选择 -->
        <div v-else class="workdir-bar-inner workdir-bar-empty">
          <n-icon size="13"><FolderOutline /></n-icon>
          <span>{{ t('chat.workDirNone') }}</span>
          <n-button size="tiny" quaternary class="workdir-bar-set-btn" @click="handleWorkDirMenu('browse')">
            <template #icon><n-icon size="13"><FolderOpenOutline /></n-icon></template>
            {{ t('chat.workDirSet') }}
          </n-button>
        </div>
      </div>
    </div>

    <!-- Goal Sidebar -->
    <RightSidebar v-model:mobile-visible="rightSidebarMobileVisible" />

    <!-- Mobile right sidebar toggle FAB -->
    <n-button
      class="right-sidebar-fab"
      circle
      size="large"
      @click="rightSidebarMobileVisible = !rightSidebarMobileVisible"
      :title="t('sidebar.expand')"
    >
      <template #icon>
        <n-icon :component="FlagOutline" :size="20" />
      </template>
    </n-button>

    <!-- Work Directory Picker Modal -->
    <n-modal v-model:show="showDirPicker" :title="t('chat.workDir')" preset="card" class="modal-responsive" style="width: 520px; max-width: 96vw;">
      <!-- Breadcrumb -->
      <div class="dir-picker-breadcrumb">
        <n-button size="tiny" quaternary :disabled="!dirParent" @click="navigateDir(dirParent)">
          <template #icon><n-icon><FolderOpenOutline /></n-icon></template>
          ..
        </n-button>
        <n-text class="dir-picker-current" :title="dirCurrentPath">{{ dirCurrentPath }}</n-text>
        <n-button size="tiny" quaternary @click="startNewFolder" :title="t('chat.newFolder')">
          <template #icon><n-icon><AddOutline /></n-icon></template>
        </n-button>
      </div>

      <!-- New folder input -->
      <div v-if="showNewFolderInput" class="dir-picker-new-folder">
        <n-input
          v-model:value="newFolderName"
          size="small"
          :placeholder="t('chat.newFolder')"
          @keyup.enter="createNewFolder"
          @blur="cancelNewFolder"
          ref="newFolderInputRef"
        />
      </div>

      <!-- Directory list -->
      <div class="dir-picker-list">
        <div v-if="dirLoading" class="dir-picker-loading">
          <n-spin size="small" />
        </div>
        <div v-else-if="dirEntries.length === 0" class="dir-picker-empty">
          <n-text depth="3">{{ t('chat.workDirEmpty') }}</n-text>
        </div>
        <div
          v-for="entry in dirEntries"
          v-else
          :key="entry.path"
          class="dir-picker-item"
          @click="navigateDir(entry.path)"
        >
          <n-icon size="16"><FolderOutline /></n-icon>
          <span>{{ entry.name }}</span>
        </div>
      </div>

      <template #footer>
        <n-space justify="end">
          <n-button @click="showDirPicker = false">{{ t('common.cancel') }}</n-button>
          <n-button type="primary" @click="handleWorkDirMenu('set')" :disabled="!chatStore.activeSessionId || !dirCurrentPath">
            {{ t('chat.workDirSet') }}
          </n-button>
        </n-space>
      </template>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, h, onMounted, onUnmounted, nextTick, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useMessage } from 'naive-ui'
import { marked } from 'marked'
import hljs from 'highlight.js/lib/core'
import javascript from 'highlight.js/lib/languages/javascript'
import typescript from 'highlight.js/lib/languages/typescript'
import python from 'highlight.js/lib/languages/python'
import go from 'highlight.js/lib/languages/go'
import bash from 'highlight.js/lib/languages/bash'
import json from 'highlight.js/lib/languages/json'
import xml from 'highlight.js/lib/languages/xml'
import css from 'highlight.js/lib/languages/css'
import markdown from 'highlight.js/lib/languages/markdown'
import 'highlight.js/styles/github-dark.css'

hljs.registerLanguage('javascript', javascript)
hljs.registerLanguage('typescript', typescript)
hljs.registerLanguage('python', python)
hljs.registerLanguage('go', go)
hljs.registerLanguage('bash', bash)
hljs.registerLanguage('json', json)
hljs.registerLanguage('xml', xml)
hljs.registerLanguage('css', css)
hljs.registerLanguage('markdown', markdown)
import { useChatStore, type ToolCallEvent } from '@/stores/chat'
import { useGoalsStore } from '@/stores/goals'
import { useModelsStore } from '@/stores/models'
import ReasoningContent from '@/components/ReasoningContent.vue'
import RightSidebar from '@/components/RightSidebar.vue'
import TaskTimeline from '@/components/TaskTimeline.vue'
import ChatApprovalCard from '@/components/ChatApprovalCard.vue'
import type { TimelineStep } from '@/components/TaskTimeline.vue'
import { AttachOutline, SendOutline, StopCircleOutline, DocumentOutline, PencilOutline, FlagOutline, FolderOpenOutline, FolderOutline, AddOutline, LockClosedOutline, CloseCircleOutline, TrashOutline, ChevronDownOutline, ChevronForwardOutline, RefreshOutline, BuildOutline } from '@vicons/ionicons5'
import type { UploadCustomRequestOptions } from 'naive-ui'
import * as sessionsApi from '@/api/sessions'
import { useRouter } from 'vue-router'

const { t } = useI18n()
const chatStore = useChatStore()
const goalsStore = useGoalsStore()
const modelsStore = useModelsStore()
const router = useRouter()
const message = useMessage()
const inputValue = ref('')
const rightSidebarMobileVisible = ref(false)
const mobileSessionExpanded = ref(false)
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
const messagesRef = ref<HTMLDivElement>()
const sessionListRef = ref<HTMLDivElement>()

// Elapsed timer for streaming
const elapsedSeconds = ref(0)
let elapsedTimer: ReturnType<typeof setInterval> | null = null

watch(() => chatStore.streaming, (streaming) => {
  if (streaming) {
    elapsedSeconds.value = 0
    elapsedTimer = setInterval(() => {
      elapsedSeconds.value++
    }, 1000)
  } else {
    if (elapsedTimer) {
      clearInterval(elapsedTimer)
      elapsedTimer = null
    }
  }
})

onUnmounted(() => {
  if (elapsedTimer) clearInterval(elapsedTimer)
})

const elapsedDisplay = computed(() => {
  const s = elapsedSeconds.value
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  const sec = s % 60
  return `${m}m${sec}s`
})

// Model selection
const modelOptions = computed(() => modelsStore.modelSelectOptions)
const currentModelId = ref(modelsStore.currentModelInfo?.id || '')

// Sync currentModelId when store changes
watch(() => modelsStore.currentModelInfo, (info) => {
  if (info?.id) {
    currentModelId.value = info.id
  }
}, { immediate: true })

function handleModelChange(value: string) {
  const [provider, ...rest] = value.split('/')
  const model = rest.join('/')
  if (provider && model) {
    modelsStore.setModel(model, provider.trim())
  }
}

function renderModelLabel(option: { label: string; value: string }) {
  const parts = option.label.split(' / ')
  const provider = parts[0] || ''
  const model = parts[1] || ''
  return h('div', {
    style: 'display: flex; align-items: center; gap: 6px; padding: 4px 0;'
  }, [
    h('span', null, provider),
    h('span', null, `/ ${model}`),
  ])
}

// Current agent phase
const agentPhase = computed(() => {
  if (!chatStore.streaming) return ''
  const runningTools = chatStore.activeToolCalls
  if (runningTools.length > 0) {
    return t('chat.executingTool', { name: runningTools[0].name })
  }
  if (chatStore.streamContent) {
    return t('chat.generating')
  }
  if (chatStore.toolCalls.length > 0) {
    return t('chat.generating')
  }
  return t('chat.thinkingPhase')
})

// 工具调用统计：不再穿插展示工具详情，只在思考过程上方汇总展示。
// 兼容两种数据源：流式中的 chatStore.toolCalls 和历史消息的 tool_calls_snapshot。
interface ToolStatItem {
  name: string
  total: number
  success: number
  failed: number
  running: number
}

function toolCallStats(msg?: sessionsApi.Message): { total: number; success: number; failed: number; running: number; tools: ToolStatItem[] } {
  const list: ToolCallEvent[] = msg
    ? ((msg.tool_calls_snapshot ?? []) as ToolCallEvent[])
    : chatStore.toolCalls
  let success = 0
  let failed = 0
  let running = 0
  const byName = new Map<string, ToolStatItem>()
  for (const tc of list) {
    let s = 0, f = 0, r = 0
    if (tc.status === 'running') { running++; r = 1 }
    else if (tc.status === 'error' || tc.success === false) { failed++; f = 1 }
    else { success++; s = 1 }
    let item = byName.get(tc.name)
    if (!item) {
      item = { name: tc.name, total: 0, success: 0, failed: 0, running: 0 }
      byName.set(tc.name, item)
    }
    item.total++
    item.success += s
    item.failed += f
    item.running += r
  }
  return { total: list.length, success, failed, running, tools: Array.from(byName.values()) }
}

// 流式期间只展示思考过程与最终回答文本（不再按 timeline 切片穿插工具块）
const streamContentOnly = computed(() => chatStore.streamContent || '')

// Rotating hints during thinking
const thinkingHints = [
  'chat.hintAnalyzing',
  'chat.hintPlanning',
  'chat.hintReasoning',
]
const hintIndex = ref(0)
let hintTimer: ReturnType<typeof setInterval> | null = null

watch(() => chatStore.streaming, (streaming) => {
  if (streaming && !chatStore.streamContent && chatStore.activeToolCalls.length === 0) {
    hintIndex.value = 0
    hintTimer = setInterval(() => {
      hintIndex.value = (hintIndex.value + 1) % thinkingHints.length
    }, 4000)
  } else {
    if (hintTimer) {
      clearInterval(hintTimer)
      hintTimer = null
    }
  }
})

watch(() => chatStore.streamContent, (val) => {
  if (val && hintTimer) {
    clearInterval(hintTimer)
    hintTimer = null
  }
})

onUnmounted(() => {
  if (hintTimer) clearInterval(hintTimer)
})

// Rename session
const editingSessionId = ref<string | null>(null)
const editingName = ref('')

// File upload
const selectedFiles = ref<sessionsApi.UploadedFile[]>([])
const uploadingFiles = ref<Set<string>>(new Set())

// Work directory
const showDirPicker = ref(false)
const dirCurrentPath = ref('')
const dirEntries = ref<sessionsApi.DirEntry[]>([])
const dirLoading = ref(false)
const showNewFolderInput = ref(false)
const newFolderName = ref('')
const newFolderInputRef = ref<{ focus: () => void } | null>(null)

const dirParent = computed(() => dirEntries.value.find(e => e.name === '..')?.path || '')

// Session goals cache
const sessionGoals = ref<Record<string, sessionsApi.SessionGoal[]>>({})

// Popover show state
const popoverShow = ref<Record<string, boolean>>({})

// Custom code renderer for highlight.js
const codeRenderer = (code: string, lang?: string): string => {
  const language = lang && hljs.getLanguage(lang) ? lang : null
  const highlighted = language
    ? hljs.highlight(code, { language }).value
    : hljs.highlightAuto(code).value
  // 移除 inline onclick，改用 class + 事件委托（见 handleCodeBlockClick）
  const copyBtn = `<button class="code-copy-btn" type="button">Copy</button>`
  return `<div class="code-block">${copyBtn}<pre><code class="hljs${language ? ` language-${language}` : ''}">${highlighted}</code></pre></div>`
}

// ReasoningContent 已改用独立 Marked 实例，不再往全局单例上叠加配置；
// 这里显式声明 breaks/gfm，保持正文渲染行为与之前（配置被叠加后）一致。
marked.use({ renderer: { code: codeRenderer }, breaks: true, gfm: true })

// 处理代码块按钮点击（事件委托替代 inline onclick，避免 v-html + inline handler XSS 风险）
function handleCodeBlockClick(e: MouseEvent) {
  const target = e.target as HTMLElement
  const btn = target.closest('.code-copy-btn') as HTMLElement | null
  if (!btn) return
  const codeEl = btn.parentElement?.querySelector('code')
  const code = codeEl?.textContent || ''
  navigator.clipboard.writeText(code).catch(() => { /* ignore */ })
  const original = btn.textContent
  btn.textContent = '✓ Copied'
  setTimeout(() => { btn.textContent = original }, 2000)
}

onMounted(() => {
  document.addEventListener('click', handleCodeBlockClick)
})

onUnmounted(() => {
  document.removeEventListener('click', handleCodeBlockClick)
})

// Markdown 渲染缓存：避免 v-for 重渲染时重复解析同一内容
const mdCache = new Map<string, string>()
const MD_CACHE_LIMIT = 200

function renderMarkdown(content: string): string {
  const cached = mdCache.get(content)
  if (cached !== undefined) return cached
  const html = marked.parse(content) as string
  // 简单的 LRU：超过上限时清空最早的一半
  if (mdCache.size >= MD_CACHE_LIMIT) {
    const keys = mdCache.keys()
    for (let i = 0; i < MD_CACHE_LIMIT / 2; i++) {
      mdCache.delete(keys.next().value)
    }
  }
  mdCache.set(content, html)
  return html
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

// 兼容三种 timestamp 格式：
//   1. ISO8601 字符串  (推荐，如 "2026-07-30T12:34:56.789Z")
//   2. 毫秒级数字      (如 1785474896789)
//   3. 秒级数字        (如 1785474896，< 1e12 时自动当作秒处理)
function toDate(ts: string | number): Date {
  if (typeof ts === 'number') {
    const n = ts as number
    // 秒级 Unix timestamp: 10 位数 → 2001-09 ~ 2286-11 之间
    // 毫秒级 Unix timestamp: 13 位数 → 2001-09 ~ 2286-11 之间 *1000
    // 也兼容纳秒字符串被转成大数字
    if (n < 1e12) return new Date(n * 1000) // 秒 → 毫秒
    return new Date(n) // 已是毫秒
  }
  // 字符串
  const s = String(ts).trim()
  if (!s) return new Date(NaN)
  // 纯数字字符串
  if (/^-?\d+$/.test(s)) {
    const n = Number(s)
    if (isFinite(n)) {
      if (n < 1e12) return new Date(n * 1000)
      return new Date(n)
    }
  }
  // 普通日期字符串（含 ISO RFC3339、中文格式等）
  const d = new Date(s)
  if (!isNaN(d.getTime())) return d
  // 补 Z 兜底（UTC 无时区标识）
  return new Date(s + 'Z')
}

// File handling - upload to server
function handleFileSelect({ file, onFinish, onError }: UploadCustomRequestOptions) {
  const nativeFile = file.file
  if (!nativeFile) {
    onError()
    return
  }

  const fileKey = nativeFile.name + '-' + nativeFile.size
  if (uploadingFiles.value.has(fileKey)) {
    onError()
    return
  }
  uploadingFiles.value.add(fileKey)

  sessionsApi.uploadFile(nativeFile)
    .then((uploaded) => {
      selectedFiles.value.push(uploaded)
      onFinish()
    })
    .catch((e) => {
      console.error('Upload failed:', e)
      message.error(`${t('chat.fileUploadFailed')} ${(e as Error).message}`)
      onError()
    })
    .finally(() => {
      uploadingFiles.value.delete(fileKey)
    })
}

function removeFile(index: number) {
  selectedFiles.value.splice(index, 1)
}

function goToFilesPage() {
  router.push('/files')
}

// Work directory functions
async function loadDirs(path?: string) {
  dirLoading.value = true
  try {
    const res = await sessionsApi.listDirs(path)
    dirCurrentPath.value = res.current
    dirEntries.value = res.dirs || []
  } catch (e) {
    console.error('Failed to list directories:', e)
    dirEntries.value = []
  } finally {
    dirLoading.value = false
  }
}

function navigateDir(path: string) {
  if (!path) return
  showNewFolderInput.value = false
  newFolderName.value = ''
  loadDirs(path)
}

function startNewFolder() {
  showNewFolderInput.value = true
  newFolderName.value = ''
  nextTick(() => {
    newFolderInputRef.value?.focus()
  })
}

function cancelNewFolder() {
  setTimeout(() => {
    showNewFolderInput.value = false
    newFolderName.value = ''
  }, 150)
}

async function createNewFolder() {
  const name = newFolderName.value.trim()
  if (!name) {
    showNewFolderInput.value = false
    return
  }
  try {
    await sessionsApi.createDir(dirCurrentPath.value, name)
    newFolderName.value = ''
    showNewFolderInput.value = false
    loadDirs(dirCurrentPath.value)
  } catch (e: any) {
    message.error(e?.message || t('common.operationFailed'))
  }
}

async function handleWorkDirMenu(key: string) {
  if (key === 'browse') {
    // 已锁定的会话不再允许重新选择目录
    if (chatStore.currentWorkDirUserSet) return
    showDirPicker.value = true
    await loadDirs(chatStore.currentWorkDir || undefined)
  } else if (key === 'set') {
    if (!chatStore.activeSessionId || !dirCurrentPath.value) return
    try {
      await chatStore.updateSessionWorkDir(chatStore.activeSessionId, dirCurrentPath.value)
      showDirPicker.value = false
      message.success(t('chat.workDir') + ': ' + dirCurrentPath.value)
    } catch (e: any) {
      message.error(e?.message || t('chat.workDirLocked'))
    }
  } else if (key === 'clear') {
    if (chatStore.activeSessionId) {
      try {
        await chatStore.updateSessionWorkDir(chatStore.activeSessionId, '')
        message.success(t('chat.workDirCleared'))
      } catch (e: any) {
        message.error(e?.message || t('chat.workDirLocked'))
      }
    }
  }
}

function clearWorkDir() {
  if (chatStore.activeSessionId) {
    chatStore.updateSessionWorkDir(chatStore.activeSessionId, '')
      .then(() => message.success(t('chat.workDirCleared')))
      .catch((e: any) => message.error(e?.message || t('chat.workDirLocked')))
  }
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

async function createSession() {
  await chatStore.createSession()
  await chatStore.loadSessions()
}

async function deleteSession(id: string) {
  const session = chatStore.sessions.find(s => s.id === id)
  const deleteFiles = !session?.work_dir_user_set
  await chatStore.deleteSession(id, deleteFiles)
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

// Load goals for a session
async function loadSessionGoals(sessionId: string) {
  // 等待 Vue 更新完成后再检查缓存
  await nextTick()
  if (sessionGoals.value[sessionId]) return
  try {
    sessionGoals.value[sessionId] = await sessionsApi.getSessionGoals(sessionId)
  } catch (e) {
    sessionGoals.value[sessionId] = []
  }
}

// Get goals for a session
function getSessionGoals(sessionId: string): sessionsApi.SessionGoal[] {
  return sessionGoals.value[sessionId] || []
}

// Get goal progress for display
function getSessionGoalProgress(goalId: string, sessionId: string): number {
  const goals = getSessionGoals(sessionId)
  const goal = goals.find(g => g.id === goalId)
  return goal?.progress || 0
}

// Navigate to goals page
function goToGoal(goalId: string) {
  router.push(`/goals/${goalId}`)
}

// Unlink session from goal
async function unlinkSessionGoal(goalId: string, sessionId: string, popoverKey: string) {
  try {
    console.log('Unlinking session:', goalId, sessionId)
    await goalsStore.unlinkSession(goalId, sessionId)
    console.log('Unlink successful')
    // 替换对象触发响应式更新
    popoverShow.value = { ...popoverShow.value, [popoverKey]: false }
    const newGoals = { ...sessionGoals.value }
    delete newGoals[sessionId]
    sessionGoals.value = newGoals
    await loadSessionGoals(sessionId)
    message.success(t('goals.unlinked'))
  } catch (e: any) {
    console.error('Unlink failed:', e)
    message.error(e?.message || t('common.operationFailed'))
  }
}

function scrollToBottom() {
  nextTick(() => {
    messagesRef.value?.scrollTo({ top: messagesRef.value.scrollHeight, behavior: 'smooth' })
  })
}

// Handle session list scroll - load more when scrolled to bottom
let loadMoreThrottleTimer: ReturnType<typeof setTimeout> | null = null
function handleSessionScroll(e: Event) {
  if (loadMoreThrottleTimer) return
  if (chatStore.sessionsLoading) return
  if (!chatStore.sessionsHasMore) return

  const target = e.target as HTMLDivElement
  const threshold = 100
  const isNearBottom = target.scrollHeight - target.scrollTop - target.clientHeight < threshold

  if (isNearBottom) {
    loadMoreThrottleTimer = setTimeout(() => {
      loadMoreThrottleTimer = null
      chatStore.loadMoreSessions()
    }, 200)
  }
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
// 新审批到达时自动滚动到底部，避免用户在查看历史时错过待审批卡片
watch(() => chatStore.pendingApprovals.length, scrollToBottom)

// 审批快捷键：A=批准首个待审批，D=拒绝首个待审批。输入框聚焦时不响应。
function handleApprovalKeydown(e: KeyboardEvent) {
  const target = e.target as HTMLElement
  if (target && (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.isContentEditable)) {
    return
  }
  const pending = chatStore.activePendingApprovals
  if (pending.length === 0) return
  const key = e.key.toLowerCase()
  if (key === 'a') {
    e.preventDefault()
    const first = pending[0]
    if (first) chatStore.resolveChatApproval(chatStore.activeSessionId || '', first.id, true)
  } else if (key === 'd') {
    e.preventDefault()
    const first = pending[0]
    if (first) chatStore.resolveChatApproval(chatStore.activeSessionId || '', first.id, false)
  }
}
onMounted(() => {
  window.addEventListener('keydown', handleApprovalKeydown)
})
onUnmounted(() => {
  window.removeEventListener('keydown', handleApprovalKeydown)
})

// 监听关联变化，清空会话目标缓存
watch(() => goalsStore.linkVersion, () => {
  // 清空所有会话的目标缓存，下次鼠标悬停时会重新加载
  sessionGoals.value = {}
})

// Do NOT watch streamContent - the throttled buffer flush in chatStore handles updates

onMounted(async () => {
  await chatStore.loadSessions()
  modelsStore.loadModels()
  // Bind scroll event for session list infinite scroll
  if (sessionListRef.value) {
    sessionListRef.value.addEventListener('scroll', handleSessionScroll)
  }
})
</script>

<style scoped>
.chat-container {
  display: flex;
  height: 100vh;
  min-height: 0;
}

/* ========== Sidebar ========== */
.session-sidebar {
  width: 240px;
  border-right: 1px solid #e0e0e0;
  display: flex;
  flex-direction: column;
  flex-shrink: 0;
  background: #fff;
  height: 100vh;
  overflow: hidden;
}

.sidebar-header {
  flex-shrink: 0;
  padding: 12px;
  border-bottom: 1px solid #e0e0e0;
  height: 49px;
  box-sizing: border-box;
}

.session-list {
  flex: 1;
  overflow-y: auto;
  min-height: 0;
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
  display: flex;
  align-items: center;
  gap: 4px;
}

.session-title-text {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
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
  min-width: 360px;
  background: #fff;
}

.messages {
  flex: 1;
  overflow-y: auto;
  padding: 20px 24px;
  padding-bottom: 80px;
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
  max-width: 900px;
  margin-left: auto;
  margin-right: auto;
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

/* Assistant 回答不使用气泡，撑满宽度 */
.message-body.assistant-body {
  max-width: calc(100% - 50px);
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

/* Assistant 回答不使用气泡，直接展示内容 */
.assistant-content {
  color: #1f2937;
  line-height: 1.75;
  word-break: break-word;
  overflow-wrap: break-word;
}

.tool-calls-wrap {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-top: 10px;
  margin-bottom: 6px;
}

/* 工具调用统计（思考过程下方）：汇总行 + 各工具明细 chip */
.tool-stats-block {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin-top: 8px;
}

.tool-stats-line {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 3px 10px;
  border-radius: 12px;
  background: rgba(108, 92, 231, 0.08);
  color: #6c5ce7;
  font-size: 12px;
  line-height: 1.4;
  width: fit-content;
}

.tool-stats-line .n-icon {
  opacity: 0.85;
}

.tool-stats-detail {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  padding-left: 2px;
}

.tool-chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 2px 8px;
  border-radius: 10px;
  background: rgba(108, 92, 231, 0.06);
  border: 1px solid rgba(108, 92, 231, 0.18);
  font-size: 11px;
  line-height: 1.5;
  color: #6c5ce7;
  max-width: 100%;
}

.tool-chip-name {
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.tool-chip-count {
  opacity: 0.75;
}

.tool-chip-ok {
  color: #18a058;
  font-weight: 600;
}

.tool-chip-fail {
  color: #d03050;
  font-weight: 600;
}

.tool-chip-run {
  color: #f0a020;
  font-weight: 600;
}

.tool-chip.has-failed {
  border-color: rgba(208, 48, 80, 0.35);
  background: rgba(208, 48, 80, 0.05);
}

.tool-chip.is-running {
  border-color: rgba(240, 160, 32, 0.4);
  background: rgba(240, 160, 32, 0.06);
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
.task-timeline-wrap {
  margin: 8px auto;
  max-width: 900px;
}

.long-task-progress {
  padding: 12px 16px;
  margin: 8px auto;
  background: #f0f7ff;
  border: 1px solid #d0e3ff;
  border-radius: 8px;
  max-width: 900px;
}

/* Agent status panel */
.agent-status-panel {
  padding: 14px 18px;
  background: #f5f5f5;
  border-radius: 16px;
  border-bottom-left-radius: 4px;
  min-width: 200px;
}

.status-header {
  display: flex;
  align-items: center;
  gap: 10px;
}

.status-spinner {
  width: 16px;
  height: 16px;
  border: 2px solid #d0d0d0;
  border-top-color: #666;
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
  flex-shrink: 0;
}

@keyframes spin {
  to { transform: rotate(360deg); }
}

.status-phase {
  font-size: 13px;
  color: #333;
  font-weight: 500;
  flex: 1;
}

.status-elapsed {
  font-size: 12px;
  color: #999;
  font-variant-numeric: tabular-nums;
  background: #e8e8e8;
  padding: 2px 8px;
  border-radius: 8px;
}

.status-hint {
  font-size: 12px;
  color: #888;
  margin-top: 8px;
  padding-left: 26px;
  animation: fadeInOut 4s ease-in-out infinite;
}

@keyframes fadeInOut {
  0%, 100% { opacity: 0.5; }
  50% { opacity: 1; }
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
  max-width: 900px;
  margin-left: auto;
  margin-right: auto;
}

/* 底部固定审批栏 */
.approval-bar {
  padding: 8px 16px;
  background: linear-gradient(180deg, #fffdf5 0%, #fff 100%);
  border-top: 1px solid #f0e0c0;
  box-shadow: 0 -2px 8px rgba(240, 160, 32, 0.08);
}

.approval-bar-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
  max-height: 260px;
  overflow-y: auto;
  max-width: 900px;
  margin-left: auto;
  margin-right: auto;
}

.preview-item {
  position: relative;
  display: inline-block;
}

.preview-remove {
  position: absolute;
  top: -6px;
  right: -6px;
  min-width: 18px;
  min-height: 18px;
  padding: 0;
  line-height: 1;
  display: inline-flex;
  align-items: center;
  justify-content: center;
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
  flex-shrink: 0;
}

.chat-input-box {
  max-width: 900px;
  margin-left: auto;
  margin-right: auto;
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

.workdir-btn {
  display: flex;
  align-items: center;
  gap: 4px;
  max-width: 200px;
}

.workdir-label {
  font-size: 11px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
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

  .assistant-content {
    color: #e5e7eb;
  }

  .tool-stats-line {
    background: rgba(139, 124, 246, 0.15);
    color: #a29bfe;
  }

  .tool-chip {
    background: rgba(139, 124, 246, 0.12);
    border-color: rgba(139, 124, 246, 0.3);
    color: #a29bfe;
  }

  .tool-chip-ok {
    color: #63e2b7;
  }

  .tool-chip-fail {
    color: #e88080;
  }

  .tool-chip-run {
    color: #f2c97d;
  }

  .tool-chip.has-failed {
    border-color: rgba(232, 128, 128, 0.4);
    background: rgba(232, 128, 128, 0.08);
  }

  .tool-chip.is-running {
    border-color: rgba(242, 201, 125, 0.4);
    background: rgba(242, 201, 125, 0.08);
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

  .agent-status-panel {
    background: #1e1e1e;
  }

  .status-spinner {
    border-color: #333;
    border-top-color: #888;
  }

  .status-phase {
    color: #ddd;
  }

  .status-elapsed {
    color: #666;
    background: #2a2a2a;
  }

  .status-hint {
    color: #666;
  }

  .message-time {
    color: #555;
  }

  .input-area {
    background: #1a1a1a;
    border-top-color: #333;
  }

  .approval-bar {
    background: linear-gradient(180deg, #2a2014 0%, #1e1e1e 100%);
    border-top-color: #4a3818;
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

  .workdir-bar {
    background: #161616;
    border-top-color: #333;
  }

  .workdir-bar-inner {
    color: #aaa;
  }

  .workdir-bar-path {
    color: #aaa;
  }

  .workdir-bar-hint {
    background: #2a2a2a;
    color: #777;
  }

  .workdir-bar-locked .workdir-bar-path {
    color: #777;
  }

  .workdir-bar-empty {
    color: #777;
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

/* Work directory picker */
.work-dir-label {
  font-size: 11px;
  max-width: 80px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  margin-left: 4px;
}

.dir-picker-breadcrumb {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 0;
  border-bottom: 1px solid #f0f0f0;
}

.dir-picker-current {
  font-size: 12px;
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: monospace;
  color: #666;
}

.dir-picker-new-folder {
  padding: 8px 0;
  border-bottom: 1px solid #f0f0f0;
}

.dir-picker-list {
  max-height: 300px;
  overflow-y: auto;
  padding: 8px 0;
}

.dir-picker-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  border-radius: 4px;
  cursor: pointer;
  font-size: 13px;
  transition: background 0.15s;
}

.dir-picker-item:hover {
  background: #f0f0f0;
}

.dir-picker-empty,
.dir-picker-loading {
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
}

/* ========== Work Directory Bar（chat 最下层） ========== */
.workdir-bar {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  min-width: 0;
  padding: 6px 24px;
  background: #fafafa;
  border-top: 1px solid #e8e8e8;
  font-size: 12px;
}

.workdir-bar-inner {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
  cursor: pointer;
  color: #555;
  max-width: 900px;
  margin-left: auto;
  margin-right: auto;
  width: 100%;
}

.workdir-bar-path {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: 'SF Mono', 'Fira Code', 'Consolas', monospace;
  font-size: 12px;
  color: #555;
}

.workdir-bar-hint {
  font-size: 11px;
  color: #999;
  flex-shrink: 0;
  padding: 1px 6px;
  background: #ececec;
  border-radius: 4px;
}

.workdir-bar-locked {
  cursor: default;
}

.workdir-bar-locked .workdir-bar-path {
  color: #999;
}

.workdir-bar-empty {
  color: #999;
  cursor: default;
}

.workdir-bar-clear {
  cursor: pointer;
  opacity: 0.55;
  flex-shrink: 0;
}

.workdir-bar-clear:hover {
  opacity: 1;
}

.workdir-bar-set-btn {
  font-size: 11px;
  flex-shrink: 0;
}

.right-sidebar-fab {
  display: none;
}

/* Mobile drag handle - hidden on desktop */
.mobile-session-handle {
  display: none;
}

/* Responsive: Mobile devices */
@media (max-width: 768px) {
  .chat-container {
    flex-direction: column;
  }
  
  .session-sidebar {
    width: 100%;
    height: auto;
    max-height: 30px;
    border-right: none;
    border-bottom: 1px solid #e0e0e0;
    transition: max-height 0.3s ease;
    overflow: hidden;
  }

  .session-sidebar.mobile-expanded {
    max-height: 60vh;
  }
  
  .session-list {
    position: relative;
    top: 0;
    max-height: none;
    flex: 1;
  }

  .mobile-session-handle {
    display: flex;
    justify-content: center;
    align-items: center;
    padding: 10px 0;
    cursor: pointer;
  }

  .handle-bar {
    width: 56px;
    height: 6px;
    border-radius: 3px;
    background: #bbb;
    transition: background 0.2s;
  }

  .mobile-session-handle:hover .handle-bar {
    background: #888;
  }
  
  .chat-main {
    flex: 1;
    min-height: 0;
  }
  
  .messages {
    padding: 8px;
  }
  
  .message {
    gap: 6px;
  }
  
  .avatar {
    width: 28px;
    height: 28px;
    font-size: 14px;
  }
  
  .message-bubble {
    max-width: 85%;
    padding: 8px 10px;
    font-size: 14px;
  }
  
  .input-area {
    padding: 8px;
    padding-bottom: calc(8px + env(safe-area-inset-bottom));
  }

  .right-sidebar-fab {
    display: flex;
    position: fixed;
    right: 6px;
    bottom: 200px;
    z-index: 150;
    box-shadow: 0 4px 12px rgba(0, 0, 0, 0.2);
  }
}
</style>