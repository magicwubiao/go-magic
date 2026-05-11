<template>
  <div class="sessions-view">
    <n-space vertical :size="16">
      <n-space justify="space-between" align="center">
        <h2>{{ $t('sessions.title') }}</h2>
        <n-button type="primary" @click="createNewSession">
          <template #icon>
            <n-icon :component="Add" />
          </template>
          {{ $t('chat.newSession') }}
        </n-button>
      </n-space>

      <n-input
        v-model:value="searchQuery"
        :placeholder="$t('sessions.search')"
        clearable
      >
        <template #prefix>
          <n-icon :component="Search" />
        </template>
      </n-input>

      <n-grid :cols="3" :x-gap="16" :y-gap="16">
        <n-gi v-for="session in filteredSessions" :key="session.id">
          <n-card
            hoverable
            :class="{ active: currentSession?.id === session.id }"
            @click="selectSession(session)"
          >
            <template #header>
              <n-space justify="space-between" align="center">
                <span class="session-name">{{ session.name }}</span>
                <n-dropdown
                  :options="sessionOptions"
                  @select="(key: string) => handleSessionAction(key, session)"
                >
                  <n-button quaternary circle size="small">
                    <template #icon>
                      <n-icon :component="EllipsisVertical" />
                    </template>
                  </n-button>
                </n-dropdown>
              </n-space>
            </template>
            <template #default>
              <div class="session-info">
                <n-tag size="small" v-if="session.model">{{ session.model }}</n-tag>
                <span class="message-count">{{ session.message_count }} messages</span>
              </div>
              <div class="session-time">
                {{ formatTime(session.updated_at) }}
              </div>
            </template>
          </n-card>
        </n-gi>
      </n-grid>

      <n-empty v-if="filteredSessions.length === 0" :description="$t('sessions.empty')" />
    </n-space>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, h } from 'vue'
import { useRouter } from 'vue-router'
import { NIcon } from 'naive-ui'
import { Add, Search, EllipsisVertical, Trash, Refresh } from '@vicons/ionicons5'
import { sessionApi } from '@/api'
import type { Session } from '@/types'

const router = useRouter()
const searchQuery = ref('')
const sessions = ref<Session[]>([])
const currentSession = ref<Session | null>(null)

const filteredSessions = computed(() => {
  if (!searchQuery.value) return sessions.value
  const query = searchQuery.value.toLowerCase()
  return sessions.value.filter(
    (s) =>
      s.name.toLowerCase().includes(query) ||
      s.model?.toLowerCase().includes(query)
  )
})

interface DropdownOption {
  label: string
  key: string
  icon?: () => ReturnType<typeof h>
}

const sessionOptions: DropdownOption[] = [
  { label: 'Rename', key: 'rename', icon: () => h(NIcon, null, { default: () => h(Refresh) }) },
  { label: 'Delete', key: 'delete', icon: () => h(NIcon, null, { default: () => h(Trash) }) },
]

function formatTime(timestamp: string): string {
  return new Date(timestamp).toLocaleString()
}

async function loadSessions() {
  try {
    const response = await sessionApi.list()
    sessions.value = response.data
  } catch (e) {
    console.error('Failed to load sessions:', e)
  }
}

async function createNewSession() {
  try {
    const response = await sessionApi.create({
      name: `Session ${sessions.value.length + 1}`,
    })
    sessions.value.unshift(response.data)
    selectSession(response.data)
  } catch (e) {
    console.error('Failed to create session:', e)
  }
}

function selectSession(session: Session) {
  currentSession.value = session
  router.push({ name: 'chat', params: { id: session.id } })
}

async function handleSessionAction(action: string, session: Session) {
  if (action === 'delete') {
    try {
      await sessionApi.delete(session.id)
      sessions.value = sessions.value.filter((s) => s.id !== session.id)
    } catch (e) {
      console.error('Failed to delete session:', e)
    }
  }
}

onMounted(() => {
  loadSessions()
})
</script>

<style lang="scss" scoped>
.sessions-view {
  h2 {
    margin: 0;
  }
}

.session-name {
  font-weight: 600;
}

.session-info {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}

.message-count {
  color: var(--text-color-3);
  font-size: 12px;
}

.session-time {
  color: var(--text-color-3);
  font-size: 12px;
}

.n-card.active {
  border-color: var(--primary-color);
}
</style>
