<template>
  <n-modal
    :show="show"
    preset="card"
    style="width: 560px; max-width: 94vw;"
    :title="t('cli.oauthTitle', { name: displayName || name })"
    @update:show="(v: boolean) => emit('update:show', v)"
  >
    <n-text depth="3" style="font-size: 12px; display: block;">
      {{ t('cli.oauthHint', { env: tokenEnv }) }}
    </n-text>

    <template v-if="device">
      <div class="co-code-box">
        <div class="co-code-label">{{ t('cli.oauthCode') }}</div>
        <div class="co-code">{{ device.user_code }}</div>
        <n-space :size="8" justify="center" style="margin-top: 10px;">
          <n-button size="small" type="primary" @click="openBrowser">
            {{ t('cli.oauthOpen') }}
          </n-button>
          <n-button size="small" @click="copy(device.user_code)">
            {{ t('cli.catalogCopy') }}
          </n-button>
        </n-space>
      </div>
      <n-text depth="3" style="font-size: 12px; display: block; margin-top: 10px;">
        {{ t('cli.oauthWaiting') }}
      </n-text>
    </template>

    <div class="co-status">
      <n-spin v-if="!finished" :size="14" />
      <n-tag v-if="status?.state === 'authorized'" size="small" type="success" :bordered="false">
        {{ t('cli.oauthAuthorized') }}
      </n-tag>
      <n-tag v-else-if="status?.state === 'failed'" size="small" type="error" :bordered="false">
        {{ t('cli.oauthFailed') }}
      </n-tag>
      <n-text v-else depth="3" style="font-size: 12px;">{{ t('cli.oauthPending') }}</n-text>
    </div>

    <n-text v-if="status?.error" depth="3" class="co-err">{{ status.error }}</n-text>

    <template #footer>
      <n-space justify="end">
        <n-button v-if="signedIn" size="small" type="error" secondary @click="signOut">
          {{ t('cli.oauthSignout') }}
        </n-button>
        <n-button size="small" type="primary" @click="emit('update:show', false)">
          {{ t('common.close') }}
        </n-button>
      </n-space>
    </template>
  </n-modal>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { NButton, NModal, NSpace, NSpin, NTag, NText, useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import {
  getCLIConnectorOAuth,
  getPluginCLIOAuth,
  signOutCLIConnectorOAuth,
  signOutPluginCLIOAuth,
  startCLIConnectorOAuth,
  startPluginCLIOAuth,
  type CLIDeviceCode,
  type CLIOAuthStatus,
} from '@/api/cli'

/**
 * 连接器级 OAuth 登录（设备码流程）。
 *
 * 与"委托登录"的区别：这里 token 由 go-magic 自己拿、自己存、执行前注入
 * 环境变量，CLI 本身不需要支持登录命令。适合只认 GH_TOKEN 这类变量的工具。
 *
 * plugin 非空时走插件命名空间的路由：同一个连接器名在不同插件里是两套
 * 凭据，后端也按 cliconn:agentplugin/<plugin>/<connector> 分开存。
 */
const props = defineProps<{
  show: boolean
  name: string
  /** 归属插件名；空 = 全局连接器 */
  plugin?: string
  displayName?: string
  /** 展示用：token 将被注入哪个环境变量（来自连接器配置） */
  tokenEnv?: string
}>()

const emit = defineEmits<{
  (e: 'update:show', v: boolean): void
  (e: 'done', payload: { authorized: boolean }): void
}>()

const { t } = useI18n()
const message = useMessage()

const device = ref<CLIDeviceCode | null>(null)
const status = ref<CLIOAuthStatus | null>(null)
const finished = computed(() => status.value?.state === 'authorized' || status.value?.state === 'failed')
const signedIn = computed(() => status.value?.state === 'authorized')

let timer: number | undefined

function stopTimer() {
  if (timer !== undefined) {
    window.clearInterval(timer)
    timer = undefined
  }
}

function startPolling() {
  stopTimer()
  timer = window.setInterval(() => void poll(), 2000)
}

async function poll() {
  try {
    const res = props.plugin
      ? await getPluginCLIOAuth(props.plugin, props.name)
      : await getCLIConnectorOAuth(props.name)
    status.value = res.status
    if (res.status.state === 'authorized') {
      stopTimer()
      device.value = null
      message.success(t('cli.oauthAuthorized'))
      emit('done', { authorized: true })
    } else if (res.status.state === 'failed') {
      stopTimer()
      message.error(res.status.error || t('cli.oauthFailed'))
      emit('done', { authorized: false })
    }
  } catch {
    stopTimer()
  }
}

async function start() {
  device.value = null
  status.value = null
  stopTimer()
  try {
    const res = props.plugin
      ? await startPluginCLIOAuth(props.plugin, props.name)
      : await startCLIConnectorOAuth(props.name)
    status.value = res.status
    if (res.already_authorized) {
      message.success(t('cli.oauthAuthorized'))
      emit('done', { authorized: true })
      return
    }
    device.value = res.device || null
    startPolling()
  } catch (e: any) {
    message.error(String(e?.message || e))
    emit('update:show', false)
  }
}

async function signOut() {
  try {
    if (props.plugin) await signOutPluginCLIOAuth(props.plugin, props.name)
    else await signOutCLIConnectorOAuth(props.name)
    status.value = null
    message.success(t('cli.oauthSignedOut'))
    emit('done', { authorized: false })
    void start()
  } catch (e: any) {
    message.error(String(e?.message || e))
  }
}

function openBrowser() {
  const d = device.value
  if (!d) return
  const url = d.verification_uri_complete || d.verification_uri
  if (url) window.open(url, '_blank', 'noopener')
}

async function copy(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    message.success(t('cli.catalogCopied'))
  } catch {
    message.warning(text)
  }
}

watch(
  () => props.show,
  (visible) => {
    if (visible) void start()
    else stopTimer()
  },
)

onBeforeUnmount(stopTimer)
</script>

<style scoped>
.co-code-box {
  margin-top: 14px;
  padding: 16px;
  border-radius: 10px;
  background: #f7f8fa;
  border: 1px dashed #dcdfe6;
  text-align: center;
}
.co-code-label {
  font-size: 12px;
  color: #909399;
}
.co-code {
  margin-top: 6px;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 26px;
  letter-spacing: 4px;
  font-weight: 600;
}
.co-status {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 14px;
}
.co-err {
  display: block;
  margin-top: 8px;
  font-size: 12px;
  word-break: break-all;
}

@media (prefers-color-scheme: dark) {
  .co-code-box {
    background: #1f1f24;
    border-color: #333338;
  }
}
</style>
