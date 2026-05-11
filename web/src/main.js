import { createApp } from 'vue'
import {
  NConfigProvider, NLayout, NLayoutSider, NLayoutHeader, NLayoutContent,
  NMenu, NButton, NSpace, NAvatar, NIcon, NInput, NChat, useMessage,
  NCard, NModal, NForm, NFormItem, NSelect, NSwitch, NTabs, NTabPane,
  NDataTable, NTag, NTooltip, NPopconfirm, NSpin
} from 'naive-ui'
import App from './App.vue'

const app = createApp(App)

// Use naive-ui components
const components = [
  NConfigProvider, NLayout, NLayoutSider, NLayoutHeader, NLayoutContent,
  NMenu, NButton, NSpace, NAvatar, NIcon, NInput, NChat, useMessage,
  NCard, NModal, NForm, NFormItem, NSelect, NSwitch, NTabs, NTabPane,
  NDataTable, NTag, NTooltip, NPopconfirm, NSpin
]

components.forEach(comp => {
  app.use(comp)
})

app.mount('#app')
