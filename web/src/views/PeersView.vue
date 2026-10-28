<template>
  <div class="peers-shell" :class="{ 'in-chat': !!dmPeer }">
    <!-- ===== 左栏：Peer 列表（对齐 Bot 页 rail） ===== -->
    <aside class="peer-rail">
      <div class="rail-header">
        <div class="rail-heading">
          <h2 class="rail-title">{{ t('peers.title') }}</h2>
          <n-text depth="3" class="rail-sub">
            {{ peers.length }} · {{ onlineCount }} {{ t('peers.statOnline') }}
          </n-text>
        </div>
        <n-button type="primary" size="small" circle :title="t('peers.add')" @click="openAdd">
          <template #icon><n-icon><AddOutline /></n-icon></template>
        </n-button>
      </div>

      <div class="rail-search">
        <n-input v-model:value="search" size="small" clearable :placeholder="t('peers.searchPlaceholder')">
          <template #prefix><n-icon><SearchOutline /></n-icon></template>
        </n-input>
      </div>

      <div class="peer-list">
        <div v-if="loading && !peers.length" class="list-placeholder">
          <n-spin size="small" />
        </div>
        <n-empty v-else-if="!peers.length" :description="t('peers.empty')" size="small" class="list-placeholder">
          <template #extra>
            <n-button size="small" type="primary" @click="openAdd">{{ t('peers.addFirst') }}</n-button>
          </template>
        </n-empty>
        <n-empty v-else-if="!filteredPeers.length" :description="t('peers.noSearchResult')" size="small" class="list-placeholder" />

        <div
          v-for="p in filteredPeers"
          :key="p.name"
          class="peer-row"
          :class="{ active: selectedPeer === p.name }"
          @click="selectPeer(p)"
        >
          <n-avatar round :size="36" class="row-avatar" :style="{ backgroundColor: avatarColor(p.name) }">
            {{ avatarText(p.name) }}
          </n-avatar>
          <div class="row-main">
            <div class="row-name">
              <span class="row-name-text">{{ p.name }}</span>
              <n-icon v-if="!p.has_token" size="13" class="row-token-warn" :title="t('peers.noToken')">
                <AlertCircleOutline />
              </n-icon>
            </div>
            <div class="row-sub" :title="previewOf(p)">{{ previewOf(p) }}</div>
          </div>
          <div class="row-meta">
            <span class="row-time">{{ lastTimeOf(p) }}</span>
            <span class="row-status" :class="statusClass(p.name)" :title="statusTitle(p.name)" />
          </div>
          <n-dropdown
            trigger="click"
            placement="bottom-end"
            :options="rowMenuOptions"
            @select="(key: string) => onRowMenu(key, p)"
            @click.stop
          >
            <n-button quaternary size="tiny" class="row-menu" @click.stop>
              <template #icon><n-icon><EllipsisHorizontalOutline /></n-icon></template>
            </n-button>
          </n-dropdown>
        </div>
      </div>

      <div class="rail-footer">
        <n-button quaternary size="tiny" :loading="allProbing" @click="probeAll">
          <template #icon><n-icon><PulseOutline /></n-icon></template>
          {{ t('peers.checkAll') }}
        </n-button>
        <n-button quaternary size="tiny" @click="showIdentity = true">
          <template #icon><n-icon><IdCardOutline /></n-icon></template>
          {{ t('peers.accessTitle') }}
        </n-button>
      </div>
    </aside>

    <!-- ===== 右栏：欢迎页 / 与远端 Bot 的会话 ===== -->
    <section class="peer-pane">
      <div v-if="!dmPeer" class="welcome">
        <div class="welcome-inner">
          <div class="welcome-greeting">{{ t('peers.welcomeTitle') }}</div>
          <p class="welcome-sub">{{ t('peers.welcomeSubtitle') }}</p>

          <n-space v-if="peers.length" justify="center" :size="12" class="welcome-stats">
            <div class="stat-card">
              <div class="stat-value">{{ peers.length }}</div>
              <div class="stat-label">{{ t('peers.statTotal') }}</div>
            </div>
            <div class="stat-card">
              <div class="stat-value stat-online">{{ onlineCount }}</div>
              <div class="stat-label">{{ t('peers.statOnline') }}</div>
            </div>
            <div class="stat-card">
              <div class="stat-value">{{ totalRemoteBots }}</div>
              <div class="stat-label">{{ t('peers.statBots') }}</div>
            </div>
          </n-space>

          <template v-if="peers.length">
            <div class="welcome-pick">{{ t('peers.welcomePick') }}</div>
            <div class="welcome-chips">
              <button
                v-for="p in peers.slice(0, 8)"
                :key="p.name"
                type="button"
                class="welcome-chip"
                @click="selectPeer(p)"
              >
                <n-avatar round :size="24" :style="{ backgroundColor: avatarColor(p.name), fontSize: '11px' }">
                  {{ avatarText(p.name) }}
                </n-avatar>
                <span class="chip-name">{{ p.name }}</span>
                <span class="chip-status" :class="statusClass(p.name)" />
              </button>
            </div>
          </template>
          <n-button v-else type="primary" class="welcome-add" @click="openAdd">
            <template #icon><n-icon><AddOutline /></n-icon></template>
            {{ t('peers.addFirst') }}
          </n-button>
        </div>
      </div>

      <template v-else>
        <header class="chat-header">
          <n-button quaternary size="small" class="back-btn" :title="t('common.back')" @click="leaveChat">
            <template #icon><n-icon><ArrowBackOutline /></n-icon></template>
          </n-button>
          <n-avatar round :size="34" :style="{ backgroundColor: avatarColor(dmPeer.name) }">
            {{ avatarText(dmPeer.name) }}
          </n-avatar>
          <div class="chat-title-text">
            <div class="chat-name">{{ dmPeer.name }}</div>
            <n-text code depth="3" class="chat-url">{{ dmPeer.base_url }}</n-text>
          </div>
          <n-space :size="6" align="center" class="chat-actions" wrap>
            <n-tag :type="statusTagType" size="small" :bordered="false" :title="statusTitle(dmPeer.name)">
              {{ statusLabel }}
            </n-tag>
            <n-button
              quaternary
              size="small"
              :loading="botsLoading"
              :title="t('peers.dmBotRefresh')"
              @click="refreshPeer"
            >
              <template #icon><n-icon><RefreshOutline /></n-icon></template>
            </n-button>
            <n-button quaternary size="small" @click="editCurrent">
              <template #icon><n-icon><CreateOutline /></n-icon></template>
              {{ t('peers.edit') }}
            </n-button>
            <n-button quaternary size="small" :disabled="!messages.length" @click="exportChat">
              <template #icon><n-icon><DownloadOutline /></n-icon></template>
              {{ t('peers.exportChat') }}
            </n-button>
            <!-- 清空会话：与导出同排（删除 Peer 在左栏行菜单里，头部不再重复） -->
            <n-popconfirm @positive-click="clearHistory">
              <template #trigger>
                <n-button
                  quaternary
                  size="small"
                  :disabled="waiting || !messages.length"
                  :title="t('peers.dmClearConfirm')"
                >
                  <template #icon><n-icon><TrashOutline /></n-icon></template>
                  {{ t('peers.dmClear') }}
                </n-button>
              </template>
              {{ t('peers.dmClearConfirm') }}
            </n-popconfirm>
          </n-space>
        </header>

        <!-- 远端 Bot 选择：全部公开的 Bot 常驻可见，当前目标高亮 -->
        <div class="chat-bots">
          <template v-if="botOptions.length">
            <button
              v-for="b in botOptions"
              :key="b.value"
              type="button"
              class="bot-chip"
              :class="{ active: dmForm.bot === b.value }"
              @click="dmForm.bot = b.value"
            >
              <n-avatar v-if="isImageAvatar(b.avatar)" round :size="24" :src="b.avatar" />
              <span v-else class="bot-chip-avatar">{{ botInitial(b.value) }}</span>
              <span class="bot-chip-label">{{ b.label }}</span>
            </button>
            <n-text depth="3" class="bots-count">{{ t('peers.botCount', { count: botOptions.length }) }}</n-text>
          </template>
          <n-text v-else-if="botsLoading" depth="3" class="bots-hint">{{ t('peers.dmBotLoading') }}</n-text>
          <template v-else>
            <n-text type="warning" class="bots-hint">
              {{ botsError ? t('peers.dmBotLoadFailed') : t('peers.dmBotNone') }}
            </n-text>
            <n-input
              v-model:value="dmForm.bot"
              size="small"
              :placeholder="t('peers.dmBotPlaceholder')"
              class="bots-manual"
            />
          </template>
          <n-popover v-model:show="showCustomBot" trigger="click" placement="bottom-start">
            <template #trigger>
              <button type="button" class="bot-chip add">+ {{ t('peers.dmCustomBot') }}</button>
            </template>
            <div class="bot-chip-custom">
              <n-input
                v-model:value="customBot"
                size="small"
                :placeholder="t('peers.dmBotPlaceholder')"
                @keyup.enter="applyCustomBot"
              />
              <n-text depth="3" class="bots-hint">{{ t('peers.dmBotHint') }}</n-text>
              <n-button size="small" type="primary" @click="applyCustomBot">{{ t('common.confirm') }}</n-button>
            </div>
          </n-popover>
        </div>

        <div class="chat-body-wrap">
          <div ref="dmListEl" class="chat-body" @scroll="onScroll">
            <div v-if="!dmForm.bot" class="chat-empty">
              <n-empty :description="t('peers.dmPickBot')" />
            </div>
            <div v-else-if="!messages.length && !waiting" class="chat-empty">
              <n-avatar round :size="54" :style="{ backgroundColor: avatarColor(dmForm.bot) }">
                {{ botInitial(dmForm.bot) }}
              </n-avatar>
              <div class="empty-name">{{ dmForm.bot }}</div>
              <n-text depth="3">{{ t('peers.dmEmpty') }}</n-text>
            </div>

            <template v-else>
              <template v-for="(group, dateKey) in groupedMessages" :key="dateKey || 'today'">
                <div v-if="dateKey" class="date-separator"><span>{{ dateKey }}</span></div>
                <div v-for="m in group" :key="m.id" class="msg" :class="m.role">
                  <template v-if="m.role === 'note'">
                    <div class="msg-note">{{ m.content }}</div>
                  </template>
                  <template v-else>
                    <div class="msg-avatar" :class="m.role === 'user' ? 'user-avatar' : 'bot-avatar'">
                      <img v-if="m.role !== 'user' && isImageAvatar(botAvatarOf(dmForm.bot))" :src="botAvatarOf(dmForm.bot)" alt="" />
                      <span v-else>{{ m.role === 'user' ? 'U' : botInitial(dmForm.bot) }}</span>
                    </div>
                    <div class="msg-body">
                      <div class="msg-head">
                        <span class="msg-sender">{{ m.role === 'user' ? t('bots.you') : '@' + dmForm.bot }}</span>
                        <span class="msg-time">{{ fmtTime(m.ts) }}</span>
                      </div>
                      <div v-if="m.role === 'bot'" class="msg-bubble bot">
                        <div class="msg-md" v-html="renderMd(m.content)"></div>
                        <div class="msg-foot">
                          <n-button text size="tiny" :title="t('peers.copy')" @click="copy(m.content)">
                            <template #icon><n-icon><CopyOutline /></n-icon></template>
                          </n-button>
                        </div>
                      </div>
                      <div v-else-if="m.role === 'user'" class="msg-bubble user">{{ m.content }}</div>
                      <div v-else class="msg-bubble error">
                        <div class="msg-err-title">{{ t('peers.dmFailed') }}</div>
                        <div class="msg-err-body">{{ m.content }}</div>
                        <n-button v-if="canRetry" size="tiny" type="warning" secondary @click="retryLast">{{ t('peers.dmRetry') }}</n-button>
                      </div>
                    </div>
                  </template>
                </div>
              </template>

              <div v-if="waiting" class="msg bot">
                <div class="msg-avatar bot-avatar">
                  <img v-if="isImageAvatar(botAvatarOf(dmForm.bot))" :src="botAvatarOf(dmForm.bot)" alt="" />
                  <span v-else>{{ botInitial(dmForm.bot) }}</span>
                </div>
                <div class="msg-body">
                  <div class="msg-head">
                    <span class="msg-sender">@{{ dmForm.bot }}</span>
                  </div>
                  <div class="msg-bubble bot typing">
                    <span class="typing-dot"></span><span class="typing-dot"></span><span class="typing-dot"></span>
                    <span class="typing-text">
                      {{ t('peers.dmWaiting') }}
                      <n-text depth="3" class="typing-elapsed">{{ t('peers.dmWaitingElapsed', { s: waitSeconds }) }}</n-text>
                    </span>
                  </div>
                </div>
              </div>
            </template>
          </div>

          <button v-if="showJump" type="button" class="jump-bottom" :title="t('peers.jumpToLatest')" @click="scrollToBottom(true)">
            <n-icon size="18"><ArrowDownOutline /></n-icon>
          </button>
        </div>

        <!-- 输入区：对齐 Bot 页 composer（边框卡片 + 右下角圆形发送键） -->
        <footer class="input-area">
          <div class="input-wrapper">
            <n-input
              v-model:value="dmForm.message"
              type="textarea"
              :autosize="{ minRows: 1, maxRows: 6 }"
              :placeholder="dmForm.bot ? t('peers.dmMessagePlaceholder') : t('peers.dmPickBot')"
              :disabled="!dmForm.bot || waiting"
              class="chat-input"
              :input-props="{ onKeydown: onComposerKeydown }"
            />

            <button
              class="send-btn-inline"
              :class="{ stopping: waiting }"
              :disabled="!waiting && !canSend"
              :title="waiting ? t('peers.dmStop') : t('peers.send')"
              @click="waiting ? stopWaiting() : sendDM()"
              @mousedown.prevent
            >
              <svg v-if="!waiting" viewBox="0 0 24 24" width="18" height="18" fill="currentColor">
                <path d="M3.4 20.4l17.45-7.48a1 1 0 000-1.84L3.4 3.6a.993.993 0 00-1.39.91L2 9.12c0 .5.37.93.87.99L17 12 2.87 13.88c-.5.07-.87.5-.87 1l.01 4.61c0 .71.73 1.2 1.39.91z" />
              </svg>
              <svg v-else viewBox="0 0 24 24" width="16" height="16" fill="currentColor">
                <rect x="6" y="6" width="12" height="12" rx="2" />
              </svg>
            </button>
          </div>

          <!-- 提示行：等待时换成计时，省掉再堆一个「停止等待」按钮 -->
          <div class="input-hint">
            <span v-if="waiting">{{ t('peers.dmWaitingElapsed', { s: waitSeconds }) }}</span>
            <span v-else>{{ t('peers.dmHint') }}</span>
          </div>
        </footer>
      </template>
    </section>

    <!-- ===== 本机身份：对方添加本机时需要的地址与实例 ID ===== -->
    <n-drawer v-model:show="showIdentity" :width="420">
      <n-drawer-content :title="t('peers.identityTitle')" closable>
        <n-descriptions :column="1" label-placement="left" size="small">
          <n-descriptions-item :label="t('peers.baseUrl')">
            <n-space align="center" :size="8">
              <n-text code>{{ identityUrl }}</n-text>
              <n-button size="tiny" quaternary @click="copy(identityUrl)">
                <template #icon><n-icon><CopyOutline /></n-icon></template>
                {{ t('peers.copy') }}
              </n-button>
            </n-space>
          </n-descriptions-item>
          <n-descriptions-item :label="t('peers.instanceId')">
            <n-space align="center" :size="8">
              <n-text code>{{ overview.instance_id || '—' }}</n-text>
              <n-button v-if="overview.instance_id" size="tiny" quaternary @click="copy(overview.instance_id)">
                <template #icon><n-icon><CopyOutline /></n-icon></template>
                {{ t('peers.copy') }}
              </n-button>
            </n-space>
          </n-descriptions-item>
          <n-descriptions-item :label="t('peers.magicHome')">
            <n-text code>{{ overview.magic_home || '—' }}</n-text>
          </n-descriptions-item>
        </n-descriptions>
        <n-text class="peer-hint" depth="3">{{ t('peers.identityHint') }}</n-text>
      </n-drawer-content>
    </n-drawer>

    <!-- ===== 添加 / 编辑 Peer ===== -->
    <n-modal
      v-model:show="showAdd"
      preset="card"
      class="modal-responsive"
      style="width: 520px; max-width: 96vw;"
      :title="editingName ? t('peers.editTitle') : t('peers.addTitle')"
    >
      <n-form label-placement="top">
        <n-form-item :label="t('peers.name')">
          <n-input v-model:value="form.name" :disabled="!!editingName" :placeholder="t('peers.namePlaceholder')" />
          <template #feedback>
            <n-text depth="3" class="field-hint">{{ t('peers.nameHint') }}</n-text>
          </template>
        </n-form-item>
        <n-form-item :label="t('peers.baseUrl')">
          <n-input v-model:value="form.base_url" :placeholder="t('peers.baseUrlPlaceholder')" />
          <template #feedback>
            <n-text depth="3" class="field-hint">{{ t('peers.baseUrlHint') }}</n-text>
          </template>
        </n-form-item>
        <n-form-item :label="t('peers.token')">
          <n-input
            v-model:value="form.token"
            type="password"
            show-password-on="click"
            :placeholder="editingName ? t('peers.tokenKeepPlaceholder') : t('peers.tokenPlaceholder')"
          />
          <template #feedback>
            <n-text depth="3" class="field-hint">
              {{ editingName ? t('peers.tokenKeepHint') : t('peers.tokenHint') }}
            </n-text>
          </template>
        </n-form-item>
      </n-form>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showAdd = false">{{ t('common.cancel') }}</n-button>
          <n-button type="primary" :loading="saving" @click="savePeer">{{ t('common.save') }}</n-button>
        </n-space>
      </template>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onActivated, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { useDialog, useMessage } from 'naive-ui'
import {
  NAvatar, NButton, NDescriptions, NDescriptionsItem, NDrawer, NDrawerContent, NDropdown, NEmpty,
  NForm, NFormItem, NIcon, NInput, NModal, NPopconfirm, NPopover, NSpace, NSpin, NTag, NText,
} from 'naive-ui'
import {
  AddOutline, AlertCircleOutline, ArrowBackOutline, ArrowDownOutline, CopyOutline, CreateOutline,
  DownloadOutline, EllipsisHorizontalOutline, IdCardOutline, PulseOutline, RefreshOutline,
  SearchOutline, TrashOutline,
} from '@vicons/ionicons5'
import { useI18n } from 'vue-i18n'
import { marked } from 'marked'
import {
  addPeer, deletePeer, getPeers, listPeerBots, pingPeer, sendPeerDM, updatePeer,
  type PeerBotInfo, type PeerInfo,
} from '@/api/peers'

const { t } = useI18n()
const message = useMessage()
const dialog = useDialog()

// ===== 列表 / 身份 =====
const loading = ref(false)
const saving = ref(false)
const peers = ref<PeerInfo[]>([])
const overview = ref<{ instance_id: string; magic_home: string }>({ instance_id: '', magic_home: '' })
const search = ref('')
const showIdentity = ref(false)
// A remote operator needs the address this dashboard is reachable at; the
// browser already knows it, so no config round-trip is required.
const identityUrl = window.location.origin

const filteredPeers = computed(() => {
  const q = search.value.trim().toLowerCase()
  if (!q) return peers.value
  return peers.value.filter(
    (p) => p.name.toLowerCase().includes(q) || p.base_url.toLowerCase().includes(q),
  )
})

// ===== 连通性（每个 peer 一个状态点） =====
// 远端从没被私聊过时没有任何状态，仅凭 DM 结果无法显示在线与否；
// 页面打开时对每个 peer 做一次短超时探测（服务端 6s 上限）。
interface PeerStatus {
  state: 'unknown' | 'checking' | 'online' | 'offline'
  latency?: number
  bots?: number
  error?: string
  at?: number
}
const statuses = reactive<Record<string, PeerStatus>>({})
const probeSeq = new Map<string, number>()
const allProbing = ref(false)

function statusOf(name: string): PeerStatus {
  return statuses[name] ?? { state: 'unknown' }
}

const onlineCount = computed(
  () => peers.value.filter((p) => statusOf(p.name).state === 'online').length,
)
const totalRemoteBots = computed(
  () => peers.value.reduce((n, p) => n + (statusOf(p.name).bots ?? 0), 0),
)

function statusClass(name: string): string {
  return statusOf(name).state
}

function statusTitle(name: string): string {
  const s = statusOf(name)
  switch (s.state) {
    case 'checking':
      return t('peers.checking')
    case 'online':
      return s.latency == null
        ? `${t('peers.online')} · ${t('peers.botCount', { count: s.bots ?? 0 })}`
        : `${t('peers.online')} · ${t('peers.botCount', { count: s.bots ?? 0 })} · ${t('peers.latency', { ms: s.latency })}`
    case 'offline':
      return s.error || t('peers.offline')
    default:
      return t('peers.neverChecked')
  }
}

const currentStatus = computed<PeerStatus>(() =>
  dmPeer.value ? statusOf(dmPeer.value.name) : { state: 'unknown' },
)
const statusTagType = computed<'success' | 'error' | 'default'>(() => {
  switch (currentStatus.value.state) {
    case 'online':
      return 'success'
    case 'offline':
      return 'error'
    default:
      return 'default'
  }
})
const statusLabel = computed(() => {
  const s = currentStatus.value
  switch (s.state) {
    case 'checking':
      return t('peers.checking')
    case 'online':
      return s.latency == null
        ? t('peers.online')
        : `${t('peers.online')} · ${t('peers.latency', { ms: s.latency })}`
    case 'offline':
      return t('peers.offline')
    default:
      return t('peers.neverChecked')
  }
})

async function probePeer(name: string, force = false) {
  const cur = statuses[name]
  // 刚拿过结果（例如打开页面时顺带拉到的 Bot 列表）就不必再打一次远端
  if (!force && cur && Date.now() - (cur.at ?? 0) < 15000) return
  const seq = (probeSeq.get(name) ?? 0) + 1
  probeSeq.set(name, seq)
  if (!statuses[name] || statuses[name].state !== 'online') {
    statuses[name] = { ...statuses[name], state: 'checking' }
  }
  try {
    const res = await pingPeer(name)
    if (probeSeq.get(name) !== seq) return
    statuses[name] = { state: 'online', latency: res.latency_ms, bots: res.bot_count, at: Date.now() }
  } catch (e) {
    if (probeSeq.get(name) !== seq) return
    statuses[name] = { state: 'offline', error: msgOf(e), at: Date.now() }
  }
}

async function probeAll() {
  if (!peers.value.length || allProbing.value) return
  allProbing.value = true
  try {
    await Promise.all(peers.value.map((p) => probePeer(p.name)))
  } finally {
    allProbing.value = false
  }
}

// ===== 选中 / 会话切换 =====
const selectedPeer = ref<string | null>(null)
const dmPeer = ref<PeerInfo | null>(null)
const dmForm = reactive({ bot: '', message: '' })

const remoteBots = ref<PeerBotInfo[]>([])
const botsLoading = ref(false)
const botsError = ref(false)

const botOptions = computed(() =>
  remoteBots.value.map((b) => ({
    label: b.title ? `${b.name} · ${b.title}` : b.name,
    value: b.name,
    avatar: b.avatar || '',
  })),
)

const showCustomBot = ref(false)
const customBot = ref('')

const isMobile = ref(window.innerWidth <= 768)

function onResize() {
  isMobile.value = window.innerWidth <= 768
  if (!isMobile.value && !selectedPeer.value && peers.value.length) {
    selectPeer(peers.value[0])
  }
}

function leaveChat() {
  selectedPeer.value = null
  dmPeer.value = null
}

function selectPeer(p: PeerInfo) {
  if (selectedPeer.value === p.name) return
  selectedPeer.value = p.name
  dmPeer.value = p
  dmForm.message = ''
  remoteBots.value = []
  botsError.value = false
  showCustomBot.value = false
  customBot.value = ''
  // 先清空再恢复该 peer 上次的 Bot：同名 Bot 出现在不同 peer 上时，
  // 直接赋值不会触发 watcher，会话就串到上一个 peer 的历史里了。
  dmForm.bot = ''
  const lastBot = readLastBot(p.name)
  if (lastBot) dmForm.bot = lastBot
  loadPeerBots()
  nextTick(() => scrollToBottom(true))
}

function applyCustomBot() {
  const name = customBot.value.trim()
  if (!name) return
  dmForm.bot = name
  customBot.value = ''
  showCustomBot.value = false
}

function botInitial(name: string): string {
  return (name.trim()[0] || '?').toUpperCase()
}

function botAvatarOf(name: string): string {
  return remoteBots.value.find((b) => b.name === name)?.avatar || ''
}

function isImageAvatar(avatar?: string): boolean {
  if (!avatar) return false
  return /^(https?:\/\/|data:image\/)/.test(avatar.trim())
}

// ===== 会话历史：按「peer + bot」分线程 =====
// 之前的实现只有一个 chatHistory 数组：切到别的 peer 后，上一轮迟到的回复
// 会被 append 到"当前"数组并持久化到"旧" key，两个会话互相污染。
// 改成 threads[key] 后，回复永远落回它自己的线程。
interface PeerChatMsg {
  id: string
  role: 'user' | 'bot' | 'error' | 'note'
  content: string
  ts: number
}

const HISTORY_LIMIT = 200
let msgSeq = 0
const threads = reactive<Record<string, PeerChatMsg[]>>({})
const pending = reactive<Record<string, { startedAt: number; controller: AbortController }>>({})
const previewVersion = ref(0)
const tick = ref(0)
let clock: ReturnType<typeof setInterval> | null = null

const historyKey = computed(() =>
  dmPeer.value && dmForm.bot ? threadKey(dmPeer.value.name, dmForm.bot) : '',
)
const messages = computed<PeerChatMsg[]>(() =>
  historyKey.value ? threads[historyKey.value] ?? [] : [],
)
const waiting = computed(() => !!(historyKey.value && pending[historyKey.value]))
const waitSeconds = computed(() => {
  void tick.value // 让计时随 tick 重算
  const key = historyKey.value
  const job = key ? pending[key] : null
  if (!job) return 0
  return Math.max(0, Math.floor((Date.now() - job.startedAt) / 1000))
})
const canSend = computed(() => !!dmForm.bot.trim() && !!dmForm.message.trim() && !waiting.value)

function threadKey(peer: string, bot: string): string {
  return `gm.peer-dm.${peer}.${bot}`
}

function lastBotKey(peer: string): string {
  return `gm.peer-dm.lastbot.${peer}`
}

function readLastBot(peer: string): string {
  try {
    return localStorage.getItem(lastBotKey(peer)) || ''
  } catch {
    return ''
  }
}

function readThread(key: string): PeerChatMsg[] {
  try {
    const raw = localStorage.getItem(key)
    if (!raw) return []
    const parsed = JSON.parse(raw)
    return Array.isArray(parsed) ? (parsed as PeerChatMsg[]) : []
  } catch {
    // 损坏的历史不该挡住聊天：直接当空会话处理
    return []
  }
}

function loadThread(key: string): PeerChatMsg[] {
  if (!threads[key]) threads[key] = readThread(key)
  return threads[key]
}

function persistThread(key: string) {
  const list = threads[key]
  if (!list) return
  try {
    localStorage.setItem(key, JSON.stringify(list.slice(-HISTORY_LIMIT)))
  } catch {
    // 配额不足等：内存里的会话仍然可用
  }
}

function appendTo(key: string, role: PeerChatMsg['role'], content: string) {
  const list = loadThread(key)
  list.push({ id: `m${Date.now()}-${msgSeq++}`, role, content, ts: Date.now() })
  persistThread(key)
  previewVersion.value++
  if (key === historyKey.value && role !== 'note') nextTick(() => scrollToBottom(true))
}

function clearHistory() {
  const key = historyKey.value
  if (!key) return
  threads[key] = []
  persistThread(key)
  previewVersion.value++
  message.success(t('peers.dmCleared'))
}

watch(historyKey, (key) => {
  if (key) loadThread(key)
  nextTick(() => scrollToBottom(true))
}, { immediate: true })

// 记住每个 peer 上次对话的 Bot，回到该 peer 时恢复上下文
watch(() => dmForm.bot, (bot) => {
  if (dmPeer.value && bot) {
    try {
      localStorage.setItem(lastBotKey(dmPeer.value.name), bot)
    } catch {
      // localStorage 不可用：只是不记忆上次的 Bot
    }
  }
})

// 列表预览：最后一条消息
function lastActivity(peer: string): { ts: number; text: string } | null {
  void previewVersion.value
  try {
    const lastBot = readLastBot(peer)
    if (!lastBot) return null
    const key = threadKey(peer, lastBot)
    const msgs = threads[key] ?? readThread(key)
    const last = msgs[msgs.length - 1]
    if (!last) return null
    const prefix = last.role === 'error' ? `${t('peers.dmFailed')}: ` : ''
    return { ts: last.ts, text: (prefix + last.content).replace(/\s+/g, ' ').slice(0, 80) }
  } catch {
    return null
  }
}

function previewOf(p: PeerInfo): string {
  const a = lastActivity(p.name)
  return a ? a.text : t('peers.dmIdle')
}

function lastTimeOf(p: PeerInfo): string {
  const a = lastActivity(p.name)
  return a ? fmtTime(a.ts) : ''
}

// ===== 远端 Bot 列表 =====
async function loadPeerBots() {
  const peer = dmPeer.value
  if (!peer) return
  botsLoading.value = true
  botsError.value = false
  // 这次调用的往返本身就是一次连通性测量：不必再额外打一次 ping
  const startedAt = Date.now()
  try {
    const res = await listPeerBots(peer.name)
    // 期间用户可能切到别的 peer：只认当前 peer 的结果
    if (dmPeer.value?.name !== peer.name) return
    remoteBots.value = res.bots || []
    statuses[peer.name] = {
      state: 'online',
      bots: remoteBots.value.length,
      latency: Date.now() - startedAt,
      at: Date.now(),
    }
    if (!dmForm.bot && remoteBots.value.length === 1) dmForm.bot = remoteBots.value[0].name
  } catch (e) {
    if (dmPeer.value?.name !== peer.name) return
    botsError.value = true
    statuses[peer.name] = { state: 'offline', error: msgOf(e), at: Date.now() }
  } finally {
    botsLoading.value = false
  }
}

function refreshPeer() {
  if (!dmPeer.value) return
  probePeer(dmPeer.value.name, true)
  loadPeerBots()
}

// ===== 添加 / 编辑 Peer =====
const showAdd = ref(false)
const editingName = ref<string | null>(null)
const form = reactive({ name: '', base_url: '', token: '' })

function openAdd() {
  editingName.value = null
  form.name = ''
  form.base_url = ''
  form.token = ''
  showAdd.value = true
}

function startEdit(p: PeerInfo) {
  editingName.value = p.name
  form.name = p.name
  form.base_url = p.base_url
  form.token = ''
  showAdd.value = true
}

// 模板里的事件回调拿不到 v-if 收窄后的非空类型，统一在这转一次。
// （删除 Peer 只保留左栏行菜单一个入口，头部不再有 removeCurrent。）
function editCurrent() {
  if (dmPeer.value) startEdit(dmPeer.value)
}

async function savePeer() {
  if (!form.name.trim() || !form.base_url.trim()) {
    message.warning(t('peers.fillRequired'))
    return
  }
  saving.value = true
  try {
    if (editingName.value) {
      await updatePeer(editingName.value, {
        base_url: form.base_url.trim(),
        token: form.token.trim(),
      })
      message.success(t('peers.updated'))
      showAdd.value = false
      const name = editingName.value
      editingName.value = null
      await load()
      // 地址可能变了：立刻重探，别让旧状态点继续骗人
      remoteBots.value = []
      probePeer(name, true)
      loadPeerBots()
    } else {
      const created = await addPeer({
        name: form.name.trim(),
        base_url: form.base_url.trim(),
        token: form.token.trim(),
      })
      message.success(t('peers.created'))
      showAdd.value = false
      await load()
      const added = peers.value.find((p) => p.name === (created?.name || form.name.trim()))
      if (added) selectPeer(added)
      probePeer(created?.name || form.name.trim(), true)
    }
  } catch (e) {
    message.error(msgOf(e))
  } finally {
    saving.value = false
  }
}

async function load() {
  loading.value = true
  try {
    const data = await getPeers()
    overview.value = { instance_id: data.instance_id, magic_home: data.magic_home }
    peers.value = data.peers || []
    if (selectedPeer.value) {
      const current = peers.value.find((p) => p.name === selectedPeer.value)
      if (current) dmPeer.value = current
      else {
        selectedPeer.value = null
        dmPeer.value = null
      }
    } else if (peers.value.length && !isMobile.value) {
      selectPeer(peers.value[0])
    }
  } catch (e) {
    message.error(msgOf(e))
  } finally {
    loading.value = false
  }
}

async function removePeer(p: PeerInfo) {
  try {
    await deletePeer(p.name)
    message.success(t('peers.deleted'))
    if (selectedPeer.value === p.name) {
      selectedPeer.value = null
      dmPeer.value = null
    }
    delete statuses[p.name]
    await load()
  } catch (e) {
    message.error(msgOf(e))
  }
}

// 行内菜单：对齐 Bot 页的 hover 操作入口
const rowMenuOptions = computed(() => [
  { label: t('peers.checkTitle'), key: 'probe' },
  { label: t('peers.edit'), key: 'edit' },
  { label: t('peers.copy'), key: 'copy' },
  { type: 'divider', key: 'd1' },
  { label: t('common.delete'), key: 'delete', props: { style: 'color: #d03050' } },
])

function onRowMenu(key: string, p: PeerInfo) {
  switch (key) {
    case 'probe':
      probePeer(p.name, true)
      break
    case 'edit':
      startEdit(p)
      break
    case 'copy':
      copy(p.base_url)
      break
    case 'delete':
      confirmDelete(p)
      break
  }
}

// 行内菜单删除：给一次确认，避免误触把链路删掉
function confirmDelete(p: PeerInfo) {
  dialog.warning({
    title: t('peers.deleteConfirm', { name: p.name }),
    content: p.base_url,
    positiveText: t('common.delete'),
    negativeText: t('common.cancel'),
    onPositiveClick: () => { removePeer(p) },
  })
}

// ===== 发送 / 停止 / 重试 =====
const lastAttempt = ref<{ peer: string; bot: string; message: string } | null>(null)
// 重试只对"本次会话里刚失败的那一条"有意义：刷新页面后没有可重放的文本
const canRetry = computed(
  () => !!lastAttempt.value && !!dmPeer.value && lastAttempt.value.peer === dmPeer.value.name,
)

function onComposerKeydown(e: KeyboardEvent) {
  // Enter 发送、Shift+Enter 换行；isComposing 挡掉中文输入法确认候选词的那一次 Enter
  if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
    e.preventDefault()
    sendDM()
  }
}

async function sendDM(explicitBot?: string, explicitContent?: string) {
  const peer = dmPeer.value
  if (!peer || waiting.value) return
  const bot = (explicitBot ?? dmForm.bot).trim()
  const content = (explicitContent ?? dmForm.message).trim()
  if (!bot || !content) {
    message.warning(t('peers.fillDm'))
    return
  }
  const key = threadKey(peer.name, bot)
  if (pending[key]) return

  if (!explicitContent) dmForm.message = ''
  lastAttempt.value = { peer: peer.name, bot, message: content }
  appendTo(key, 'user', content)

  const controller = new AbortController()
  pending[key] = { startedAt: Date.now(), controller }
  startClock()
  try {
    const res = await sendPeerDM(peer.name, bot, content, controller.signal)
    appendTo(key, 'bot', res.reply || '')
  } catch (e) {
    if (isAbortError(e)) appendTo(key, 'note', t('peers.dmStopped'))
    else appendTo(key, 'error', msgOf(e))
  } finally {
    delete pending[key]
    stopClockIfIdle()
  }
}

// 只中断"当前正在等的这一轮"：远端回合仍在跑，只是浏览器不再等它。
function stopWaiting() {
  const key = historyKey.value
  if (!key) return
  pending[key]?.controller.abort()
}

function retryLast() {
  const last = lastAttempt.value
  if (!last || waiting.value) return
  if (dmPeer.value?.name !== last.peer) return
  if (dmForm.bot !== last.bot) dmForm.bot = last.bot
  sendDM(last.bot, last.message)
}

function startClock() {
  if (clock) return
  clock = setInterval(() => { tick.value++ }, 1000)
}

function stopClockIfIdle() {
  if (Object.keys(pending).length === 0 && clock) {
    clearInterval(clock)
    clock = null
  }
}

function isAbortError(e: unknown): boolean {
  return e instanceof DOMException ? e.name === 'AbortError' : (e as Error)?.name === 'AbortError'
}

function msgOf(e: unknown): string {
  return e instanceof Error ? e.message : t('common.error')
}

// ===== 滚动 / 渲染 =====
const dmListEl = ref<HTMLElement | null>(null)
const showJump = ref(false)

function onScroll() {
  const el = dmListEl.value
  if (!el) return
  showJump.value = el.scrollHeight - el.scrollTop - el.clientHeight > 120
}

function scrollToBottom(force = false) {
  const el = dmListEl.value
  if (!el) return
  if (force || !showJump.value) {
    el.scrollTop = el.scrollHeight
    showJump.value = false
  }
}

function fmtTime(ts: number): string {
  return new Date(ts).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

// 日期分隔：今天不显示，昨天用词条，其余显示日期（与 Bot 页一致）
function formatDate(ts: number): string {
  if (!ts) return ''
  const d = new Date(ts)
  if (isNaN(d.getTime())) return ''
  const now = new Date()
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  const yesterday = new Date(today.getTime() - 86400000)
  const day = new Date(d.getFullYear(), d.getMonth(), d.getDate())
  if (day.getTime() === today.getTime()) return ''
  if (day.getTime() === yesterday.getTime()) return t('peers.yesterday')
  return d.toLocaleDateString([], { month: 'short', day: 'numeric', year: 'numeric' })
}

const groupedMessages = computed(() => {
  const groups: Record<string, PeerChatMsg[]> = {}
  for (const m of messages.value) {
    const key = formatDate(m.ts)
    if (!groups[key]) groups[key] = []
    groups[key].push(m)
  }
  return groups
})

marked.use({ breaks: true, gfm: true })
function renderMd(content: string): string {
  if (!content) return ''
  try {
    return marked.parse(content) as string
  } catch {
    return ''
  }
}

function exportChat() {
  const peer = dmPeer.value
  const bot = dmForm.bot
  if (!peer || !bot || !messages.value.length) return
  const body = messages.value
    .filter((m) => m.role !== 'note')
    .map((m) => {
      const who = m.role === 'user' ? t('bots.you') : `@${bot}`
      return `### ${who} · ${new Date(m.ts).toLocaleString()}\n\n${m.content}`
    })
    .join('\n\n---\n\n')
  const blob = new Blob([`# ${peer.name} · @${bot} (${peer.base_url})\n\n${body}\n`], {
    type: 'text/markdown;charset=utf-8',
  })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `peer-${peer.name}-${bot}.md`
  a.click()
  URL.revokeObjectURL(url)
  message.success(t('peers.exported'))
}

// 头像是名字哈希到固定调色板，同一个 peer 每次都是同一颜色
const palette = ['#4f7cff', '#18a058', '#f0a020', '#8b5cf6', '#e0559a', '#0ea5e9', '#14b8a6', '#f97316']
function avatarColor(name: string): string {
  let h = 0
  for (let i = 0; i < name.length; i++) h = (h * 31 + name.charCodeAt(i)) % 997
  return palette[h % palette.length]
}

function avatarText(name: string): string {
  const n = name.replace(/^@/, '').trim()
  return n ? n.slice(0, 2).toUpperCase() : 'P'
}

async function copy(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    message.success(t('peers.copied'))
  } catch {
    message.error(t('peers.copyFailed'))
  }
}

onMounted(async () => {
  window.addEventListener('resize', onResize)
  await load()
  probeAll()
})

// 页面被 keep-alive 缓存：切回来时刷新连通性，避免状态点停留在几分钟前
onActivated(() => {
  if (peers.value.length) probeAll()
})

onUnmounted(() => {
  window.removeEventListener('resize', onResize)
  if (clock) clearInterval(clock)
})
</script>

<style scoped>
/* ===== 两栏：Peer 列表 + 会话（信息层级参考 Bot 页） =====
   与 BotsView.bots-shell 一致：整屏铺满、无外边距/圆角（/peers 在 App.vue 里
   走 full-content，内容区 padding 为 0），高度用 100dvh 兼容移动端地址栏收起。 */
.peers-shell {
  display: flex;
  height: 100vh;
  height: 100dvh;
  min-height: 0;
  overflow: hidden;
}

/* ----- 左栏 ----- */
.peer-rail {
  width: 300px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  border-right: 1px solid #eeeff3;
  background: #fafafc;
}

.rail-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 14px 14px 8px;
  flex-shrink: 0;
}

.rail-heading {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.rail-title {
  margin: 0;
  font-size: 16px;
  font-weight: 700;
  color: #1f2937;
}

.rail-sub {
  font-size: 12px;
}

.rail-search {
  padding: 0 14px 8px;
  flex-shrink: 0;
}

.peer-list {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 2px 8px 8px;
}

.list-placeholder {
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 40px 0;
}

/* ----- Peer 行 ----- */
.peer-row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 10px;
  border-radius: 10px;
  cursor: pointer;
  position: relative;
  transition: background 0.15s;
}

.peer-row:hover {
  background: rgba(0, 0, 0, 0.045);
}

.peer-row.active {
  background: rgba(79, 124, 255, 0.12);
}

.peer-row.active::before {
  content: '';
  position: absolute;
  left: 0;
  top: 50%;
  transform: translateY(-50%);
  width: 3px;
  height: 22px;
  border-radius: 2px;
  background: #4f7cff;
}

.row-avatar {
  flex-shrink: 0;
  color: #fff;
  font-size: 13px;
  font-weight: 600;
}

.row-main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 1px;
}

.row-name {
  display: flex;
  align-items: center;
  gap: 5px;
  min-width: 0;
}

.row-name-text {
  font-size: 13px;
  font-weight: 600;
  color: #1f2937;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.row-token-warn {
  color: #f0a020;
  flex-shrink: 0;
}

.row-sub {
  font-size: 12px;
  color: #9ca3af;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.row-meta {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 5px;
  flex-shrink: 0;
}

.row-time {
  font-size: 11px;
  color: #b8bcc6;
}

.row-status,
.chip-status {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #d1d5db;
  flex-shrink: 0;
}

.row-status.online,
.chip-status.online {
  background: #18a058;
}

.row-status.offline,
.chip-status.offline {
  background: #e0556b;
}

.row-status.checking,
.chip-status.checking {
  background: #f0a020;
  animation: status-pulse 1.1s ease-in-out infinite;
}

@keyframes status-pulse {
  0%, 100% { opacity: 0.35; }
  50% { opacity: 1; }
}

.row-menu {
  flex-shrink: 0;
  opacity: 0;
  transition: opacity 0.15s;
}

.peer-row:hover .row-menu,
.row-menu:focus-within {
  opacity: 1;
}

.rail-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 6px 10px;
  border-top: 1px solid #eeeff3;
  flex-shrink: 0;
}

/* ----- 右栏 ----- */
.peer-pane {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  background: #fff;
}

.welcome {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  overflow-y: auto;
}

.welcome-inner {
  max-width: 560px;
  text-align: center;
}

.welcome-greeting {
  font-size: 26px;
  font-weight: 700;
  color: #1f2937;
  margin-bottom: 8px;
}

.welcome-sub {
  color: #6b7280;
  font-size: 14px;
  margin: 0 0 26px;
}

.welcome-stats {
  margin-bottom: 8px;
}

.stat-card {
  min-width: 108px;
  padding: 14px 18px;
  border-radius: 12px;
  background: #fafafa;
  border: 1px solid #eee;
}

.stat-value {
  font-size: 24px;
  font-weight: 700;
  color: #1f2937;
}

.stat-value.stat-online {
  color: #18a058;
}

.stat-label {
  font-size: 12px;
  color: #9ca3af;
  margin-top: 2px;
}

.welcome-pick {
  margin-top: 22px;
  font-size: 13px;
  color: #9ca3af;
}

.welcome-chips {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  justify-content: center;
  margin-top: 14px;
}

.welcome-chip {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 12px 6px 6px;
  border-radius: 999px;
  border: 1px solid #e5e7eb;
  background: #fff;
  cursor: pointer;
  transition: border-color 0.15s, box-shadow 0.15s, transform 0.15s;
}

.welcome-chip:hover {
  border-color: #4f7cff;
  box-shadow: 0 2px 8px rgba(79, 124, 255, 0.15);
  transform: translateY(-1px);
}

.chip-name {
  font-size: 13px;
  font-weight: 600;
  color: #374151;
}

.welcome-add {
  margin-top: 22px;
}

/* ----- 会话头 ----- */
.chat-header {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 12px 16px;
  border-bottom: 1px solid #eeeff3;
  flex-shrink: 0;
}

/* 移动端返回按钮：桌面隐藏 */
.back-btn {
  display: none;
}

.chat-title-text {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-width: 0;
  line-height: 1.35;
}

.chat-name {
  font-size: 16px;
  font-weight: 700;
  color: #1f2937;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.chat-url {
  font-size: 12px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.chat-actions {
  flex-shrink: 0;
}

/* ----- 远端 Bot 选择条 ----- */
.chat-bots {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  padding: 10px 16px;
  border-bottom: 1px solid #eeeff3;
  flex-shrink: 0;
}

.bot-chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  max-width: 100%;
  padding: 3px 12px 3px 4px;
  border: 1px solid #e9eaf0;
  border-radius: 999px;
  background: #fafafc;
  font-size: 13px;
  color: inherit;
  cursor: pointer;
  transition: border-color 0.15s, background-color 0.15s, color 0.15s;
}

.bot-chip:hover {
  border-color: rgba(79, 124, 255, 0.5);
}

.bot-chip.active {
  border-color: #4f7cff;
  background: rgba(79, 124, 255, 0.1);
  color: #3a5fd9;
  font-weight: 600;
}

.bot-chip-avatar {
  width: 24px;
  height: 24px;
  border-radius: 50%;
  background: #e8efff;
  color: #4f7cff;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: 12px;
  font-weight: 600;
  flex-shrink: 0;
}

.bot-chip.active .bot-chip-avatar {
  background: #4f7cff;
  color: #fff;
}

.bot-chip-label {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.bot-chip.add {
  padding: 4px 12px;
  border-style: dashed;
  color: #888;
}

.bots-count,
.bots-hint {
  font-size: 12px;
}

.bots-manual {
  width: 220px;
}

.bot-chip-custom {
  display: flex;
  flex-direction: column;
  gap: 8px;
  width: 250px;
}

/* ----- 消息区 ----- */
.chat-body-wrap {
  flex: 1;
  min-height: 0;
  position: relative;
  display: flex;
}

.chat-body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 18px 20px 24px;
  display: flex;
  flex-direction: column;
}

.chat-empty {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 40px 16px;
}

.empty-name {
  font-size: 18px;
  font-weight: 700;
  color: #1f2937;
}

.date-separator {
  display: flex;
  justify-content: center;
  margin: 10px 0 14px;
}

.date-separator span {
  font-size: 11px;
  color: #9ca3af;
  background: #f4f5f7;
  padding: 2px 10px;
  border-radius: 999px;
}

/* 消息布局与 Bot 页一致：头像在左/右，正文限宽 */
.msg {
  display: flex;
  gap: 10px;
  margin-bottom: 16px;
  max-width: 960px;
  width: 100%;
  margin-left: auto;
  margin-right: auto;
}

.msg.user {
  flex-direction: row-reverse;
}

.msg-avatar {
  width: 32px;
  height: 32px;
  border-radius: 50%;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  font-size: 12px;
  font-weight: 600;
  overflow: hidden;
  user-select: none;
}

.msg-avatar img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  border-radius: 50%;
}

.user-avatar {
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
}

.bot-avatar {
  background: linear-gradient(135deg, #18a058 0%, #36ad6a 100%);
}

.msg-body {
  max-width: 78%;
  min-width: 0;
  display: flex;
  flex-direction: column;
}

.msg.user .msg-body {
  align-items: flex-end;
}

.msg-head {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 4px;
  font-size: 12px;
}

.msg.user .msg-head {
  flex-direction: row-reverse;
}

.msg-sender {
  font-weight: 600;
  color: #333;
}

.msg-time {
  font-size: 11px;
  color: #bbb;
}

.msg-bubble {
  padding: 10px 14px;
  border-radius: 14px;
  font-size: 13px;
  line-height: 1.7;
  word-break: break-word;
  overflow-wrap: break-word;
}

.msg-bubble.user {
  background: linear-gradient(135deg, #4f7cff 0%, #3a5fd9 100%);
  color: #fff;
  white-space: pre-wrap;
  border-bottom-right-radius: 4px;
}

.msg-bubble.bot {
  background: #fff;
  color: #1f2937;
  border: 1px solid #e8e8e8;
  border-bottom-left-radius: 4px;
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.05);
}

.msg-bubble.error {
  background: #fff5f6;
  border: 1px solid #f0c8cf;
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 6px;
}

.msg-err-title {
  font-weight: 600;
  color: #b5334a;
  font-size: 12px;
}

.msg-err-body {
  color: #b5533c;
  font-size: 12px;
  white-space: pre-wrap;
}

.msg-foot {
  display: flex;
  justify-content: flex-end;
  margin-top: 2px;
  opacity: 0;
  transition: opacity 0.15s;
}

.msg-bubble.bot:hover .msg-foot {
  opacity: 1;
}

/* 系统提示（例如"已停止等待"）居中、无气泡 */
.msg.note {
  justify-content: center;
  margin-bottom: 12px;
}

.msg-note {
  font-size: 12px;
  color: #9ca3af;
  background: #f6f7f9;
  padding: 3px 12px;
  border-radius: 999px;
}

/* Markdown 正文 */
.msg-md :deep(p) {
  margin: 0 0 8px;
}

.msg-md :deep(p:last-child) {
  margin-bottom: 0;
}

.msg-md :deep(pre) {
  background: #1e1e1e;
  color: #e8e8e8;
  padding: 10px;
  border-radius: 6px;
  overflow-x: auto;
  font-size: 12px;
  margin: 6px 0;
}

.msg-md :deep(code) {
  font-size: 12px;
}

.msg-md :deep(ul),
.msg-md :deep(ol) {
  margin: 6px 0;
  padding-left: 20px;
}

.msg-md :deep(table) {
  border-collapse: collapse;
  margin: 6px 0;
  font-size: 12px;
}

.msg-md :deep(th),
.msg-md :deep(td) {
  border: 1px solid #ddd;
  padding: 4px 8px;
}

/* 等待态：三点动效 + 秒数 */
.msg-bubble.typing {
  display: flex;
  align-items: center;
  gap: 4px;
}

.typing-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: #18a058;
  display: inline-block;
  animation: typing-bounce 1.4s ease-in-out infinite;
}

.typing-dot:nth-child(2) {
  animation-delay: 0.2s;
}

.typing-dot:nth-child(3) {
  animation-delay: 0.4s;
}

@keyframes typing-bounce {
  0%, 60%, 100% { transform: translateY(0); opacity: 0.4; }
  30% { transform: translateY(-4px); opacity: 1; }
}

.typing-text {
  margin-left: 6px;
  font-size: 12px;
  color: #4b5563;
}

.typing-elapsed {
  font-size: 12px;
  margin-left: 2px;
}

/* 回到底部 */
.jump-bottom {
  position: absolute;
  right: 18px;
  bottom: 14px;
  width: 32px;
  height: 32px;
  border-radius: 50%;
  border: 1px solid #e5e7eb;
  background: #fff;
  color: #4b5563;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.12);
}

.jump-bottom:hover {
  border-color: #4f7cff;
  color: #4f7cff;
}

/* ----- 输入区（对齐 Bot 页：边框卡片 + 内嵌工具键 + 右下角圆形发送键） ----- */
.input-area {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 12px 24px 14px;
  border-top: 1px solid #e0e0e0;
  background: #fff;
  align-items: stretch;
  flex-shrink: 0;
}

/* 与消息区同宽上限，宽屏下不会拉成一条长条 */
.input-wrapper,
.input-hint {
  width: 100%;
  max-width: 960px;
  margin-left: auto;
  margin-right: auto;
}

.input-wrapper {
  position: relative;
  display: flex;
  align-items: flex-end;
  gap: 8px;
  min-width: 0;
  padding: 10px 48px 10px 14px;
  border: 1px solid #d9d9d9;
  border-radius: 12px;
  background: #fff;
  transition: border-color 0.2s, box-shadow 0.2s;
}

.input-wrapper:focus-within {
  border-color: #18a058;
  box-shadow: 0 0 0 2px rgba(24, 160, 88, 0.15);
}

.input-wrapper .chat-input {
  flex: 1;
  min-width: 0;
}

.send-btn-inline {
  position: absolute;
  right: 8px;
  bottom: 8px;
  width: 32px;
  height: 32px;
  border: none;
  border-radius: 8px;
  background: #18a058;
  color: #fff;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: background 0.2s, opacity 0.2s;
  flex-shrink: 0;
}

.send-btn-inline:hover:not(:disabled) {
  background: #20803a;
}

.send-btn-inline:disabled {
  background: #c5c5c5;
  cursor: not-allowed;
}

/* 等待中：方块停止键（只中断本地等待，远端回合继续跑） */
.send-btn-inline.stopping {
  background: #f0a020;
}

.send-btn-inline.stopping:hover {
  background: #d98610;
}

/* 输入框本体去掉 naive-ui 的边框与底色，由外层卡片负责 */
.chat-input {
  --n-border: none !important;
  --n-border-hover: none !important;
  --n-border-focus: none !important;
  --n-box-shadow-focus: none !important;
  --n-padding-left: 0 !important;
  --n-padding-right: 0 !important;
  background: transparent !important;
}

.chat-input :deep(.n-input) {
  background: transparent !important;
  box-shadow: none !important;
}

.chat-input :deep(.n-input__border),
.chat-input :deep(.n-input__border-focus),
.chat-input :deep(.n-input__state-border) {
  border: none !important;
  box-shadow: none !important;
  display: none !important;
}

.chat-input :deep(.n-input__textarea-el) {
  resize: none;
  line-height: 1.6;
}

.input-hint {
  font-size: 12px;
  color: #999;
  text-align: center;
}

.field-hint,
.peer-hint {
  font-size: 12px;
}

.peer-hint {
  display: block;
  margin-top: 10px;
}

/* ===== 移动端（<=768px）：列表与会话各占整宽 ===== */
@media (max-width: 768px) {
  .peers-shell {
    height: 100vh;
    height: 100dvh;
  }

  .peer-rail {
    width: 100%;
    border-right: none;
  }

  .peers-shell.in-chat .peer-rail {
    display: none;
  }

  .peers-shell:not(.in-chat) .peer-pane {
    display: none;
  }

  .back-btn {
    display: inline-flex;
    flex-shrink: 0;
  }

  .chat-header {
    padding: 10px 12px;
    flex-wrap: wrap;
  }

  .chat-actions {
    width: 100%;
    justify-content: flex-end;
  }

  .chat-bots {
    padding: 8px 12px;
  }

  .chat-body {
    padding: 12px;
  }

  .msg-body {
    max-width: 84%;
  }

  /* 触屏没有 hover：复制按钮常驻 */
  .msg-foot {
    opacity: 1;
  }

  .input-area {
    padding: 8px 10px;
    padding-bottom: calc(8px + env(safe-area-inset-bottom));
  }

  .input-wrapper {
    padding: 8px 12px;
    padding-right: 44px; /* 内置发送键仍留空间 */
  }

  /* 触屏没有物理键盘，提示行隐藏（等待秒数在消息区的 typing 气泡里已显示） */
  .input-hint {
    display: none;
  }
}
</style>
