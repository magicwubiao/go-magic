<template>
  <div>
    <n-space justify="space-between" style="margin-bottom: 16px;">
      <h2>Gateway</h2>
      <n-space>
        <n-switch v-model:value="gatewayEnabled" @update:value="saveGatewayEnabled">
          <template #checked>Enabled</template>
          <template #unchecked>Disabled</template>
        </n-switch>
        <n-popconfirm @positive-click="restartGateway">
          <template #trigger>
            <n-button type="warning" :disabled="!gatewayEnabled">Restart</n-button>
          </template>
          Restart gateway? All platforms will reconnect.
        </n-popconfirm>
      </n-space>
    </n-space>

    <!-- Status -->
    <n-card size="small" style="margin-bottom: 16px;">
      <n-space>
        <n-tag :type="gatewayEnabled ? 'success' : 'default'" size="large">
          {{ gatewayEnabled ? '● Enabled' : '○ Disabled' }}
        </n-tag>
        <n-text depth="3">
          {{ enabledCount }} / {{ platforms.length }} platform(s) configured
        </n-text>
      </n-space>
    </n-card>

    <!-- Platform Cards -->
    <n-grid :cols="3" :x-gap="12" :y-gap="12">
      <n-gi v-for="platform in platforms" :key="platform.id">
        <n-card size="small" :title="platform.label">
          <template #header-extra>
            <n-switch v-model:value="platform.enabled" size="small" @update:value="savePlatform(platform)" />
          </template>
          <n-space vertical size="small">
            <n-text depth="3">{{ platform.description }}</n-text>
            <n-tag :type="platform.enabled ? 'success' : 'default'" size="small">
              {{ platform.enabled ? 'Enabled' : 'Disabled' }}
            </n-tag>
          </n-space>
          <template #action>
            <n-space>
              <n-button size="small" @click="openEditModal(platform)">Configure</n-button>
              <n-button v-if="platform.supportsQR" size="small" type="info" @click="showQRInfo(platform)">
                QR Login
              </n-button>
            </n-space>
          </template>
        </n-card>
      </n-gi>
    </n-grid>

    <!-- Edit Platform Modal -->
    <n-modal v-model:show="showEditModal" :title="editingPlatform?.label" preset="dialog" style="width: 500px;">
      <n-form v-if="editingPlatform" label-placement="left" label-width="120" size="small">
        <n-form-item label="Token">
          <n-input
            v-model:value="editingPlatform.token"
            type="password"
            show-password-on="click"
            :placeholder="editingPlatform.tokenPlaceholder"
          />
        </n-form-item>

        <!-- Platform-specific fields -->
        <template v-if="editingPlatform.id === 'wecom'">
          <n-form-item label="Corp ID">
            <n-input v-model:value="editingPlatform.corpId" placeholder="Enterprise Corp ID" />
          </n-form-item>
          <n-form-item label="Agent ID">
            <n-input v-model:value="editingPlatform.agentId" placeholder="Agent ID" />
          </n-form-item>
          <n-form-item label="Secret">
            <n-input v-model:value="editingPlatform.secret" type="password" show-password-on="click" placeholder="Secret" />
          </n-form-item>
        </template>

        <template v-if="editingPlatform.id === 'dingtalk' || editingPlatform.id === 'feishu'">
          <n-form-item label="App Key">
            <n-input v-model:value="editingPlatform.appKey" placeholder="App Key" />
          </n-form-item>
          <n-form-item label="App Secret">
            <n-input v-model:value="editingPlatform.appSecret" type="password" show-password-on="click" placeholder="App Secret" />
          </n-form-item>
        </template>

        <template v-if="editingPlatform.id === 'feishu'">
          <n-form-item label="App ID">
            <n-input v-model:value="editingPlatform.appId" placeholder="App ID" />
          </n-form-item>
        </template>

        <template v-if="editingPlatform.id === 'wechat_ilink' || editingPlatform.id === 'wechat'">
          <n-form-item label="Mode">
            <n-select v-model:value="editingPlatform.mode" :options="editingPlatform.modeOptions" />
          </n-form-item>
          <n-form-item v-if="editingPlatform.id === 'wechat_ilink'" label="Auto Login">
            <n-switch v-model:value="editingPlatform.autoLogin" />
          </n-form-item>
        </template>

        <template v-if="editingPlatform.id === 'whatsapp'">
          <n-form-item label="Mode">
            <n-select v-model:value="editingPlatform.mode" :options="editingPlatform.modeOptions" />
          </n-form-item>
        </template>

        <template v-if="editingPlatform.id === 'qq'">
          <n-form-item label="Number">
            <n-input v-model:value="editingPlatform.number" placeholder="QQ Number" />
          </n-form-item>
          <n-form-item label="Password">
            <n-input v-model:value="editingPlatform.password" type="password" show-password-on="click" placeholder="Password" />
          </n-form-item>
        </template>
      </n-form>
      <template #action>
        <n-space justify="end">
          <n-button @click="showEditModal = false">Cancel</n-button>
          <n-button type="primary" @click="saveEditingPlatform">Save</n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- QR Info Modal -->
    <n-modal v-model:show="showQRModal" title="QR Code Login" preset="dialog">
      <n-alert type="info">
        QR code login is available via CLI. Run the following command in your terminal:
        <pre style="margin-top: 8px; padding: 8px; background: #f5f5f5; border-radius: 4px;">magic gateway start</pre>
      </n-alert>
      <p>Then scan the QR code displayed in the terminal with your {{ qrPlatform?.label }} app.</p>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import { useMessage } from 'naive-ui'
import { useGatewayStore } from '@/stores/gateway'
import { useConfigStore } from '@/stores/config'

interface Platform {
  id: string
  label: string
  description: string
  enabled: boolean
  token: string
  tokenLabel: string
  tokenType: string
  tokenPlaceholder: string
  supportsQR: boolean
  corpId: string
  agentId: string
  secret: string
  appKey: string
  appSecret: string
  appId: string
  mode: string
  modeOptions: { label: string; value: string }[]
  autoLogin: boolean
  number: string
  password: string
}

const message = useMessage()
const gatewayStore = useGatewayStore()
const configStore = useConfigStore()
const gatewayEnabled = ref(false)
const showEditModal = ref(false)
const showQRModal = ref(false)
const editingPlatform = ref<Platform | null>(null)
const qrPlatform = ref<Platform | null>(null)

function createPlatform(id: string, label: string, description: string, tokenLabel: string, tokenPlaceholder: string, supportsQR = false): Platform {
  return reactive({
    id, label, description, enabled: false, token: '',
    tokenLabel, tokenType: 'password', tokenPlaceholder, supportsQR,
    corpId: '', agentId: '', secret: '',
    appKey: '', appSecret: '', appId: '',
    mode: '', modeOptions: [], autoLogin: false,
    number: '', password: '',
  })
}

const platforms = ref<Platform[]>([
  createPlatform('telegram', 'Telegram', 'Telegram Bot', 'Bot Token', 'Token from @BotFather'),
  createPlatform('discord', 'Discord', 'Discord Bot', 'Bot Token', 'Discord Bot Token'),
  createPlatform('slack', 'Slack', 'Slack Bot', 'Bot Token', 'Slack Bot Token'),
  createPlatform('wechat', 'WeChat', 'WeChat Official Account', 'Token', 'WeChat Token', true),
  createPlatform('wechat_ilink', 'WeChat iLink', 'WeChat Personal', 'Token', 'iLink Token', true),
  createPlatform('wecom', 'WeCom', 'Enterprise WeChat', 'Token', 'WeCom Token', true),
  createPlatform('qq', 'QQ', 'QQ Bot', 'App ID', 'QQ App ID'),
  createPlatform('dingtalk', 'DingTalk', 'DingTalk Bot', 'Token', 'DingTalk Token'),
  createPlatform('feishu', 'Feishu/Lark', 'Feishu/Lark Bot', 'Token', 'Feishu Token'),
  createPlatform('whatsapp', 'WhatsApp', 'WhatsApp Bot', 'Token', 'WhatsApp Token', true),
  createPlatform('line', 'LINE', 'LINE Bot', 'Channel Token', 'LINE Channel Token'),
  createPlatform('matrix', 'Matrix', 'Matrix Protocol', 'Token', 'Matrix Token'),
])

// Set mode options for platforms that need them
const wechatPlatform = platforms.value.find(p => p.id === 'wechat')
if (wechatPlatform) wechatPlatform.modeOptions = [{ label: 'QR Code Login', value: 'qr' }, { label: 'Webhook Callback', value: 'callback' }]

const wechatILinkPlatform = platforms.value.find(p => p.id === 'wechat_ilink')
if (wechatILinkPlatform) wechatILinkPlatform.modeOptions = [{ label: 'QR Code Login', value: 'qr' }, { label: 'iLink API', value: 'ilink' }]

const wecomPlatform = platforms.value.find(p => p.id === 'wecom')
if (wecomPlatform) wecomPlatform.modeOptions = [{ label: 'QR Code Login', value: 'qr' }, { label: 'App Callback', value: 'app' }]

const whatsappPlatform = platforms.value.find(p => p.id === 'whatsapp')
if (whatsappPlatform) whatsappPlatform.modeOptions = [{ label: 'Personal (QR)', value: 'personal' }, { label: 'Business API', value: 'business' }]

const enabledCount = computed(() => platforms.value.filter(p => p.enabled).length)

function populateFromConfig(cfg: any) {
  const gw = cfg.gateway || {}
  gatewayEnabled.value = gw.enabled || false
  const platformConfigs = gw.platforms || {}

  for (const platform of platforms.value) {
    const pc = platformConfigs[platform.id] || {}
    platform.enabled = pc.enabled || false
    platform.token = pc.token || ''
    platform.corpId = pc.corp_id || ''
    platform.agentId = pc.agent_id || ''
    platform.secret = pc.secret || ''
    platform.appKey = pc.app_key || ''
    platform.appSecret = pc.app_secret || ''
    platform.appId = pc.app_id || ''
    platform.mode = pc.mode || ''
    platform.autoLogin = pc.auto_login || false
    platform.number = pc.number || ''
    platform.password = pc.password || ''
  }
}

function buildPlatformPayload(platform: Platform): Record<string, any> {
  const payload: any = {
    enabled: platform.enabled,
    token: platform.token,
  }
  if (platform.corpId) payload.corp_id = platform.corpId
  if (platform.agentId) payload.agent_id = platform.agentId
  if (platform.secret) payload.secret = platform.secret
  if (platform.appKey) payload.app_key = platform.appKey
  if (platform.appSecret) payload.app_secret = platform.appSecret
  if (platform.appId) payload.app_id = platform.appId
  if (platform.mode) payload.mode = platform.mode
  if (platform.autoLogin) payload.auto_login = platform.autoLogin
  if (platform.number) payload.number = platform.number
  if (platform.password) payload.password = platform.password
  return payload
}

async function savePlatform(platform: Platform): Promise<void> {
  try {
    const platformsPayload: any = {}
    platformsPayload[platform.id] = buildPlatformPayload(platform)
    await configStore.updateConfig({ gateway: { enabled: gatewayEnabled.value, platforms: platformsPayload } })
  } catch (e) {
    message.error('Failed to save: ' + (e instanceof Error ? e.message : 'Unknown error'))
  }
}

async function saveGatewayEnabled(): Promise<void> {
  try {
    await configStore.updateConfig({ gateway: { enabled: gatewayEnabled.value } })
    message.success(gatewayEnabled.value ? 'Gateway enabled' : 'Gateway disabled')
  } catch (e) {
    message.error('Failed to update: ' + (e instanceof Error ? e.message : 'Unknown error'))
  }
}

async function restartGateway(): Promise<void> {
  try {
    await gatewayStore.restart()
    message.success('Gateway restarting...')
  } catch (e) {
    message.error('Failed to restart: ' + (e instanceof Error ? e.message : 'Unknown error'))
  }
}

function openEditModal(platform: Platform): void {
  editingPlatform.value = platform
  showEditModal.value = true
}

function showQRInfo(platform: Platform): void {
  qrPlatform.value = platform
  showQRModal.value = true
}

async function saveEditingPlatform(): Promise<void> {
  if (!editingPlatform.value) return
  await savePlatform(editingPlatform.value)
  showEditModal.value = false
  message.success('Platform configuration saved')
}

onMounted(async () => {
  await gatewayStore.loadStatus()
  await configStore.loadConfig()
  if (configStore.config) {
    populateFromConfig(configStore.config)
  }
})
</script>
