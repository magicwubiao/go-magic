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
          v-model:collapsed="siderCollapsed"
        >
          <sidebar-logo :collapsed="siderCollapsed" />
          <n-menu
            :collapsed-width="64"
            :collapsed-icon-size="22"
            :options="menuOptions"
            :value="activeKey"
            @update:value="handleMenuClick"
          />
          <div style="padding: 8px 0; border-top: 1px solid #e0e0e0; margin-top: auto;">
            <n-menu
              :collapsed-width="64"
              :collapsed-icon-size="22"
              :options="logoutOption"
              @update:value="handleLogoutClick"
            />
          </div>
        </n-layout-sider>

        <!-- Logout Confirm Modal -->
        <n-modal
          v-model:show="showLogoutConfirm"
          preset="dialog"
          :title="t('common.logout')"
          :content="t('common.confirmLogout')"
          :positive-text="t('common.confirm')"
          :negative-text="t('common.cancel')"
          @positive-click="handleLogout"
        />
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
import { computed, h, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { NIcon, NModal } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import SidebarLogo from '@/components/SidebarLogo.vue'
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
  FlagOutline,
  LogOutOutline,
  ShieldCheckmarkOutline,
} from '@vicons/ionicons5'
import { useAuthStore } from '@/stores/auth'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()
const showLogoutConfirm = ref(false)
const siderCollapsed = ref(false)

const isLoginPage = computed(() => route.path === '/login')

const activeKey = computed(() => route.path)

function handleMenuClick(key: string) {
  router.push(key)
}

function handleLogoutClick() {
  showLogoutConfirm.value = true
}

function handleLogout() {
  authStore.logout()
  router.push('/login')
}

const menuOptions = computed(() => [
  { label: t('nav.chat'), key: '/chat', icon: renderIcon(ChatbubbleOutline) },
  { label: t('nav.kanban'), key: '/kanban', icon: renderIcon(GridOutline) },
  { label: t('goals.title'), key: '/goals', icon: renderIcon(FlagOutline) },
  { type: 'divider' as const },
  { label: t('models.title'), key: '/models-providers', icon: renderIcon(CubeOutline) },
  { label: t('nav.tools'), key: '/tools', icon: renderIcon(HammerOutline) },
  { label: t('nav.skills'), key: '/skills', icon: renderIcon(StarOutline) },
  { label: t('nav.plugins'), key: '/plugins', icon: renderIcon(ExtensionPuzzleOutline) },
  { type: 'divider' as const },
  { label: t('nav.cronJobs'), key: '/cron', icon: renderIcon(TimeOutline) },
  { label: t('nav.gateway'), key: '/gateway', icon: renderIcon(GitNetworkOutline) },
  { label: t('nav.groupChat'), key: '/groupchat', icon: renderIcon(PeopleOutline) },
  { type: 'divider' as const },
  { label: t('nav.approval'), key: '/approval', icon: renderIcon(ShieldCheckmarkOutline) },
  { label: t('nav.profiles'), key: '/profiles', icon: renderIcon(PersonOutline) },
  { label: t('nav.logs'), key: '/logs', icon: renderIcon(DocumentTextOutline) },
  { label: t('nav.system'), key: '/system', icon: renderIcon(HardwareChipOutline) },
  { label: t('nav.config'), key: '/config', icon: renderIcon(SettingsOutline) },
])

const logoutOption = computed(() => [
  { label: t('common.logout'), key: 'logout', icon: renderIcon(LogOutOutline) },
])

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
