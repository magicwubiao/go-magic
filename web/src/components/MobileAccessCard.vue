<template>
  <div class="mobile-access-panel">
    <div class="ma-title">{{ t('mobileAccess.title') }}</div>

    <div class="ma-qr-wrap">
      <n-spin v-if="loading" size="small" style="height: 176px; display: flex; align-items: center;" />
      <template v-else-if="qrData">
        <img class="ma-qr" :class="{ 'ma-qr--expired': isExpired }" :src="qrData" :alt="t('mobileAccess.title')" />
        <!-- 免密态：有效期与刷新并排一行，不再在面板底部单独占一行 -->
        <div v-if="pairingEnabled && expiresIn >= 0" class="ma-status">
          <span :class="{ 'ma-status--warn': isExpired }">
            {{ isExpired ? t('mobileAccess.expired') : t('mobileAccess.expiresIn', { time: countdown }) }}
          </span>
          <n-button
            quaternary
            circle
            size="tiny"
            :title="t('mobileAccess.refresh')"
            :loading="refreshing"
            @click="regenerate()"
          >
            <template #icon>
              <n-icon :size="14"><refresh-outline /></n-icon>
            </template>
          </n-button>
        </div>
      </template>
      <!-- 失败/无地址：原先只写"请重试"却没有可点的重试入口，这里补上 -->
      <n-empty v-else size="small" :description="error || t('mobileAccess.noLan')" class="ma-empty">
        <template #extra>
          <n-button size="tiny" quaternary @click="loadInfo()">{{ t('mobileAccess.retry') }}</n-button>
        </template>
      </n-empty>
    </div>

    <div v-if="displayUrl" class="ma-url-row">
      <span class="ma-url" :title="displayUrl">{{ displayUrl }}</span>
      <n-button quaternary circle size="tiny" :title="t('mobileAccess.copyUrl')" @click="copyUrl()">
        <template #icon>
          <n-icon :size="14"><copy-outline /></n-icon>
        </template>
      </n-button>
    </div>

    <div class="ma-hint">{{ pairingEnabled ? t('mobileAccess.pairHint') : t('mobileAccess.qrHint') }}</div>
    <div v-if="isIpUrl" class="ma-firewall-hint">{{ t('mobileAccess.firewallHint') }}</div>

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
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
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
/**
 * 二维码剩余有效秒数。
 * -1 = 当前是普通地址二维码（不带免密信息），0 = 免密二维码已过期。
 * 用 -1 区分这两种「没有有效期」的状态，免得普通二维码被误判成过期而置灰。
 */
const expiresIn = ref(-1)

/**
 * 二维码/链接应使用的访问根地址。
 *
 * 优先跟随当前浏览器 origin —— 服务器部署时手机真正可达的地址是「你访问面板
 * 用的那个地址」：域名、反向代理、端口映射（如 8642）都在 origin 里；而容器/
 * 云主机的网卡 IP（172.x 内网、被安全组挡住的公网 IP）从手机往往不可达。
 * 只有桌面端场景（localhost / WebView 自定义协议）才退回后端探测的局域网 IP。
 */
function resolveBaseUrl(): string {
  const { protocol, host, hostname } = window.location
  if (protocol === 'http:' || protocol === 'https:') {
    const isLocal =
      hostname === 'localhost' || hostname === '127.0.0.1' ||
      hostname === '::1' || hostname === '[::1]'
    if (!isLocal) return `${protocol}//${host}`
  }
  return info.value?.url || ''
}

const displayUrl = computed(() => resolveBaseUrl())

/** 地址是裸 IP 时提示排查防火墙/安全组；域名访问（反代已通）则不需要。 */
const isIpUrl = computed(() => {
  if (!displayUrl.value) return false
  try {
    const host = new URL(displayUrl.value).hostname
    return /^\d{1,3}(\.\d{1,3}){3}$/.test(host) || host.includes(':')
  } catch {
    return false
  }
})

/** mm:ss 倒计时，比「约 N 分钟」精确，也不必再重复说明「可点击刷新」。 */
const countdown = computed(() => {
  const seconds = Math.max(0, expiresIn.value)
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}`
})
const isExpired = computed(() => pairingEnabled.value && expiresIn.value === 0)

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
    // 免密关闭时二维码编码普通地址：扫码后仍可打开面板，只是要手动输密码。
    const base = resolveBaseUrl()
    if (res.pairing_enabled) {
      await regenerate()
    } else if (base) {
      await renderQR(base)
    } else {
      error.value = t('mobileAccess.noLan')
    }
  } catch {
    error.value = t('mobileAccess.loadFailed')
  } finally {
    loading.value = false
  }
}

/** 签发新二维码；刷新按钮自带 loading + 二维码变化，不再额外弹成功提示。 */
async function regenerate(): Promise<void> {
  refreshing.value = true
  try {
    const res = await createPairToken()
    // 用当前浏览器 origin 拼链接，而不是后端返回的 url（容器/云主机 IP 手机不可达）。
    const base = resolveBaseUrl()
    if (!base) {
      qrData.value = ''
      error.value = t('mobileAccess.noLan')
      return
    }
    await renderQR(`${base}/#/login?pair=${encodeURIComponent(res.token)}`)
    startExpireTimer(res.expires_in)
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
      // 开关打开后立刻签发，让二维码马上变成"扫码直接登录"形态。
      await regenerate()
    } else {
      // 关闭后二维码退回普通地址形态（扫码后需手动输密码）。
      const base = resolveBaseUrl()
      if (base) await renderQR(base)
      expiresIn.value = -1
    }
  } catch {
    message.error(t('mobileAccess.loadFailed'))
  } finally {
    toggling.value = false
  }
}

async function copyUrl(): Promise<void> {
  if (!displayUrl.value) return
  try {
    await navigator.clipboard.writeText(displayUrl.value)
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
.mobile-access-panel .ma-qr--expired {
  opacity: 0.3;
  filter: grayscale(1);
}
.mobile-access-panel .ma-status {
  display: flex;
  align-items: center;
  gap: 2px;
  margin-top: 4px;
  font-size: 12px;
  color: #999;
}
.mobile-access-panel .ma-status--warn {
  color: #d03050;
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
  color: #888;
  text-align: center;
}
/* 排障信息属于次要层级：常驻但不用告警红，避免一打开面板就像出了错 */
.mobile-access-panel .ma-firewall-hint {
  margin-top: 4px;
  font-size: 12px;
  color: #aaa;
  text-align: center;
  line-height: 1.4;
}
.mobile-access-panel .ma-divider {
  height: 1px;
  background: #efeff5;
  margin: 8px 0;
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
</style>
