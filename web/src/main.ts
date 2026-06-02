import { createApp } from 'vue'
import { createPinia } from 'pinia'
import { createRouter, createWebHashHistory } from 'vue-router'
import naive from 'naive-ui'
import App from './App.vue'
import { i18n } from './locales'
import { getAuthToken } from './api/client'
import { getAuthStatus } from './api/auth'

import AuthView from './views/AuthView.vue'
import ChatView from './views/ChatView.vue'
import ConfigView from './views/ConfigView.vue'
import ModelsProvidersView from './views/ModelsProvidersView.vue'
import ToolsView from './views/ToolsView.vue'
import SkillsView from './views/SkillsView.vue'
import LogsView from './views/LogsView.vue'
import SystemView from './views/SystemView.vue'
import KanbanView from './views/KanbanView.vue'
import CronView from './views/CronView.vue'
import PluginsView from './views/PluginsView.vue'
import GatewayView from './views/GatewayView.vue'
import GroupChatView from './views/GroupChatView.vue'
import ProfilesView from './views/ProfilesView.vue'
import GoalsView from './views/GoalsView.vue'
import ApprovalView from './views/ApprovalView.vue'

const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: '/login', component: AuthView, meta: { public: true } },
    { path: '/', redirect: '/chat' },
    { path: '/chat', component: ChatView },
    { path: '/kanban', component: KanbanView },
    { path: '/config', component: ConfigView },
    { path: '/models-providers', component: ModelsProvidersView },
    { path: '/tools', component: ToolsView },
    { path: '/skills', component: SkillsView },
    { path: '/plugins', component: PluginsView },
    { path: '/cron', component: CronView },
    { path: '/gateway', component: GatewayView },
    { path: '/groupchat', component: GroupChatView },
    { path: '/logs', component: LogsView },
    { path: '/system', component: SystemView },
    { path: '/profiles', component: ProfilesView },
    { path: '/goals', component: GoalsView },
    { path: '/approval', component: ApprovalView },
  ],
})

// Auth route guard
router.beforeEach(async (to) => {
  try {
    const status = await getAuthStatus()

    // Auth not configured → always go to /login (setup page)
    if (!status.configured) {
      if (to.path !== '/login') {
        return { path: '/login' }
      }
      return true
    }

    // Auth configured → check token
    const token = getAuthToken()
    if (!token) {
      // No token → must login
      if (to.path !== '/login') {
        return { path: '/login' }
      }
      return true
    }

    // Has token → authenticated, allow all routes
    // If on login page, redirect to main
    if (to.path === '/login') {
      return { path: '/' }
    }
    return true
  } catch {
    // Server unreachable, allow through
    return true
  }
})

const app = createApp(App)
app.use(createPinia())
app.use(router)
app.use(naive)
app.use(i18n)
app.mount('#app')
