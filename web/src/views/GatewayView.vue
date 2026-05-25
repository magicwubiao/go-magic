<template>
  <div>
    <n-space justify="space-between" style="margin-bottom: 16px;">
      <h2>{{ t('gateway.title') }}</h2>
      <n-space>
        <n-switch v-model:value="gatewayEnabled" @update:value="saveGatewayEnabled">
          <template #checked>{{ t('common.enabled') }}</template>
          <template #unchecked>{{ t('common.disabled') }}</template>
        </n-switch>
        <n-tag v-if="gatewayStore.status" :type="gatewayStore.status.running ? 'success' : 'error'" size="small">
          {{ gatewayStore.status.running ? `${t('gateway.running')} (PID: ${gatewayStore.status.pid})` : t('gateway.notRunning') }}
        </n-tag>
        <n-popconfirm @positive-click="restartGateway">
          <template #trigger>
            <n-button type="warning" :disabled="!gatewayEnabled">{{ t('gateway.restart') }}</n-button>
          </template>
          {{ t('gateway.restartConfirm') }}
        </n-popconfirm>
      </n-space>
    </n-space>

    <!-- Status -->
    <n-card size="small" style="margin-bottom: 16px;">
      <n-space>
        <n-tag :type="gatewayEnabled ? 'success' : 'default'" size="large">
          {{ gatewayEnabled ? '● ' + t('gateway.enabled') : '○ ' + t('gateway.disabled') }}
        </n-tag>
        <n-tag v-if="gatewayStore.status" :type="gatewayStore.status.running ? 'success' : 'warning'" size="large">
          {{ gatewayStore.status.running ? '● ' + t('gateway.running') : '○ ' + t('gateway.notRunning') }}
        </n-tag>
        <n-tag v-if="gatewayStore.status?.health_ok" type="success" size="large">
          ● {{ t('gateway.healthOk') }}
        </n-tag>
        <n-text depth="3">
          {{ enabledCount }} / {{ platforms.length }} {{ t('gateway.platforms') }}
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
              {{ platform.enabled ? t('common.enabled') : t('common.disabled') }}
            </n-tag>
          </n-space>
          <template #action>
            <n-space>
              <n-button size="small" @click="openEditModal(platform)">{{ t('gateway.config') }}</n-button>
              <n-button v-if="platform.supportsQR" size="small" type="info" @click="showQRInfo(platform)">
                {{ t('gateway.qrLogin') }}
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

        <template v-if="editingPlatform.id === 'dingtalk'">
          <n-form-item label="App Key">
            <n-input v-model:value="editingPlatform.appKey" placeholder="App Key" />
          </n-form-item>
        </template>

        <template v-if="editingPlatform.id === 'feishu'">
          <n-form-item label="App ID">
            <n-input v-model:value="editingPlatform.appId" placeholder="App ID" />
          </n-form-item>
        </template>

        <template v-if="editingPlatform.id === 'wechat'">
          <n-form-item label="App ID">
            <n-input v-model:value="editingPlatform.appId" placeholder="WeChat Open Platform App ID" />
          </n-form-item>
          <n-text depth="3" style="font-size: 12px;">
            微信开放平台应用 ID，用于 QR 登录
          </n-text>
        </template>

        <template v-if="editingPlatform.id === 'wechat_ilink'">
          <n-form-item label="Auto Login">
            <n-switch v-model:value="editingPlatform.autoLogin" />
          </n-form-item>
        </template>

        <!-- WhatsApp only supports Personal (QR) mode currently -->

        <template v-if="editingPlatform.id === 'qq'">
          <n-form-item label="App ID">
            <n-input v-model:value="editingPlatform.appId" placeholder="QQ Bot App ID" />
          </n-form-item>
          <n-form-item label="App Secret">
            <n-input v-model:value="editingPlatform.appSecret" type="password" show-password-on="click" placeholder="Bot Token / App Secret" />
          </n-form-item>
          <n-text depth="3" style="font-size: 12px;">
            QQ Bot 需要在 QQ 开放平台 注册后获取 App ID 和 Token
          </n-text>
        </template>
      </n-form>
      <template #action>
        <n-space justify="end">
          <n-button @click="showEditModal = false">{{ t('common.cancel') }}</n-button>
          <n-button type="primary" @click="saveEditingPlatform">{{ t('common.save') }}</n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- QR Login Modal -->
    <n-modal v-model:show="showQRModal" :title="`QR Code Login - ${qrPlatform?.label}`" preset="card" style="width: 400px;">
      <div class="qr-modal-content">
        <!-- QR Code Display -->
        <div v-if="qrStatus === 'loading'" class="qr-loading">
          <n-spin size="large" />
          <n-text depth="3">{{ t('gateway.qrGenerating') }}</n-text>
        </div>
        
        <div v-else-if="qrStatus === 'error'" class="qr-error">
          <n-result status="error" :title="t('gateway.qrError')" :description="qrMessage" />
          <n-button type="primary" @click="initQRCode">{{ t('gateway.qrRetry') }}</n-button>
        </div>
        
        <div v-else-if="qrStatus === 'expired'" class="qr-expired">
          <n-result status="warning" :title="t('gateway.qrExpired')" :description="t('gateway.qrExpiredDesc')" />
          <n-button type="primary" @click="initQRCode">{{ t('gateway.qrRefresh') }}</n-button>
        </div>
        
        <div v-else class="qr-display">
          <!-- QR Code Image -->
          <div class="qr-canvas-container">
            <img v-if="qrImageUrl" :src="qrImageUrl" class="qr-image" style="width: 200px; height: 200px;" />
            <canvas v-else ref="qrCanvas" class="qr-canvas"></canvas>
          </div>
          
          <!-- Status Message -->
          <div class="qr-status" :class="`qr-status--${qrStatus}`">
            <n-icon v-if="qrStatus === 'pending'" size="24">
              <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor"><path d="M3 11h8V3H3v8zm2-6h4v4H5V5zm8-2v8h-2V3h-4v8h6zm2 10h2v-6h-2v6zm-6-6v2h-2v-2h2zm8-8v6h2V3h-6v2h4z"/></svg>
            </n-icon>
            <n-icon v-else-if="qrStatus === 'scanning'" size="24" class="qr-icon--pulse">
              <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor"><path d="M9.5 6.5v3h-3v-3h3M11 5H5v6h6V5zm-1.5 9.5v3h-3v-3h3M11 13H5v6h6v-6zm6.5-6.5v3h-3v-3h3M19 5h-6v6h6V5zm-6 8h1.5v1.5H13V13zm1.5 1.5H16V16h-1.5v-1.5zM16 13h1.5v1.5H16V13zm-3 3h1.5v1.5H13V16zm1.5 1.5H16V19h-1.5v-1.5zM16 16h1.5v1.5H16V16zm1.5-1.5H19V16h-1.5v-1.5zm0 3H19V19h-1.5v-1.5zM19 13v1.5h-1.5V13H19z"/></svg>
            </n-icon>
            <n-icon v-else-if="qrStatus === 'confirmed'" size="24">
              <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor"><path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm-2 15l-5-5 1.41-1.41L10 14.17l7.59-7.59L19 8l-9 9z"/></svg>
            </n-icon>
            <n-text>{{ qrMessage }}</n-text>
          </div>
          
          <!-- Countdown Timer -->
          <div v-if="qrStatus === 'pending' || qrStatus === 'scanning'" class="qr-countdown">
            <n-progress
              type="circle"
              :percentage="qrCountdownPercent"
              :status="qrCountdownPercent < 20 ? 'error' : 'default'"
              :stroke-width="8"
              :size="48"
            >
              <template #default>
                <n-text depth="3" style="font-size: 12px;">{{ qrCountdown }}s</n-text>
              </template>
            </n-progress>
            <n-text depth="3" style="font-size: 12px;">{{ t('gateway.qrExpiresIn') }} {{ qrCountdown }}s</n-text>
          </div>
        </div>
      </div>
      
      <template #footer>
        <n-space justify="end">
          <n-button @click="closeQRModal">{{ t('common.cancel') }}</n-button>
          <n-button v-if="qrStatus !== 'confirmed' && qrStatus !== 'loading'" type="primary" @click="initQRCode">
            {{ t('common.refresh') }}
          </n-button>
        </n-space>
      </template>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, onUnmounted, nextTick } from 'vue'
import { useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import QRCode from 'qrcode'
import { request } from '@/api/client'
import { useGatewayStore } from '@/stores/gateway'
import { useConfigStore } from '@/stores/config'

const { t } = useI18n()

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
}

interface QRResponse {
  platform: string
  status: string // pending, scanning, confirmed, expired, error
  qr_code?: string
  message?: string
  expires_in?: number
}

const message = useMessage()
const gatewayStore = useGatewayStore()
const configStore = useConfigStore()
const gatewayEnabled = ref(false)
const showEditModal = ref(false)
const showQRModal = ref(false)
const editingPlatform = ref<Platform | null>(null)
const qrPlatform = ref<Platform | null>(null)
const qrCanvas = ref<HTMLCanvasElement | null>(null)

// QR State
const qrStatus = ref<'loading' | 'pending' | 'scanning' | 'confirmed' | 'expired' | 'error'>('loading')
const qrMessage = ref('Please wait...')
const qrCountdown = ref(60)
const qrCountdownPercent = ref(100)
const qrExpiresIn = ref(60)
const qrImageUrl = ref<string>('')
let qrPollInterval: ReturnType<typeof setInterval> | null = null
let qrCountdownInterval: ReturnType<typeof setInterval> | null = null

function createPlatform(id: string, label: string, description: string, tokenLabel: string, tokenPlaceholder: string, supportsQR = false): Platform {
  return reactive({
    id, label, description, enabled: false, token: '',
    tokenLabel, tokenType: 'password', tokenPlaceholder, supportsQR,
    corpId: '', agentId: '', secret: '',
    appKey: '', appSecret: '', appId: '',
    mode: '', modeOptions: [], autoLogin: false,
  })
}

const platforms = ref<Platform[]>([
  createPlatform('telegram', 'Telegram', 'Telegram Bot', 'Bot Token', 'Token from @BotFather'),
  createPlatform('discord', 'Discord', 'Discord Bot', 'Bot Token', 'Discord Bot Token'),
  createPlatform('slack', 'Slack', 'Slack Bot', 'Bot Token', 'Slack Bot Token'),
  createPlatform('wechat', 'WeChat', 'WeChat Official Account', 'Token', 'WeChat Token', true),
  createPlatform('wechat_ilink', 'WeChat iLink', 'WeChat Personal', 'Token', 'iLink Token', true),
  createPlatform('wecom', 'WeCom', 'Enterprise WeChat', 'Token', 'WeCom Token', true),
  createPlatform('qq', 'QQ', 'QQ Guild Bot (频道机器人)', 'App ID', 'QQ Bot App ID'),
  createPlatform('dingtalk', 'DingTalk', 'DingTalk Bot', 'Token', 'DingTalk Token', true),
  createPlatform('feishu', 'Feishu/Lark', 'Feishu/Lark Bot', 'Token', 'Feishu Token', true),
  createPlatform('whatsapp', 'WhatsApp', 'WhatsApp Bot', 'Token', 'WhatsApp Token', true),
  createPlatform('line', 'LINE', 'LINE Bot', 'Channel Token', 'LINE Channel Token'),
  createPlatform('matrix', 'Matrix', 'Matrix Protocol', 'Token', 'Matrix Token'),
])

// Set mode options for platforms that need them
const wechatPlatform = platforms.value.find(p => p.id === 'wechat')
if (wechatPlatform) wechatPlatform.modeOptions = [{ label: 'QR Code Login', value: 'qr' }, { label: 'Webhook Callback', value: 'callback' }]

// wechat_ilink only supports QR login, no mode selection needed

// Mode options removed - platforms now use their default/login methods:
// - wecom: QR login (app mode not implemented in QR login API)
// - whatsapp: Personal QR mode only (Business API not implemented)

const enabledCount = computed(() => (platforms.value || []).filter(p => p.enabled).length)

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
  return payload
}

async function savePlatform(platform: Platform): Promise<void> {
  try {
    const platformsPayload: any = {}
    platformsPayload[platform.id] = buildPlatformPayload(platform)
    await configStore.updateConfig({ gateway: { enabled: gatewayEnabled.value, platforms: platformsPayload } })
  } catch (e) {
    message.error(t('gateway.saveFailed') + ': ' + (e instanceof Error ? e.message : 'Unknown error'))
  }
}

async function saveGatewayEnabled(): Promise<void> {
  try {
    await configStore.updateConfig({ gateway: { enabled: gatewayEnabled.value } })
    message.success(gatewayEnabled.value ? t('gateway.enabled') : t('gateway.disabled'))
  } catch (e) {
    message.error(t('gateway.updateFailed') + ': ' + (e instanceof Error ? e.message : 'Unknown error'))
  }
}

async function restartGateway(): Promise<void> {
  try {
    await gatewayStore.restart()
    message.success(t('gateway.restarting'))
    
    // Force immediate status refresh
    await gatewayStore.loadStatus()
    
    // Poll status more frequently until gateway is running again or timeout
    let pollCount = 0
    const maxPolls = 30
    const pollInterval = setInterval(async () => {
      pollCount++
      await gatewayStore.loadStatus()
      // Stop polling if running or timeout
      if (gatewayStore.status?.running || pollCount >= maxPolls) {
        clearInterval(pollInterval)
        if (gatewayStore.status?.running) {
          message.success(t('gateway.running'))
        }
      }
    }, 1000)
  } catch (e) {
    message.error(t('gateway.restartFailed') + ': ' + (e instanceof Error ? e.message : 'Unknown error'))
  }
}

function openEditModal(platform: Platform): void {
  editingPlatform.value = platform
  showEditModal.value = true
}

function showQRInfo(platform: Platform): void {
  qrPlatform.value = platform
  showQRModal.value = true
  nextTick(() => {
    initQRCode()
  })
}

async function initQRCode(): Promise<void> {
  if (!qrPlatform.value) return
  
  qrStatus.value = 'loading'
  qrMessage.value = t('gateway.qrGenerating')
  
  try {
    const data: QRResponse = await request(`/gateway/qr?platform=${qrPlatform.value.id}`)
    
    qrStatus.value = data.status as typeof qrStatus.value
    qrMessage.value = data.message || getDefaultMessage(data.status)
    
    if (data.status === 'pending' || data.status === 'scanning') {
      qrExpiresIn.value = data.expires_in || 60
      qrCountdown.value = qrExpiresIn.value
      qrCountdownPercent.value = 100
      
      // Set QR code image from backend (base64 data URL)
      if (data.qr_code) {
        qrImageUrl.value = data.qr_code
      }
      
      // Start polling
      startPolling()
      startCountdown()
    } else if (data.status === 'confirmed') {
      message.success(t('gateway.qrLoginSuccess'))
      setTimeout(() => {
        closeQRModal()
      }, 1500)
    }
  } catch (e) {
    qrStatus.value = 'error'
    qrMessage.value = t('gateway.qrScanError')
    console.error('QR code error:', e)
  }
}

async function generateQRCodeImage(data: string): Promise<void> {
  if (!qrCanvas.value) return
  
  try {
    await QRCode.toCanvas(qrCanvas.value, data, {
      width: 200,
      margin: 2,
      color: {
        dark: '#000000',
        light: '#ffffff'
      }
    })
  } catch (e) {
    console.error('Failed to generate QR code:', e)
  }
}

function startPolling(): void {
  stopPolling()
  qrPollInterval = setInterval(async () => {
    if (!qrPlatform.value || !showQRModal.value) {
      stopPolling()
      return
    }
    
    try {
      const data: QRResponse = await request(`/gateway/qr/status?platform=${qrPlatform.value.id}`)
      
      if (data.status !== qrStatus.value) {
        qrStatus.value = data.status as typeof qrStatus.value
        qrMessage.value = data.message || getDefaultMessage(data.status)
        
        if (data.status === 'confirmed') {
          message.success(t('gateway.qrLoginSuccess'))
          stopPolling()
          stopCountdown()
          setTimeout(() => {
            closeQRModal()
          }, 1500)
        } else if (data.status === 'expired' || data.status === 'error') {
          stopPolling()
          stopCountdown()
        }
      }
      
      if (data.expires_in !== undefined) {
        qrExpiresIn.value = data.expires_in
      }
    } catch (e) {
      console.error('Poll error:', e)
    }
  }, 2000)
}

function startCountdown(): void {
  stopCountdown()
  qrCountdownInterval = setInterval(() => {
    if (qrCountdown.value > 0) {
      qrCountdown.value--
      qrCountdownPercent.value = Math.round((qrCountdown.value / qrExpiresIn.value) * 100)
    } else {
      qrStatus.value = 'expired'
      qrMessage.value = t('gateway.qrScanExpired')
      stopCountdown()
      stopPolling()
    }
  }, 1000)
}

function stopPolling(): void {
  if (qrPollInterval) {
    clearInterval(qrPollInterval)
    qrPollInterval = null
  }
}

function stopCountdown(): void {
  if (qrCountdownInterval) {
    clearInterval(qrCountdownInterval)
    qrCountdownInterval = null
  }
}

function closeQRModal(): void {
  stopPolling()
  stopCountdown()
  showQRModal.value = false
  qrPlatform.value = null
  qrStatus.value = 'loading'
  qrImageUrl.value = ''
}

function getDefaultMessage(status?: string): string {
  switch (status) {
    case 'pending': return t('gateway.qrScanPending')
    case 'scanning': return t('gateway.qrScanScanning')
    case 'confirmed': return t('gateway.qrScanConfirmed')
    case 'expired': return t('gateway.qrScanExpired')
    case 'error': return t('gateway.qrScanError')
    default: return t('gateway.qrWait')
  }
}

async function saveEditingPlatform(): Promise<void> {
  if (!editingPlatform.value) return
  await savePlatform(editingPlatform.value)
  showEditModal.value = false
  message.success(t('gateway.platformSaved'))
}

onMounted(async () => {
  await gatewayStore.loadStatus()
  await configStore.loadConfig()
  if (configStore.config) {
    populateFromConfig(configStore.config)
  }
})

onUnmounted(() => {
  stopPolling()
  stopCountdown()
})
</script>

<style scoped>
.chat-container { display: flex; height: 100%; }
.session-sidebar { width: 260px; border-right: 1px solid #e0e0e0; display: flex; flex-direction: column; }
.sidebar-header { padding: 12px; border-bottom: 1px solid #e0e0e0; }
.session-list { flex: 1; overflow-y: auto; }
.profile-group-header { padding: 8px 12px; font-size: 12px; color: #999; background: #f5f5f5; }
.session-item { padding: 8px 12px; cursor: pointer; border-bottom: 1px solid #f0f0f0; position: relative; }
.session-item:hover { background: #f5f5f5; }
.session-item.active { background: #e6f7ff; }
.session-title { font-size: 14px; margin-bottom: 4px; }
.session-meta { font-size: 12px; color: #999; }
.session-delete { position: absolute; right: 8px; top: 50%; transform: translateY(-50%); opacity: 0; }
.session-item:hover .session-delete { opacity: 1; }
.chat-main { flex: 1; display: flex; flex-direction: column; }
.messages { flex: 1; overflow-y: auto; padding: 16px; }
.message { margin-bottom: 16px; }
.message-bubble { max-width: 80%; padding: 12px 16px; border-radius: 8px; word-break: break-word; }
.message-user .message-bubble { background: #1890ff; color: white; margin-left: auto; }
.message-assistant .message-bubble { background: #f5f5f5; }
.message-time { font-size: 11px; color: #999; margin-top: 4px; }
.content :deep(pre) { background: #f0f0f0; padding: 12px; border-radius: 8px; overflow-x: auto; }
.content :deep(code) { font-family: 'Fira Code', monospace; font-size: 14px; color: #333; }
.content :deep(p) { margin: 0 0 8px 0; }
.content :deep(p:last-child) { margin-bottom: 0; }
.content :deep(ul), .content :deep(ol) { margin: 8px 0; padding-left: 24px; }
.content :deep(li) { margin: 4px 0; }
.input-area { display: flex; gap: 12px; padding: 16px; border-top: 1px solid #e0e0e0; }
.input-area .n-input { flex: 1; }

/* QR Modal Styles */
.qr-modal-content { display: flex; flex-direction: column; align-items: center; padding: 24px; min-height: 300px; }
.qr-loading, .qr-error, .qr-expired { display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 16px; height: 250px; }
.qr-display { display: flex; flex-direction: column; align-items: center; gap: 20px; }
.qr-canvas-container { padding: 16px; background: white; border-radius: 12px; box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1); }
.qr-canvas { display: block; }
.qr-status { display: flex; align-items: center; gap: 8px; padding: 12px 20px; border-radius: 8px; font-size: 14px; }
.qr-status--pending { background: #e6f7ff; color: #1890ff; }
.qr-status--scanning { background: #fff7e6; color: #fa8c16; }
.qr-status--confirmed { background: #f6ffed; color: #52c41a; }
.qr-status--expired, .qr-status--error { background: #fff1f0; color: #ff4d4f; }
.qr-icon--pulse { animation: pulse 1.5s ease-in-out infinite; }
@keyframes pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.5; } }
.qr-countdown { display: flex; flex-direction: column; align-items: center; gap: 8px; }
</style>
