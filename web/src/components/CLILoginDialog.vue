<template>
  <n-modal
    :show="show"
    preset="card"
    style="width: 640px; max-width: 94vw;"
    :title="t('cli.loginTitle', { name: displayName })"
    @update:show="(v: boolean) => emit('update:show', v)"
  >
    <n-text depth="3" style="font-size: 12px; display: block;">
      {{ t('cli.loginHint') }}
    </n-text>

    <div v-if="session" class="cl-cmd">
      <code>{{ session.command }}</code>
    </div>

    <!-- 设备码 / 一次性短码：CLI 打印出来后通常要用户手输或确认 -->
    <n-alert
      v-if="session?.user_code"
      type="info"
      :bordered="false"
      style="margin-top: 12px;"
    >
      <div class="cl-code-row">
        <span class="cl-code-label">{{ t('cli.loginCode') }}</span>
        <span class="cl-code">{{ session.user_code }}</span>
        <n-button text size="tiny" @click="copy(session.user_code)">
          {{ t('cli.catalogCopy') }}
        </n-button>
      </div>
      <n-button
        v-if="session.auth_url"
        size="tiny"
        type="primary"
        style="margin-top: 8px;"
        @click="openAuth"
      >
        {{ t('cli.loginOpenBrowser') }}
      </n-button>
    </n-alert>

    <div class="cl-status">
      <n-spin v-if="phase === 'running'" :size="14" />
      <n-text v-if="phase === 'running'" depth="3" style="font-size: 12px;">
        {{ t('cli.loginRunning') }}
      </n-text>
      <n-tag v-else-if="phase === 'done'" size="small" type="success" :bordered="false">
        {{ session?.check_message ? t('cli.loginDoneAs', { who: session.check_message }) : t('cli.loginDone') }}
      </n-tag>
      <n-tag v-else-if="phase === 'cancelled'" size="small" type="default" :bordered="false">
        {{ t('cli.loginCancelled') }}
      </n-tag>
      <n-tag v-else size="small" type="error" :bordered="false">
        {{ t('cli.loginFailed') }}
      </n-tag>
    </div>

    <div v-if="session?.output" class="cl-out-wrap">
      <div class="cl-out-label">{{ t('agentplugins.cliInstallOutput') }}</div>
      <pre class="cl-out">{{ session.output }}</pre>
    </div>

    <!-- 交互式提问（"Press Enter" / "用哪个账号登录"）走这里转发 -->
    <div v-if="phase === 'running'" class="cl-input">
      <n-input
        v-model:value="input"
        size="small"
        :placeholder="t('cli.loginInputPlaceholder')"
        @keydown.enter="send"
      />
      <n-button size="small" :disabled="!input.trim()" @click="send">
        {{ t('cli.loginSend') }}
      </n-button>
    </div>

    <n-text v-if="phase === 'failed' && session?.error" depth="3" class="cl-err">
      {{ session.error }}
    </n-text>

    <template #footer>
      <n-space justify="end">
        <n-button v-if="phase === 'running'" size="small" @click="stop">
          {{ t('cli.loginCancel') }}
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
import { NAlert, NButton, NInput, NModal, NSpace, NSpin, NTag, NText, useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import {
  cancelCLILogin,
  getCLILoginSession,
  sendCLILoginInput,
  startCLILogin,
  type CLILoginSession,
} from '@/api/cli'

/**
 * CLI 委托登录弹窗：由服务端跑目录里登记的登录命令。
 *
 * 用户只需要看两个东西——"去哪个页面"和"输什么码"，其余（等待回调、
 * 写凭据、复检登录态）都在后台完成。命令如果问问题，输入框会转发 stdin。
 */
const props = defineProps<{
  show: boolean
  catalogId: string
  displayName?: string
}>()

const emit = defineEmits<{
  (e: 'update:show', v: boolean): void
  /** done: 登录流程结束，loggedIn 表示就绪检查已通过 */
  (e: 'done', payload: { loggedIn: boolean }): void
}>()

const { t } = useI18n()
const message = useMessage()

const session = ref<CLILoginSession | null>(null)
const input = ref('')
const phase = computed(() => session.value?.state ?? 'running')
const displayName = computed(() => props.displayName || props.catalogId)

let timer: number | undefined

function stopTimer() {
  if (timer !== undefined) {
    window.clearInterval(timer)
    timer = undefined
  }
}

function startPolling(id: string) {
  stopTimer()
  timer = window.setInterval(() => void poll(id), 1500)
}

async function poll(id: string) {
  try {
    const res = await getCLILoginSession(id)
    session.value = res.session
    if (res.session.state !== 'running') {
      stopTimer()
      finish(res.session)
    }
  } catch {
    stopTimer()
    message.error(t('cli.loginFailed'))
  }
}

function finish(done: CLILoginSession) {
  const ok = done.state === 'done'
  emit('done', { loggedIn: ok })
  if (ok) {
    message.success(done.check_message ? t('cli.loginDoneAs', { who: done.check_message }) : t('cli.loginDone'))
  } else if (done.state === 'failed') {
    message.error(done.error || t('cli.loginFailed'))
  }
}

async function start() {
  session.value = null
  input.value = ''
  stopTimer()
  try {
    const res = await startCLILogin(props.catalogId)
    session.value = res.session
    if (res.reused) message.info(t('cli.loginReused'))
    if (res.session.state === 'running') startPolling(res.session.id)
    else finish(res.session)
  } catch (e: any) {
    message.error(String(e?.message || e))
    emit('update:show', false)
  }
}

async function send() {
  const text = input.value
  if (!text.trim() || !session.value) return
  input.value = ''
  try {
    await sendCLILoginInput(session.value.id, text)
  } catch (e: any) {
    message.error(String(e?.message || e))
  }
}

async function stop() {
  if (!session.value) return
  try {
    await cancelCLILogin(session.value.id)
  } catch {
    // 会话可能已经结束，取消失败不影响用户关闭弹窗
  }
  stopTimer()
  session.value = { ...session.value, state: 'cancelled' }
}

function openAuth() {
  const url = session.value?.auth_url
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
.cl-cmd {
  margin-top: 10px;
}
.cl-cmd code {
  display: block;
  font-size: 12px;
  padding: 6px 10px;
  border-radius: 6px;
  background: #f5f6f8;
  color: #333;
  word-break: break-all;
}
.cl-code-row {
  display: flex;
  align-items: center;
  gap: 10px;
}
.cl-code-label {
  font-size: 12px;
  color: #909399;
}
.cl-code {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 18px;
  letter-spacing: 2px;
  font-weight: 600;
}
.cl-status {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 12px;
}
.cl-out-wrap {
  margin-top: 12px;
}
.cl-out-label {
  font-size: 12px;
  color: #909399;
  margin-bottom: 6px;
}
.cl-out {
  max-height: 240px;
  overflow: auto;
  margin: 0;
  padding: 10px;
  border-radius: 8px;
  background: #f8f9fb;
  border: 1px solid #ebedf0;
  font-size: 12px;
  line-height: 1.5;
  white-space: pre-wrap;
  word-break: break-all;
}
.cl-input {
  display: flex;
  gap: 8px;
  margin-top: 12px;
}
.cl-err {
  display: block;
  margin-top: 8px;
  font-size: 12px;
  word-break: break-all;
}

@media (prefers-color-scheme: dark) {
  .cl-cmd code {
    background: #26262b;
    color: #d8d8d8;
  }
  .cl-out {
    background: #18181c;
    border-color: #2c2c30;
    color: #d8d8d8;
  }
}
</style>
