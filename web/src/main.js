import { createApp } from 'vue'
import {
  NConfigProvider, NLayout, NLayoutSider, NLayoutHeader, NLayoutContent,
  NMenu, NButton, NSpace, NAvatar, NIcon, NInput, NCard, NModal, NForm, 
  NFormItem, NSelect, NSwitch, NTabs, NTabPane, NDataTable, NTag, 
  NTooltip, NPopconfirm, NSpin, NScrollbar, NList, NListItem,
  NEmpty, NDivider, NBadge, NAvatarGroup, NText
} from 'naive-ui'
import App from './App.vue'

const app = createApp(App)

// Use naive-ui components
const components = [
  NConfigProvider, NLayout, NLayoutSider, NLayoutHeader, NLayoutContent,
  NMenu, NButton, NSpace, NAvatar, NIcon, NInput, NCard, NModal, NForm, 
  NFormItem, NSelect, NSwitch, NTabs, NTabPane, NDataTable, NTag, 
  NTooltip, NPopconfirm, NSpin, NScrollbar, NList, NListItem,
  NEmpty, NDivider, NBadge, NAvatarGroup, NText
]

components.forEach(comp => {
  app.use(comp)
})

app.mount('#app')
