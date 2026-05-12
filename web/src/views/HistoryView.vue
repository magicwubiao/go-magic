<template>
  <div class="history-view">
    <div class="history-header">
      <h1>📜 Chat History</h1>
      <div class="header-actions">
        <input
          v-model="searchQuery"
          type="text"
          placeholder="Search conversations..."
          class="search-input"
        />
        <button @click="exportAll" class="btn-secondary">Export All</button>
      </div>
    </div>

    <div class="history-filters">
      <button
        v-for="filter in filters"
        :key="filter.value"
        @click="selectedFilter = filter.value"
        class="filter-btn"
        :class="{ active: selectedFilter === filter.value }"
      >
        {{ filter.label }}
      </button>
    </div>

    <div class="history-list">
      <div
        v-for="item in filteredHistory"
        :key="item.id"
        class="history-item"
        @click="loadConversation(item.id)"
      >
        <div class="item-header">
          <span class="item-date">{{ formatDate(item.createdAt) }}</span>
          <span class="item-platform">{{ item.platform || 'Web' }}</span>
        </div>
        <div class="item-preview">
          {{ item.messages[0]?.content?.substring(0, 100) || 'Empty conversation' }}...
        </div>
        <div class="item-stats">
          <span>{{ item.messageCount }} messages</span>
          <span>{{ item.model }}</span>
        </div>
        <div class="item-actions">
          <button @click.stop="deleteHistory(item.id)" class="action-btn" title="Delete">🗑️</button>
          <button @click.stop="exportHistory(item.id)" class="action-btn" title="Export">📤</button>
        </div>
      </div>

      <div v-if="filteredHistory.length === 0" class="empty-state">
        <span class="empty-icon">📭</span>
        <span>No conversations found</span>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'

defineEmits(['loadConversation', 'close'])

const searchQuery = ref('')
const selectedFilter = ref('all')

const filters = [
  { label: 'All', value: 'all' },
  { label: 'Today', value: 'today' },
  { label: 'This Week', value: 'week' },
  { label: 'This Month', value: 'month' },
]

const history = ref<Array<{
  id: string
  createdAt: string
  platform?: string
  model: string
  messageCount: number
  messages: Array<{content: string}>
}>>([])

// Mock data
history.value = [
  {
    id: '1',
    createdAt: new Date().toISOString(),
    platform: 'Telegram',
    model: 'GPT-4o',
    messageCount: 12,
    messages: [{ content: 'Hello, how can I help you today?' }],
  },
  {
    id: '2',
    createdAt: new Date(Date.now() - 86400000).toISOString(),
    platform: 'Discord',
    model: 'Claude-3.5',
    messageCount: 8,
    messages: [{ content: 'What would you like to know?' }],
  },
]

const filteredHistory = computed(() => {
  let result = history.value

  if (searchQuery.value) {
    const query = searchQuery.value.toLowerCase()
    result = result.filter(item =>
      item.messages.some(m => m.content.toLowerCase().includes(query))
    )
  }

  return result
})

function formatDate(dateStr: string): string {
  const date = new Date(dateStr)
  const now = new Date()
  const diff = now.getTime() - date.getTime()
  const days = Math.floor(diff / (1000 * 60 * 60 * 24))

  if (days === 0) return 'Today'
  if (days === 1) return 'Yesterday'
  if (days < 7) return `${days} days ago`
  return date.toLocaleDateString()
}

function loadConversation(id: string) {
  // emit('loadConversation', id)
  alert('Load conversation: ' + id)
}

function deleteHistory(id: string) {
  if (confirm('Delete this conversation?')) {
    history.value = history.value.filter(h => h.id !== id)
  }
}

function exportHistory(id: string) {
  const item = history.value.find(h => h.id === id)
  if (item) {
    downloadJSON(item, `conversation-${id}.json`)
  }
}

function exportAll() {
  downloadJSON(filteredHistory.value, 'all-conversations.json')
}

function downloadJSON(data: any, filename: string) {
  const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  a.click()
  URL.revokeObjectURL(url)
}
</script>

<style scoped>
.history-view {
  padding: 24px;
  height: 100%;
  overflow-y: auto;
}
.history-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 24px;
}
.history-header h1 {
  font-size: 24px;
  margin: 0;
}
.header-actions {
  display: flex;
  gap: 12px;
}
.search-input {
  padding: 10px 16px;
  border: 1px solid var(--border-color);
  border-radius: 8px;
  background: var(--bg-secondary);
  color: var(--text-primary);
  width: 250px;
}
.history-filters {
  display: flex;
  gap: 8px;
  margin-bottom: 24px;
}
.filter-btn {
  padding: 8px 16px;
  border: 1px solid var(--border-color);
  border-radius: 20px;
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
  font-size: 13px;
}
.filter-btn.active,
.filter-btn:hover {
  background: var(--primary-color);
  color: white;
  border-color: var(--primary-color);
}
.history-list {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
  gap: 16px;
}
.history-item {
  background: var(--bg-secondary);
  border: 1px solid var(--border-color);
  border-radius: 12px;
  padding: 16px;
  cursor: pointer;
  transition: all 0.2s;
}
.history-item:hover {
  border-color: var(--primary-color);
  transform: translateY(-2px);
}
.item-header {
  display: flex;
  justify-content: space-between;
  margin-bottom: 12px;
}
.item-date {
  font-size: 13px;
  color: var(--text-secondary);
}
.item-platform {
  font-size: 11px;
  padding: 2px 8px;
  background: var(--bg-tertiary);
  border-radius: 10px;
  color: var(--text-secondary);
}
.item-preview {
  font-size: 14px;
  color: var(--text-primary);
  margin-bottom: 12px;
  line-height: 1.5;
}
.item-stats {
  display: flex;
  gap: 16px;
  font-size: 12px;
  color: var(--text-secondary);
}
.item-actions {
  display: flex;
  gap: 8px;
  margin-top: 12px;
  opacity: 0;
  transition: opacity 0.2s;
}
.history-item:hover .item-actions {
  opacity: 1;
}
.action-btn {
  background: var(--bg-tertiary);
  border: none;
  padding: 6px 10px;
  border-radius: 6px;
  cursor: pointer;
  font-size: 14px;
}
.empty-state {
  grid-column: 1 / -1;
  text-align: center;
  padding: 60px 20px;
  color: var(--text-secondary);
}
.empty-icon {
  font-size: 48px;
  display: block;
  margin-bottom: 16px;
}
</style>
