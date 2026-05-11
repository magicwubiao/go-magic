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
        path: 'history',
        name: 'history',
        component: () => import('@/views/History.vue'),
      },
      {
        path: 'models',
        name: 'models',
        component: () => import('@/views/Models.vue'),
      },
      {
        path: 'channels',
        name: 'channels',
        component: () => import('@/views/Channels.vue'),
      },
      {
        path: 'profiles',
        name: 'profiles',
        component: () => import('@/views/Profiles.vue'),
      },
      {
        path: 'gateways',
        name: 'gateways',
        component: () => import('@/views/Gateways.vue'),
      },
      {
        path: 'skills',
        name: 'skills',
        component: () => import('@/views/Skills.vue'),
      },
      {
        path: 'plugins',
        name: 'plugins',
        component: () => import('@/views/Plugins.vue'),
      },
      {
        path: 'memory',
        name: 'memory',
        component: () => import('@/views/Memory.vue'),
      },
      {
        path: 'jobs',
        name: 'jobs',
        component: () => import('@/views/Jobs.vue'),
      },
      {
        path: 'files',
        name: 'files',
        component: () => import('@/views/Files.vue'),
      },
      {
        path: 'terminal',
        name: 'terminal',
        component: () => import('@/views/Terminal.vue'),
      },
      {
        path: 'usage',
        name: 'usage',
        component: () => import('@/views/Usage.vue'),
      },
      {
        path: 'logs',
        name: 'logs',
        component: () => import('@/views/Logs.vue'),
      },
      {
        path: 'settings',
        name: 'settings',
        component: () => import('@/views/Settings.vue'),
      },
      {
        path: 'group-chat',
        name: 'group-chat',
        component: () => import('@/views/GroupChat.vue'),
      },
    ],
  },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
})

export default router
