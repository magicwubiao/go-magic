<template>
  <n-notification-provider>
    <n-message-provider>
      <!-- Login page: no sidebar -->
      <template v-if="isLoginPage">
        <router-view />
      </template>
      <!-- Main layout: with sidebar -->
      <n-layout v-else has-sider style="height: 100vh;">
        <n-layout-sider
          bordered
          collapse-mode="width"
          :collapsed-width="64"
          :width="220"
          show-trigger
        >
          <div style="padding: 16px 16px 8px; text-align: center;">
            <n-text strong style="font-size: 18px;">Magic</n-text>
          </div>
          <n-menu
            :collapsed-width="64"
            :collapsed-icon-size="22"
            :options="menuOptions"
            :value="activeKey"
            @update:value="handleMenuClick"
          />
          <div style="padding: 12px; border-top: 1px solid #e0e0e0; margin-top: auto;">
            <n-button block quaternary size="small" @click="handleLogout">
              ↩ Logout
            </n-button>
          </div>
        </n-layout-sider>
        <n-layout>
          <n-layout-content style="padding: 24px; overflow: auto;">
            <router-view />
          </n-layout-content>
        </n-layout>
      </n-layout>
    </n-message-provider>
  </n-notification-provider>
</template>

<script setup lang="ts">
import { computed, h } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { NIcon } from 'naive-ui'
import {
  ChatbubbleOutline,
  SettingsOutline,
  CubeOutline,
  HammerOutline,
  StarOutline,
  DocumentTextOutline,
  HardwareChipOutline,
  GridOutline,
  TimeOutline,
  ExtensionPuzzleOutline,
  GitNetworkOutline,
  PeopleOutline,
  PersonOutline,
} from '@vicons/ionicons5'
import { useAuthStore } from '@/stores/auth'

const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()

const isLoginPage = computed(() => route.path === '/login')

const activeKey = computed(() => route.path)

function handleMenuClick(key: string) {
  router.push(key)
}

function handleLogout() {
  authStore.logout()
  router.push('/login')
}

const menuOptions = [
  { label: 'Chat', key: '/chat', icon: renderIcon(ChatbubbleOutline) },
  { label: 'Kanban', key: '/kanban', icon: renderIcon(GridOutline) },
  { type: 'divider' as const },
  { label: 'Models & Providers', key: '/models-providers', icon: renderIcon(CubeOutline) },
  { label: 'Tools', key: '/tools', icon: renderIcon(HammerOutline) },
  { label: 'Skills', key: '/skills', icon: renderIcon(StarOutline) },
  { label: 'Plugins', key: '/plugins', icon: renderIcon(ExtensionPuzzleOutline) },
  { type: 'divider' as const },
  { label: 'Cron Jobs', key: '/cron', icon: renderIcon(TimeOutline) },
  { label: 'Gateway', key: '/gateway', icon: renderIcon(GitNetworkOutline) },
  { label: 'Group Chat', key: '/groupchat', icon: renderIcon(PeopleOutline) },
  { type: 'divider' as const },
  { label: 'Profiles', key: '/profiles', icon: renderIcon(PersonOutline) },
  { label: 'Logs', key: '/logs', icon: renderIcon(DocumentTextOutline) },
  { label: 'System', key: '/system', icon: renderIcon(HardwareChipOutline) },
  { label: 'Config', key: '/config', icon: renderIcon(SettingsOutline) },
]

function renderIcon(icon: any) {
  return () => h(NIcon, null, { default: () => h(icon) })
}
</script>

<style>
* {
  margin: 0;
  padding: 0;
  box-sizing: border-box;
}

body {
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, sans-serif;
}

::-webkit-scrollbar {
  width: 6px;
}

::-webkit-scrollbar-thumb {
  background: #c0c0c0;
  border-radius: 3px;
}
</style>
