<template>
  <n-layout has-sider class="app-layout">
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
      <div class="logo" :class="{ collapsed }">
        <n-icon :component="Sparkles" size="24" />
        <span v-if="!collapsed">Go Magic</span>
      </div>

      <n-menu
        v-model:value="activeMenu"
        :collapsed="collapsed"
        :collapsed-width="64"
        :collapsed-icon-size="22"
        :options="menuOptions"
      />
    </n-layout-sider>

    <!-- Main Content -->
    <n-layout>
      <n-layout-header bordered class="header">
        <div class="header-left">
          <n-breadcrumb>
            <n-breadcrumb-item>{{ $t(`nav.${activeMenu}`) }}</n-breadcrumb-item>
          </n-breadcrumb>
        </div>
        <div class="header-right">
          <n-badge :value="notificationCount" :max="99">
            <n-button quaternary circle @click="showNotifications = true">
              <template #icon>
                <n-icon :component="Notifications" />
              </template>
            </n-button>
          </n-badge>
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
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { h } from 'vue'
import { NIcon } from 'naive-ui'
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
} from '@vicons/ionicons5'
import type { MenuOption } from 'naive-ui'

const collapsed = ref(false)
const activeMenu = ref('chat')
const notificationCount = ref(3)
const showNotifications = ref(false)
const showProfile = ref(false)
const currentProfile = ref('default')

const menuOptions = computed<MenuOption[]>(() => [
  {
    label: () => 'Chat',
    key: 'chat',
    icon: () => h(NIcon, null, { default: () => h(Chatbubbles) }),
  },
  {
    label: () => 'Sessions',
    key: 'sessions',
    icon: () => h(NIcon, null, { default: () => h(List) }),
  },
  {
    label: () => 'Toolsets',
    key: 'toolsets',
    icon: () => h(NIcon, null, { default: () => h(Construct) }),
  },
  {
    label: () => 'Skills',
    key: 'skills',
    icon: () => h(NIcon, null, { default: () => h(Book) }),
  },
  {
    label: () => 'Cron',
    key: 'cron',
    icon: () => h(NIcon, null, { default: () => h(Time) }),
  },
  {
    label: () => 'Platforms',
    key: 'platforms',
    icon: () => h(NIcon, null, { default: () => h(Grid) }),
  },
  {
    label: () => 'Analytics',
    key: 'analytics',
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
</script>

<style lang="scss" scoped>
.app-layout {
  height: 100vh;
}

.logo {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 16px;
  font-size: 18px;
  font-weight: 600;
  color: var(--primary-color);
  border-bottom: 1px solid var(--border-color);

  &.collapsed {
    justify-content: center;
    padding: 16px 8px;
  }
}

.header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  height: 56px;
  padding: 0 24px;
  background: var(--card-color);
}

.header-left {
  display: flex;
  align-items: center;
}

.header-right {
  display: flex;
  align-items: center;
  gap: 12px;
}

.content {
  padding: 24px;
  height: calc(100vh - 56px);
  overflow-y: auto;
}
</style>
