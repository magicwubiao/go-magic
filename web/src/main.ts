import { createApp } from 'vue'
import { createPinia } from 'pinia'
import { createRouter, createWebHashHistory } from 'vue-router'
import App from './App.vue'
import { i18n } from './locales'
import { getAuthToken, setAuthToken } from './api/client'
import { getAuthStatus } from './api/auth'

// 页面组件全部按需加载。
//
// 原先这 19 个 view 是静态 import，会被打进同一份首屏 chunk（实测 index chunk
// 约 1.67MB）：用户只想打开 /chat，也要先下载并解析完工具箱、看板、用量这些
// 根本没打开的页面。改成动态 import 后它们各自成 chunk，首屏只拉 ChatView
// 这条链路，其余在路由命中时才取。
const AuthView = () => import('./views/AuthView.vue')
const ChatView = () => import('./views/ChatView.vue')
const ConfigView = () => import('./views/ConfigView.vue')
const ModelsProvidersView = () => import('./views/ModelsProvidersView.vue')
const ToolsView = () => import('./views/ToolsView.vue')
const SkillsView = () => import('./views/SkillsView.vue')
const LogsView = () => import('./views/LogsView.vue')
const SystemView = () => import('./views/SystemView.vue')
const KanbanView = () => import('./views/KanbanView.vue')
const CronView = () => import('./views/CronView.vue')
const GatewayView = () => import('./views/GatewayView.vue')
const BotsView = () => import('./views/BotsView.vue')
const PeersView = () => import('./views/PeersView.vue')
const ProfilesView = () => import('./views/ProfilesView.vue')
const GoalsView = () => import('./views/GoalsView.vue')
const ApprovalView = () => import('./views/ApprovalView.vue')
const FilesView = () => import('./views/FilesView.vue')
const UsageView = () => import('./views/UsageView.vue')
const MCPView = () => import('./views/MCPView.vue')
const AgentPluginsView = () => import('./views/AgentPluginsView.vue')

const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: '/login', component: AuthView, meta: { public: true } },
    { path: '/', redirect: '/chat' },
    // keepAlive：切到别的页面再回来时保留页面组件状态（输入框草稿、消息区滚动
    // 位置、侧栏展开态），避免重新挂载 → 重新拉数据 → 重铺骨架。出口的分流见 App.vue。
    // /bots 同样是聊天形态（bot 单聊 + 群聊），有草稿、有消息区滚动位置、有
    // 侧栏选中态，语义与 /chat 一致，一并缓存。
    { path: '/chat', component: ChatView, meta: { keepAlive: true } },
    { path: '/kanban', component: KanbanView },
    { path: '/config', component: ConfigView },
    { path: '/models-providers', component: ModelsProvidersView },
    { path: '/tools', component: ToolsView },
    { path: '/skills', component: SkillsView },
    { path: '/cron', component: CronView },
    { path: '/gateway', component: GatewayView },
    // Bot 群聊已整合进 /bots（左侧 rail 内切换），旧链接重定向
    { path: '/rooms', redirect: '/bots' },
    { path: '/bots', component: BotsView, meta: { keepAlive: true } },
    // Bot Mode 跨机 peer：本机身份 + 远端实例表 + 直接私聊远端 Bot
    { path: '/peers', component: PeersView },
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
// naive-ui **不做**全局注册（原来是 `app.use(naive)`）。
//
// 整包注册会把库里每一个组件都拉进依赖图：实测产物里那份 naive-ui chunk 1.34MB，
// 而且被 index.html 的 modulepreload 预加载，属于首屏关键路径 —— 用户打开 /chat
// 也要先下载解析这 1.34MB。
//
// 现在改为各 SFC 自己 import 自己模板用到的组件（就像本项目原本就在做的那样）。
// 只集中注册一份清单是不够的：那份清单本身挂在入口，等于把"用到的组件"全塞回首屏。
// 只有让每个页面各带各的，Rollup 才能把"只有一个页面用"的重组件（date-picker、
// data-table、upload……）放进那个页面的 chunk，首屏只留外壳（n-layout / n-menu /
// n-modal / n-config-provider 这些，App.vue 里局部引入）。
//
// ⚠️ 新增页面用到新的 n-* 组件时，必须在**该文件**里 import，否则运行时是
// "Failed to resolve component"：组件静默不渲染，构建和类型检查全都通过，极难排查。
// 守卫脚本 `npm run check:naive` 会逐文件比对模板标签与局部 import。
app.use(i18n)
app.mount('#app')