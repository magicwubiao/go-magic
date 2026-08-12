<template>
  <n-config-provider :locale="naiveLocale" :date-locale="naiveDateLocale">
  <n-notification-provider>
    <n-message-provider>
      <!-- Login page: no header -->
      <template v-if="isLoginPage">
        <router-view />
      </template>
      <!-- Main layout: no top header -->
      <n-layout v-else style="height: 100vh;" position="absolute">
        <!-- Main Content Area -->
        <n-layout
          has-sider
          position="absolute"
          style="top: 0; bottom: 0; left: 0; right: 0;"
        >
          <!-- Sidebar -->
          <n-layout-sider
            bordered
            collapse-mode="width"
            :collapsed-width="64"
            :width="220"
            show-trigger
            v-model:collapsed="siderCollapsed"
          >
            <div class="sider-flex">
              <n-menu
                class="sider-menu"
                :collapsed-width="64"
                :collapsed-icon-size="22"
                :options="menuOptions"
                :value="activeKey"
                @update:value="handleMenuClick"
              />
              <!-- 底部"设置"下拉:点击向上弹出 -->
              <div class="sider-footer">
                <n-dropdown
                  placement="top-start"
                  :options="headerOptions"
                  @select="handleHeaderSelect"
                >
                  <div class="settings-trigger" :class="{ collapsed: siderCollapsed }">
                    <n-icon size="20"><settings-outline /></n-icon>
                    <span v-if="!siderCollapsed" class="settings-label">{{ t('nav.settings') }}</span>
                  </div>
                </n-dropdown>
              </div>
            </div>
          </n-layout-sider>

          <!-- Content -->
          <n-layout>
            <n-layout-content :class="{ 'full-content': isChatPage }" style="padding: 24px; overflow: auto;">
              <router-view />
            </n-layout-content>
          </n-layout>
        </n-layout>
      </n-layout>

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
    </n-message-provider>
  </n-notification-provider>
  </n-config-provider>
</template>

<script setup lang="ts">
import { computed, h, ref, onUnmounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { NIcon, zhCN, dateZhCN, enUS, dateEnUS } from 'naive-ui'
import { useI18n } from 'vue-i18n'
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
  ShieldCheckmarkOutline,
  FolderOutline,
  PieChartOutline,
  ServerOutline,
  LogOutOutline,
  BriefcaseOutline,
} from '@vicons/ionicons5'
import { useAuthStore } from '@/stores/auth'
import { useChatStore } from '@/stores/chat'

const { t, locale } = useI18n()
const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()
const showLogoutConfirm = ref(false)
const siderCollapsed = ref(false)

// naive-ui 组件库语言跟随 i18n，确保 popconfirm/date-picker 等内置按钮翻译正确
const naiveLocale = computed(() => locale.value === 'zh' ? zhCN : enUS)
const naiveDateLocale = computed(() => locale.value === 'zh' ? dateZhCN : dateEnUS)

const isLoginPage = computed(() => route.path === '/login')
const isChatPage = computed(() => route.path === '/chat' || route.path === '/groupchat')
const activeKey = computed(() => route.path)

onUnmounted(() => {
  useChatStore().cleanup()
})

function handleMenuClick(key: string) {
  router.push(key)
}

function handleLogoutClick() {
  showLogoutConfirm.value = true
}

function handleLogout() {
  useChatStore().cleanup()
  authStore.logout()
  router.push('/login')
}

function handleHeaderSelect(key: string) {
  // 以 "/" 开头的是路由跳转(管理下拉的审批/日志/用量)。
  if (key.startsWith('/')) {
    router.push(key)
    return
  }
  switch (key) {
    case 'system':
      router.push('/system')
      break
    case 'config':
      router.push('/config')
      break
    case 'logout':
      showLogoutConfirm.value = true
      break
  }
}

const menuOptions = computed(() => [
  { label: t('nav.chat'), key: '/chat', icon: renderIcon(ChatbubbleOutline) },
  { label: t('nav.kanban'), key: '/kanban', icon: renderIcon(GridOutline) },
  { label: t('goals.title'), key: '/goals', icon: renderIcon(FlagOutline) },
  { type: 'divider' as const },
  { label: t('models.title'), key: '/models-providers', icon: renderIcon(CubeOutline) },
  { label: t('nav.tools'), key: '/tools', icon: renderIcon(HammerOutline) },
  { label: t('nav.skills'), key: '/skills', icon: renderIcon(StarOutline) },
  { type: 'divider' as const },
  { label: t('nav.cronJobs'), key: '/cron', icon: renderIcon(TimeOutline) },
  { label: t('nav.gateway'), key: '/gateway', icon: renderIcon(GitNetworkOutline) },
  { label: t('nav.groupChat'), key: '/groupchat', icon: renderIcon(PeopleOutline) },
  { label: t('nav.files'), key: '/files', icon: renderIcon(FolderOutline) },
  { type: 'divider' as const },
  { label: t('nav.mcp'), key: '/mcp', icon: renderIcon(ServerOutline) },
  { label: t('nav.plugins'), key: '/plugins', icon: renderIcon(ExtensionPuzzleOutline) },
  { label: t('nav.profiles'), key: '/profiles', icon: renderIcon(PersonOutline) },
])

// 顶部"设置"下拉:管理类(审批/日志/用量) + 系统类(系统/配置) + 退出
const headerOptions = computed(() => [
  { label: t('nav.approval'), key: '/approval', icon: renderIcon(ShieldCheckmarkOutline) },
  { label: t('nav.logs'), key: '/logs', icon: renderIcon(DocumentTextOutline) },
  { label: t('nav.usage'), key: '/usage', icon: renderIcon(PieChartOutline) },
  { type: 'divider' as const },
  { label: t('nav.system'), key: 'system', icon: renderIcon(HardwareChipOutline) },
  { label: t('nav.config'), key: 'config', icon: renderIcon(SettingsOutline) },
  { type: 'divider' as const },
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

.full-content {
  padding: 0 !important;
}

/* 侧边栏 flex 布局:菜单占满,设置下拉固定在底部 */
.sider-flex {
  display: flex;
  flex-direction: column;
  height: 100%;
}
.sider-menu {
  flex: 1;
  overflow-y: auto;
  overflow-x: hidden;
}
.sider-footer {
  border-top: 1px solid #e0e0e0;
  flex-shrink: 0;
}
.settings-trigger {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 0 20px;
  height: 48px;
  cursor: pointer;
  color: #333;
  font-size: 14px;
  transition: background 0.2s;
}
.settings-trigger:hover {
  background: #f5f5f5;
}
.settings-trigger.collapsed {
  justify-content: center;
  padding: 0;
}
.settings-label {
  white-space: nowrap;
}
</style>
