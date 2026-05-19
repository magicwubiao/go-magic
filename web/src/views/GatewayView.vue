<template>
  <div>
    <n-space justify="space-between" style="margin-bottom: 16px;">
      <h2>Gateway</h2>
      <n-space>
        <n-switch v-model:value="gatewayEnabled" @update:value="saveGatewayEnabled">
          <template #checked>Running</template>
          <template #unchecked>Stopped</template>
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
      <n-space align="center">
        <n-tag :type="gatewayEnabled ? 'success' : 'default'" size="large">
          {{ gatewayEnabled ? '● Running' : '○ Stopped' }}
        </n-tag>
        <n-text depth="3">
          {{ enabledCount }} / {{ platforms.length }} platform(s) enabled
        </n-text>
      </n-space>
    </n-card>

    <!-- Platform Cards -->
    <n-grid :cols="2" :x-gap="12" :y-gap="12">
      <n-gi v-for="platform in platforms" :key="platform.id">
        <n-card size="small" :title="platform.label">
          <template #header-extra>
            <n-switch v-model:value="platform.enabled" size="small" @update:value="savePlatform(platform)" />
          </template>

          <!-- Token Config -->
          <n-form label-placement="left" label-width="100" size="small">
            <n-form-item :label="platform.tokenLabel">
              <n-input
                v-model:value="platform.token"
                :type="platform.tokenType"
                show-password-on="click"
                :placeholder="platform.tokenPlaceholder"
                size="small"
                @blur="savePlatform(platform)"
              />
            </n-form-item>

            <!-- Platform-specific fields -->
            <template v-if="platform.id === 'wecom'">
              <n-form-item label="Corp ID">
                <n-input v-model:value="platform.corpId" placeholder="Enterprise Corp ID" size="small" @blur="savePlatform(platform)" />
              </n-form-item>
              <n-form-item label="Agent ID">
                <n-input v-model:value="platform.agentId" placeholder="Agent ID" size="small" @blur="savePlatform(platform)" />
              </n-form-item>
              <n-form-item label="Secret">
                <n-input v-model:value="platform.secret" type="password" show-password-on="click" placeholder="Secret" size="small" @blur="savePlatform(platform)" />
              </n-form-item>
            </template>

            <template v-if="platform.id === 'dingtalk' || platform.id === 'feishu'">
              <n-form-item label="App Key">
                <n-input v-model:value="platform.appKey" placeholder="App Key" size="small" @blur="savePlatform(platform)" />
              </n-form-item>
              <n-form-item label="App Secret">
                <n-input v-model:value="platform.appSecret" type="password" show-password-on="click" placeholder="App Secret" size="small" @blur="savePlatform(platform)" />
              </n-form-item>
            </template>

            <template v-if="platform.id === 'feishu'">
              <n-form-item label="App ID">
                <n-input v-model:value="platform.appId" placeholder="App ID" size="small" @blur="savePlatform(platform)" />
              </n-form-item>
            </template>

            <template v-if="platform.id === 'wechat_ilink' || platform.id === 'wechat'">
              <n-form-item label="Mode">
                <n-select v-model:value="platform.mode" :options="platform.modeOptions" size="small" @update:value="savePlatform(platform)" />
              </n-form-item>
              <n-form-item v-if="platform.id === 'wechat_ilink'" label="Auto Login">
                <n-switch v-model:value="platform.autoLogin" size="small" @update:value="savePlatform(platform)" />
              </n-form-item>
            </template>

            <template v-if="platform.id === 'whatsapp'">
              <n-form-item label="Mode">
                <n-select v-model:value="platform.mode" :options="platform.modeOptions" size="small" @update:value="savePlatform(platform)" />
              </n-form-item>
            </template>

            <template v-if="platform.id === 'qq'">
              <n-form-item label="Number">
                <n-input v-model:value="platform.number" placeholder="QQ Number" size="small" @blur="savePlatform(platform)" />
              </n-form-item>
              <n-form-item label="Password">
                <n-input v-model:value="platform.password" type="password" show-password-on="click" placeholder="Password" size="small" @blur="savePlatform(platform)" />
              </n-form-item>
            </template>
          </n-form>

          <!-- QR Code Section -->
          <template v-if="platform.supportsQR">
            <n-divider style="margin: 8px 0;" />
            <n-space vertical>
              <n-button
                size="small"
                type="info"
                :loading="platform.qrLoading"
                @click="requestQRCode(platform)"
              >
                📱 Scan QR to Login
              </n-button>
              <n-image
                v-if="platform.qrCode"
                :src="'data:image/png;base64,' + platform.qrCode"
                width="160"
                height="160"
                style="border: 1px solid #e0e0e0; border-radius: 4px;"
              />
              <n-text v-if="platform.qrStatus" depth="3" style="font-size: 12px;">
                Status: {{ platform.qrStatus }}
              </n-text>
            </n-space>
          </template>
        </n-card>
      </n-gi>
    </n-grid>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import { useMessage } from 'naive-ui'
import { useGatewayStore } from '@/stores/gateway'
import { useConfigStore } from '@/stores/config'
import { request } from '@/api/client'

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
  qrCode: string
  qrStatus: string
  qrLoading: boolean
  // Extra fields
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

function createPlatform(id: string, label: string, description: string, tokenLabel: string, tokenPlaceholder: string, supportsQR = false): Platform {
  return reactive({
    id, label, description, enabled: false, token: '',
    tokenLabel, tokenType: 'password', tokenPlaceholder, supportsQR,
    qrCode: '', qrStatus: '', qrLoading: false,
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

async function requestQRCode(platform: Platform): Promise<void> {
  platform.qrLoading = true
  try {
    const res = await request<{ qr_code?: string; qr_data?: string; status?: string }>(`/login/qr/${platform.id}`, { method: 'POST' })
    if (res.qr_code) {
      platform.qrCode = res.qr_code
      platform.qrStatus = 'Waiting for scan...'
      pollQRStatus(platform)
    } else if (res.qr_data) {
      platform.qrStatus = 'QR data received, check terminal'
    }
  } catch (e) {
    message.error('Failed to get QR code: ' + (e instanceof Error ? e.message : 'Unknown error'))
  } finally {
    platform.qrLoading = false
  }
}

function pollQRStatus(platform: Platform): void {
  const interval = setInterval(async () => {
    try {
      const res = await request<{ statuses: Array<{ platform: string; status: string }> }>('/login/status')
      const status = res.statuses?.find(s => s.platform === platform.id)
      if (status) {
        platform.qrStatus = status.status
        if (status.status === 'confirmed' || status.status === 'logged_in') {
          platform.qrCode = ''
          clearInterval(interval)
          message.success(`${platform.label} logged in successfully`)
        } else if (status.status === 'expired') {
          platform.qrCode = ''
          clearInterval(interval)
        }
      }
    } catch {
      clearInterval(interval)
    }
  }, 3000)
}

onMounted(async () => {
  await gatewayStore.loadStatus()
  await configStore.loadConfig()
  if (configStore.config) {
    populateFromConfig(configStore.config)
  }
})
</script>
