<template>
  <div class="channels-view">
    <n-grid :cols="4" :x-gap="16" :y-gap="16">
      <n-gi :span="1">
        <!-- Channel List -->
        <n-card title="Channels" class="channel-list-card">
          <template #header-extra>
            <n-button size="small" @click="refreshChannels">
              <template #icon>
                <n-icon :component="Refresh" />
              </template>
            </n-button>
          </template>

          <n-list hoverable clickable @click="selectChannel(channel)" v-if="channels.length > 0">
            <n-list-item
              v-for="channel in channels"
              :key="channel.id"
              :class="{ active: selectedChannel?.id === channel.id }"
            >
              <n-thing>
                <template #avatar>
                  <n-badge :dot="channel.enabled" :type="channel.enabled ? 'success' : 'default'">
                    <n-avatar round>
                      <n-icon :component="getChannelIcon(channel.type)" />
                    </n-avatar>
                  </n-badge>
                </template>
                <template #header>
                  {{ channel.name }}
                </template>
                <template #description>
                  <n-tag :type="channel.configured ? 'success' : 'warning'" size="tiny">
                    {{ channel.configured ? 'Configured' : 'Not Configured' }}
                  </n-tag>
                </template>
              </n-thing>
            </n-list-item>
          </n-list>
          <n-empty v-else description="No channels available" />
        </n-card>
      </n-gi>

      <n-gi :span="3">
        <!-- Channel Configuration -->
        <n-card :title="selectedChannel?.name || 'Channel Configuration'" v-if="selectedChannel">
          <template #header-extra>
            <n-space>
              <n-switch v-model:value="selectedChannel.enabled" @update:value="toggleChannel">
                <template #checked>Enabled</template>
                <template #unchecked>Disabled</template>
              </n-switch>
            </n-space>
          </template>

          <!-- Telegram Config -->
          <n-form
            v-if="selectedChannel.type === 'telegram'"
            :model="telegramConfig"
            label-placement="top"
          >
            <n-form-item label="Bot Token">
              <n-input
                v-model:value="telegramConfig.botToken"
                type="password"
                placeholder="Your Telegram bot token"
                show-password-on="click"
              />
            </n-form-item>
            <n-form-item label="Allowed Users">
              <n-dynamic-input
                v-model:value="telegramConfig.allowedUsers"
                placeholder="User ID"
              />
            </n-form-item>
            <n-form-item label="Mention Control">
              <n-space>
                <n-switch v-model:value="telegramConfig.requireMention" />
                <n-text>Require mention to respond</n-text>
              </n-space>
            </n-form-item>
            <n-form-item label="Auto Reactions">
              <n-space>
                <n-switch v-model:value="telegramConfig.autoReactions" />
                <n-text>Auto react to messages</n-text>
              </n-space>
            </n-form-item>
            <n-form-item label="Free Response">
              <n-space>
                <n-switch v-model:value="telegramConfig.freeResponse" />
                <n-text>Allow free-response chats</n-text>
              </n-space>
            </n-form-item>
            <n-form-item label="Allowed Chats">
              <n-dynamic-input
                v-model:value="telegramConfig.allowedChats"
                placeholder="Chat ID"
              />
            </n-form-item>
            <n-form-item label="Blocked Chats">
              <n-dynamic-input
                v-model:value="telegramConfig.blockedChats"
                placeholder="Chat ID"
              />
            </n-form-item>
          </n-form>

          <!-- Discord Config -->
          <n-form
            v-if="selectedChannel.type === 'discord'"
            :model="discordConfig"
            label-placement="top"
          >
            <n-form-item label="Bot Token">
              <n-input
                v-model:value="discordConfig.botToken"
                type="password"
                placeholder="Your Discord bot token"
                show-password-on="click"
              />
            </n-form-item>
            <n-form-item label="Guild ID">
              <n-input
                v-model:value="discordConfig.guildId"
                placeholder="Discord guild/server ID"
              />
            </n-form-item>
            <n-form-item label="Mention Control">
              <n-space>
                <n-switch v-model:value="discordConfig.requireMention" />
                <n-text>Require mention to respond</n-text>
              </n-space>
            </n-form-item>
            <n-form-item label="Auto Thread">
              <n-space>
                <n-switch v-model:value="discordConfig.autoThread" />
                <n-text>Create threads automatically</n-text>
              </n-space>
            </n-form-item>
            <n-form-item label="Auto Reactions">
              <n-space>
                <n-switch v-model:value="discordConfig.autoReactions" />
                <n-text>Auto react to messages</n-text>
              </n-space>
            </n-form-item>
            <n-form-item label="Allowed Channels">
              <n-dynamic-input
                v-model:value="discordConfig.allowedChannels"
                placeholder="Channel ID"
              />
            </n-form-item>
          </n-form>

          <!-- Slack Config -->
          <n-form
            v-if="selectedChannel.type === 'slack'"
            :model="slackConfig"
            label-placement="top"
          >
            <n-form-item label="Bot Token">
              <n-input
                v-model:value="slackConfig.botToken"
                type="password"
                placeholder="xoxb-..."
                show-password-on="click"
              />
            </n-form-item>
            <n-form-item label="App Token">
              <n-input
                v-model:value="slackConfig.appToken"
                type="password"
                placeholder="xapp-..."
                show-password-on="click"
              />
            </n-form-item>
            <n-form-item label="Mention Control">
              <n-space>
                <n-switch v-model:value="slackConfig.requireMention" />
                <n-text>Require mention to respond</n-text>
              </n-space>
            </n-form-item>
          </n-form>

          <!-- WhatsApp Config -->
          <n-form
            v-if="selectedChannel.type === 'whatsapp'"
            :model="whatsappConfig"
            label-placement="top"
          >
            <n-form-item label="Phone ID">
              <n-input
                v-model:value="whatsappConfig.phoneId"
                placeholder="WhatsApp Business Phone ID"
              />
            </n-form-item>
            <n-form-item label="API Key">
              <n-input
                v-model:value="whatsappConfig.apiKey"
                type="password"
                placeholder="WhatsApp API key"
                show-password-on="click"
              />
            </n-form-item>
            <n-form-item label="Mention Control">
              <n-space>
                <n-switch v-model:value="whatsappConfig.requireMention" />
                <n-text>Require mention to respond</n-text>
              </n-space>
            </n-form-item>
            <n-form-item label="Mention Pattern">
              <n-input
                v-model:value="whatsappConfig.mentionPattern"
                placeholder="@username"
              />
            </n-form-item>
          </n-form>

          <!-- Matrix Config -->
          <n-form
            v-if="selectedChannel.type === 'matrix'"
            :model="matrixConfig"
            label-placement="top"
          >
            <n-form-item label="Homeserver">
              <n-input
                v-model:value="matrixConfig.homeserver"
                placeholder="https://matrix.example.com"
              />
            </n-form-item>
            <n-form-item label="User ID">
              <n-input
                v-model:value="matrixConfig.userId"
                placeholder="@user:matrix.example.com"
              />
            </n-form-item>
            <n-form-item label="Access Token">
              <n-input
                v-model:value="matrixConfig.accessToken"
                type="password"
                placeholder="Matrix access token"
                show-password-on="click"
              />
            </n-form-item>
            <n-form-item label="Auto Thread">
              <n-space>
                <n-switch v-model:value="matrixConfig.autoThread" />
                <n-text>Create threads automatically</n-text>
              </n-space>
            </n-form-item>
          </n-form>

          <!-- Feishu Config -->
          <n-form
            v-if="selectedChannel.type === 'feishu'"
            :model="feishuConfig"
            label-placement="top"
          >
            <n-form-item label="App ID">
              <n-input
                v-model:value="feishuConfig.appId"
                placeholder="cli_xxx"
              />
            </n-form-item>
            <n-form-item label="App Secret">
              <n-input
                v-model:value="feishuConfig.appSecret"
                type="password"
                placeholder="App secret"
                show-password-on="click"
              />
            </n-form-item>
            <n-form-item label="Mention Control">
              <n-space>
                <n-switch v-model:value="feishuConfig.requireMention" />
                <n-text>Require mention to respond</n-text>
              </n-space>
            </n-form-item>
          </n-form>

          <!-- WeChat Config -->
          <n-form
            v-if="selectedChannel.type === 'wechat'"
            :model="wechatConfig"
            label-placement="top"
          >
            <n-form-item label="Login">
              <n-space vertical>
                <n-button type="primary" @click="showWechatQR = true">
                  <template #icon>
                    <n-icon :component="QrCode" />
                  </template>
                  Scan QR Code
                </n-button>
                <n-text depth="3" v-if="wechatConfig.loggedIn">
                  Logged in as {{ wechatConfig.nickname }}
                </n-text>
              </n-space>
            </n-form-item>
          </n-form>

          <!-- WeCom Config -->
          <n-form
            v-if="selectedChannel.type === 'wecom'"
            :model="wecomConfig"
            label-placement="top"
          >
            <n-form-item label="Corp ID">
              <n-input
                v-model:value="wecomConfig.corpId"
                placeholder="wwxxx"
              />
            </n-form-item>
            <n-form-item label="Agent ID">
              <n-input
                v-model:value="wecomConfig.agentId"
                placeholder="1000001"
              />
            </n-form-item>
            <n-form-item label="Agent Secret">
              <n-input
                v-model:value="wecomConfig.agentSecret"
                type="password"
                placeholder="Agent secret"
                show-password-on="click"
              />
            </n-form-item>
          </n-form>

          <!-- Save Button -->
          <n-divider />
          <n-space justify="end">
            <n-button @click="testConnection">Test Connection</n-button>
            <n-button type="primary" @click="saveChannel" :loading="saving">
              Save Configuration
            </n-button>
          </n-space>
        </n-card>

        <n-card v-else title="Channel Configuration">
          <n-empty description="Select a channel to configure" />
        </n-card>
      </n-gi>
    </n-grid>

    <!-- WeChat QR Modal -->
    <n-modal v-model:show="showWechatQR" preset="card" title="WeChat QR Login" style="width: 300px">
      <n-space vertical align="center">
        <n-spin size="large" v-if="!qrUrl" />
        <img v-else :src="qrUrl" alt="WeChat QR Code" style="width: 200px; height: 200px" />
        <n-text>Scan with WeChat to login</n-text>
      </n-space>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import {
  NCard,
  NGrid,
  NGi,
  NList,
  NListItem,
  NThing,
  NAvatar,
  NBadge,
  NTag,
  NSwitch,
  NSpace,
  NIcon,
  NButton,
  NForm,
  NFormItem,
  NInput,
  NDynamicInput,
  NEmpty,
  NDivider,
  NSpin,
  NModal,
  NText,
} from 'naive-ui'
import {
  Refresh,
  LogoTelegram,
  LogoDiscord,
  LogoSlack,
  Chatbubbles,
  LogoMicrosoft,
  LogoGithub,
  QrCode,
  LogoWechat,
} from '@vicons/ionicons5'

interface Channel {
  id: string
  type: string
  name: string
  enabled: boolean
  configured: boolean
}

const channels = ref<Channel[]>([
  { id: 'telegram', type: 'telegram', name: 'Telegram', enabled: false, configured: false },
  { id: 'discord', type: 'discord', name: 'Discord', enabled: false, configured: false },
  { id: 'slack', type: 'slack', name: 'Slack', enabled: false, configured: false },
  { id: 'whatsapp', type: 'whatsapp', name: 'WhatsApp', enabled: false, configured: false },
  { id: 'matrix', type: 'matrix', name: 'Matrix', enabled: false, configured: false },
  { id: 'feishu', type: 'feishu', name: 'Feishu/Lark', enabled: false, configured: false },
  { id: 'wechat', type: 'wechat', name: 'WeChat', enabled: false, configured: false },
  { id: 'wecom', type: 'wecom', name: 'WeCom', enabled: false, configured: false },
])

const selectedChannel = ref<Channel | null>(null)
const saving = ref(false)
const showWechatQR = ref(false)
const qrUrl = ref('')

// Telegram config
const telegramConfig = reactive({
  botToken: '',
  allowedUsers: [] as string[],
  requireMention: true,
  autoReactions: true,
  freeResponse: false,
  allowedChats: [] as string[],
  blockedChats: [] as string[],
})

// Discord config
const discordConfig = reactive({
  botToken: '',
  guildId: '',
  requireMention: true,
  autoThread: false,
  autoReactions: true,
  allowedChannels: [] as string[],
})

// Slack config
const slackConfig = reactive({
  botToken: '',
  appToken: '',
  requireMention: true,
})

// WhatsApp config
const whatsappConfig = reactive({
  phoneId: '',
  apiKey: '',
  requireMention: false,
  mentionPattern: '@',
})

// Matrix config
const matrixConfig = reactive({
  homeserver: '',
  userId: '',
  accessToken: '',
  autoThread: false,
})

// Feishu config
const feishuConfig = reactive({
  appId: '',
  appSecret: '',
  requireMention: true,
})

// WeChat config
const wechatConfig = reactive({
  loggedIn: false,
  nickname: '',
})

// WeCom config
const wecomConfig = reactive({
  corpId: '',
  agentId: '',
  agentSecret: '',
})

function getChannelIcon(type: string) {
  switch (type) {
    case 'telegram':
      return LogoTelegram
    case 'discord':
      return LogoDiscord
    case 'slack':
      return LogoSlack
    case 'whatsapp':
      return Chatbubbles
    case 'matrix':
      return LogoGithub
    case 'feishu':
      return LogoMicrosoft
    case 'wechat':
      return LogoWechat
    case 'wecom':
      return LogoMicrosoft
    default:
      return Chatbubbles
  }
}

function selectChannel(channel: Channel) {
  selectedChannel.value = channel
  loadChannelConfig(channel.type)
}

async function loadChannelConfig(type: string) {
  try {
    const res = await fetch(`/api/gateway/config/${type}`)
    if (res.ok) {
      const data = await res.json()
      // Merge with configs
      Object.assign(getConfig(type), data)
    }
  } catch (e) {
    console.error('Failed to load channel config:', e)
  }
}

function getConfig(type: string) {
  switch (type) {
    case 'telegram':
      return telegramConfig
    case 'discord':
      return discordConfig
    case 'slack':
      return slackConfig
    case 'whatsapp':
      return whatsappConfig
    case 'matrix':
      return matrixConfig
    case 'feishu':
      return feishuConfig
    case 'wechat':
      return wechatConfig
    case 'wecom':
      return wecomConfig
    default:
      return {}
  }
}

async function saveChannel() {
  if (!selectedChannel.value) return
  saving.value = true

  try {
    const config = getConfig(selectedChannel.value.type)
    await fetch(`/api/gateway/config/${selectedChannel.value.type}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(config),
    })

    // Update channel status
    const channel = channels.value.find((c) => c.id === selectedChannel.value?.id)
    if (channel) {
      channel.configured = true
    }
  } catch (e) {
    console.error('Failed to save channel config:', e)
  } finally {
    saving.value = false
  }
}

async function toggleChannel(enabled: boolean) {
  if (!selectedChannel.value) return
  try {
    await fetch(`/api/gateway/${selectedChannel.value.type}/${enabled ? 'enable' : 'disable'}`, {
      method: 'POST',
    })
  } catch (e) {
    console.error('Failed to toggle channel:', e)
  }
}

async function testConnection() {
  if (!selectedChannel.value) return
  try {
    const res = await fetch(`/api/gateway/test/${selectedChannel.value.type}`)
    if (res.ok) {
      alert('Connection successful!')
    } else {
      alert('Connection failed')
    }
  } catch (e) {
    alert('Connection failed')
  }
}

async function refreshChannels() {
  try {
    const res = await fetch('/api/gateway/channels')
    if (res.ok) {
      const data = await res.json()
      channels.value = data
    }
  } catch (e) {
    console.error('Failed to refresh channels:', e)
  }
}

onMounted(() => {
  refreshChannels()
})
</script>

<style lang="scss" scoped>
.channels-view {
  height: calc(100vh - 84px);
}

.channel-list-card {
  height: 100%;
}

.n-list-item {
  &.active {
    background: var(--selected-color, #e8f5e9);
  }
}
</style>
