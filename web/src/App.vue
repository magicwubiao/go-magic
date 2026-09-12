<template>
  <n-config-provider :locale="naiveLocale" :date-locale="naiveDateLocale">
  <n-notification-provider>
    <n-message-provider>
      <n-dialog-provider>
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
                  :trigger="isMobile ? 'click' : 'hover'"
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
            <n-layout-content :class="{'full-content': isChatPage}" style="padding: 24px; overflow: auto;">
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
      </n-dialog-provider>
    </n-message-provider>
  </n-notification-provider>
  </n-config-provider>
</template>

<script setup lang="ts">
import { computed, h, ref, onMounted, onUnmounted } from 'vue'
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
  PersonOutline,
  FlagOutline,
  ShieldCheckmarkOutline,
  FolderOutline,
  PieChartOutline,
  ServerOutline,
  LogOutOutline,
} from '@vicons/ionicons5'
import { useAuthStore } from '@/stores/auth'
import { useChatStore } from '@/stores/chat'

const { t, locale } = useI18n()
const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()
const showLogoutConfirm = ref(false)
const isMobile = ref(window.innerWidth <= 768)
const siderCollapsed = ref(isMobile.value)

function handleAppResize() {
  const nowMobile = window.innerWidth <= 768
  // 从桌面端缩放到移动端时，将导航栏默认折叠（移出屏幕外）；
  // 从移动端切回桌面端则展开。仅在小屏/大屏切换边界处调整，
  // 避免打断用户在移动端手动展开/折叠的意图。
  if (nowMobile && !isMobile.value) {
    siderCollapsed.value = true
  } else if (!nowMobile && isMobile.value) {
    siderCollapsed.value = false
  }
  isMobile.value = nowMobile
}

onMounted(() => {
  window.addEventListener('resize', handleAppResize)
})

onUnmounted(() => {
  window.removeEventListener('resize', handleAppResize)
})

// naive-ui 组件库语言跟随 i18n，确保 popconfirm/date-picker 等内置按钮翻译正确
const naiveLocale = computed(() => locale.value === 'zh' ? zhCN : enUS)
const naiveDateLocale = computed(() => locale.value === 'zh' ? dateZhCN : dateEnUS)

const isLoginPage = computed(() => route.path === '/login')
const isChatPage = computed(() => route.path === '/chat' || route.path === '/rooms' || route.path === '/bots')
const activeKey = computed(() => route.path)

// 移动端顶部工具条标题：根据当前路由映射到对应菜单文案
const pageTitle = computed(() => {
  const map: Record<string, string> = {
    '/chat': t('nav.chat'),
    '/kanban': t('nav.kanban'),
    '/goals': t('goals.title'),
    '/models-providers': t('models.title'),
    '/tools': t('nav.tools'),
    '/skills': t('nav.skills'),
    '/cron': t('nav.cronJobs'),
    '/bots': t('bots.title'),
    '/gateway': t('nav.gateway'),
    '/files': t('nav.files'),
    '/mcp': t('nav.mcp'),
    '/plugins': t('nav.plugins'),
    '/profiles': t('nav.profiles'),
    '/approval': t('nav.approval'),
    '/logs': t('nav.logs'),
    '/usage': t('nav.usage'),
    '/system': t('nav.system'),
    '/config': t('nav.config'),
  }
  return map[route.path] || t('nav.chat')
})

onUnmounted(() => {
  useChatStore().cleanup()
})

function handleMenuClick(key: string) {
  router.push(key)
  // 移动端切换导航后自动收起侧边栏，避免遮挡内容
  if (isMobile.value) {
    siderCollapsed.value = true
  }
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
    // 移动端切换导航后自动收起侧边栏
    if (isMobile.value) {
      siderCollapsed.value = true
    }
    return
  }
  switch (key) {
    case 'system':
      router.push('/system')
      if (isMobile.value) siderCollapsed.value = true
      break
    case 'config':
      router.push('/config')
      if (isMobile.value) siderCollapsed.value = true
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
  { label: t('bots.title'), key: '/bots', icon: renderIcon(HardwareChipOutline) },
  { label: t('nav.gateway'), key: '/gateway', icon: renderIcon(GitNetworkOutline) },
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
  height: 6px;
}

/* 滚动条默认隐藏,悬停/滚动所在容器时才显示 */
::-webkit-scrollbar-thumb {
  background: transparent;
  border-radius: 3px;
  transition: background 0.3s ease;
}

/* Chrome/Edge/Safari: 悬停在可滚动容器上时 thumb 淡入 */
*:hover::-webkit-scrollbar-thumb {
  background: #c0c4cc;
}
*::-webkit-scrollbar-thumb:hover,
*:hover::-webkit-scrollbar-thumb:hover {
  background: #9ea3ab;
}

/* Firefox: 默认隐藏,hover 时显示 */
* {
  scrollbar-width: thin;
  scrollbar-color: transparent transparent;
}
*:hover {
  scrollbar-color: #c0c4cc transparent;
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

/* 弹窗全局响应式规范:宽度自适应 + 内容滚动 */
.modal-responsive {
  max-width: 96vw;
}

.modal-scroll {
  max-height: 85vh;
  overflow-y: auto;
  overscroll-behavior: contain;
}

/* Responsive: Mobile devices */
@media (max-width: 768px) {
  /* 页面网格在小屏统一堆叠为单列(统计卡/技能卡等) */
  .n-grid {
    grid-template-columns: repeat(1, minmax(0, 1fr)) !important;
  }

  /* 页面标题区换行、缩小留白 */
  h2 {
    font-size: 18px;
  }

  /* 卡片内边距收敛,腾出内容空间 */
  .n-card > .n-card-header {
    padding-top: 12px;
    padding-bottom: 8px;
  }
  .n-card > .n-card__content {
    padding: 10px 14px;
  }

  /* 表格在小屏用更小字号,容器允许横向滚动 */
  .n-data-table {
    font-size: 13px;
  }
  .n-data-table-wrapper {
    overflow-x: auto;
  }

  /* 触控目标不低于 36px 高 */
  .n-button--small-type {
    height: 34px;
    padding: 0 10px;
  }

  /* 标签页可横向滚动 */
  .n-tabs .n-tabs-nav {
    overflow-x: auto;
  }

  /* 底部安全区(刘海屏/手势条) */
  .n-layout-scroll-container {
    padding-bottom: env(safe-area-inset-bottom);
  }

  .n-layout-sider {
    position: fixed !important;
    z-index: 200;
    height: 100vh;
    left: -220px;
    transition: left 0.3s ease;
  }
  
  .n-layout-sider.n-layout-sider--collapsed {
    left: -220px;
  }
  
  .n-layout-sider:not(.n-layout-sider--collapsed) {
    left: 0;
    box-shadow: 2px 0 8px rgba(0,0,0,0.15);
  }
  
  .n-layout-content {
    margin-left: 0 !important;
    /* 悬浮工具条不占布局空间，内容占满全屏，不再让出顶部高度 */
  }

  /* 聊天页 full-content 同样不需要再让出工具条高度 */
  .n-layout-content.full-content {
    padding-top: 0 !important;
  }
  
  /* 移动端：侧边栏自带的展开/关闭触发器（替代原汉堡按钮）。
     实际 DOM 元素类名是 .n-layout-toggle-button（naive-ui 实现），
     而不是 .n-layout-sider__trigger。
     保持 naive-ui 原生的「一半嵌在抽屉边缘」样式。注意不能用 right:0 锚定：
     收起时 naive-ui 会把 sider 宽度动画到 collapsed-width=64，right 边缘跟着
     缩进屏幕外（left:-220px + width 64px），按钮会整颗消失。
     因此用 left:220px（抽屉固定宽）锚定可见右缘，随抽屉一起滑动。
     尺寸对齐 ChatView 右侧 right-sidebar-fab：40px 圆形 + 20px 图标。 */
  .n-layout-sider > .n-layout-toggle-button {
    display: flex !important;
    align-items: center;
    justify-content: center;
    position: absolute !important;
    top: 50% !important;
    left: 220px !important;
    right: auto !important;
    transform: translate(-50%, -50%) !important;
    width: 40px !important;
    height: 40px !important;
    min-width: 40px !important;
    border-radius: 50% !important;
    background: #fff !important;
    border: 1px solid #e0e0e0 !important;
    box-shadow: 0 2px 8px rgba(0, 0, 0, 0.12);
    z-index: 250;
  }
  .n-layout-sider > .n-layout-toggle-button:hover {
    background: #f5f5f5 !important;
  }
  /* 触发器内图标与右侧 FAB 一致（GridOutline :size=20） */
  .n-layout-sider > .n-layout-toggle-button .n-base-icon {
    font-size: 20px !important;
  }
}

/* 深色模式下侧边栏触发器配色跟随系统 */
@media (prefers-color-scheme: dark) {
  .n-layout-sider > .n-layout-toggle-button {
    background: #1f1f1f !important;
    border-color: #333 !important;
    box-shadow: 0 2px 10px rgba(0, 0, 0, 0.5);
  }
  .n-layout-sider > .n-layout-toggle-button:hover {
    background: #2a2a2a !important;
  }
}
</style>