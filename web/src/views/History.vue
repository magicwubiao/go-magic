<template>
  <div class="history-view">
    <n-grid :cols="4" :x-gap="16" :y-gap="16" v-if="!isMobile || showSessions">
      <!-- Sessions List -->
      <n-gi :span="isMobile ? 4 : 1">
        <n-card title="Sessions" class="sessions-card">
          <template #header-extra>
            <n-space>
              <n-button size="small" @click="showNewSessionModal = true">
                <template #icon>
                  <n-icon :component="Add" />
                </template>
                New
              </n-button>
              <n-button size="small" quaternary @click="refreshSessions">
                <template #icon>
                  <n-icon :component="Refresh" />
                </template>
              </n-button>
            </n-space>
          </template>

          <!-- Search -->
          <n-input
            v-model:value="searchQuery"
            placeholder="Search sessions... (Ctrl+K)"
            clearable
            size="small"
            class="search-input"
          >
            <template #prefix>
              <n-icon :component="Search" />
            </template>
          </n-input>

          <!-- Filters -->
          <n-space size="small" class="filters">
            <n-select
              v-model:value="sourceFilter"
              :options="sourceOptions"
              placeholder="Source"
              size="small"
              clearable
              style="width: 120px"
            />
            <n-date-picker
              v-model:value="dateRange"
              type="daterange"
              size="small"
              clearable
              style="width: 220px"
            />
          </n-space>

          <!-- Session List -->
          <n-scrollbar style="max-height: calc(100vh - 280px)">
            <n-list hoverable clickable @click="selectSession(session)" v-if="filteredSessions.length > 0">
              <n-list-item
                v-for="session in filteredSessions"
                :key="session.id"
                :class="{ active: selectedSession?.id === session.id }"
              >
                <n-thing
                  :title="session.title || 'New Chat'"
                  :description="formatTime(session.updatedAt)"
                >
                  <template #avatar>
                    <n-badge :dot="session.active" :type="session.active ? 'success' : 'default'">
                      <n-avatar round>
                        <n-icon :component="ChatbubblesOutline" />
                      </n-avatar>
                    </n-badge>
                  </template>
                  <template #header-extra>
                    <n-space size="small">
                      <n-tag v-if="session.model" size="tiny">{{ session.model }}</n-tag>
                      <n-badge :value="session.messageCount" :max="99" v-if="session.messageCount" />
                    </n-space>
                  </template>
                  <template #description>
                    <n-text depth="3" class="session-source" v-if="session.source">
                      {{ session.source }}
                    </n-text>
                  </template>
                </n-thing>
              </n-list-item>
            </n-list>
            <n-empty v-else description="No sessions found" />
          </n-scrollbar>
        </n-card>
      </n-gi>

      <!-- Chat Area -->
      <n-gi :span="3">
        <n-card class="chat-card">
          <template #header>
            <n-space justify="space-between" align="center">
              <n-space align="center">
                <n-button
                  v-if="isMobile"
                  quaternary
                  @click="showSessions = false"
                >
                  <n-icon :component="ArrowBack" />
                </n-button>
                <span>{{ selectedSession?.title || 'Select a session' }}</span>
              </n-space>
              <n-space v-if="selectedSession">
                <n-button size="small" quaternary @click="exportSession">
                  <template #icon>
                    <n-icon :component="Download" />
                  </template>
                </n-button>
                <n-dropdown :options="sessionOptions" @select="handleSessionAction">
                  <n-button size="small" quaternary>
                    <template #icon>
                      <n-icon :component="More" />
                    </template>
                  </n-button>
                </n-dropdown>
              </n-space>
            </n-space>
          </template>

          <!-- Chat Messages -->
          <n-scrollbar v-if="selectedSession" style="max-height: calc(100vh - 320px)" ref="scrollbarRef">
            <div class="messages">
              <div
                v-for="message in selectedSession.messages"
                :key="message.id"
                class="message"
                :class="message.role"
              >
                <div class="message-avatar">
                  <n-avatar round>
                    <n-icon
                      :component="message.role === 'user' ? Person : Sparkles"
                    />
                  </n-avatar>
                </div>
                <div class="message-content">
                  <div class="message-header">
                    <span class="message-role">{{ message.role === 'user' ? 'You' : 'Assistant' }}</span>
                    <span class="message-time">{{ formatTime(message.timestamp) }}</span>
                  </div>
                  <div class="message-body" v-if="message.content">
                    <n-markdown :source="message.content" />
                  </div>
                  <!-- Tool Call -->
                  <div class="tool-call" v-if="message.toolName">
                    <n-collapse>
                      <n-collapse-item :title="`Tool: ${message.toolName}`">
                        <n-code :code="message.toolArgs || '{}'" language="json" />
                        <n-divider />
                        <n-code :code="message.content" language="json" />
                      </n-collapse-item>
                    </n-collapse>
                  </div>
                  <!-- Reasoning -->
                  <div class="reasoning" v-if="message.reasoning">
                    <n-details title="Reasoning">
                      {{ message.reasoning }}
                    </n-details>
                  </div>
                </div>
              </div>
            </div>
          </n-scrollbar>

          <n-empty v-else description="Select a session to view messages" />
        </n-card>
      </n-gi>
    </n-grid>

    <!-- Mobile Chat Only -->
    <n-card v-if="isMobile && !showSessions" class="chat-card">
      <template #header>
        <n-space justify="space-between">
          <n-button quaternary @click="showSessions = true">
            <template #icon>
              <n-icon :component="ArrowBack" />
            </template>
            Back
          </n-button>
          <span>{{ selectedSession?.title || 'Chat' }}</span>
          <n-button quaternary @click="showMobileKeyboard = true">
            <template #icon>
              <n-icon :component="Search" />
            </template>
          </n-button>
        </n-space>
      </template>
      <!-- Same chat content as above -->
    </n-card>

    <!-- Rename Modal -->
    <n-modal v-model:show="showRenameModal" preset="card" title="Rename Session" style="width: 400px">
      <n-input v-model:value="newTitle" placeholder="Session title" />
      <template #footer>
        <n-space justify="end">
          <n-button @click="showRenameModal = false">Cancel</n-button>
          <n-button type="primary" @click="renameSession">Save</n-button>
        </n-space>
      </template>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import {
  NCard,
  NGrid,
  NGi,
  NInput,
  NButton,
  NIcon,
  NList,
  NListItem,
  NThing,
  NAvatar,
  NBadge,
  NTag,
  NSpace,
  NScrollbar,
  NDropdown,
  NEmpty,
  NDivider,
  NCode,
  NCollapse,
  NCollapseItem,
  NModal,
  NDatePicker,
  NSelect,
  NMarkdown,
  NDetails,
} from 'naive-ui'
import {
  Add,
  Refresh,
  Search,
  ChatbubblesOutline,
  Person,
  Sparkles,
  Download,
  More,
  ArrowBack,
} from '@vicons/ionicons5'

interface Message {
  id: string
  sessionId: string
  role: 'user' | 'assistant' | 'system' | 'tool'
  content: string
  timestamp: number
  toolName?: string
  toolArgs?: string
  toolStatus?: string
  reasoning?: string
}

interface Session {
  id: string
  title: string
  source: string
  createdAt: number
  updatedAt: number
  model?: string
  messageCount: number
  inputTokens?: number
  outputTokens?: number
  active?: boolean
  messages: Message[]
}

const searchQuery = ref('')
const sourceFilter = ref<string | null>(null)
const dateRange = ref<[number, number] | null>(null)
const selectedSession = ref<Session | null>(null)
const sessions = ref<Session[]>([])
const showNewSessionModal = ref(false)
const showRenameModal = ref(false)
const newTitle = ref('')
const showSessions = ref(true)
const isMobile = ref(false)
const scrollbarRef = ref()

const sourceOptions = [
  { label: 'CLI', value: 'cli' },
  { label: 'Telegram', value: 'telegram' },
  { label: 'Discord', value: 'discord' },
  { label: 'Slack', value: 'slack' },
]

const sessionOptions = [
  { label: 'Rename', key: 'rename' },
  { label: 'Export', key: 'export' },
  { label: 'Delete', key: 'delete' },
]

const filteredSessions = computed(() => {
  let result = sessions.value

  // Search filter
  if (searchQuery.value) {
    const query = searchQuery.value.toLowerCase()
    result = result.filter(
      (s) =>
        s.title.toLowerCase().includes(query) ||
        s.messages.some((m) => m.content.toLowerCase().includes(query))
    )
  }

  // Source filter
  if (sourceFilter.value) {
    result = result.filter((s) => s.source === sourceFilter.value)
  }

  // Date range filter
  if (dateRange.value) {
    const [start, end] = dateRange.value
    result = result.filter((s) => s.updatedAt >= start && s.updatedAt <= end)
  }

  // Sort by most recent
  return result.sort((a, b) => b.updatedAt - a.updatedAt)
})

async function refreshSessions() {
  try {
    const res = await fetch('/api/sessions')
    if (res.ok) {
      sessions.value = await res.json()
    }
  } catch (e) {
    console.error('Failed to load sessions:', e)
  }
}

function selectSession(session: Session) {
  selectedSession.value = session
  if (isMobile.value) {
    showSessions.value = false
  }
}

function formatTime(timestamp: number): string {
  const date = new Date(timestamp)
  const now = new Date()
  const diff = now.getTime() - date.getTime()

  if (diff < 60000) return 'Just now'
  if (diff < 3600000) return `${Math.floor(diff / 60000)}m ago`
  if (diff < 86400000) return `${Math.floor(diff / 3600000)}h ago`
  if (diff < 604800000) return `${Math.floor(diff / 86400000)}d ago`

  return date.toLocaleDateString()
}

async function renameSession() {
  if (!selectedSession.value || !newTitle.value) return
  try {
    await fetch(`/api/sessions/${selectedSession.value.id}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ title: newTitle.value }),
    })
    selectedSession.value.title = newTitle.value
    showRenameModal.value = false
  } catch (e) {
    console.error('Failed to rename session:', e)
  }
}

async function exportSession() {
  if (!selectedSession.value) return
  // Export logic
}

function handleSessionAction(key: string) {
  switch (key) {
    case 'rename':
      newTitle.value = selectedSession.value?.title || ''
      showRenameModal.value = true
      break
    case 'export':
      exportSession()
      break
    case 'delete':
      deleteSession()
      break
  }
}

async function deleteSession() {
  if (!selectedSession.value) return
  if (!confirm('Are you sure you want to delete this session?')) return

  try {
    await fetch(`/api/sessions/${selectedSession.value.id}`, {
      method: 'DELETE',
    })
    sessions.value = sessions.value.filter((s) => s.id !== selectedSession.value?.id)
    selectedSession.value = null
  } catch (e) {
    console.error('Failed to delete session:', e)
  }
}

// Keyboard shortcut for search
function handleKeydown(e: KeyboardEvent) {
  if ((e.ctrlKey || e.metaKey) && e.key === 'k') {
    e.preventDefault()
    const searchInput = document.querySelector('.search-input input') as HTMLInputElement
    searchInput?.focus()
  }
}

onMounted(() => {
  refreshSessions()
  isMobile.value = window.innerWidth < 768
  window.addEventListener('resize', () => {
    isMobile.value = window.innerWidth < 768
  })
  window.addEventListener('keydown', handleKeydown)
})

onUnmounted(() => {
  window.removeEventListener('keydown', handleKeydown)
})
</script>

<style lang="scss" scoped>
.history-view {
  height: calc(100vh - 84px);
}

.sessions-card {
  height: 100%;
}

.search-input {
  margin-bottom: 12px;
}

.filters {
  margin-bottom: 12px;
}

.session-source {
  font-size: 12px;
}

.message {
  display: flex;
  gap: 12px;
  padding: 12px 0;
  border-bottom: 1px solid var(--border-color, #f0f0f0);

  &.user {
    flex-direction: row-reverse;
  }

  &.tool {
    margin-left: 40px;
    background: var(--hover-color, #f5f5f5);
    border-radius: 8px;
    padding: 12px;
  }
}

.message-avatar {
  flex-shrink: 0;
}

.message-content {
  flex: 1;
  min-width: 0;
}

.message-header {
  display: flex;
  justify-content: space-between;
  margin-bottom: 4px;
}

.message-role {
  font-weight: 600;
  font-size: 14px;
}

.message-time {
  font-size: 12px;
  color: var(--text-color-3, #999);
}

.message-body {
  font-size: 14px;
  line-height: 1.6;
}

.tool-call {
  margin-top: 8px;
}

.n-list-item {
  &.active {
    background: var(--selected-color, #e8f5e9);
  }
}
</style>
