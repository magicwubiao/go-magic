<template>
  <n-layout has-sider class="app-layout">
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
      class="sidebar"
    >
      <div class="logo" :class="{ collapsed }">
        <img v-if="!collapsed" src="/logo.png" alt="Go Magic" class="logo-img" />
        <span v-if="collapsed" class="logo-icon">⚡</span>
        <span v-if="!collapsed" class="logo-text">Go Magic</span>
      </div>

      <n-menu
        v-model:value="activeMenu"
        :collapsed="collapsed"
        :collapsed-width="64"
        :collapsed-icon-size="22"
        :options="menuOptions"
      />

      <!-- Gateway Status -->
      <div class="gateway-status" v-if="!collapsed">
        <n-badge :dot="gatewayStatus === 'running'" :type="gatewayStatus === 'running' ? 'success' : 'default'">
          <span class="status-text">{{ gatewayStatus === 'running' ? 'Gateway Running' : 'Gateway Stopped' }}</span>
        </n-badge>
      </div>
    </n-layout-sider>

    <!-- Main Content -->
    <n-layout>
      <n-layout-header bordered class="header">
        <div class="header-left">
          <n-space>
            <n-button quaternary @click="collapsed = !collapsed">
              <template #icon>
                <n-icon :component="collapsed ? MenuOutline : Menu" />
              </template>
            </n-button>
            <n-breadcrumb>
              <n-breadcrumb-item>{{ $t(`nav.${activeMenu}`) }}</n-breadcrumb-item>
            </n-breadcrumb>
          </n-space>
        </div>
        <div class="header-right">
          <!-- Model Selector -->
          <n-dropdown :options="modelOptions" @select="handleModelSelect">
            <n-button quaternary size="small">
              <template #icon>
                <n-icon :component="Cpu" />
              </template>
              {{ currentModel }}
            </n-button>
          </n-dropdown>

          <n-badge :value="notificationCount" :max="99" v-if="notificationCount > 0">
            <n-button quaternary circle @click="showNotifications = true">
              <template #icon>
                <n-icon :component="Notifications" />
              </template>
            </n-button>
          </n-badge>
          <n-button quaternary circle v-else>
            <template #icon>
              <n-icon :component="Notifications" />
            </template>
          </n-button>

          <n-button quaternary @click="showProfile = true">
            <template #icon>
              <n-icon :component="Person" />
            </template>
            {{ currentProfile }}
          </n-button>
        </div>
      </n-layout-header>

      <n-layout-content class="content">
        <router-view />
      </n-layout-content>
    </n-layout>
  </n-layout>

  <!-- Notifications Modal -->
  <n-modal v-model:show="showNotifications" preset="card" title="Notifications" style="width: 400px">
    <n-list hoverable clickable>
      <n-list-item v-for="notif in notifications" :key="notif.id">
        <n-thing :title="notif.title" :description="notif.time">
          {{ notif.message }}
        </n-thing>
      </n-list-item>
      <n-empty v-if="notifications.length === 0" description="No notifications" />
    </n-list>
  </n-modal>

  <!-- Profile Modal -->
  <n-modal v-model:show="showProfile" preset="card" title="Profile" style="width: 500px">
    <n-space vertical>
      <n-select
        v-model:value="currentProfile"
        :options="profileOptions"
        placeholder="Select Profile"
      />
      <n-button block @click="openTerminal">
        <template #icon>
          <n-icon :component="Terminal" />
        </template>
        Open Terminal
      </n-button>
    </n-space>
  </n-modal>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { h } from 'vue'
import { NIcon, NBadge, NEmpty } from 'naive-ui'
import {
  Chatbubbles,
  List,
  Construct,
  Book,
  Time,
  Grid,
  StatsChart,
  Settings,
  DocumentText,
  Notifications,
  Person,
  Sparkles,
  Menu,
  MenuOutline,
  Cpu,
  Terminal,
  Folder,
  Server,
  Timer,
  CloudUpload,
  TerminalOutline,
  Key,
  Database,
  ExtensionPuzzle,
  Cube,
  Gift,
  HardwareChip,
  People,
} from '@vicons/ionicons5'
import type { MenuOption } from 'naive-ui'

const collapsed = ref(false)
const activeMenu = ref('chat')
const notificationCount = ref(0)
const showNotifications = ref(false)
const showProfile = ref(false)
const currentProfile = ref('default')
const currentModel = ref('deepseek-chat')
const gatewayStatus = ref<'running' | 'stopped'>('stopped')

interface Notification {
  id: string
  title: string
  message: string
  time: string
  read: boolean
}

const notifications = ref<Notification[]>([])

const profileOptions = [
  { label: 'default', value: 'default' },
  { label: 'work', value: 'work' },
  { label: 'dev', value: 'dev' },
]

const modelOptions = [
  { label: 'deepseek-chat', value: 'deepseek-chat' },
  { label: 'gpt-4', value: 'gpt-4' },
  { label: 'claude-3', value: 'claude-3' },
]

const menuOptions = computed<MenuOption[]>(() => [
  {
    label: () => 'Chat',
    key: 'chat',
    icon: () => h(NIcon, null, { default: () => h(Chatbubbles) }),
  },
  {
    label: () => 'History',
    key: 'history',
    icon: () => h(NIcon, null, { default: () => h(Time) }),
  },
  {
    type: 'divider',
    key: 'd1',
  },
  {
    label: () => 'Models',
    key: 'models',
    icon: () => h(NIcon, null, { default: () => h(HardwareChip) }),
  },
  {
    label: () => 'Channels',
    key: 'channels',
    icon: () => h(NIcon, null, { default: () => h(Grid) }),
  },
  {
    label: () => 'Profiles',
    key: 'profiles',
    icon: () => h(NIcon, null, { default: () => h(ExtensionPuzzle) }),
  },
  {
    label: () => 'Group Chat',
    key: 'group-chat',
    icon: () => h(NIcon, null, { default: () => h(People) }),
  },
  {
    label: () => 'Gateways',
    key: 'gateways',
    icon: () => h(NIcon, null, { default: () => h(Server) }),
  },
  {
    type: 'divider',
    key: 'd2',
  },
  {
    label: () => 'Skills',
    key: 'skills',
    icon: () => h(NIcon, null, { default: () => h(Book) }),
  },
  {
    label: () => 'Plugins',
    key: 'plugins',
    icon: () => h(NIcon, null, { default: () => h(Cube) }),
  },
  {
    label: () => 'Memory',
    key: 'memory',
    icon: () => h(NIcon, null, { default: () => h(Database) }),
  },
  {
    label: () => 'Jobs',
    key: 'jobs',
    icon: () => h(NIcon, null, { default: () => h(Timer) }),
  },
  {
    type: 'divider',
    key: 'd3',
  },
  {
    label: () => 'Files',
    key: 'files',
    icon: () => h(NIcon, null, { default: () => h(Folder) }),
  },
  {
    label: () => 'Terminal',
    key: 'terminal',
    icon: () => h(NIcon, null, { default: () => h(TerminalOutline) }),
  },
  {
    type: 'divider',
    key: 'd4',
  },
  {
    label: () => 'Usage',
    key: 'usage',
    icon: () => h(NIcon, null, { default: () => h(StatsChart) }),
  },
  {
    label: () => 'Logs',
    key: 'logs',
    icon: () => h(NIcon, null, { default: () => h(DocumentText) }),
  },
  {
    label: () => 'Settings',
    key: 'settings',
    icon: () => h(NIcon, null, { default: () => h(Settings) }),
  },
])

function handleModelSelect(key: string) {
  currentModel.value = key
  // Save to backend
  fetch('/api/config/model', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ model: key }),
  })
}

function openTerminal() {
  showProfile.value = false
  // Navigate to terminal
}

onMounted(async () => {
  // Fetch gateway status
  try {
    const res = await fetch('/api/gateway/status')
    if (res.ok) {
      const data = await res.json()
      gatewayStatus.value = data.status === 'running' ? 'running' : 'stopped'
    }
  } catch (e) {
    console.error('Failed to fetch gateway status:', e)
  }
})
</script>

<style lang="scss" scoped>
.app-layout {
  height: 100vh;
}

.sidebar {
  display: flex;
  flex-direction: column;
}

.logo {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 16px;
  font-size: 18px;
  font-weight: 600;
  color: var(--primary-color, #18a058);
  border-bottom: 1px solid var(--border-color, #f0f0f0);
  transition: all 0.2s;

  &.collapsed {
    justify-content: center;
    padding: 16px 8px;
  }
}

.logo-img {
  width: 32px;
  height: 32px;
  object-fit: contain;
}

.logo-icon {
  font-size: 24px;
}

.gateway-status {
  padding: 12px 16px;
  border-top: 1px solid var(--border-color, #f0f0f0);
  margin-top: auto;
}

.status-text {
  font-size: 12px;
  color: var(--text-color-2, #666);
}

.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 8px 16px;
  height: 52px;
}

.header-left {
  display: flex;
  align-items: center;
  gap: 12px;
}

.header-right {
  display: flex;
  align-items: center;
  gap: 12px;
}

.content {
  padding: 16px;
  background: var(--body-color, #f5f5f5);
  overflow-y: auto;
}
</style>
