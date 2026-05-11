import { createRouter, createWebHistory } from 'vue-router'
import type { RouteRecordRaw } from 'vue-router'

const routes: RouteRecordRaw[] = [
  {
    path: '/',
    component: () => import('@/views/Layout.vue'),
    children: [
      {
        path: '',
        name: 'chat',
        component: () => import('@/views/Chat.vue'),
      },
      {
        path: 'sessions',
        name: 'sessions',
        component: () => import('@/views/Sessions.vue'),
      },
      {
        path: 'toolsets',
        name: 'toolsets',
        component: () => import('@/views/Toolsets.vue'),
      },
      {
        path: 'skills',
        name: 'skills',
        component: () => import('@/views/Skills.vue'),
      },
      {
        path: 'cron',
        name: 'cron',
        component: () => import('@/views/Cron.vue'),
      },
      {
        path: 'platforms',
        name: 'platforms',
        component: () => import('@/views/Platforms.vue'),
      },
      {
        path: 'analytics',
        name: 'analytics',
        component: () => import('@/views/Analytics.vue'),
      },
      {
        path: 'settings',
        name: 'settings',
        component: () => import('@/views/Settings.vue'),
      },
      {
        path: 'logs',
        name: 'logs',
        component: () => import('@/views/Logs.vue'),
      },
    ],
  },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
})

export default router
