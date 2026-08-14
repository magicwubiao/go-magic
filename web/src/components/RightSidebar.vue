<template>
  <div class="right-sidebar" :class="{ collapsed: isCollapsed }">
    <!-- Collapse toggle button -->
    <n-button 
      size="small" 
      quaternary 
      circle 
      class="collapse-toggle"
      @click="isCollapsed = !isCollapsed"
      :title="isCollapsed ? t('sidebar.expand') : t('sidebar.collapse')"
    >
      <template #icon>
        <n-icon :component="isCollapsed ? ChevronForwardOutline : ChevronBackOutline" :size="16" />
      </template>
    </n-button>

    <!-- Main content -->
    <div class="sidebar-content" v-show="!isCollapsed">
      <!-- Tab navigation -->
      <div class="sidebar-tabs">
        <div 
          class="tab-item" 
          :class="{ active: activeTab === 'goals' }"
          @click="activeTab = 'goals'"
        >
          <n-icon :component="FlagOutline" :size="14" />
          <span>{{ t('goals.title') }}</span>
        </div>
        <div 
          class="tab-item" 
          :class="{ active: activeTab === 'files' }"
          @click="activeTab = 'files'"
        >
          <n-icon :component="FolderOpenOutline" :size="14" />
          <span>{{ t('chat.files') }}</span>
        </div>
      </div>

      <!-- Goals Tab -->
      <div v-if="activeTab === 'goals'" class="tab-content">
        <!-- Header -->
        <div class="tab-header">
          <n-space align="center">
            <n-icon :component="FlagOutline" :size="16" color="#2080f0" />
            <n-text strong>{{ t('goals.title') }}</n-text>
          </n-space>
          <n-button size="tiny" quaternary @click="openNewGoal">
            <template #icon><n-icon :component="AddOutline" :size="14" /></template>
          </n-button>
        </div>

        <!-- Goal list -->
        <div class="goal-list">
          <n-empty v-if="!activeGoals.length" :description="t('goals.noGoals')" size="small" />
          
          <div 
            v-for="goal in activeGoals" 
            :key="goal.id" 
            class="goal-card"
            :class="{ active: currentGoal?.id === goal.id }"
          >
            <!-- Goal header -->
            <div class="goal-card-header" @click="selectGoal(goal)">
              <n-space align="center" justify="space-between" style="width: 100%;">
                <n-text strong style="font-size: 14px; overflow: hidden; text-overflow: ellipsis; display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; word-break: break-all;">
                  {{ goal.title }}
                </n-text>
              </n-space>
            </div>

            <!-- Goal details -->
            <n-collapse-transition>
              <div v-if="expandedGoals.includes(goal.id)" class="goal-card-details">
                <!-- Description -->
                <n-text v-if="goal.description && goal.description !== goal.title" depth="3" style="font-size: 12px; display: block; margin-bottom: 8px;">
                  {{ goal.description }}
                </n-text>

                <!-- Progress -->
                <div class="goal-progress">
                  <n-progress
                    type="line"
                    :percentage="goal.progress"
                    :status="goal.progress === 100 ? 'success' : 'default'"
                    :show-indicator="false"
                    :height="6"
                  />
                  <n-text style="font-size: 11px; margin-top: 4px; display: block;">{{ goal.progress }}%</n-text>
                </div>

                <!-- Quick progress buttons -->
                <n-space :size="4" style="margin-top: 8px;" align="center">
                  <n-button v-if="goal.status === 'active'" size="tiny" @click.stop="quickUpdate(goal, 25)">+25%</n-button>
                  <n-button v-if="goal.status === 'active'" size="tiny" @click.stop="quickUpdate(goal, 75)">75%</n-button>
                  <n-button v-if="goal.status === 'active'" size="tiny" type="success" @click.stop="completeGoal(goal)">
                    {{ t('goals.complete') }}
                  </n-button>
                </n-space>

                <!-- Linked sessions -->
                <div class="linked-sessions">
                  <n-space align="center" :size="4" style="margin-bottom: 4px;">
                    <n-icon :component="ChatbubblesOutline" :size="12" />
                    <n-text depth="3" style="font-size: 11px;">
                      {{ t('goals.linkedSessions') }}: {{ goalSessions[goal.id]?.length || goal.session_ids?.length || 0 }}
                    </n-text>
                  </n-space>
                  
                  <div v-if="sessionLoading[goal.id]" style="padding: 4px;">
                    <n-spin size="small" />
                  </div>
                  <div v-else-if="goalSessions[goal.id]?.length" class="session-items">
                    <div 
                      v-for="session in goalSessions[goal.id]" 
                      :key="session.id" 
                      class="session-item"
                      @click.stop="goToSession(session.id)"
                    >
                      <n-text style="font-size: 12px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; display: block;">
                        {{ session.title }}
                      </n-text>
                      <n-text depth="3" style="font-size: 10px;">{{ formatTime(session.updated_at) }}</n-text>
                    </div>
                  </div>
                  <div v-else class="session-items">
                    <n-text depth="3" style="font-size: 11px;">{{ t('goals.noLinkedSessions') }}</n-text>
                  </div>

                  <!-- Link current session button -->
                  <div v-if="sessionId && !goal.session_ids?.includes(sessionId)" style="margin-top: 8px;">
                    <n-button size="tiny" type="primary" @click.stop="linkCurrentSession(goal)">
                      {{ t('goals.linkSession') }}
                    </n-button>
                  </div>
                </div>

                <!-- Actions -->
                <n-space justify="end" :size="4" style="margin-top: 8px; padding-top: 8px; border-top: 1px solid #f0f0f0;">
                  <n-button size="tiny" text @click.stop="goToGoalsPage">
                    {{ t('goals.details') }}
                  </n-button>
                  <n-button v-if="currentGoal?.id === goal.id && sessionId && goal.session_ids?.includes(sessionId)" size="tiny" text type="error" @click.stop="unlinkSession(goal)">
                    {{ t('goals.unlinkGoal') }}
                  </n-button>
                </n-space>
              </div>
            </n-collapse-transition>

            <!-- Toggle button -->
            <n-button 
              size="tiny" 
              quaternary 
              circle 
              class="expand-toggle"
              @click="toggleExpand(goal.id)"
            >
              <template #icon>
                <n-icon 
                  :component="expandedGoals.includes(goal.id) ? ChevronDownOutline : ChevronForwardOutline" 
                  :size="12" 
                />
              </template>
            </n-button>
          </div>
        </div>
      </div>

      <!-- Files Tab - Full File Manager -->
      <div v-if="activeTab === 'files'" class="tab-content">
        <!-- Header with actions -->
        <div class="tab-header">
          <n-space align="center">
            <n-icon :component="FolderOpenOutline" :size="16" color="#18a058" />
            <n-text strong>{{ t('chat.files') }}</n-text>
          </n-space>
          <n-space :size="4">
            <n-button size="tiny" quaternary :disabled="!dirParent" @click="navigateDir(dirParent)" :title="t('chat.goParent')">
              <template #icon><n-icon :component="ArrowUpOutline" :size="14" /></template>
            </n-button>
            <n-button size="tiny" quaternary @click="loadFiles(dirCurrentPath)" :title="t('chat.refresh')">
              <template #icon><n-icon :component="RefreshOutline" :size="14" /></template>
            </n-button>
            <n-button size="tiny" quaternary @click="startNewFolder" :title="t('chat.newFolder')">
              <template #icon><n-icon :component="AddOutline" :size="14" /></template>
            </n-button>
            <n-button size="tiny" quaternary @click="startNewFile" :title="t('chat.newFile')">
              <template #icon><n-icon :component="FileTrayOutline" :size="14" /></template>
            </n-button>
            <n-button size="tiny" quaternary @click="downloadZip" :title="t('chat.downloadZip')" :disabled="!dirCurrentPath || isDownloading">
              <template #icon><n-icon :component="DownloadOutline" :size="14" /></template>
            </n-button>
          </n-space>
        </div>

        <!-- Current work directory -->
        <div class="files-workdir">
          <n-icon :component="FolderOutline" :size="14" color="#18a058" />
          <n-text v-if="chatStore.currentWorkDir" class="files-workdir-path" :title="chatStore.currentWorkDir">
            {{ chatStore.currentWorkDir }}
          </n-text>
          <n-text v-else depth="3" style="font-size: 12px;">{{ t('chat.workDirNone') }}</n-text>
        </div>

        <!-- Sort header -->
        <div class="files-sort-header">
          <n-button
            size="tiny"
            quaternary
            :class="{ 'sort-active': fileSortKey === 'name' }"
            @click="toggleSort('name')"
          >
            {{ t('chat.sortName') }}
            <n-icon v-if="fileSortKey === 'name'" :component="fileSortOrder === 'asc' ? ChevronUpOutline : ChevronDownOutline" :size="12" />
          </n-button>
          <n-button
            size="tiny"
            quaternary
            :class="{ 'sort-active': fileSortKey === 'size' }"
            @click="toggleSort('size')"
          >
            {{ t('chat.sortSize') }}
            <n-icon v-if="fileSortKey === 'size'" :component="fileSortOrder === 'asc' ? ChevronUpOutline : ChevronDownOutline" :size="12" />
          </n-button>
          <n-button
            size="tiny"
            quaternary
            :class="{ 'sort-active': fileSortKey === 'time' }"
            @click="toggleSort('time')"
          >
            {{ t('chat.sortTime') }}
            <n-icon v-if="fileSortKey === 'time'" :component="fileSortOrder === 'asc' ? ChevronUpOutline : ChevronDownOutline" :size="12" />
          </n-button>
        </div>

        <!-- New folder/file input -->
        <div v-if="showNewFolderInput || showNewFileInput" class="files-new-input">
          <n-input
            v-model:value="newItemName"
            size="small"
            :placeholder="showNewFolderInput ? t('chat.newFolder') : t('chat.newFile')"
            @keyup.enter="createNewItem"
            @blur="cancelNewItem"
            ref="newItemInputRef"
          />
        </div>

        <!-- File tree -->
        <div class="files-tree">
          <div v-if="filesLoading" class="files-loading">
            <n-spin size="small" />
          </div>
          <div v-else-if="fsEntries.length === 0" class="files-empty">
            <n-text depth="3" style="font-size: 12px;">{{ t('chat.filesEmpty') }}</n-text>
          </div>
          <div
            v-for="entry in sortedFsEntries"
            v-else
            :key="entry.path"
            class="file-tree-item"
            :class="{ 'is-dir': entry.is_dir }"
            @click="handleFileClick(entry)"
          >
            <n-icon size="16" :color="entry.is_dir ? '#18a058' : '#666'">
              <component :is="entry.is_dir ? FolderOutline : DocumentTextOutline" />
            </n-icon>
            <span class="file-name">{{ entry.name }}</span>
            <span v-if="!entry.is_dir" class="file-size">{{ formatSize(entry.size) }}</span>
            <div class="file-actions" @click.stop>
              <n-dropdown
                trigger="click"
                :options="getFileActions(entry)"
                @select="(key: string) => handleFileAction(key, entry)"
              >
                <n-button size="tiny" quaternary circle>
                  <template #icon><n-icon :component="EllipsisHorizontalOutline" :size="14" /></template>
                </n-button>
              </n-dropdown>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- Collapsed indicator -->
    <div v-if="isCollapsed" class="collapsed-indicator">
      <n-icon :component="activeTab === 'goals' ? FlagOutline : FolderOpenOutline" :size="20" :color="activeTab === 'goals' ? '#2080f0' : '#18a058'" />
      <n-text style="font-size: 10px; writing-mode: vertical-rl; margin-top: 4px;">
        {{ activeTab === 'goals' ? t('goals.title') : t('chat.files') }}
      </n-text>
    </div>

    <!-- New goal modal -->
    <n-modal v-model:show="showNewGoalModal" :title="t('goals.newGoal')">
      <n-card style="width: 400px;">
        <n-form>
          <n-form-item :label="t('goals.goalTitle')">
            <n-input v-model:value="newGoalForm.title" :placeholder="t('goals.goalTitle')" />
          </n-form-item>
          <n-form-item :label="t('goals.goalDescription')">
            <n-input v-model:value="newGoalForm.description" type="textarea" :rows="3" :placeholder="t('goals.goalDescription')" />
          </n-form-item>
        </n-form>
        <template #footer>
          <n-space justify="end">
            <n-button @click="showNewGoalModal = false">{{ t('common.cancel') }}</n-button>
            <n-button type="primary" @click="createGoal" :loading="creating">{{ t('common.create') }}</n-button>
          </n-space>
        </template>
      </n-card>
    </n-modal>

    <!-- Rename modal -->
    <n-modal v-model:show="showRenameModal" :title="t('chat.rename')" preset="dialog">
      <n-input v-model:value="renameNewName" :placeholder="t('chat.rename')" @keyup.enter="confirmRename" />
      <template #action>
        <n-space justify="end">
          <n-button @click="showRenameModal = false">{{ t('common.cancel') }}</n-button>
          <n-button type="primary" @click="confirmRename">{{ t('common.confirm') }}</n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- Delete modal -->
    <n-modal v-model:show="showDeleteModal" :title="t('chat.delete')" preset="dialog">
      <n-text>{{ t('chat.confirmDelete', { name: deleteTarget?.name || '' }) }}</n-text>
      <template #action>
        <n-space justify="end">
          <n-button @click="showDeleteModal = false">{{ t('common.cancel') }}</n-button>
          <n-button type="error" @click="confirmDelete">{{ t('chat.delete') }}</n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- File preview modal -->
    <n-modal
      v-model:show="showFilePreview"
      preset="card"
      :title="previewFile?.name || ''"
      :style="{ width: '700px', maxHeight: '80vh' }"
    >
      <div v-if="previewLoading" style="text-align: center; padding: 40px;">
        <n-spin size="large" />
        <n-text depth="3" style="display: block; margin-top: 12px;">Loading...</n-text>
      </div>
      <div v-else-if="previewError" style="text-align: center; padding: 40px;">
        <n-text type="error">{{ previewError }}</n-text>
      </div>
      <div v-else style="max-height: 60vh; overflow: auto;">
        <pre style="white-space: pre-wrap; word-break: break-all; font-size: 13px; line-height: 1.5; margin: 0;">{{ previewContent }}</pre>
      </div>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, watch, nextTick, h } from 'vue'
import { useRouter } from 'vue-router'
import { useMessage, NIcon } from 'naive-ui'
import { 
  FlagOutline, 
  AddOutline, 
  ChevronBackOutline, 
  ChevronForwardOutline, 
  ChevronDownOutline,
  ChevronUpOutline,
  ArrowUpOutline,
  ChatbubblesOutline,
  FolderOpenOutline,
  FolderOutline,
  FileTrayOutline,
  DocumentTextOutline,
  DownloadOutline,
  EllipsisHorizontalOutline,
  PencilOutline,
  TrashOutline,
  RefreshOutline
} from '@vicons/ionicons5'
import { useI18n } from 'vue-i18n'
import { useGoalsStore } from '@/stores/goals'
import { useChatStore } from '@/stores/chat'
import { useConfigStore } from '@/stores/config'
import type { Goal } from '@/api/goals'
import { getGoalSessions } from '@/api/goals'
import * as sessionsApi from '@/api/sessions'

const { t } = useI18n()
const router = useRouter()
const message = useMessage()

const goalsStore = useGoalsStore()
const chatStore = useChatStore()
const configStore = useConfigStore()

const isCollapsed = ref(true)
const activeTab = ref<'goals' | 'files'>('goals')
const expandedGoals = ref<string[]>([])
const goalSessions = ref<Record<string, any[]>>({})
const sessionLoading = ref<Record<string, boolean>>({})

const showNewGoalModal = ref(false)
const creating = ref(false)
const newGoalForm = reactive({
  title: '',
  description: '',
})

// File manager state
const dirCurrentPath = ref('')
const isDownloading = ref(false)
const fsEntries = ref<sessionsApi.FSEntry[]>([])
const filesLoading = ref(false)
const showNewFolderInput = ref(false)
const showNewFileInput = ref(false)
const newItemName = ref('')
const newItemInputRef = ref<{ focus: () => void } | null>(null)

// Sort state
const fileSortKey = ref<'name' | 'size' | 'time'>('name')
const fileSortOrder = ref<'asc' | 'desc'>('asc')

// Rename modal state
const showRenameModal = ref(false)
const renameTarget = ref<sessionsApi.FSEntry | null>(null)
const renameNewName = ref('')

// Delete modal state
const showDeleteModal = ref(false)
const deleteTarget = ref<sessionsApi.FSEntry | null>(null)

// File preview state
const showFilePreview = ref(false)
const previewFile = ref<sessionsApi.FSEntry | null>(null)
const previewContent = ref('')
const previewLoading = ref(false)
const previewError = ref('')

const dirParent = computed(() => {
  if (!dirCurrentPath.value) return ''
  const parts = dirCurrentPath.value.split('/')
  parts.pop()
  return parts.join('/') || '/'
})

const sortedFsEntries = computed(() => {
  const entries = [...fsEntries.value]
  const key = fileSortKey.value
  const order = fileSortOrder.value
  
  entries.sort((a, b) => {
    // Directories first
    if (a.is_dir !== b.is_dir) {
      return a.is_dir ? -1 : 1
    }
    
    let cmp = 0
    if (key === 'name') {
      cmp = a.name.localeCompare(b.name)
    } else if (key === 'size') {
      cmp = (a.size || 0) - (b.size || 0)
    } else if (key === 'time') {
      cmp = (a.modified || 0) - (b.modified || 0)
    }
    
    return order === 'asc' ? cmp : -cmp
  })
  
  return entries
})

const currentGoal = computed(() => goalsStore.currentGoal)
const activeGoals = computed(() => goalsStore.activeGoals)
const sessionId = computed(() => chatStore.activeSessionId || '')

onMounted(() => {
  goalsStore.loadCurrentGoal()
  goalsStore.loadGoals('active')
  configStore.loadConfig()
  loadFiles(chatStore.currentWorkDir || undefined)
})

watch(activeTab, (newTab) => {
  if (newTab === 'files') {
    loadFiles(chatStore.currentWorkDir || undefined)
  }
})

watch(() => chatStore.currentWorkDir, (newDir) => {
  if (activeTab.value === 'files') {
    loadFiles(newDir || undefined)
  }
})

watch(sessionId, async (newSessionId) => {
  if (newSessionId && currentGoal.value && configStore.config?.auto_link_goals) {
    if (!currentGoal.value.session_ids?.includes(newSessionId)) {
      try {
        await goalsStore.linkSession(currentGoal.value.id, newSessionId)
      } catch (e) {}
    }
  }
}, { immediate: true })

// Goals functions
async function selectGoal(goal: Goal) {
  goalsStore.setCurrentGoal(goal)
  if (sessionId.value && !goal.session_ids?.includes(sessionId.value) && configStore.config?.auto_link_goals) {
    await goalsStore.linkSession(goal.id, sessionId.value)
  }
}

function toggleExpand(goalId: string) {
  const idx = expandedGoals.value.indexOf(goalId)
  if (idx >= 0) {
    expandedGoals.value.splice(idx, 1)
  } else {
    expandedGoals.value.push(goalId)
    loadSessionGoals(goalId)
  }
}

async function loadSessionGoals(goalId: string) {
  await nextTick()
  if (goalSessions.value[goalId] !== undefined || sessionLoading.value[goalId]) return
  sessionLoading.value[goalId] = true
  try {
    const result = await getGoalSessions(goalId)
    goalSessions.value[goalId] = result.sessions || []
  } catch (e) {
    goalSessions.value[goalId] = []
  } finally {
    sessionLoading.value[goalId] = false
  }
}

async function quickUpdate(goal: Goal, progress: number) {
  try {
    await goalsStore.updateGoal(goal.id, { progress })
    message.success(t('goals.progressUpdated', { progress }))
  } catch (e) {
    message.error(t('common.operationFailed'))
  }
}

async function completeGoal(goal: Goal) {
  try {
    await goalsStore.completeGoal(goal.id)
    message.success(t('goals.goalCompleted'))
  } catch (e) {
    message.error(t('common.operationFailed'))
  }
}

function goToSession(sessionId: string) {
  router.push(`/chat?session=${sessionId}`)
}

function goToGoalsPage() {
  router.push('/goals')
}

function openNewGoal() {
  newGoalForm.title = ''
  newGoalForm.description = ''
  showNewGoalModal.value = true
}

async function createGoal() {
  if (!newGoalForm.title.trim()) {
    message.warning(t('goals.goalTitle'))
    return
  }
  
  creating.value = true
  try {
    const goal = await goalsStore.createGoal(newGoalForm.title, newGoalForm.description)
    goalsStore.setCurrentGoal(goal)
    if (sessionId.value) {
      await goalsStore.linkSession(goal.id, sessionId.value)
    }
    showNewGoalModal.value = false
    message.success(t('goals.created'))
  } catch (e: any) {
    message.error(e?.message || t('common.error'))
  } finally {
    creating.value = false
  }
}

async function unlinkSession(goal: Goal) {
  if (!sessionId.value) return
  const wasExpanded = expandedGoals.value.includes(goal.id)
  try {
    await goalsStore.unlinkSession(goal.id, sessionId.value)
    if (wasExpanded) {
      const newSessions = { ...goalSessions.value }
      delete newSessions[goal.id]
      goalSessions.value = newSessions
      await goalsStore.loadGoals('active')
      await loadSessionGoals(goal.id)
    } else {
      await goalsStore.loadGoals('active')
    }
    message.success(t('goals.unlinked'))
  } catch (e: any) {
    message.error(e?.message || t('common.operationFailed'))
  }
}

async function linkCurrentSession(goal: Goal) {
  if (!sessionId.value) return
  const wasExpanded = expandedGoals.value.includes(goal.id)
  try {
    await goalsStore.linkSession(goal.id, sessionId.value)
    if (wasExpanded) {
      const newSessions = { ...goalSessions.value }
      delete newSessions[goal.id]
      goalSessions.value = newSessions
      await goalsStore.loadGoals('active')
      await loadSessionGoals(goal.id)
    } else {
      await goalsStore.loadGoals('active')
    }
    message.success(t('goals.sessionLinked'))
  } catch (e) {
    message.error(t('common.operationFailed'))
  }
}

function formatTime(timestamp: number): string {
  const date = new Date(timestamp * 1000)
  const now = new Date()
  const diffMs = now.getTime() - date.getTime()
  const diffDays = Math.floor(diffMs / (1000 * 60 * 60 * 24))
  
  if (diffDays === 0) {
    return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  } else if (diffDays === 1) {
    return t('common.yesterday')
  } else if (diffDays < 7) {
    return t('common.daysAgo', { count: diffDays })
  } else {
    return date.toLocaleDateString([], { month: 'short', day: 'numeric' })
  }
}

// File manager functions
async function loadFiles(path?: string) {
  filesLoading.value = true
  try {
    const res = await sessionsApi.listFSEntries(path, chatStore.activeSessionId || undefined)
    dirCurrentPath.value = res.current
    fsEntries.value = res.entries || []
  } catch (e) {
    console.error('Failed to list files:', e)
    fsEntries.value = []
  } finally {
    filesLoading.value = false
  }
}

function toggleSort(key: 'name' | 'size' | 'time') {
  if (fileSortKey.value === key) {
    fileSortOrder.value = fileSortOrder.value === 'asc' ? 'desc' : 'asc'
  } else {
    fileSortKey.value = key
    fileSortOrder.value = 'asc'
  }
}

function navigateDir(path: string) {
  if (!path && path !== '') return
  cancelNewItem()
  loadFiles(path || undefined)
}

async function handleFileClick(entry: sessionsApi.FSEntry) {
  if (entry.is_dir) {
    navigateDir(entry.path)
  } else {
    // Show file preview
    await openFilePreview(entry)
  }
}

async function openFilePreview(entry: sessionsApi.FSEntry) {
  previewFile.value = entry
  previewContent.value = ''
  previewError.value = ''
  showFilePreview.value = true
  previewLoading.value = true
  
  try {
    previewContent.value = await sessionsApi.readFSFile(entry.path, chatStore.activeSessionId || undefined)
  } catch (e: any) {
    previewError.value = e.message || 'Failed to read file'
    previewContent.value = ''
  } finally {
    previewLoading.value = false
  }
}

function startNewFolder() {
  showNewFolderInput.value = true
  showNewFileInput.value = false
  newItemName.value = ''
  nextTick(() => {
    newItemInputRef.value?.focus()
  })
}

function startNewFile() {
  showNewFileInput.value = true
  showNewFolderInput.value = false
  newItemName.value = ''
  nextTick(() => {
    newItemInputRef.value?.focus()
  })
}

function cancelNewItem() {
  setTimeout(() => {
    showNewFolderInput.value = false
    showNewFileInput.value = false
    newItemName.value = ''
  }, 150)
}

async function createNewItem() {
  const name = newItemName.value.trim()
  if (!name) {
    cancelNewItem()
    return
  }
  
  try {
    if (showNewFolderInput.value) {
      await sessionsApi.createDir(dirCurrentPath.value, name, chatStore.activeSessionId || undefined)
    } else {
      // Create empty file
      const filePath = dirCurrentPath.value ? `${dirCurrentPath.value}/${name}` : name
      await sessionsApi.writeFSFile(filePath, '', chatStore.activeSessionId || undefined)
    }
    newItemName.value = ''
    showNewFolderInput.value = false
    showNewFileInput.value = false
    loadFiles(dirCurrentPath.value)
    message.success(showNewFolderInput.value ? t('chat.folderCreated') : t('chat.fileCreated'))
  } catch (e: any) {
    message.error(e?.message || t('common.operationFailed'))
  }
}

function downloadFile(url: string, filename?: string) {
  const link = document.createElement('a')
  link.href = url
  link.download = filename || 'download'
  link.style.display = 'none'
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
  message.success(t('common.downloadComplete'))
}

function downloadZip() {
  if (!dirCurrentPath.value) return
  const url = sessionsApi.getFSZipUrl(dirCurrentPath.value, chatStore.activeSessionId!)
  downloadFile(url, 'files.zip')
}

function getFileActions(entry: sessionsApi.FSEntry): any[] {
  const actions: any[] = []
  
  if (!entry.is_dir) {
    actions.push({
      label: t('chat.download'),
      key: 'download',
      icon: () => h(NIcon, { size: 14 }, { default: () => h(DownloadOutline) }),
    })
  }
  
  actions.push({
    label: t('chat.rename'),
    key: 'rename',
    icon: () => h(NIcon, { size: 14 }, { default: () => h(PencilOutline) }),
  })
  
  actions.push({
    label: t('chat.delete'),
    key: 'delete',
    icon: () => h(NIcon, { size: 14 }, { default: () => h(TrashOutline) }),
    props: { style: { color: '#d03050' } },
  })
  
  return actions
}

async function handleFileAction(key: string, entry: sessionsApi.FSEntry) {
  if (key === 'download') {
    const url = sessionsApi.getFSDownloadUrl(entry.path, chatStore.activeSessionId || undefined)
    downloadFile(url, entry.name)
  } else if (key === 'rename') {
    renameTarget.value = entry
    renameNewName.value = entry.name
    showRenameModal.value = true
  } else if (key === 'delete') {
    deleteTarget.value = entry
    showDeleteModal.value = true
  }
}

async function confirmRename() {
  if (!renameTarget.value || !renameNewName.value.trim()) {
    showRenameModal.value = false
    return
  }
  const newName = renameNewName.value.trim()
  if (newName === renameTarget.value.name) {
    showRenameModal.value = false
    return
  }
  try {
    await sessionsApi.renameFSPath(renameTarget.value.path, newName, chatStore.activeSessionId || undefined)
    loadFiles(dirCurrentPath.value)
    message.success(t('chat.renamed'))
  } catch (e: any) {
    message.error(e?.message || t('common.operationFailed'))
  } finally {
    showRenameModal.value = false
    renameTarget.value = null
  }
}

async function confirmDelete() {
  if (!deleteTarget.value) {
    showDeleteModal.value = false
    return
  }
  try {
    await sessionsApi.deleteFSPath(deleteTarget.value.path, chatStore.activeSessionId || undefined)
    loadFiles(dirCurrentPath.value)
    message.success(t('chat.deleted'))
  } catch (e: any) {
    message.error(e?.message || t('common.operationFailed'))
  } finally {
    showDeleteModal.value = false
    deleteTarget.value = null
  }
}

function formatSize(bytes: number): string {
  if (bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i]
}
</script>

<style scoped>
.right-sidebar {
  width: 280px;
  border-left: 1px solid #e0e0e0;
  background: #fff;
  display: flex;
  flex-direction: column;
  position: relative;
  transition: width 0.2s;
  flex-shrink: 0;
}

.right-sidebar.collapsed {
  width: 48px;
}

.collapse-toggle {
  position: absolute;
  left: -14px;
  top: 50%;
  transform: translateY(-50%);
  z-index: 10;
  background: #fff !important;
  border: 1px solid #e0e0e0 !important;
  box-shadow: 0 2px 8px rgba(0,0,0,0.1);
}

.sidebar-content {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  padding-right: 10px;
}

/* Tab navigation */
.sidebar-tabs {
  display: flex;
  border-bottom: 1px solid #e0e0e0;
  flex-shrink: 0;
}

.tab-item {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  padding: 10px 8px;
  cursor: pointer;
  font-size: 13px;
  color: #666;
  transition: all 0.2s;
  border-bottom: 2px solid transparent;
}

.tab-item:hover {
  color: #333;
  background: #f5f5f5;
}

.tab-item.active {
  color: #18a058;
  border-bottom-color: #18a058;
}

.tab-content {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.tab-header {
  padding: 10px 12px;
  border-bottom: 1px solid #f0f0f0;
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-shrink: 0;
}

/* Goals section */
.goal-list {
  flex: 1;
  overflow-y: auto;
  padding: 8px;
}

.goal-card {
  background: #fafafa;
  border-radius: 8px;
  margin-bottom: 8px;
  padding: 8px;
  cursor: pointer;
  transition: background 0.2s;
  position: relative;
  word-break: break-all;
  overflow-wrap: break-word;
}

.goal-card:hover {
  background: #f0f0f0;
}

.goal-card.active {
  background: #e8f4ff;
  border: 1px solid #2080f0;
}

.goal-card-header {
  padding: 4px;
}

.goal-card-details {
  padding: 8px 4px;
  border-top: 1px solid #e0e0e0;
  margin-top: 4px;
}

.goal-progress {
  margin-bottom: 8px;
}

.linked-sessions {
  margin-top: 8px;
  padding-top: 8px;
  border-top: 1px solid #e0e0e0;
}

.session-items {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.session-item {
  padding: 4px 6px;
  background: #fff;
  border-radius: 4px;
  cursor: pointer;
  transition: background 0.2s;
}

.session-item:hover {
  background: #e8f4ff;
}

.expand-toggle {
  position: absolute;
  right: 4px;
  top: 8px;
}

/* Files section */
.files-workdir {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 12px;
  border-bottom: 1px solid #f0f0f0;
  flex-shrink: 0;
}

.files-workdir-path {
  font-size: 11px;
  font-family: monospace;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 1;
}

.files-breadcrumb {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 12px;
  border-bottom: 1px solid #f0f0f0;
  flex-shrink: 0;
}

.files-sort-header {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 4px 12px;
  border-bottom: 1px solid #f0f0f0;
  background: #fafafa;
  flex-shrink: 0;
}

.files-sort-header .n-button {
  font-size: 12px;
  height: 24px;
  padding: 0 8px;
}

.files-sort-header .sort-active {
  color: #2080f0;
  font-weight: 500;
}

.files-new-input {
  padding: 8px 12px;
  border-bottom: 1px solid #f0f0f0;
  flex-shrink: 0;
}

.files-tree {
  flex: 1;
  overflow-y: auto;
  padding: 4px 8px;
}

.file-tree-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 8px;
  border-radius: 4px;
  cursor: pointer;
  font-size: 12px;
  transition: background 0.15s;
}

.file-tree-item:hover {
  background: #f0f0f0;
}

.file-tree-item:hover .file-actions {
  opacity: 1;
}

.file-name {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.file-size {
  font-size: 10px;
  color: #999;
  margin-right: 4px;
}

.file-actions {
  opacity: 0;
  transition: opacity 0.15s;
}

.files-empty,
.files-loading {
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
}

.collapsed-indicator {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 100%;
  padding: 8px;
}

/* Dark mode */
@media (prefers-color-scheme: dark) {
  .right-sidebar {
    background: #1e1e1e;
    border-left-color: #333;
  }
  
  .collapse-toggle {
    background: #1e1e1e !important;
    border-color: #333 !important;
  }
  
  .sidebar-tabs {
    border-bottom-color: #333;
  }
  
  .tab-item {
    color: #999;
  }
  
  .tab-item:hover {
    color: #fff;
    background: #2a2a2a;
  }
  
  .tab-item.active {
    color: #18a058;
  }
  
  .tab-header {
    border-bottom-color: #333;
  }
  
  .goal-card {
    background: #252525;
  }
  
  .goal-card:hover {
    background: #2d2d2d;
  }
  
  .goal-card.active {
    background: #1a3a5c;
    border-color: #2080f0;
  }
  
  .session-item {
    background: #1e1e1e;
  }
  
  .session-item:hover {
    background: #2a4a6c;
  }
  
  .goal-card-details {
    border-top-color: #333;
  }
  
  .linked-sessions {
    border-top-color: #333;
  }
  
  .files-workdir {
    border-bottom-color: #333;
  }
  
  .files-breadcrumb {
    border-bottom-color: #333;
  }
  
  .files-new-input {
    border-bottom-color: #333;
  }
  
  .file-tree-item:hover {
    background: #2a2a2a;
  }
}
</style>
