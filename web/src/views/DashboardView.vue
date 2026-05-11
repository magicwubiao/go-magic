<template>
  <div class="dashboard">
    <n-grid :cols="4" :x-gap="16" :y-gap="16">
      <!-- Server Status -->
      <n-gi>
        <n-card class="status-card" :class="healthStatus">
          <div class="status-header">
            <span class="status-icon">{{ healthStatus === 'healthy' ? '✅' : '⚠️' }}</span>
            <span class="status-title">Server Status</span>
          </div>
          <div class="status-value">{{ healthStatus === 'healthy' ? 'Healthy' : 'Issues Detected' }}</div>
          <div class="status-detail">Uptime: {{ uptime }}</div>
        </n-card>
      </n-gi>

      <!-- Sessions -->
      <n-gi>
        <n-card class="stat-card" hoverable>
          <div class="stat-header">
            <span class="stat-icon">💬</span>
            <span class="stat-title">Sessions</span>
          </div>
          <div class="stat-value">{{ stats.sessions }}</div>
          <n-progress v-if="stats.sessions > 0" type="line" :percentage="Math.min(stats.sessions * 10, 100)" :show-indicator="false" />
        </n-card>
      </n-gi>

      <!-- Tools -->
      <n-gi>
        <n-card class="stat-card" hoverable>
          <div class="stat-header">
            <span class="stat-icon">🛠️</span>
            <span class="stat-title">Tools</span>
          </div>
          <div class="stat-value">{{ stats.tools }}</div>
          <n-tag size="small" type="success">{{ enabledTools }} Active</n-tag>
        </n-card>
      </n-gi>

      <!-- Models -->
      <n-gi>
        <n-card class="stat-card" hoverable>
          <div class="stat-header">
            <span class="stat-icon">🤖</span>
            <span class="stat-title">Model</span>
          </div>
          <div class="stat-value model-name">{{ currentModel }}</div>
          <n-tag size="small" type="info">In Use</n-tag>
        </n-card>
      </n-gi>
    </n-grid>

    <!-- Quick Actions -->
    <n-card title="Quick Actions" style="margin-top: 20px;">
      <n-space>
        <n-button type="primary" @click="$emit('navigate', 'chat')">
          💬 Start New Chat
        </n-button>
        <n-button @click="refreshStats">
          🔄 Refresh Stats
        </n-button>
        <n-button @click="$emit('navigate', 'config')">
          ⚙️ Configure
        </n-button>
      </n-space>
    </n-card>

    <!-- System Info -->
    <n-grid :cols="3" :x-gap="16" :y-gap="16" style="margin-top: 20px;">
      <n-gi>
        <n-card title="Memory Usage">
          <n-progress type="line" :percentage="memoryUsage" :color="memoryUsage > 80 ? '#ef4444' : '#6366f1'">
            <template #default>
              {{ memoryUsage }}% used
            </template>
          </n-progress>
        </n-card>
      </n-gi>
      
      <n-gi>
        <n-card title="Active Platforms">
          <n-space vertical>
            <n-tag v-for="platform in platforms" :key="platform" size="small" type="success">
              {{ platform }}
            </n-tag>
          </n-space>
        </n-card>
      </n-gi>
      
      <n-gi>
        <n-card title="Recent Activity">
          <n-timeline>
            <n-timeline-item v-for="activity in recentActivity" :key="activity.id" :type="activity.type">
              <template #content>
                <div class="activity-item">
                  <span>{{ activity.text }}</span>
                  <span class="activity-time">{{ activity.time }}</span>
                </div>
              </template>
            </n-timeline-item>
          </n-timeline>
        </n-card>
      </n-gi>
    </n-grid>

    <!-- Configuration Preview -->
    <n-card title="Configuration" style="margin-top: 20px;">
      <n-descriptions :column="3" bordered>
        <n-descriptions-item label="Provider">
          {{ config.provider || 'Not configured' }}
        </n-descriptions-item>
        <n-descriptions-item label="Model">
          {{ config.model || 'Not configured' }}
        </n-descriptions-item>
        <n-descriptions-item label="Temperature">
          {{ config.temperature || '0.7' }}
        </n-descriptions-item>
        <n-descriptions-item label="Max Tokens">
          {{ config.maxTokens || '4096' }}
        </n-descriptions-item>
        <n-descriptions-item label="Theme">
          {{ config.theme || 'dark' }}
        </n-descriptions-item>
        <n-descriptions-item label="Language">
          {{ config.language || 'en' }}
        </n-descriptions-item>
      </n-descriptions>
    </n-card>
  </div>
</template>

<script setup>
import { ref, onMounted, onUnmounted } from 'vue'

const emit = defineEmits(['navigate'])

const healthStatus = ref('healthy')
const uptime = ref('Calculating...')
const startTime = Date.now()

const stats = ref({
  sessions: 0,
  tools: 0
})

const enabledTools = ref(0)
const currentModel = ref('default')
const memoryUsage = ref(45)
const platforms = ref(['WhatsApp', 'Telegram', 'Discord'])
const recentActivity = ref([
  { id: 1, type: 'success', text: 'Server started', time: '2 min ago' },
  { id: 2, type: 'info', text: 'WhatsApp connected', time: '5 min ago' },
  { id: 3, type: 'success', text: 'New session created', time: '10 min ago' }
])

const config = ref({
  provider: 'openai',
  model: 'gpt-4',
  temperature: '0.7',
  maxTokens: '4096',
  theme: 'dark',
  language: 'en'
})

let uptimeInterval = null

const refreshStats = async () => {
  try {
    const response = await fetch('/api/health')
    if (response.ok) {
      healthStatus.value = 'healthy'
    }
  } catch (err) {
    healthStatus.value = 'warning'
  }
  
  // Fetch actual stats
  try {
    const [sessionsRes, toolsRes, configRes] = await Promise.all([
      fetch('/api/sessions').catch(() => null),
      fetch('/api/toolsets').catch(() => null),
      fetch('/api/config').catch(() => null)
    ])
    
    if (sessionsRes?.ok) {
      const data = await sessionsRes.json()
      stats.value.sessions = Array.isArray(data) ? data.length : 0
    }
    
    if (toolsRes?.ok) {
      const data = await toolsRes.json()
      stats.value.tools = Array.isArray(data) ? data.length : 0
    }
  } catch (err) {
    console.error('Failed to fetch stats:', err)
  }
}

const formatUptime = () => {
  const seconds = Math.floor((Date.now() - startTime) / 1000)
  const hours = Math.floor(seconds / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  uptime.value = `${hours}h ${minutes}m`
}

onMounted(() => {
  refreshStats()
  uptimeInterval = setInterval(formatUptime, 1000)
  formatUptime()
})

onUnmounted(() => {
  if (uptimeInterval) clearInterval(uptimeInterval)
})
</script>

<style scoped>
.dashboard {
  padding: 0;
}

.status-card {
  text-align: center;
}

.status-card.healthy {
  border-left: 4px solid #22c55e;
}

.status-card.warning {
  border-left: 4px solid #f59e0b;
}

.status-header {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  margin-bottom: 8px;
}

.status-icon {
  font-size: 24px;
}

.status-title {
  font-weight: 600;
}

.status-value {
  font-size: 24px;
  font-weight: 700;
  margin: 8px 0;
}

.status-detail {
  font-size: 12px;
  opacity: 0.7;
}

.stat-header {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}

.stat-icon {
  font-size: 24px;
}

.stat-title {
  font-weight: 600;
  opacity: 0.8;
}

.stat-value {
  font-size: 32px;
  font-weight: 700;
  margin: 8px 0;
}

.model-name {
  font-size: 20px;
}

.activity-item {
  display: flex;
  justify-content: space-between;
  gap: 16px;
}

.activity-time {
  opacity: 0.6;
  font-size: 12px;
}
</style>
