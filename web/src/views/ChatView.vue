<template>
  <div class="chat-container">
    <!-- Session Sidebar -->
    <div class="session-sidebar" :class="{ 'mobile-expanded': mobileSessionExpanded }">
      <!-- Mobile drag handle -->
      <div class="mobile-session-handle" @click="mobileSessionExpanded = !mobileSessionExpanded">
        <div class="handle-bar"></div>
      </div>
      <div class="sidebar-header" v-show="!isMobile || mobileSessionExpanded">
        <!-- 会话搜索：本地过滤已加载会话；命中不足时自动补齐后端全量 web 会话 -->
        <n-input
          v-model:value="sessionSearch"
          size="small"
          round
          clearable
          class="sidebar-search-input"
          :placeholder="t('chat.searchPlaceholder')"
          @keydown.esc="sessionSearch = ''"
        >
          <template #prefix>
            <n-icon :component="SearchOutline" :size="14" />
          </template>
        </n-input>
        <!-- 刷新会话列表 -->
        <n-button size="small" quaternary circle :title="t('chat.refreshSessions')" :loading="sidebarRefreshing" @click="refreshSessions">
          <template #icon>
            <n-icon :component="RefreshOutline" />
          </template>
        </n-button>
        <!-- 新建聊天 -->
        <n-button type="primary" class="new-chat-btn" @click="createSession" size="small">
          <template #icon>
            <n-icon><AddOutline /></n-icon>
          </template>
        </n-button>
      </div>
      <div class="session-list" ref="sessionListRef" v-show="!isMobile || mobileSessionExpanded">
        <div v-if="isSearching" class="profile-group-header">{{ t('chat.searchResults', { count: visibleSessions.length }) }}</div>
          <div
            v-for="session in visibleSessions"
            :key="session.id"
            class="session-item"
            :class="{ active: chatStore.activeSessionId === session.id }"
            @click="handleSessionClick(session.id)"
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
        <div v-if="chatStore.sessionsLoading || (isSearching && searchLoading)" style="padding: 16px; text-align: center;">
          <n-spin size="small" />
        </div>
        <n-text v-if="isSearching && !chatStore.sessionsLoading && !searchLoading && visibleSessions.length === 0" depth="3" style="padding: 16px; display: block; text-align: center;">
          {{ t('chat.searchNoResults') }}
        </n-text>
        <n-text v-if="!isSearching && !chatStore.sessions.length && !chatStore.sessionsLoading" depth="3" style="padding: 16px; display: block; text-align: center;">
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
                <!-- 按 streaming_timeline_snapshot 顺序穿插渲染：推理文本 ↔ 工具调用卡片。 -->
                <TimelineMessage
                  :content="msg.content"
                  :segments="msg.streaming_timeline_snapshot"
                  :tools="messageToolCalls(msg)"
                />
                <!-- 本轮变更的文件（仅写/删/批动作，按路径去重），默认折叠可展开 -->
                <div v-if="changedFiles(msg).length > 0" class="file-changes-block">
                  <div class="file-changes-head" role="button" tabindex="0" @click.stop="toggleFileChanges(`msg-${msg.id}`)" @keydown.enter.stop="toggleFileChanges(`msg-${msg.id}`)">
                    <n-icon size="13" class="file-changes-arrow" :class="{ 'file-changes-arrow-open': isFileChangesExpanded(`msg-${msg.id}`) }"><ChevronForwardOutline /></n-icon>
                    <n-icon size="13"><DocumentTextOutline /></n-icon>
                    <span>{{ t('chat.changedFilesTitle') }}</span>
                    <span class="file-changes-count">{{ changedFiles(msg).length }}</span>
                  </div>
                  <div v-show="isFileChangesExpanded(`msg-${msg.id}`)" class="file-changes-list">
                    <div
                      v-for="f in changedFiles(msg)"
                      :key="f.path"
                      class="file-change-item"
                      :class="{ 'file-change-deleted': f.action === 'delete' }"
                      :title="f.path"
                    >
                      <span class="file-change-action" :class="`action-${f.action}`">{{ fileActionLabel(f.action) }}</span>
                      <span class="file-change-path">
                        <span v-if="fileDirName(f.path)" class="file-change-dir">{{ fileDirName(f.path) }}/</span>
                        <span class="file-change-base">{{ fileBaseName(f.path) }}</span>
                      </span>
                    </div>
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
          <!-- Streaming message with tool calls inline -->
          <div class="message assistant">
            <div class="avatar bot-avatar">🤖</div>
            <div class="message-body assistant-body">
              <!-- Status panel when no content yet & no running tools -->
              <div v-if="!chatStore.streamContent && chatStore.activeToolCalls.length === 0 && chatStore.pendingApprovals.length === 0" class="agent-status-panel">
                <div class="status-header">
                  <div class="status-spinner"></div>
                  <span class="status-phase">{{ agentPhase }}</span>
                  <span class="status-elapsed">{{ elapsedDisplay }}</span>
                </div>
                <div class="status-hint">{{ t(thinkingHints[hintIndex]) }}</div>
              </div>

              <!-- 思考过程 + 最终回答 + 工具调用：按 streamingSegments 顺序穿插渲染。 -->
              <TimelineMessage
                v-if="chatStore.streamContent || chatStore.streamingSegments.length > 0"
                :content="chatStore.streamContent"
                :segments="chatStore.streamingSegments"
                :tools="chatStore.toolCalls"
                :streaming="chatStore.streaming"
              />
              <!-- 流式期间实时展示本轮已变更的文件，默认折叠可展开 -->
              <div v-if="changedFiles().length > 0" class="file-changes-block">
                <div class="file-changes-head" role="button" tabindex="0" @click.stop="toggleFileChanges('streaming')" @keydown.enter.stop="toggleFileChanges('streaming')">
                  <n-icon size="13" class="file-changes-arrow" :class="{ 'file-changes-arrow-open': isFileChangesExpanded('streaming') }"><ChevronForwardOutline /></n-icon>
                  <n-icon size="13"><DocumentTextOutline /></n-icon>
                  <span>{{ t('chat.changedFilesTitle') }}</span>
                  <span class="file-changes-count">{{ changedFiles().length }}</span>
                </div>
                <div v-show="isFileChangesExpanded('streaming')" class="file-changes-list">
                  <div
                    v-for="f in changedFiles()"
                    :key="f.path"
                    class="file-change-item"
                    :class="{ 'file-change-deleted': f.action === 'delete' }"
                    :title="f.path"
                  >
                    <span class="file-change-action" :class="`action-${f.action}`">{{ fileActionLabel(f.action) }}</span>
                    <span class="file-change-path">
                      <span v-if="fileDirName(f.path)" class="file-change-dir">{{ fileDirName(f.path) }}/</span>
                      <span class="file-change-base">{{ fileBaseName(f.path) }}</span>
                    </span>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </template>

        <n-text v-if="!chatStore.messages.length && !chatStore.streaming" depth="3" class="empty-hint">
          {{ t('chat.selectSession') }}
        </n-text>
      </div>

      <!-- 执行状态条（沉底）：任务执行期间固定在消息区下方、输入框上方，单行提示“正在执行” -->
      <div v-if="taskDockVisible" class="task-dock-zone">
        <TaskTimeline
          :steps="taskTimelineSteps"
          :progress="chatStore.taskProgress"
        />
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
            <n-tag v-if="file.size" size="tiny" type="info">{{ formatFileSize(file.size) }}</n-tag>
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
                <n-button size="tiny" quaternary class="toolbar-btn" :title="t('chat.uploadHint')">
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

      <!-- 底部状态栏：紧凑一行 = 当前会话所属分身（只读）+ 工作目录（只读）+ 打开文件夹 -->
      <div class="workdir-bar">
        <div class="workdir-bar-inner">
          <span class="workdir-bar-profile" :title="t('chat.workDirProfile')">
            <n-icon size="12"><PersonOutline /></n-icon>
            <span class="workdir-bar-profile-name">{{ activeProfileName }}</span>
          </span>
          <span v-if="chatStore.currentWorkDir" class="workdir-bar-path" :title="chatStore.currentWorkDir">
            <n-icon size="12"><FolderOutline /></n-icon>
            <span class="workdir-bar-path-text">{{ chatStore.currentWorkDir }}</span>
          </span>
          <span v-else class="workdir-bar-path workdir-bar-empty" :title="t('chat.workDirNone')">
            <n-icon size="12"><FolderOutline /></n-icon>
            <span>{{ t('chat.workDirNone') }}</span>
          </span>
          <n-button
            v-if="!chatStore.currentWorkDirUserSet"
            size="tiny"
            quaternary
            class="workdir-bar-set-btn"
            :title="t('chat.workDirSet')"
            @click.stop="handleWorkDirMenu('browse')"
          >
            <template #icon><n-icon size="13"><FolderOpenOutline /></n-icon></template>
            {{ t('chat.workDirSet') }}
          </n-button>
          <n-icon
            v-if="chatStore.currentWorkDir"
            size="13"
            class="workdir-bar-open"
            :title="t('chat.workDirOpen')"
            @click.stop="openWorkDirInExplorer()"
          >
            <OpenOutline />
          </n-icon>
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
      <!-- Recommended: previously used directories -->
      <div v-if="recommendedDirs.length" class="dir-picker-recommended">
        <div class="dir-picker-recommended-title">{{ t('chat.workDirRecommended') }}</div>
        <div
          v-for="d in recommendedDirs"
          :key="d"
          class="dir-picker-recommended-item"
          :title="d"
          @click="applyRecommendedDir(d)"
        >
          <n-icon size="16"><FolderOpenOutline /></n-icon>
          <span class="dir-picker-recommended-path">{{ d }}</span>
          <n-icon size="14" class="dir-picker-recommended-open" :title="t('chat.workDirOpen')" @click.stop="openWorkDirInExplorer(d)">
            <OpenOutline />
          </n-icon>
        </div>
      </div>

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
import { stripZeroWidth } from '@/utils/text'
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
import RightSidebar from '@/components/RightSidebar.vue'
import TaskTimeline from '@/components/TaskTimeline.vue'
import ChatApprovalCard from '@/components/ChatApprovalCard.vue'
import TimelineMessage from '@/components/TimelineMessage.vue'
import type { TimelineStep } from '@/components/TaskTimeline.vue'
import { AttachOutline, SendOutline, StopCircleOutline, DocumentOutline, PencilOutline, FlagOutline, FolderOpenOutline, FolderOutline, AddOutline, CloseCircleOutline, TrashOutline, DocumentTextOutline, ChevronForwardOutline, SearchOutline, RefreshOutline, OpenOutline, PersonOutline } from '@vicons/ionicons5'
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

// 单条消息对应的工具调用列表：兼容两种数据源——
// - 历史消息：msg.tool_calls_snapshot（前端在流式结束时写入内存快照）
// - 流式进行中：chatStore.toolCalls（实时更新）
function messageToolCalls(msg?: sessionsApi.Message): ToolCallEvent[] {
  if (msg) {
    const snap = (msg.tool_calls_snapshot ?? []) as ToolCallEvent[]
    return snap
  }
  return chatStore.toolCalls
}

// ===== 变更的文件（内嵌列表 + 点击预览） =====
// 仅把"写/删/批"视为变更动作；read/list/search/access 不进入列表。
const FILE_CHANGED_ACTIONS = new Set(['write', 'delete', 'batch'])

function fileBaseName(path: string): string {
  const norm = path.replace(/\\/g, '/')
  const idx = norm.lastIndexOf('/')
  return idx >= 0 ? norm.slice(idx + 1) : path
}

function fileDirName(path: string): string {
  const norm = path.replace(/\\/g, '/')
  const idx = norm.lastIndexOf('/')
  return idx > 0 ? norm.slice(0, idx) : ''
}

// 本轮全部 file_ops：
// - 历史消息：优先后端落库返回的 msg.file_ops（刷新后仍可用），
//   内存态消息退化为从 tool_calls_snapshot 中聚合（流式结束写入的内存快照）；
// - 流式进行中：直接聚合 chatStore.toolCalls。
function collectTurnFileOps(msg?: sessionsApi.Message): sessionsApi.FileOp[] {
  if (msg) {
    if (msg.file_ops && msg.file_ops.length > 0) return msg.file_ops
    const snap = (msg.tool_calls_snapshot ?? []) as ToolCallEvent[]
    return snap.flatMap(tc => tc.file_ops || [])
  }
  return chatStore.toolCalls.flatMap(tc => tc.file_ops || [])
}

// 变更的文件：按路径去重；同路径先写后删时以 delete 为准（反映最终状态）。
function changedFiles(msg?: sessionsApi.Message): { action: string; path: string }[] {
  const entries: { action: string; path: string }[] = []
  const index = new Map<string, number>()
  for (const op of collectTurnFileOps(msg)) {
    if (!op || !op.path) continue
    if (!FILE_CHANGED_ACTIONS.has(op.action)) continue
    const existing = index.get(op.path)
    if (existing === undefined) {
      index.set(op.path, entries.length)
      entries.push({ action: op.action, path: op.path })
    } else if (op.action === 'delete' && entries[existing].action !== 'delete') {
      entries[existing].action = 'delete'
    }
  }
  return entries
}

function fileActionLabel(action: string): string {
  const key = `chat.fileActions.${action}`
  const label = t(key)
  return label === key ? action : label
}

// 变更文件列表折叠状态（默认收起，点击头部展开/收起）
const expandedFileChanges = ref(new Set<string>())

function isFileChangesExpanded(key: string): boolean {
  return expandedFileChanges.value.has(key)
}

function toggleFileChanges(key: string) {
  const next = new Set(expandedFileChanges.value)
  if (next.has(key)) {
    next.delete(key)
  } else {
    next.add(key)
  }
  expandedFileChanges.value = next
}

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

function formatFileSize(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) return ''
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`
}

// Work directory
const showDirPicker = ref(false)
const dirCurrentPath = ref('')
const dirEntries = ref<sessionsApi.DirEntry[]>([])
const dirLoading = ref(false)
const showNewFolderInput = ref(false)
const newFolderName = ref('')
const workDirHistory = ref<string[]>([])

// 统一路径分隔符并忽略 Windows 盘符大小写，用于排除当前会话已设置的工作目录
function normalizeDirPath(p: string): string {
  let s = (p || '').trim().replace(/[\\/]+/g, '\\').replace(/[\\]+$/, '')
  if (/^[A-Za-z]:/.test(s)) s = s.toLowerCase()
  return s
}

// 已使用过的目录作为推荐项；排除当前会话已设置的工作目录
const recommendedDirs = computed(() => {
  const current = normalizeDirPath(chatStore.currentWorkDir || '')
  return workDirHistory.value.filter(d => normalizeDirPath(d) !== current)
})
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
  content = stripZeroWidth(content)
  const cached = mdCache.get(content)
  if (cached !== undefined) return cached
  const html = marked.parse(content) as string
  // 简单的 LRU：超过上限时清空最早的一半
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

// ===== 会话侧栏搜索（本地过滤） =====
// 说明：会话列表本身按 20 条/页懒加载，仅过滤"已加载"的会话会让搜索漏掉
// 更早的历史会话；因此搜索激活时首次会一次性补齐后端全部 web 会话到本地
// 缓存（searchCache），后续过滤均为纯前端操作，不引入服务端搜索语义。
const sessionSearch = ref('')
const searchCache = ref<sessionsApi.Session[] | null>(null) // null=尚未全量补齐
const searchLoading = ref(false)
const SESSION_FETCH_STEP = 100

const isSearching = computed(() => sessionSearch.value.trim().length > 0)

function isWebSession(s: sessionsApi.Session): boolean {
  return !s.source || s.source === 'web'
}

// 当前侧栏实际展示的会话：普通浏览 = store 分页列表；搜索 = 过滤后的候选集。
const visibleSessions = computed(() => {
  const q = sessionSearch.value.trim().toLowerCase()
  if (!q) return chatStore.sessions
  const pool = searchCache.value ?? chatStore.sessions
  return pool.filter(s => {
    if (!isWebSession(s)) return false
    const title = (s.title || '').toLowerCase()
    const preview = (s.preview || '').toLowerCase()
    return title.includes(q) || preview.includes(q)
  })
})

// 一次性拉全后端 web 会话作为搜索候选（分页循环，避免截断）
async function loadFullSessions(): Promise<void> {
  if (searchLoading.value || searchCache.value) return
  searchLoading.value = true
  try {
    const web: sessionsApi.Session[] = []
    const first = await sessionsApi.getSessions(SESSION_FETCH_STEP, 0)
    web.push(...first.sessions.filter(isWebSession))
    const total = first.total ?? 0
    for (let offset = SESSION_FETCH_STEP; offset < total; offset += SESSION_FETCH_STEP) {
      const res = await sessionsApi.getSessions(SESSION_FETCH_STEP, offset)
      web.push(...res.sessions.filter(isWebSession))
      if (res.sessions.length < SESSION_FETCH_STEP) break
    }
    searchCache.value = web
  } catch (e) {
    // 拉全量失败时退化为过滤已加载会话，不阻断搜索输入
    console.error('Failed to load full sessions for search:', e)
  } finally {
    searchLoading.value = false
  }
}

// 防抖触发全量补齐：首次输入后 250ms 内不重复请求
let searchDebounceTimer: ReturnType<typeof setTimeout> | null = null
watch(sessionSearch, (val) => {
  if (searchDebounceTimer) {
    clearTimeout(searchDebounceTimer)
    searchDebounceTimer = null
  }
  if (!val.trim() || searchCache.value) return
  searchDebounceTimer = setTimeout(loadFullSessions, 250)
})

// 搜索候选缓存建立后，store 中新增/刷新出的会话保持同步（如新建会话后立即可搜到）
watch(() => chatStore.sessions, (list) => {
  if (!searchCache.value) return
  const known = new Set(searchCache.value.map(s => s.id))
  const fresh = list.filter(s => isWebSession(s) && !known.has(s.id))
  if (fresh.length > 0) searchCache.value = [...searchCache.value, ...fresh]
})

// 点击搜索命中的会话：若不在 store 已加载列表中，先并入再进入，保证派生状态完整
async function handleSessionClick(id: string) {
  if (isSearching.value && searchCache.value) {
    const hit = searchCache.value.find(s => s.id === id)
    if (hit) chatStore.mergeSessions([hit])
  }
  await selectSession(id)
}

// 侧栏会话列表：不再按分身分组，展示一份展平的统一列表（按更新时间/后端返回顺序）。
// 分身在单条会话的可视化上以会话自身的 profile 标记呈现（见底部栏“当前分身”）。
const isGatewaySession = computed(() => {
  const session = chatStore.activeSession
  return session && session.source && session.source !== 'web'
})

const activeSessionSource = computed(() => {
  return chatStore.activeSession?.source || ''
})

// 底部栏展示的“当前分身”：取当前活动会话归属的 profile，default/空归一为聊天默认名
const activeProfileName = computed(() => {
  const p = chatStore.activeSession?.profile?.trim()
  if (!p || p.toLowerCase() === 'default') return t('chat.default')
  return p
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

  // Phase 3: Synthesis (if streaming and no running tools; 仅真正的工具/长任务回合才展示该阶段，
  // 纯文本回答不显示任务坞)
  const toolTurnActive = chatStore.toolCalls.length > 0 || chatStore.taskProgress !== null
  const showSynthesis = toolTurnActive && chatStore.streaming && chatStore.activeToolCalls.length === 0 && chatStore.streamContent
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

// 任务坞仅在真正的工具/长任务回合显示（纯文本回答不出现，避免噪音）
const taskDockVisible = computed(() => {
  if (!chatStore.streaming) return false
  return chatStore.taskProgress !== null || chatStore.toolCalls.length > 0 || chatStore.activeToolCalls.length > 0
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
async function sha1OfFile(file: File): Promise<string> {
  // SubtleCrypto gives us a real content hash for dedup without shipping the
  // entire file off the device first. The old name+size key was trivially
  // bypassed by changing one byte; sha1 catches real duplicates.
  try {
    if (typeof crypto !== 'undefined' && crypto.subtle && typeof crypto.subtle.digest === 'function') {
      const buf = await crypto.subtle.digest('SHA-1', await file.arrayBuffer())
      const bytes = new Uint8Array(buf)
      let hex = ''
      for (let i = 0; i < bytes.length; i++) {
        hex += bytes[i].toString(16).padStart(2, '0')
      }
      return hex
    }
  } catch (_e) {
    // fall through to name-based key
  }
  return ''
}

async function handleFileSelect({ file, onFinish, onError }: UploadCustomRequestOptions) {
  const nativeFile = file.file
  if (!nativeFile) {
    onError()
    return
  }

  // Two-tier dedup key: sha1 (when supported) plus name+size as a fallback so
  // very old browsers still get some protection.
  const hash = await sha1OfFile(nativeFile)
  const fileKey = (hash || `${nativeFile.name}-${nativeFile.size}-${nativeFile.lastModified}`)
  if (uploadingFiles.value.has(fileKey)) {
    onError()
    return
  }
  uploadingFiles.value.add(fileKey)

  sessionsApi.uploadFile(nativeFile, chatStore.activeSessionId ?? undefined)
    .then((uploaded) => {
      // Keep the raw File in memory: the multimodal channel reads it directly
      // with FileReader (the /api/uploads URL sits behind requireAuth, so a
      // bare fetch of it would 401 and silently kill the image channel).
      uploaded._native = nativeFile
      selectedFiles.value.push(uploaded)
      onFinish()
    })
    .catch((e) => {
      console.error('Upload failed:', e)
      message.error(describeUploadError(e as Error & { code?: string }, nativeFile))
      onError()
    })
    .finally(() => {
      uploadingFiles.value.delete(fileKey)
    })
}

// Translate backend upload errors into localized, human-readable messages.
// The backend tags rejects with an X-Error-Code header; fall back to the raw
// detail only for unknown failures.
function describeUploadError(e: Error & { code?: string }, f: File): string {
  switch (e.code) {
    case 'upload_type_not_allowed': {
      const ext = /\.[a-z0-9]+$/i.exec(f.name)?.[0] ?? f.name
      return t('chat.uploadUnsupportedType', { ext })
    }
    case 'upload_too_large': {
      // Backend detail is "<...> size of <N> bytes" — reuse it so the limit
      // always matches the server config.
      const m = /size of (\d+) bytes/.exec(e.message)
      const size = m ? formatFileSize(parseInt(m[1], 10)) : '100 MB'
      return t('chat.uploadTooLarge', { size })
    }
    case 'upload_content_mismatch':
      return t('chat.uploadContentMismatch')
    default:
      return `${t('chat.fileUploadFailed')} ${e.message}`
  }
}

function removeFile(index: number) {
  selectedFiles.value.splice(index, 1)
}

function goToFilesPage() {
  router.push('/files')
}

// Work directory functions
async function loadWorkDirHistory() {
  try {
    workDirHistory.value = await sessionsApi.listWorkDirHistory()
  } catch (e) {
    console.error('Failed to load work dir history:', e)
    workDirHistory.value = []
  }
}

// 点击推荐目录直接应用为当前会话的工作目录
async function applyRecommendedDir(path: string) {
  if (!chatStore.activeSessionId) return
  try {
    await chatStore.updateSessionWorkDir(chatStore.activeSessionId, path)
    showDirPicker.value = false
    message.success(t('chat.workDir') + ': ' + path)
  } catch (e: any) {
    message.error(e?.message || t('chat.workDirLocked'))
  }
}

// 用系统文件管理器（Windows 资源管理器）打开目录；不传 path 时打开当前会话工作目录
function openWorkDirInExplorer(path?: string) {
  const target = path || chatStore.currentWorkDir
  if (!target) return
  sessionsApi.openFolderInExplorer(target).catch((e: any) => {
    message.error(e?.message || t('common.operationFailed'))
  })
}

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
    loadWorkDirHistory()
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

  // Build content with file URL references for AI processing.
  // Raster image attachments are routed to the multimodal `images` channel so
  // the model actually sees the picture; everything else (including SVG,
  // which is XML text, not a raster image) falls through to the generic file
  // channel (the backend packages it as a text attachment).
  let finalContent = content
  const allFiles = [...selectedFiles.value]
  const imageDataUrls: string[] = []
  const imageUrlRefs: string[] = []
  const nonImageFiles: sessionsApi.UploadedFile[] = []

  function readAsDataURL(blob: Blob): Promise<string> {
    return new Promise((resolve, reject) => {
      const reader = new FileReader()
      reader.onload = () => resolve(reader.result as string)
      reader.onerror = reject
      reader.readAsDataURL(blob)
    })
  }

  // Downscale large raster images before sending. Multi-megabyte screenshots
  // waste the 16 MiB stream body and get re-sent on every turn; 2048px JPEG
  // keeps details visible while shrinking payloads ~10x. GIF is skipped
  // (canvas would drop animation) and small images are passed through.
  async function downscaleImage(file?: File): Promise<string | null> {
    if (!file) return null
    try {
      const bitmap = await createImageBitmap(file)
      const maxDim = 2048
      const scale = Math.min(1, maxDim / Math.max(bitmap.width, bitmap.height))
      if (scale >= 1 && file.size <= 3 * 1024 * 1024) return null // not worth re-encoding
      const canvas = document.createElement('canvas')
      canvas.width = Math.max(1, Math.round(bitmap.width * scale))
      canvas.height = Math.max(1, Math.round(bitmap.height * scale))
      const ctx = canvas.getContext('2d')
      if (!ctx) return null
      ctx.drawImage(bitmap, 0, 0, canvas.width, canvas.height)
      bitmap.close()
      return canvas.toDataURL('image/jpeg', 0.85)
    } catch (_e) {
      return null
    }
  }

  async function toImageDataUrl(f: sessionsApi.UploadedFile): Promise<string> {
    const native = f._native
    if (native && /image\/gif/i.test(native.type)) {
      return await readAsDataURL(native)
    }
    if (native && native.size <= 1.5 * 1024 * 1024) {
      return await readAsDataURL(native)
    }
    const downscaled = await downscaleImage(native)
    if (downscaled) return downscaled
    if (native) return await readAsDataURL(native)
    // No raw File (e.g. restored draft): fall back to fetching the upload
    // URL with credentials attached — a bare fetch would 401 behind auth.
    const { getAuthToken } = await import('@/api/client')
    const headers: Record<string, string> = {}
    const token = getAuthToken()
    if (token) headers['Authorization'] = `Bearer ${token}`
    const resp = await fetch(f.url, { headers })
    if (!resp.ok) throw new Error(`failed to fetch upload: ${resp.status}`)
    return await readAsDataURL(await resp.blob())
  }

  for (const file of allFiles) {
    const mime = (file as any).mime as string | undefined
    const looksLikeImage =
      (mime && mime.startsWith('image/') && mime !== 'image/svg+xml') ||
      /\.(png|jpe?g|gif|webp)(\?|$)/i.test(file.name)
    if (looksLikeImage) {
      try {
        const dataUrl = await toImageDataUrl(file)
        imageDataUrls.push(dataUrl)
        // Remember the uploaded path so the backend can persist a small
        // reference instead of megabytes of base64.
        imageUrlRefs.push(file.url)
        continue
      } catch (_e) {
        // fall through to non-image handling
      }
    }
    // No [attachment](url) markdown appended: the HTTP URL is meaningless to
    // the model (its tools are sandboxed to the workdir) and the file itself
    // travels via the `files` field of the stream request body.
    nonImageFiles.push(file)
  }

  // Drop references to uploaded files so the GC can reclaim the base64
  // data and uploaded URL closures as soon as the store has its own copy.
  inputValue.value = ''
  selectedFiles.value = []
  commandSuggestions.value = []
  await chatStore.sendMessage(
    finalContent,
    imageDataUrls.length ? imageDataUrls : undefined,
    nonImageFiles.length ? nonImageFiles : undefined,
    imageUrlRefs.length ? imageUrlRefs : undefined,
  )
}

function stopGeneration() {
  chatStore.stopGeneration()
}

async function createSession() {
  await chatStore.createSession()
  // store 已把新会话插入列表顶部，这里绝不能再调 loadSessions()：
  // 它会用后端第 1 页（20 条）整表替换列表并重置分页 offset，连续新建
  // 后侧栏永远只有 20 条，滚动加载也因此无从触发（表现为"超出隐藏、
  // 动一下窗口/刷新滚轴才出现"）。整表刷新交给手动刷新按钮。
  sessionListRef.value?.scrollTo({ top: 0 })
}

const sidebarRefreshing = ref(false)
async function refreshSessions() {
  if (sidebarRefreshing.value) return
  sidebarRefreshing.value = true
  try {
    await chatStore.loadSessions()
    if (searchCache.value) {
      // 候选缓存已建立：作废后重建，保证刷新后搜索结果同样是最新的
      searchCache.value = null
      await loadFullSessions()
    }
  } finally {
    sidebarRefreshing.value = false
  }
}

async function deleteSession(id: string) {
  // 可能直接在搜索结果中删除尚未并入 store 的会话，回退到搜索缓存查找
  const session = chatStore.sessions.find(s => s.id === id)
    ?? searchCache.value?.find(s => s.id === id)
  const deleteFiles = !session?.work_dir_user_set
  await chatStore.deleteSession(id, deleteFiles)
  await chatStore.loadSessions()
  // 同步搜索候选缓存：被删除的会话立即从搜索结果中移除
  if (searchCache.value) {
    searchCache.value = searchCache.value.filter(s => s.id !== id)
  }
}

function startRename(id: string) {
  // 搜索结果里可能选中尚未并入 store 的会话，回退到搜索缓存查找
  const session = chatStore.sessions.find(s => s.id === id)
    ?? searchCache.value?.find(s => s.id === id)
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
    // 同步搜索候选缓存：store 的 rename 是原地改对象、不会触发缓存同步
    // watcher（只监听数组引用），这里直接改缓存条目保持搜索结果即时更新
    const hit = searchCache.value?.find(s => s.id === id)
    if (hit) hit.title = editingName.value.trim()
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
  if (isSearching.value) return // 搜索模式候选集已全量，无需滚动加载更多
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

onUnmounted(() => {
  if (searchDebounceTimer) {
    clearTimeout(searchDebounceTimer)
    searchDebounceTimer = null
  }
})

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
  padding: 8px 10px;
  border-bottom: 1px solid #e0e0e0;
  height: auto;
  box-sizing: border-box;
  display: flex;
  align-items: center;
  gap: 6px;
}

.new-chat-btn {
  flex-shrink: 0;
  width: 34px;
}

/* 会话搜索框（与新建/刷新同一行） */
.sidebar-search-input {
  flex: 1;
  min-width: 0;
}

.sidebar-search-input .n-input {
  --n-height: 30px;
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
  padding-bottom: 20px;
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

/* 变更的文件（助手消息内嵌列表，绿系区分"文件已改动"） */
.file-changes-block {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin-top: 10px;
}

.file-changes-head {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 3px 10px;
  border-radius: 12px;
  background: rgba(24, 160, 88, 0.08);
  color: #18a058;
  font-size: 12px;
  line-height: 1.4;
  width: fit-content;
  cursor: pointer;
  user-select: none;
  transition: background 0.15s;
}

.file-changes-head:hover {
  background: rgba(24, 160, 88, 0.14);
}

.file-changes-head:focus-visible {
  outline: 2px solid rgba(24, 160, 88, 0.4);
  outline-offset: 1px;
}

.file-changes-arrow {
  transition: transform 0.15s;
}

.file-changes-arrow-open {
  transform: rotate(90deg);
}

.file-changes-head .n-icon {
  opacity: 0.85;
}

.file-changes-count {
  padding: 0 6px;
  border-radius: 9px;
  background: rgba(24, 160, 88, 0.14);
  font-weight: 600;
  font-size: 11px;
  line-height: 1.6;
}

.file-changes-list {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.file-change-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 8px;
  border: 1px solid #ececec;
  background: #fff;
  border-radius: 8px;
  min-width: 0;
  transition: border-color 0.15s, background 0.15s;
}

.file-change-item:hover {
  border-color: #b7ebcf;
  background: #f6fffb;
}

.file-change-action {
  flex-shrink: 0;
  min-width: 36px;
  text-align: center;
  padding: 1px 6px;
  border-radius: 6px;
  font-size: 11px;
  font-weight: 600;
  line-height: 1.7;
  color: #fff;
}

.file-change-action.action-write { background: #18a058; }
.file-change-action.action-delete { background: #d03050; }
.file-change-action.action-batch { background: #f0a020; }

.file-change-path {
  display: flex;
  align-items: baseline;
  gap: 2px;
  flex: 1;
  min-width: 0;
  overflow: hidden;
  font-family: 'SF Mono', 'Fira Code', Consolas, monospace;
  font-size: 12px;
}

.file-change-dir {
  color: #94a3b8;
  font-size: 11px;
  flex-shrink: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  min-width: 0;
}

.file-change-base {
  color: #1f2937;
  font-weight: 600;
  flex-shrink: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 60%;
}

.file-change-deleted .file-change-base {
  color: #d03050;
  text-decoration: line-through;
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

/* ========== 长任务进度坞（沉底） ========== */
.task-dock-zone {
  padding: 0 24px 10px;
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

  .file-changes-head {
    background: rgba(99, 226, 183, 0.12);
    color: #63e2b7;
  }

  .file-changes-count {
    background: rgba(99, 226, 183, 0.18);
  }

  .file-change-item {
    border-color: #2a2a2a;
    background: #1a1a1a;
  }

  .file-change-item:hover {
    border-color: #2f7a5a;
    background: #16241d;
  }

  .file-change-base {
    color: #e5e7eb;
  }

  .file-change-dir {
    color: #6b7280;
  }

  .file-change-deleted .file-change-base {
    color: #e88080;
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
    color: #888;
  }

  .workdir-bar-open {
    color: #aaa;
  }

  .workdir-bar-empty {
    color: #555;
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

.dir-picker-recommended {
  padding: 4px 0 8px;
  border-bottom: 1px solid #f0f0f0;
  margin-bottom: 4px;
}

.dir-picker-recommended-title {
  font-size: 12px;
  color: #999;
  padding: 4px 12px 6px;
}

.dir-picker-recommended-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 12px;
  border-radius: 4px;
  cursor: pointer;
  font-size: 12px;
  transition: background 0.15s;
}

.dir-picker-recommended-item:hover {
  background: #f0f0f0;
}

.dir-picker-recommended-path {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.dir-picker-recommended-open {
  opacity: 0.55;
  flex-shrink: 0;
}

.dir-picker-recommended-open:hover {
  opacity: 1;
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
/* 底部紧凑状态栏：分身(只读) + 工作目录(只读) + 设置目录/打开文件夹 */
.workdir-bar {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  min-width: 0;
  height: 30px;
  padding: 0 16px;
  background: #fafafa;
  border-top: 1px solid #e8e8e8;
  font-size: 12px;
  box-sizing: border-box;
}

.workdir-bar-inner {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  width: 100%;
  max-width: 900px;
  margin-left: auto;
  margin-right: auto;
  color: #555;
}

.workdir-bar-profile {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  flex-shrink: 0;
  color: #2080f0;
}

.workdir-bar-profile-name {
  font-weight: 500;
  max-width: 140px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.workdir-bar-path {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  flex: 1;
  min-width: 0;
  overflow: hidden;
  color: #888;
}

.workdir-bar-path-text {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: 'SF Mono', 'Fira Code', 'Consolas', monospace;
  font-size: 12px;
}

.workdir-bar-empty {
  color: #bbb;
}

.workdir-bar-open {
  cursor: pointer;
  opacity: 0.55;
  flex-shrink: 0;
  color: #555;
}

.workdir-bar-open:hover {
  opacity: 1;
}

.workdir-bar-set-btn {
  font-size: 11px;
  flex-shrink: 0;
  color: #2080f0;
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