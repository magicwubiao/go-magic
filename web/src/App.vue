<template>
  <n-config-provider :theme="isDarkTheme ? darkTheme : lightTheme" :theme-overrides="themeOverrides">
    <n-message-provider>
      <n-dialog-provider>
        <n-notification-provider>
          <n-loading-bar-provider>
            <n-layout class="app-layout" :class="{ 'light-mode': !isDarkTheme }">
              <!-- Header -->
              <n-layout-header class="app-header">
                <div class="header-left">
                  <div class="logo">
                    <span class="logo-icon">✨</span>
                    <h1 class="app-title">go-magic</h1>
                  </div>
                  <n-tag v-if="serverStatus === 'connected'" type="success" size="small" round>
                    <template #icon>
                      <span>●</span>
                    </template>
                    Online
                  </n-tag>
                  <n-tag v-else type="warning" size="small" round>
                    <template #icon>
                      <span>●</span>
                    </template>
                    Offline
                  </n-tag>
                </div>
                <div class="header-right">
                  <n-tooltip trigger="hover">
                    <template #trigger>
                      <n-button quaternary circle @click="toggleTheme">
                        {{ isDarkTheme ? '☀️' : '🌙' }}
                      </n-button>
                    </template>
                    Toggle Theme
                  </n-tooltip>
                  <n-tooltip trigger="hover">
                    <template #trigger>
                      <n-button quaternary circle @click="checkServerHealth">
                        🔄
                      </n-button>
                    </template>
                    Refresh Status
                  </n-tooltip>
                  <n-button quaternary circle @click="showSettings = true">
                    ⚙️
                  </n-button>
                </div>
              </n-layout-header>

              <n-layout has-sider>
                <!-- Sidebar -->
                <n-layout-sider
                  bordered
                  collapse-mode="width"
                  :collapsed-width="64"
                  :width="220"
                  :collapsed="collapsed"
                  show-trigger
                  @collapse="collapsed = true"
                  @expand="collapsed = false"
                  class="app-sider"
                >
                  <n-menu
                    v-model:value="activeMenu"
                    :collapsed="collapsed"
                    :collapsed-width="64"
                    :collapsed-icon-size="22"
                    :options="menuOptions"
                  />
                  
                  <!-- Quick Actions -->
                  <div class="quick-actions" v-if="!collapsed">
                    <n-divider>Quick Actions</n-divider>
                    <n-button block type="primary" @click="activeMenu = 'chat'">
                      💬 New Chat
                    </n-button>
                  </div>
                </n-layout-sider>

                <!-- Main Content -->
                <n-layout-content class="app-content">
                  <!-- Chat View -->
                  <div v-if="activeMenu === 'chat'" class="chat-view">
                    <ChatView />
                  </div>

                  <!-- Sessions View -->
                  <div v-else-if="activeMenu === 'sessions'" class="sessions-view">
                    <SessionsView />
                  </div>

                  <!-- Tools View -->
                  <div v-else-if="activeMenu === 'tools'" class="tools-view">
                    <ToolsView />
                  </div>

                  <!-- Skills View -->
                  <div v-else-if="activeMenu === 'skills'" class="skills-view">
                    <SkillsView />
                  </div>

                  <!-- Config View -->
                  <div v-else-if="activeMenu === 'config'" class="config-view">
                    <ConfigView />
                  </div>

                  <!-- Logs View -->
                  <div v-else-if="activeMenu === 'logs'" class="logs-view">
                    <LogsView />
                  </div>

                  <!-- Dashboard -->
                  <div v-else-if="activeMenu === 'dashboard'" class="dashboard-view">
                    <DashboardView />
                  </div>

                  <!-- Default / Welcome -->
                  <div v-else class="welcome-view">
                    <n-grid :cols="4" :x-gap="16" :y-gap="16">
                      <n-gi>
                        <n-card class="stat-card" hoverable @click="activeMenu = 'chat'">
                          <div class="stat-content">
                            <span class="stat-icon">💬</span>
                            <div class="stat-info">
                              <span class="stat-value">{{ stats.sessions || 0 }}</span>
                              <span class="stat-label">Sessions</span>
                            </div>
                          </div>
                        </n-card>
                      </n-gi>
                      <n-gi>
                        <n-card class="stat-card" hoverable @click="activeMenu = 'tools'">
                          <div class="stat-content">
                            <span class="stat-icon">🛠️</span>
                            <div class="stat-info">
                              <span class="stat-value">{{ stats.tools || 0 }}</span>
                              <span class="stat-label">Tools</span>
                            </div>
                          </div>
                        </n-card>
                      </n-gi>
                      <n-gi>
                        <n-card class="stat-card" hoverable @click="activeMenu = 'skills'">
                          <div class="stat-content">
                            <span class="stat-icon">📚</span>
                            <div class="stat-info">
                              <span class="stat-value">{{ stats.skills || 0 }}</span>
                              <span class="stat-label">Skills</span>
                            </div>
                          </div>
                        </n-card>
                      </n-gi>
                      <n-gi>
                        <n-card class="stat-card" hoverable @click="activeMenu = 'logs'">
                          <div class="stat-content">
                            <span class="stat-icon">📊</span>
                            <div class="stat-info">
                              <span class="stat-value">{{ stats.platforms || 0 }}</span>
                              <span class="stat-label">Platforms</span>
                            </div>
                          </div>
                        </n-card>
                      </n-gi>
                    </n-grid>
                    
                    <n-card title="Quick Start" class="welcome-card" style="margin-top: 20px;">
                      <n-space vertical>
                        <n-alert type="info">
                          Welcome to go-magic! This is your AI Agent dashboard.
                        </n-alert>
                        <n-space>
                          <n-button type="primary" @click="activeMenu = 'chat'">
                            💬 Start Chatting
                          </n-button>
                          <n-button @click="activeMenu = 'config'">
                            ⚙️ Configure
                          </n-button>
                          <n-button @click="activeMenu = 'sessions'">
                            📋 View Sessions
                          </n-button>
                        </n-space>
                      </n-space>
                    </n-card>
                  </div>
                </n-layout-content>
              </n-layout>
            </n-layout>
          </n-loading-bar-provider>
        </n-notification-provider>
      </n-dialog-provider>
    </n-message-provider>

    <!-- Settings Modal -->
    <n-modal v-model:show="showSettings" preset="card" title="Settings" style="width: 600px;">
      <SettingsForm />
    </n-modal>
  </n-config-provider>
</template>

<script setup>
import { ref, h, onMounted } from 'vue'
import { darkTheme, NIcon } from 'naive-ui'
import ChatView from './views/ChatView.vue'
import SessionsView from './views/SessionsView.vue'
import ToolsView from './views/ToolsView.vue'
import SkillsView from './views/SkillsView.vue'
import ConfigView from './views/ConfigView.vue'
import LogsView from './views/LogsView.vue'

const activeMenu = ref('dashboard')
const collapsed = ref(false)
const showSettings = ref(false)
const isDarkTheme = ref(true)
const serverStatus = ref('disconnected')
const stats = ref({
  sessions: 5,
  tools: 12,
  skills: 8,
  platforms: 3
})

const themeOverrides = {
  common: {
    primaryColor: '#6366f1',
    primaryColorHover: '#818cf8',
    primaryColorPressed: '#4f46e5',
    borderRadius: '12px'
  }
}

// Menu icons
const renderIcon = (icon) => {
  return () => h('span', {}, icon)
}

const menuOptions = [
  {
    label: 'Dashboard',
    key: 'dashboard',
    icon: renderIcon('📊')
  },
  {
    label: 'Chat',
    key: 'chat',
    icon: renderIcon('💬')
  },
  {
    label: 'Sessions',
    key: 'sessions',
    icon: renderIcon('📋')
  },
  {
    label: 'Tools',
    key: 'tools',
    icon: renderIcon('🛠️')
  },
  {
    label: 'Skills',
    key: 'skills',
    icon: renderIcon('📚')
  },
  {
    label: 'Config',
    key: 'config',
    icon: renderIcon('⚙️')
  },
  {
    label: 'Logs',
    key: 'logs',
    icon: renderIcon('📜')
  }
]

const lightTheme = {
  name: 'light',
  common: {
    primaryColor: '#6366f1',
    primaryColorHover: '#818cf8',
    primaryColorPressed: '#4f46e5',
    bodyColor: '#f5f5f5',
    cardColor: '#ffffff',
    modalColor: '#ffffff',
    popoverColor: '#ffffff',
    tableColor: '#ffffff',
    inputColor: '#ffffff',
    borderColor: '#e0e0e0'
  }
}

const toggleTheme = () => {
  isDarkTheme.value = !isDarkTheme.value
  localStorage.setItem('theme', isDarkTheme.value ? 'dark' : 'light')
}

const checkServerHealth = async () => {
  try {
    const response = await fetch('/api/health')
    if (response.ok) {
      serverStatus.value = 'connected'
    } else {
      serverStatus.value = 'disconnected'
    }
  } catch (err) {
    serverStatus.value = 'disconnected'
  }
}

const fetchStats = async () => {
  try {
    const [sessions, tools, skills] = await Promise.all([
      fetch('/api/sessions').then(r => r.ok ? r.json() : { length: 0 }).catch(() => ({ length: 0 })),
      fetch('/api/toolsets').then(r => r.ok ? r.json() : { length: 0 }).catch(() => ({ length: 0 })),
      fetch('/api/skills').then(r => r.ok ? r.json() : { length: 0 }).catch(() => ({ length: 0 }))
    ])
    
    stats.value = {
      sessions: Array.isArray(sessions) ? sessions.length : 0,
      tools: Array.isArray(tools) ? tools.length : 0,
      skills: Array.isArray(skills) ? skills.length : 0,
      platforms: 3
    }
  } catch (err) {
    console.error('Failed to fetch stats:', err)
  }
}

onMounted(() => {
  // Load saved theme
  const savedTheme = localStorage.getItem('theme')
  if (savedTheme) {
    isDarkTheme.value = savedTheme === 'dark'
  }
  
  // Check server status
  checkServerHealth()
  fetchStats()
  
  // Refresh stats every 30 seconds
  setInterval(fetchStats, 30000)
})
</script>

<style>
* {
  margin: 0;
  padding: 0;
  box-sizing: border-box;
}

html, body, #app {
  height: 100%;
  width: 100%;
  overflow: hidden;
}

.app-layout {
  height: 100vh;
  background: #0f0f1a;
}

.app-layout.light-mode {
  background: #f5f5f5;
}

.app-header {
  height: 60px;
  padding: 0 24px;
  display: flex;
  justify-content: space-between;
  align-items: center;
  background: #1a1a2e;
  border-bottom: 1px solid #333;
}

.light-mode .app-header {
  background: #ffffff;
  border-bottom: 1px solid #e0e0e0;
}

.header-left {
  display: flex;
  align-items: center;
  gap: 16px;
}

.logo {
  display: flex;
  align-items: center;
  gap: 8px;
}

.logo-icon {
  font-size: 24px;
}

.app-title {
  font-size: 20px;
  font-weight: 700;
  background: linear-gradient(135deg, #6366f1, #a855f7);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
}

.header-right {
  display: flex;
  align-items: center;
  gap: 8px;
}

.app-sider {
  background: #1a1a2e;
  height: calc(100vh - 60px);
}

.light-mode .app-sider {
  background: #ffffff;
}

.quick-actions {
  padding: 16px;
  position: absolute;
  bottom: 0;
  width: 100%;
}

.app-content {
  height: calc(100vh - 60px);
  padding: 20px;
  overflow-y: auto;
}

.stat-card {
  cursor: pointer;
  transition: transform 0.2s;
}

.stat-card:hover {
  transform: translateY(-4px);
}

.stat-content {
  display: flex;
  align-items: center;
  gap: 16px;
}

.stat-icon {
  font-size: 32px;
}

.stat-info {
  display: flex;
  flex-direction: column;
}

.stat-value {
  font-size: 28px;
  font-weight: 700;
}

.stat-label {
  font-size: 14px;
  opacity: 0.7;
}

.welcome-card {
  margin-top: 20px;
}

.light-mode .app-sider {
  background: #ffffff;
  border-right: 1px solid #e0e0e0;
}

.light-mode .app-content {
  background: #f5f5f5;
}

/* Scrollbar styling */
::-webkit-scrollbar {
  width: 8px;
  height: 8px;
}

::-webkit-scrollbar-track {
  background: transparent;
}

::-webkit-scrollbar-thumb {
  background: #444;
  border-radius: 4px;
}

::-webkit-scrollbar-thumb:hover {
  background: #555;
}

.light-mode ::-webkit-scrollbar-thumb {
  background: #ccc;
}

.light-mode ::-webkit-scrollbar-thumb:hover {
  background: #bbb;
}
</style>
