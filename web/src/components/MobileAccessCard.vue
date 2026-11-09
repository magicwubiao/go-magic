<template>
  <div class="mobile-access-panel">
    <div class="ma-title">{{ t('mobileAccess.title') }}</div>

    <div class="ma-qr-wrap">
      <n-spin v-if="loading" size="small" style="height: 176px; display: flex; align-items: center;" />
      <template v-else-if="qrData">
        <img class="ma-qr" :src="qrData" :alt="t('mobileAccess.title')" />
        <div v-if="pairingEnabled && expiresIn > 0" class="ma-expires">
          {{ t('mobileAccess.expiresIn', { minutes: Math.ceil(expiresIn / 60) }) }}
        </div>
      </template>
      <n-empty v-else size="small" :description="error || t('mobileAccess.noLan')" class="ma-empty" />
    </div>

    <div v-if="info?.url" class="ma-url-row">
      <span class="ma-url" :title="info.url">{{ info.url }}</span>
      <n-button quaternary circle size="tiny" :title="t('common.copy')" @click="copyUrl">
        <template #icon>
          <n-icon :size="14"><copy-outline /></n-icon>
        </template>
      </n-button>
    </div>
    <div class="ma-hint">{{ pairingEnabled ? t('mobileAccess.pairHint') : t('mobileAccess.qrHint') }}</div>

    <div class="ma-divider"></div>

    <div class="ma-switch-row">
      <span class="ma-switch-label">{{ t('mobileAccess.allowPair') }}</span>
      <n-switch
        size="small"
        :value="pairingEnabled"
        :loading="toggling"
        @update:value="handleToggle"
      />
    </div>

    <div v-if="pairingEnabled" class="ma-refresh-row">
      <n-button size="tiny" quaternary type="primary" :loading="refreshing" @click="regenerate(false)">
        <template #icon>
          <n-icon :size="14"><refresh-outline /></n-icon>
        </template>
        {{ t('mobileAccess.refresh') }}
      </n-button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useMessage } from 'naive-ui'
import { NButton, NEmpty, NIcon, NSpin, NSwitch } from 'naive-ui'
import { CopyOutline, RefreshOutline } from '@vicons/ionicons5'
import QRCode from 'qrcode'
import {
  createPairToken,
  getAccessInfo,
  setPairing,
  type AccessInfo,
} from '@/api/access'

const { t } = useI18n()
const message = useMessage()

const loading = ref(true)
const toggling = ref(false)
const refreshing = ref(false)
const info = ref<AccessInfo | null>(null)
const qrData = ref('')
const error = ref('')
const pairingEnabled = ref(false)
/** 配对码剩余秒数；0 表示当前二维码不带配对码（或已过期）。 */
const expiresIn = ref(0)

let expireTimer: number | undefined

function stopExpireTimer(): void {
  if (expireTimer !== undefined) {
    window.clearInterval(expireTimer)
    expireTimer = undefined
  }
}

function startExpireTimer(seconds: number): void {
  stopExpireTimer()
  const deadline = Date.now() + seconds * 1000
  const tick = () => {
    expiresIn.value = Math.max(0, Math.round((deadline - Date.now()) / 1000))
    if (expiresIn.value === 0) stopExpireTimer()
  }
  tick()
  expireTimer = window.setInterval(tick, 1000)
}

async function renderQR(text: string): Promise<void> {
  try {
    qrData.value = await QRCode.toDataURL(text, {
      width: 352,
      margin: 1,
      errorCorrectionLevel: 'M',
    })
  } catch {
    qrData.value = ''
    error.value = t('mobileAccess.loadFailed')
  }
}

async function loadInfo(): Promise<void> {
  loading.value = true
  error.value = ''
  qrData.value = ''
  try {
    const res = await getAccessInfo()
    info.value = res
    pairingEnabled.value = res.pairing_enabled
    if (res.pairing_enabled) {
      await regenerate(false)
    }
  } catch {
    error.value = t('mobileAccess.loadFailed')
  } finally {
    loading.value = false
  }
}

/** 签发新配对码并重绘二维码。silent=true 时不弹成功提示（初始化路径用）。 */
async function regenerate(silent = true): Promise<void> {
  if (!silent) refreshing.value = true
  try {
    const res = await createPairToken()
    await renderQR(res.url)
    startExpireTimer(res.expires_in)
    if (!silent) message.success(t('common.refreshed'))
  } catch {
    qrData.value = ''
    error.value = t('mobileAccess.loadFailed')
  } finally {
    refreshing.value = false
  }
}

async function handleToggle(enabled: boolean): Promise<void> {
  toggling.value = true
  try {
    await setPairing(enabled)
    pairingEnabled.value = enabled
    stopExpireTimer()
    if (enabled) {
      // 开关打开后立刻签发一个配对码，让二维码马上变成"扫码直接登录"形态。
      await regenerate(true)
    } else {
      // 关闭后二维码退回普通地址形态（扫码后需手动输密码）。
      if (info.value?.url) await renderQR(info.value.url)
      expiresIn.value = 0
    }
  } catch {
    message.error(t('mobileAccess.loadFailed'))
  } finally {
    toggling.value = false
  }
}

async function copyUrl(): Promise<void> {
  if (!info.value?.url) return
  try {
    await navigator.clipboard.writeText(info.value.url)
    message.success(t('common.copied'))
  } catch {
    message.error(t('common.copyFailed'))
  }
}

onMounted(loadInfo)
onUnmounted(stopExpireTimer)
</script>

<!-- 面板样式必须写在全局 <style>：n-popover 是 teleport 组件，
     scoped 样式缺 data-v 属性会完全失效。 -->
<style>
.mobile-access-panel {
  padding: 4px 4px 0;
  width: 240px;
}
.mobile-access-panel .ma-title {
  text-align: center;
  font-size: 14px;
  font-weight: 600;
  color: #333;
  margin-bottom: 10px;
}
.mobile-access-panel .ma-qr-wrap {
  display: flex;
  flex-direction: column;
  align-items: center;
  min-height: 176px;
  justify-content: center;
}
.mobile-access-panel .ma-qr {
  width: 176px;
  height: 176px;
  border-radius: 8px;
  background: #fff;
}
.mobile-access-panel .ma-expires {
  margin-top: 6px;
  font-size: 12px;
  color: #999;
}
.mobile-access-panel .ma-empty {
  padding: 24px 0;
}
.mobile-access-panel .ma-url-row {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 4px;
  margin-top: 8px;
}
.mobile-access-panel .ma-url {
  font-size: 12px;
  color: #18a058;
  word-break: break-all;
  text-align: center;
}
.mobile-access-panel .ma-hint {
  margin-top: 4px;
  font-size: 12px;
  color: #999;
  text-align: center;
}
.mobile-access-panel .ma-divider {
  height: 1px;
  background: #efeff5;
  margin: 10px 0;
}
.mobile-access-panel .ma-switch-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.mobile-access-panel .ma-switch-label {
  font-size: 13px;
  color: #333;
}
.mobile-access-panel .ma-refresh-row {
  display: flex;
  justify-content: center;
  margin-top: 6px;
}
</style>
