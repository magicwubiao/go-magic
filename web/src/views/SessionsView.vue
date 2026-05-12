<template>
  <div class="sessions-view">
    <div class="view-header">
      <h2>Sessions</h2>
      <n-button type="primary" @click="createSession">
        + New Session
      </n-button>
    </div>

    <div class="sessions-list" v-if="sessions.length > 0">
      <div
        v-for="session in sessions"
        :key="session.id"
        class="session-card"
        :class="{ active: currentSessionId === session.id }"
        @click="selectSession(session)"
      >
        <div class="session-info">
          <div class="session-name">{{ session.name }}</div>
          <div class="session-meta">
            <span>{{ session.message_count || 0 }} messages</span>
            <span>{{ formatDate(session.updated_at) }}</span>
          </div>
        </div>
        <div class="session-actions">
          <n-button quaternary circle size="small" @click.stop="renameSession(session)">
            ✏️
          </n-button>
          <n-button quaternary circle size="small" @click.stop="deleteSession(session.id)">
            🗑️
          </n-button>
        </div>
      </div>
    </div>

    <div v-else class="empty-state">
      <div class="empty-icon">📋</div>
      <p>No sessions yet</p>
      <n-button @click="createSession">Create your first session</n-button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { NButton, useMessage } from 'naive-ui'
import { apiService, Session } from '../api'

const message = useMessage()
const sessions = ref<Session[]>([])
const currentSessionId = ref<string | null>(null)

const emit = defineEmits(['select'])

onMounted(loadSessions)

async function loadSessions() {
  try {
    const response = await apiService.sessions.list()
    sessions.value = response.data.sort((a, b) => 
      new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime()
    )
  } catch (err) {
    message.error('Failed to load sessions')
  }
}

async function createSession() {
  try {
    const response = await apiService.sessions.create({
      name: `Chat ${new Date().toLocaleString()}`,
    })
    sessions.value.unshift(response.data)
    selectSession(response.data)
    message.success('Session created')
  } catch (err) {
    message.error('Failed to create session')
  }
}

function selectSession(session: Session) {
  currentSessionId.value = session.id
  emit('select', session)
}

async function renameSession(session: Session) {
  const name = prompt('Enter new name:', session.name)
  if (!name) return
  
  try {
    await apiService.sessions.update(session.id, { name })
    session.name = name
    message.success('Session renamed')
  } catch (err) {
    message.error('Failed to rename session')
  }
}

async function deleteSession(id: string) {
  if (!confirm('Delete this session?')) return
  
  try {
    await apiService.sessions.delete(id)
    sessions.value = sessions.value.filter(s => s.id !== id)
    if (currentSessionId.value === id) {
      currentSessionId.value = null
    }
    message.success('Session deleted')
  } catch (err) {
    message.error('Failed to delete session')
  }
}

function formatDate(date: string): string {
  const d = new Date(date)
  const now = new Date()
  const diff = now.getTime() - d.getTime()
  
  if (diff < 60000) return 'Just now'
  if (diff < 3600000) return `${Math.floor(diff / 60000)}m ago`
  if (diff < 86400000) return `${Math.floor(diff / 3600000)}h ago`
  return d.toLocaleDateString()
}
</script>

<style scoped>
.sessions-view {
  padding: 20px;
  height: 100%;
  overflow-y: auto;
}

.view-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
}

.view-header h2 {
  margin: 0;
}

.sessions-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.session-card {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 16px;
  background: var(--bg-secondary);
  border-radius: 12px;
  cursor: pointer;
  transition: all 0.2s;
}

.session-card:hover {
  background: var(--bg-tertiary);
}

.session-card.active {
  border: 1px solid var(--primary-color);
}

.session-info {
  flex: 1;
  min-width: 0;
}

.session-name {
  font-weight: 500;
  margin-bottom: 4px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.session-meta {
  display: flex;
  gap: 12px;
  font-size: 12px;
  color: var(--text-secondary);
}

.session-actions {
  display: flex;
  gap: 4px;
  opacity: 0;
  transition: opacity 0.2s;
}

.session-card:hover .session-actions {
  opacity: 1;
}

.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 300px;
  color: var(--text-secondary);
}

.empty-icon {
  font-size: 48px;
  margin-bottom: 16px;
}
</style>
