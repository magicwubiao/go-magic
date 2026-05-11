import { createI18n } from 'vue-i18n'

const messages = {
  en: {
    app: {
      title: 'Go Magic Dashboard',
      subtitle: 'AI Agent Management',
    },
    nav: {
      chat: 'Chat',
      sessions: 'Sessions',
      toolsets: 'Toolsets',
      skills: 'Skills',
      cron: 'Cron Jobs',
      platforms: 'Platforms',
      analytics: 'Analytics',
      settings: 'Settings',
      logs: 'Logs',
    },
    chat: {
      title: 'Chat',
      placeholder: 'Type a message...',
      send: 'Send',
      newSession: 'New Session',
    },
    sessions: {
      title: 'Sessions',
      search: 'Search sessions...',
      empty: 'No sessions yet',
    },
    toolsets: {
      title: 'Toolsets',
      enable: 'Enable',
      disable: 'Disable',
    },
    skills: {
      title: 'Skills',
      browse: 'Browse',
      install: 'Install',
    },
    cron: {
      title: 'Cron Jobs',
      create: 'Create Job',
      pause: 'Pause',
      resume: 'Resume',
      delete: 'Delete',
    },
    platforms: {
      title: 'Platform Channels',
      telegram: 'Telegram',
      discord: 'Discord',
      slack: 'Slack',
      whatsapp: 'WhatsApp',
      feishu: 'Feishu',
      wecom: 'WeCom',
    },
    analytics: {
      title: 'Usage Analytics',
      tokens: 'Total Tokens',
      sessions: 'Sessions',
      cost: 'Estimated Cost',
    },
    settings: {
      title: 'Settings',
      profile: 'Profile',
      provider: 'Provider',
      display: 'Display',
      agent: 'Agent',
    },
    logs: {
      title: 'Logs',
      level: 'Level',
      filter: 'Filter logs...',
    },
  },
  zh: {
    app: {
      title: 'Go Magic 管理面板',
      subtitle: 'AI Agent 管理',
    },
    nav: {
      chat: '对话',
      sessions: '会话',
      toolsets: '工具集',
      skills: '技能',
      cron: '定时任务',
      platforms: '平台',
      analytics: '分析',
      settings: '设置',
      logs: '日志',
    },
    chat: {
      title: '对话',
      placeholder: '输入消息...',
      send: '发送',
      newSession: '新会话',
    },
    sessions: {
      title: '会话管理',
      search: '搜索会话...',
      empty: '暂无会话',
    },
    toolsets: {
      title: '工具集',
      enable: '启用',
      disable: '禁用',
    },
    skills: {
      title: '技能',
      browse: '浏览',
      install: '安装',
    },
    cron: {
      title: '定时任务',
      create: '创建任务',
      pause: '暂停',
      resume: '恢复',
      delete: '删除',
    },
    platforms: {
      title: '平台渠道',
      telegram: 'Telegram',
      discord: 'Discord',
      slack: 'Slack',
      whatsapp: 'WhatsApp',
      feishu: '飞书',
      wecom: '企业微信',
    },
    analytics: {
      title: '使用分析',
      tokens: '总 Token 数',
      sessions: '会话数',
      cost: '预估费用',
    },
    settings: {
      title: '设置',
      profile: '配置',
      provider: '供应商',
      display: '显示',
      agent: 'Agent',
    },
    logs: {
      title: '日志',
      level: '级别',
      filter: '过滤日志...',
    },
  },
}

export const i18n = createI18n({
  legacy: false,
  locale: 'en',
  fallbackLocale: 'en',
  messages,
})
