<template>
  <div class="platforms-view">
    <n-card title="Platform Channels">
      <n-grid :cols="2" :x-gap="16" :y-gap="16">
        <n-gi v-for="platform in platforms" :key="platform.id">
          <n-card :title="platform.name" hoverable>
            <template #header-extra>
              <n-switch :value="platform.enabled" @update:value="togglePlatform(platform)" />
            </template>

            <n-descriptions :column="1" size="small">
              <n-descriptions-item label="Status">
                <n-tag :type="platform.configured ? 'success' : 'warning'" size="small">
                  {{ platform.configured ? 'Configured' : 'Not Configured' }}
                </n-tag>
              </n-descriptions-item>
              <n-descriptions-item label="Platform">
                {{ platform.id }}
              </n-descriptions-item>
            </n-descriptions>

            <template #footer v-if="!platform.configured">
              <n-button type="primary" size="small" block @click="openConfig(platform)">
                Configure
              </n-button>
            </template>
          </n-card>
        </n-gi>
      </n-grid>

      <n-modal v-model:show="showConfigModal" preset="card" :title="`Configure ${selectedPlatform?.name}`" style="width: 500px">
        <n-form :model="configForm" label-placement="top">
          <n-form-item
            v-for="field in selectedPlatform?.fields || []"
            :key="field.key"
            :label="field.label"
          >
            <n-input
              v-if="field.type === 'text'"
              v-model:value="configForm[field.key]"
              :placeholder="field.placeholder"
              type="password"
            />
            <n-switch v-else-if="field.type === 'boolean'" v-model:value="configForm[field.key]" />
          </n-form-item>
        </n-form>
        <template #footer>
          <n-space justify="end">
            <n-button @click="showConfigModal = false">Cancel</n-button>
            <n-button type="primary" @click="saveConfig">Save</n-button>
          </n-space>
        </template>
      </n-modal>
    </n-card>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { NCard, NGrid, NGi, NSwitch, NTag, NDescriptions, NDescriptionsItem, NButton, NModal, NForm, NFormItem, NInput, NSpace } from 'naive-ui'

interface PlatformField {
  key: string
  label: string
  type: 'text' | 'boolean'
  placeholder?: string
}

interface Platform {
  id: string
  name: string
  enabled: boolean
  configured: boolean
  fields: PlatformField[]
}

const platforms = ref<Platform[]>([
  {
    id: 'telegram',
    name: 'Telegram',
    enabled: false,
    configured: false,
    fields: [
      { key: 'bot_token', label: 'Bot Token', type: 'text', placeholder: '123456:ABC-...' }
    ]
  },
  {
    id: 'discord',
    name: 'Discord',
    enabled: false,
    configured: false,
    fields: [
      { key: 'bot_token', label: 'Bot Token', type: 'text', placeholder: 'Bot token' }
    ]
  },
  {
    id: 'slack',
    name: 'Slack',
    enabled: false,
    configured: false,
    fields: [
      { key: 'bot_token', label: 'Bot Token', type: 'text', placeholder: 'xoxb-...' }
    ]
  },
  {
    id: 'whatsapp',
    name: 'WhatsApp',
    enabled: false,
    configured: false,
    fields: []
  },
  {
    id: 'feishu',
    name: 'Feishu (Lark)',
    enabled: false,
    configured: false,
    fields: [
      { key: 'app_id', label: 'App ID', type: 'text' },
      { key: 'app_secret', label: 'App Secret', type: 'text' }
    ]
  },
  {
    id: 'wecom',
    name: 'WeCom',
    enabled: false,
    configured: false,
    fields: [
      { key: 'corp_id', label: 'Corp ID', type: 'text' },
      { key: 'agent_id', label: 'Agent ID', type: 'text' },
      { key: 'corp_secret', label: 'Corp Secret', type: 'text' }
    ]
  }
])

const showConfigModal = ref(false)
const selectedPlatform = ref<Platform | null>(null)
const configForm = ref<Record<string, any>>({})

const togglePlatform = async (platform: Platform) => {
  // TODO: Call API
}

const openConfig = (platform: Platform) => {
  selectedPlatform.value = platform
  configForm.value = {}
  showConfigModal.value = true
}

const saveConfig = async () => {
  // TODO: Call API
  showConfigModal.value = false
}

const loadPlatforms = async () => {
  // TODO: Call API
}

onMounted(() => {
  loadPlatforms()
})
</script>
