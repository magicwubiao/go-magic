<template>
  <n-config-provider :theme="darkTheme">
    <n-layout class="app-layout">
      <!-- Header -->
      <n-layout-header class="app-header">
        <div class="header-left">
          <h1 class="app-title">go-magic</h1>
          <span class="app-version">v1.0.0</span>
        </div>
        <div class="header-right">
          <n-button quaternary @click="showSettings = true">
            <template #icon>
              <span>⚙️</span>
            </template>
          </n-button>
        </div>
      </n-layout-header>

      <n-layout has-sider>
        <!-- Sidebar -->
        <n-layout-sider
          bordered
          collapse-mode="width"
          :collapsed-width="64"
          :width="200"
          :collapsed="collapsed"
          show-trigger
          @collapse="collapsed = true"
          @expand="collapsed = false"
        >
          <n-menu
            v-model:value="activeMenu"
            :collapsed="collapsed"
            :collapsed-width="64"
            :collapsed-icon-size="22"
            :options="menuOptions"
          />
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

          <!-- Default -->
          <div v-else class="welcome-view">
            <n-card title="Welcome to go-magic">
              <n-result status="info" title="AI Agent Dashboard">
                <template #footer>
                  <n-space>
                    <n-button @click="activeMenu = 'chat'">Start Chatting</n-button>
                    <n-button @click="showSettings = true">Configure</n-button>
                  </n-space>
                </template>
              </n-result>
            </n-card>
          </div>
        </n-layout-content>
      </n-layout>
    </n-layout>

    <!-- Settings Modal -->
    <n-modal v-model:show="showSettings" preset="card" title="Settings" style="width: 600px;">
      <SettingsForm />
    </n-modal>
  </n-config-provider>
</template>

<script setup>
import { ref, h } from 'vue'
import { darkTheme } from 'naive-ui'
import ChatView from './views/ChatView.vue'
import SessionsView from './views/SessionsView.vue'
import ToolsView from './views/ToolsView.vue'
import SkillsView from './views/SkillsView.vue'
import ConfigView from './views/ConfigView.vue'
import LogsView from './views/LogsView.vue'
import SettingsForm from './components/SettingsForm.vue'

const collapsed = ref(false)
const activeMenu = ref('chat')
const showSettings = ref(false)

const menuOptions = [
  { label: 'Chat', key: 'chat', icon: () => '💬' },
  { label: 'Sessions', key: 'sessions', icon: () => '📋' },
  { label: 'Tools', key: 'tools', icon: () => '🔧' },
  { label: 'Skills', key: 'skills', icon: () => '📚' },
  { label: 'Config', key: 'config', icon: () => '⚙️' },
  { label: 'Logs', key: 'logs', icon: () => '📜' }
]
</script>

<style>
.app-layout {
  height: 100vh;
}

.app-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 24px;
  background: #1a1a2e;
  border-bottom: 1px solid #333;
}

.header-left {
  display: flex;
  align-items: center;
  gap: 12px;
}

.app-title {
  font-size: 20px;
  font-weight: 600;
  color: #fff;
}

.app-version {
  font-size: 12px;
  color: #888;
}

.app-content {
  padding: 24px;
  background: #0f0f1a;
}

.welcome-view,
.chat-view,
.sessions-view,
.tools-view,
.skills-view,
.config-view,
.logs-view {
  height: calc(100vh - 120px);
  overflow: auto;
}
</style>
