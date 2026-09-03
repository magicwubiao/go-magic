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
        <n-button
          v-if="!gatewayStore.status?.running"
          type="success"
          size="small"
          :disabled="!gatewayEnabled || actionLoading"
          @click="startGateway"
        >
          {{ actionLoading ? t('gateway.starting') : t('gateway.start') }}
        </n-button>
        <n-button
          v-else
          type="warning"
          size="small"
          :disabled="actionLoading"
          @click="stopGateway"
        >
          {{ actionLoading ? t('gateway.stopping') : t('gateway.stop') }}
        </n-button>
        <n-button size="small" secondary :loading="refreshing" @click="refreshPage">
          <template #icon><n-icon><RefreshOutline /></n-icon></template>
          {{ t('common.refresh') }}
        </n-button>
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
            <n-space size="small">
              <n-tag :type="platform.enabled ? 'success' : 'default'" size="small">
                {{ platform.enabled ? t('common.enabled') : t('common.disabled') }}
              </n-tag>
              <n-tag
                v-if="gatewayRunning"
                :type="isConnected(platform) ? 'success' : 'warning'"
                size="small"
              >
                {{ isConnected(platform) ? t('gateway.connected') : t('gateway.notConnected') }}
              </n-tag>
            </n-space>
          </n-space>
          <template #action>
            <n-space>
              <n-button size="small" @click="openEditModal(platform)">{{ t('gateway.config') }}</n-button>
              <n-button v-if="platform.supportsQR" size="small" type="info" @click="showQRInfo(platform)">
                {{ t('gateway.qrLogin') }}
              </n-button>
              <!-- 连接成功后提供「取消连接」：断开运行中连接，配置与凭据保留 -->
              <n-popconfirm
                v-if="gatewayRunning && isConnected(platform)"
                :positive-text="t('common.confirm')"
                :negative-text="t('common.cancel')"
                @positive-click="runPlatformAction(platform, 'disconnect')"
              >
                <template #trigger>
                  <n-button size="small" type="error" :loading="!!platformBusy[platform.id]">
                    {{ t('gateway.disconnect') }}
                  </n-button>
                </template>
                {{ t('gateway.disconnectConfirm', { name: platform.label }) }}
              </n-popconfirm>
              <!-- 已启用但未连接：仅对可安全运行时重连的平台显示 -->
              <n-button
                v-if="gatewayRunning && platform.enabled && !isConnected(platform) && reconnectablePlatforms.has(platform.id)"
                size="small"
                type="primary"
                secondary
                :loading="!!platformBusy[platform.id]"
                @click="runPlatformAction(platform, 'connect')"
              >
                {{ t('gateway.reconnect') }}
              </n-button>
            </n-space>
          </template>
        </n-card>
      </n-gi>
    </n-grid>

    <!-- Edit Platform Modal -->
    <n-modal v-model:show="showEditModal" :title="editingPlatform?.label" preset="dialog" class="modal-responsive" style="width: 500px; max-width: 96vw;">
      <n-form v-if="editingPlatform" label-placement="left" label-width="120" size="small">
        <!-- 通用 Token 只适用于 bot-token 类平台（telegram/discord/slack/line/matrix/wechat_ilink）。
             QQ / WeCom / Teams 为 App 凭据专用表单；dingtalk / feishu 为 App 凭据模式，后端用
             appKey/appSecret 自动换取并缓存 token；googlechat / email / sms 也是专用字段表单，
             这些平台一律不走通用 Token（填了不生效）。 -->
        <n-form-item v-if="!['qq', 'wecom', 'dingtalk', 'feishu', 'teams', 'googlechat', 'email', 'sms'].includes(editingPlatform.id)" label="Token">
          <n-input
            v-model:value="editingPlatform.token"
            type="password"
            show-password-on="click"
            :placeholder="editingPlatform.tokenPlaceholder"
          />
        </n-form-item>

        <!-- WeCom：仅支持官方智能机器人（扫码创建，bot_id/secret） -->
        <template v-if="editingPlatform.id === 'wecom'">
          <n-form-item :label="t('gateway.wecomBotId')">
            <n-input v-model:value="editingPlatform.botId" placeholder="Bot ID" />
          </n-form-item>
          <n-form-item :label="t('gateway.wecomBotSecret')">
            <n-input v-model:value="editingPlatform.secret" type="password" show-password-on="click" placeholder="Bot Secret" />
          </n-form-item>
          <n-alert type="info" :show-icon="false" style="font-size: 12px; margin-top: 4px;">
            {{ t('gateway.wecomAibotHint') }}
          </n-alert>
        </template>

        <template v-if="editingPlatform.id === 'dingtalk'">
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
          <n-form-item label="App Secret">
            <n-input v-model:value="editingPlatform.appSecret" type="password" show-password-on="click" placeholder="App Secret" />
          </n-form-item>
        </template>

        <template v-if="editingPlatform.id === 'wechat_ilink'">
          <n-alert type="info" :show-icon="false" style="font-size: 12px; margin-top: 4px;">
            {{ t('gateway.wechatIlinkHint') }}
          </n-alert>
        </template>

        <template v-if="editingPlatform.id === 'qq'">
          <n-form-item label="App ID">
            <n-input v-model:value="editingPlatform.appId" placeholder="QQ Bot App ID" />
          </n-form-item>
          <n-form-item label="App Secret">
            <n-input v-model:value="editingPlatform.appSecret" type="password" show-password-on="click" placeholder="Bot Token / App Secret" />
          </n-form-item>
          <n-alert type="info" :show-icon="false" style="font-size: 12px; margin-top: 4px;">
            {{ t('gateway.qqBotHint') }}
            <a href="https://q.qq.com/qqbot/openclaw/login.html" target="_blank" rel="noopener" style="text-decoration: underline;">
              {{ t('gateway.qqBotCreate') }} ↗
            </a>
          </n-alert>
        </template>

        <!-- Teams（Bot Framework）：Microsoft App ID + App Password -->
        <template v-if="editingPlatform.id === 'teams'">
          <n-form-item label="App ID">
            <n-input v-model:value="editingPlatform.appId" placeholder="Microsoft App ID" />
          </n-form-item>
          <n-form-item label="App Password">
            <n-input v-model:value="editingPlatform.appSecret" type="password" show-password-on="click" placeholder="Microsoft App Password" />
          </n-form-item>
          <n-alert type="info" :show-icon="false" style="font-size: 12px; margin-top: 4px;">
            {{ t('gateway.teamsHint') }}
          </n-alert>
        </template>

        <!-- Google Chat：Incoming Webhook + Events API -->
        <template v-if="editingPlatform.id === 'googlechat'">
          <n-form-item label="Webhook URL">
            <n-input v-model:value="editingPlatform.webhookUrl" placeholder="https://chat.googleapis.com/v1/spaces/..." />
          </n-form-item>
          <n-form-item label="Events Token">
            <n-input v-model:value="editingPlatform.eventsToken" type="password" show-password-on="click" placeholder="Optional shared secret (delivery URL: .../gchat/events?token=)" />
          </n-form-item>
          <n-alert type="info" :show-icon="false" style="font-size: 12px; margin-top: 4px;">
            {{ t('gateway.googlechatHint') }}
          </n-alert>
        </template>

        <!-- Email：IMAP 收信 + SMTP 发信（轮询，无需公网回调端口） -->
        <template v-if="editingPlatform.id === 'email'">
          <n-form-item label="Email">
            <n-input v-model:value="editingPlatform.email" placeholder="bot@example.com" />
          </n-form-item>
          <n-form-item label="IMAP Host">
            <n-input v-model:value="editingPlatform.imapHost" placeholder="imap.example.com" />
          </n-form-item>
          <n-form-item label="IMAP Port">
            <n-input v-model:value="editingPlatform.imapPort" placeholder="993 (implicit TLS)" />
          </n-form-item>
          <n-form-item label="IMAP User">
            <n-input v-model:value="editingPlatform.imapUser" placeholder="defaults to Email address" />
          </n-form-item>
          <n-form-item label="IMAP Password">
            <n-input v-model:value="editingPlatform.imapPass" type="password" show-password-on="click" placeholder="App password" />
          </n-form-item>
          <n-form-item label="SMTP Host">
            <n-input v-model:value="editingPlatform.smtpHost" placeholder="smtp.example.com (defaults to IMAP Host)" />
          </n-form-item>
          <n-form-item label="SMTP Port">
            <n-input v-model:value="editingPlatform.smtpPort" placeholder="465 (implicit TLS)" />
          </n-form-item>
          <n-form-item label="SMTP User">
            <n-input v-model:value="editingPlatform.smtpUser" placeholder="defaults to Email address" />
          </n-form-item>
          <n-form-item label="SMTP Password">
            <n-input v-model:value="editingPlatform.smtpPass" type="password" show-password-on="click" placeholder="defaults to IMAP Password" />
          </n-form-item>
          <n-form-item label="Poll Interval (s)">
            <n-input v-model:value="editingPlatform.pollInterval" placeholder="30" />
          </n-form-item>
          <n-alert type="info" :show-icon="false" style="font-size: 12px; margin-top: 4px;">
            {{ t('gateway.emailHint') }}
          </n-alert>
        </template>

        <!-- SMS：Twilio（Account SID + Auth Token + 发信号码） -->
        <template v-if="editingPlatform.id === 'sms'">
          <n-form-item label="Account SID">
            <n-input v-model:value="editingPlatform.accountSid" placeholder="Twilio Account SID" />
          </n-form-item>
          <n-form-item label="Auth Token">
            <n-input v-model:value="editingPlatform.authToken" type="password" show-password-on="click" placeholder="Twilio Auth Token" />
          </n-form-item>
          <n-form-item label="From Number">
            <n-input v-model:value="editingPlatform.fromNumber" placeholder="+1xxxxxxxxxx (Twilio number)" />
          </n-form-item>
          <n-alert type="info" :show-icon="false" style="font-size: 12px; margin-top: 4px;">
            {{ t('gateway.smsHint') }}
          </n-alert>
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
    <n-modal v-model:show="showQRModal" :title="`QR Code Login - ${qrPlatform?.label}`" preset="card" class="modal-responsive" style="width: 400px; max-width: 96vw;">
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
            <img v-if="qrImageUrl" :src="qrImageUrl" class="qr-image" style="width: 200px;" />
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
import { request } from '@/api/client'
import { RefreshOutline } from '@vicons/ionicons5'
import { useGatewayStore } from '@/stores/gateway'
import { useConfigStore } from '@/stores/config'
import type { PlatformStatus } from '@/api/gateway'
import { getPlatforms, platformAction } from '@/api/gateway'

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
  wsUrl: string
  botId: string
  mode: string
  modeOptions: { label: string; value: string }[]
  // googlechat
  webhookUrl: string
  eventsToken: string
  // email (IMAP + SMTP)
  email: string
  imapHost: string
  imapPort: string
  imapUser: string
  imapPass: string
  smtpHost: string
  smtpPort: string
  smtpUser: string
  smtpPass: string
  pollInterval: string
  // sms (Twilio)
  accountSid: string
  authToken: string
  fromNumber: string
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
const actionLoading = ref(false)
const refreshing = ref(false)
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
let statusTimer: ReturnType<typeof setInterval> | null = null

function createPlatform(id: string, label: string, description: string, tokenLabel: string, tokenPlaceholder: string, supportsQR = false): Platform {
  return reactive({
    id, label, description, enabled: false, token: '',
    tokenLabel, tokenType: 'password', tokenPlaceholder, supportsQR,
    corpId: '', agentId: '', secret: '',
    appKey: '', appSecret: '', appId: '', wsUrl: '', botId: '',
    mode: '', modeOptions: [],
    webhookUrl: '', eventsToken: '',
    email: '', imapHost: '', imapPort: '', imapUser: '', imapPass: '',
    smtpHost: '', smtpPort: '', smtpUser: '', smtpPass: '', pollInterval: '',
    accountSid: '', authToken: '', fromNumber: '',
  })
}

const platforms = ref<Platform[]>([
  createPlatform('telegram', 'Telegram', 'Telegram Bot', 'Bot Token', 'Token from @BotFather'),
  createPlatform('discord', 'Discord', 'Discord Bot', 'Bot Token', 'Discord Bot Token'),
  createPlatform('slack', 'Slack', 'Slack Bot', 'Bot Token', 'Slack Bot Token'),
  createPlatform('wechat_ilink', 'WeChat iLink', 'WeChat Personal', 'Token', 'iLink Token', true),
  createPlatform('wecom', 'WeCom', t('gateway.wecomAibotDesc'), 'Token', 'WeCom Token', true),
  createPlatform('qq', 'QQ', t('gateway.qqGuildBot'), 'App ID', t('gateway.qqBotAppId')),
  createPlatform('dingtalk', 'DingTalk', 'DingTalk Bot', 'Token', 'DingTalk Token'),
  createPlatform('feishu', 'Feishu/Lark', 'Feishu/Lark Bot', 'Token', 'Feishu Token'),
  createPlatform('line', 'LINE', 'LINE Bot', 'Channel Token', 'LINE Channel Token'),
  createPlatform('matrix', 'Matrix', 'Matrix Protocol', 'Token', 'Matrix Token'),
  createPlatform('teams', 'Microsoft Teams', t('gateway.teamsDesc'), 'App ID', 'Microsoft App ID'),
  createPlatform('googlechat', 'Google Chat', t('gateway.googlechatDesc'), 'Webhook URL', 'https://chat.googleapis.com/v1/spaces/...'),
  createPlatform('email', 'Email', t('gateway.emailDesc'), 'Email', 'bot@example.com'),
  createPlatform('sms', 'SMS', t('gateway.smsDesc'), 'Account SID', 'Twilio Account SID'),
])

// wechat_ilink / wecom 支持扫码登录（wecom 为官方智能机器人扫码创建；whatsapp 已于 2026-09 移除）。
// qq 仅官方机器人（AppID/AppSecret）；个人 QQ 扫码 OneBot 模式已移除。
// dingtalk / feishu 是企业自建应用凭据模式、官方无个人扫码通道；
// teams / googlechat / email / sms 均为凭据/Webhook 直配模式，故这些平台不开扫码按钮。

const enabledCount = computed(() => (platforms.value || []).filter(p => p.enabled).length)

// Real-time platform connection status from gateway health endpoint
const connectedPlatforms = ref<PlatformStatus[]>([])

async function refreshConnected(): Promise<void> {
  try {
    connectedPlatforms.value = await getPlatforms()
  } catch {
    // gateway not running - stale data is fine, non-critical
  }
}

const gatewayRunning = computed(() => !!gatewayStore.status?.running)

// name -> connected lookup from the gateway health detail
const connectedMap = computed<Record<string, boolean>>(() => {
  const m: Record<string, boolean> = {}
  for (const p of connectedPlatforms.value) m[p.name] = p.connected
  return m
})

function isConnected(platform: Platform): boolean {
  return connectedMap.value[platform.id] === true
}

// Platforms whose runtime Connect is structurally safe to re-run after a
// disconnect (each Connect builds a fresh connection / session). Webhook-style
// platforms (dingtalk/feishu/slack/line/matrix + teams/googlechat/email/sms)
// restore by restarting the gateway instead.
const reconnectablePlatforms = new Set(['telegram', 'discord', 'qq', 'wecom'])

// Per-platform busy state for connect/disconnect actions
const platformBusy = ref<Record<string, boolean>>({})

async function runPlatformAction(platform: Platform, action: 'connect' | 'disconnect'): Promise<void> {
  platformBusy.value = { ...platformBusy.value, [platform.id]: true }
  try {
    await platformAction(platform.id, action)
    if (action === 'disconnect') {
      message.success(t('gateway.disconnectSuccess'))
    } else {
      message.success(t('gateway.connectSuccess'))
    }
    // Poll briefly: WS-style platforms (qq/wecom) dial in the background, so
    // the connection light may take a moment to reflect the new state.
    const target = action === 'disconnect' ? false : true
    for (let i = 0; i < 8; i++) {
      await refreshConnected()
      if (isConnected(platform) === target) return
      await new Promise((res) => setTimeout(res, 700))
    }
  } catch (e) {
    const prefix = action === 'disconnect' ? t('gateway.disconnectFailed') : t('gateway.connectFailed')
    message.error(prefix + ': ' + (e instanceof Error ? e.message : 'Unknown error'))
  } finally {
    platformBusy.value = { ...platformBusy.value, [platform.id]: false }
  }
}

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
    platform.wsUrl = pc.ws_url || ''
    platform.botId = pc.bot_id || ''
    platform.mode = pc.mode || ''
    platform.webhookUrl = pc.webhook_url || ''
    platform.eventsToken = pc.events_token || ''
    platform.email = pc.email || ''
    platform.imapHost = pc.imap_host || ''
    platform.imapPort = pc.imap_port != null ? String(pc.imap_port) : ''
    platform.imapUser = pc.imap_user || ''
    platform.imapPass = pc.imap_pass || ''
    platform.smtpHost = pc.smtp_host || ''
    platform.smtpPort = pc.smtp_port != null ? String(pc.smtp_port) : ''
    platform.smtpUser = pc.smtp_user || ''
    platform.smtpPass = pc.smtp_pass || ''
    platform.pollInterval = pc.poll_interval != null ? String(pc.poll_interval) : ''
    platform.accountSid = pc.account_sid || ''
    platform.authToken = pc.auth_token || ''
    platform.fromNumber = pc.from || ''

    if (platform.id === 'qq') {
      // 仅官方机器人：强制清掉历史 onebot/onebot_v11 mode（OneBot 接入已移除）
      platform.mode = ''
      platform.description = t('gateway.qqGuildBot')
    }

    if (platform.id === 'wecom') {
      // 企业微信仅保留官方智能机器人：历史 mode='app'/'' 一律按 aibot 处理
      platform.mode = 'aibot'
      platform.description = t('gateway.wecomAibotDesc')
      // 自建应用已移除：清掉 corp_id/agent_id，保存时同步抹除遗留配置
      platform.corpId = ''
      platform.agentId = ''
    }
  }
}

function buildPlatformPayload(platform: Platform): Record<string, any> {
  const payload: any = {
    enabled: platform.enabled,
  }
  // token 仅在非空时写出，避免空值覆盖已有配置
  if (platform.token) payload.token = platform.token
  if (platform.corpId) payload.corp_id = platform.corpId
  if (platform.agentId) payload.agent_id = platform.agentId
  if (platform.secret) payload.secret = platform.secret
  if (platform.appKey) payload.app_key = platform.appKey
  if (platform.appSecret) payload.app_secret = platform.appSecret
  if (platform.appId) payload.app_id = platform.appId
  if (platform.wsUrl) payload.ws_url = platform.wsUrl
  if (platform.botId) payload.bot_id = platform.botId
  if (platform.webhookUrl) payload.webhook_url = platform.webhookUrl
  if (platform.eventsToken) payload.events_token = platform.eventsToken
  if (platform.email) payload.email = platform.email
  if (platform.imapHost) payload.imap_host = platform.imapHost
  if (platform.imapPort) payload.imap_port = platform.imapPort
  if (platform.imapUser) payload.imap_user = platform.imapUser
  if (platform.imapPass) payload.imap_pass = platform.imapPass
  if (platform.smtpHost) payload.smtp_host = platform.smtpHost
  if (platform.smtpPort) payload.smtp_port = platform.smtpPort
  if (platform.smtpUser) payload.smtp_user = platform.smtpUser
  if (platform.smtpPass) payload.smtp_pass = platform.smtpPass
  if (platform.pollInterval) payload.poll_interval = platform.pollInterval
  if (platform.accountSid) payload.account_sid = platform.accountSid
  if (platform.authToken) payload.auth_token = platform.authToken
  if (platform.fromNumber) payload.from = platform.fromNumber

  if (platform.id === 'qq') {
    // 仅官方机器人：OneBot 模式已移除，保存时用空串清除历史 mode=onebot/onebot_v11
    payload.mode = ''
  } else if (platform.id === 'wecom') {
    // 仅官方智能机器人：mode 固定 aibot；空串清除历史 corp_id/agent_id（自建应用已移除）
    payload.mode = 'aibot'
    payload.bot_id = platform.botId || ''
    payload.corp_id = ''
    payload.agent_id = ''
  } else if (platform.mode) {
    payload.mode = platform.mode
  }
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

// 手动刷新：重读 config.json（扫码/CLI 在后台写入的改动在此生效）+
// 刷新网关运行状态与各平台实时连接状态。
async function refreshPage(): Promise<void> {
  refreshing.value = true
  try {
    await gatewayStore.loadStatus()
    await configStore.loadConfig()
    if (configStore.config) {
      populateFromConfig(configStore.config)
    }
    await refreshConnected()
  } catch (e) {
    message.error(t('gateway.refreshFailed') + ': ' + (e instanceof Error ? e.message : 'Unknown error'))
  } finally {
    refreshing.value = false
  }
}

async function startGateway(): Promise<void> {
  try {
    actionLoading.value = true
    await gatewayStore.start()
    message.success(t('gateway.starting'))
    
    // Poll status until gateway is running or timeout
    let pollCount = 0
    const maxPolls = 30
    const pollInterval = setInterval(async () => {
      pollCount++
      await gatewayStore.loadStatus()
      if (gatewayStore.status?.running || pollCount >= maxPolls) {
        clearInterval(pollInterval)
        actionLoading.value = false
        refreshConnected()
        if (gatewayStore.status?.running) {
          message.success(t('gateway.running'))
        }
      }
    }, 1000)
  } catch (e) {
    actionLoading.value = false
    message.error(t('gateway.startFailed') + ': ' + (e instanceof Error ? e.message : 'Unknown error'))
  }
}

async function stopGateway(): Promise<void> {
  try {
    actionLoading.value = true
    await gatewayStore.stop()
    message.success(t('gateway.stopping'))
    
    // Poll status until gateway is stopped or timeout
    let pollCount = 0
    const maxPolls = 15
    const pollInterval = setInterval(async () => {
      pollCount++
      await gatewayStore.loadStatus()
      if (!gatewayStore.status?.running || pollCount >= maxPolls) {
        clearInterval(pollInterval)
        actionLoading.value = false
        refreshConnected()
        if (!gatewayStore.status?.running) {
          message.success(t('gateway.stopped'))
        }
      }
    }, 1000)
  } catch (e) {
    actionLoading.value = false
    message.error(t('gateway.stopFailed') + ': ' + (e instanceof Error ? e.message : 'Unknown error'))
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

      // Refresh the QR code image whenever the server returns a new one
      // (e.g. WeChat iLink / WeCom rotate the QR every ~60s). Without this,
      // users would keep scanning the very first QR and the phone would
      // reject it as expired.
      if (data.qr_code && data.qr_code !== qrImageUrl.value) {
        qrImageUrl.value = data.qr_code
        // Reset countdown so the user gets the full window for the new QR
        qrCountdown.value = data.expires_in || qrExpiresIn.value || 60
        qrCountdownPercent.value = 100
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
  if (qrCanvas.value) {
    qrCanvas.value.width = 0 // clear canvas when modal closes
  }
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
  const p = editingPlatform.value
  if (!p) return
  // 官方智能机器人模式 bot_id 必填（二维码确认后会自动写入，也可手动粘贴）
  if (p.id === 'wecom' && !p.botId) {
    message.warning(t('gateway.wecomBotIdRequired'))
    return
  }
  await savePlatform(p)
  showEditModal.value = false
  message.success(t('gateway.platformSaved'))
}

onMounted(async () => {
  await gatewayStore.loadStatus()
  await refreshConnected()
  await configStore.loadConfig()
  if (configStore.config) {
    populateFromConfig(configStore.config)
  }
  // Keep the per-platform connection state fresh while the gateway runs so
  // the disconnect/reconnect buttons reflect reality (e.g. an external bot
  // going offline is picked up without a manual refresh).
  statusTimer = setInterval(() => {
    if (gatewayStore.status?.running) {
      refreshConnected()
    }
  }, 5000)
})

onUnmounted(() => {
  stopPolling()
  stopCountdown()
  if (statusTimer) {
    clearInterval(statusTimer)
    statusTimer = null
  }
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

/* 移动端:会话侧栏改为顶部横向抽屉 */
@media (max-width: 768px) {
  .chat-container { flex-direction: column; }
  .session-sidebar {
    width: 100%;
    max-height: 200px;
    border-right: none;
    border-bottom: 1px solid #e0e0e0;
    flex-shrink: 0;
  }
  .session-list { max-height: 150px; }
  .chat-main { min-height: 0; flex: 1; }
}
</style>