<template>
  <div class="sidebar" :class="{ collapsed }">
    <!-- Header -->
    <div class="sidebar-header">
      <h3 v-if="!collapsed">Sessions</h3>
      <button @click="collapsed = !collapsed" class="toggle-btn" :title="collapsed ? 'Expand' : 'Collapse'">
        {{ collapsed ? '>' : '<' }}
      </button>
    </div>

    <!-- New Chat Button -->
    <button v-if="!collapsed" @click="createNewSession" class="new-chat-btn">
      + New Chat
    </button>

    <!-- Session List -->
    <div v-if="!collapsed" class="session-list">
      <div
        v-for="session in sessions"
        :key="session.id"
        class="session-item"
        :class="{ active: session.id === currentSessionId }"
        @click="selectSession(session.id)"
      >
        <span class="session-icon">💬</span>
        <span class="session-title">{{ session.title || 'New Chat' }}</span>
        <button @click.stop="deleteSession(session.id)" class="delete-btn" title="Delete">×</button>
      </div>
    </div>

    <!-- Footer -->
    <div class="sidebar-footer">
      <button @click="showSettings = true" class="footer-btn" :title="'Settings'">
        ⚙️
      </button>
      <button @click="toggleTheme" class="footer-btn" :title="'Toggle Theme'">
        {{ theme === 'dark' ? '☀️' : '🌙' }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'

const collapsed = ref(false)
const theme = ref(localStorage.getItem('theme') || 'dark')
const sessions = ref<Array<{id: string; title: string}>>([])
const currentSessionId = ref<string | null>(null)
const showSettings = ref(false)

onMounted(async () => {
  await loadSessions()
})

async function loadSessions() {
  try {
    const res = await fetch('/api/sessions')
    if (res.ok) {
      sessions.value = await res.json()
    }
  } catch (e) {
    console.error('Failed to load sessions:', e)
  }
}

function createNewSession() {
  const newId = 'session_' + Date.now()
  sessions.value.unshift({ id: newId, title: 'New Chat' })
  currentSessionId.value = newId
  emit('sessionChange', newId)
}

function selectSession(id: string) {
  currentSessionId.value = id
  emit('sessionChange', id)
}

async function deleteSession(id: string) {
  sessions.value = sessions.value.filter(s => s.id !== id)
  if (currentSessionId.value === id) {
    currentSessionId.value = sessions.value[0]?.id || null
    emit('sessionChange', currentSessionId.value)
  }
}

function toggleTheme() {
  theme.value = theme.value === 'dark' ? 'light' : 'dark'
  localStorage.setItem('theme', theme.value)
  document.documentElement.setAttribute('data-theme', theme.value)
  emit('themeChange', theme.value)
}

const emit = defineEmits(['sessionChange', 'themeChange'])
</script>

<style scoped>
.sidebar {
  width: 260px;
  height: 100vh;
  background: var(--sidebar-bg);
  border-right: 1px solid var(--border-color);
  display: flex;
  flex-direction: column;
  transition: width 0.3s;
}
.sidebar.collapsed {
  width: 50px;
}
.sidebar-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 16px;
  border-bottom: 1px solid var(--border-color);
}
.sidebar-header h3 {
  margin: 0;
  font-size: 14px;
  color: var(--text-secondary);
}
.toggle-btn {
  background: none;
  border: none;
  color: var(--text-secondary);
  cursor: pointer;
  font-size: 16px;
  padding: 4px 8px;
}
.new-chat-btn {
  margin: 12px;
  padding: 10px;
  background: var(--primary-color);
  color: white;
  border: none;
  border-radius: 8px;
  cursor: pointer;
  font-weight: 500;
}
.session-list {
  flex: 1;
  overflow-y: auto;
  padding: 8px;
}
.session-item {
  display: flex;
  align-items: center;
  padding: 10px 12px;
  border-radius: 8px;
  cursor: pointer;
  margin-bottom: 4px;
  transition: background 0.2s;
}
.session-item:hover {
  background: var(--hover-bg);
}
.session-item.active {
  background: var(--active-bg);
}
.session-icon {
  margin-right: 8px;
}
.session-title {
  flex: 1;
  font-size: 13px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.delete-btn {
  background: none;
  border: none;
  color: var(--text-secondary);
  cursor: pointer;
  opacity: 0;
  font-size: 16px;
}
.session-item:hover .delete-btn {
  opacity: 1;
}
.sidebar-footer {
  display: flex;
  justify-content: space-around;
  padding: 12px;
  border-top: 1px solid var(--border-color);
}
.footer-btn {
  background: none;
  border: none;
  font-size: 18px;
  cursor: pointer;
  padding: 8px;
}
</style>
