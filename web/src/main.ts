import { createApp } from 'vue'
import { createPinia } from 'pinia'
import { createRouter, createWebHashHistory } from 'vue-router'
import naive from 'naive-ui'
import App from './App.vue'
import { i18n } from './locales'
import { getAuthToken, setAuthToken } from './api/client'
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
import GatewayView from './views/GatewayView.vue'
import BotsView from './views/BotsView.vue'
import ProfilesView from './views/ProfilesView.vue'
import GoalsView from './views/GoalsView.vue'
import ApprovalView from './views/ApprovalView.vue'
import FilesView from './views/FilesView.vue'
import UsageView from './views/UsageView.vue'
import MCPView from './views/MCPView.vue'
import AgentPluginsView from './views/AgentPluginsView.vue'

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
    { path: '/cron', component: CronView },
    { path: '/gateway', component: GatewayView },
    // Bot 群聊已整合进 /bots（左侧 rail 内切换），旧链接重定向
    { path: '/rooms', redirect: '/bots' },
    { path: '/bots', component: BotsView },
    { path: '/logs', component: LogsView },
    { path: '/system', component: SystemView },
    { path: '/profiles', component: ProfilesView },
    { path: '/goals', component: GoalsView },
    { path: '/approval', component: ApprovalView },
    { path: '/files', component: FilesView },
    { path: '/usage', component: UsageView },
    { path: '/mcp', component: MCPView },
    { path: '/plugins', component: AgentPluginsView },

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

    // 有 token 不等于会话有效（过期 / 被吊销 / 服务端换了 magic home / 升级后
    // 会话存储重建）。必须由服务端确认，否则会带着死凭据进主界面，十几个并发
    // 请求全部 401，错误提示就落在认证页上了——即"有时出现弹窗错误"。
    //
    // 只在明确 false 时拦：老后端没有 authenticated 字段（新旧混装）时退回旧行为，
    // 否则会把已登录用户永远挡在登录页。
    if (status.authenticated === false) {
      setAuthToken(null)
      if (to.path !== '/login') {
        return { path: '/login' }
      }
      return true
    }

    // Has a valid session → allow all routes
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