<template>
  <div class="bots-page">
    <!-- Chat panel (when a bot is selected) -->
    <template v-if="botsStore.activeBotName">
      <div class="chat-header">
        <n-button quaternary size="small" @click="botsStore.closeChat()">
          <template #icon><n-icon><ArrowBackOutline /></n-icon></template>
        </n-button>
        <div class="chat-title">
          <n-text strong style="font-size: 16px;">@{{ activeBot?.mention_tag || botsStore.activeBotName }}</n-text>
          <n-text v-if="activeBot?.title" depth="3" style="font-size: 12px;">{{ activeBot.title }}</n-text>
        </div>
        <n-space :size="6" align="center">
          <n-tag :type="(activeBot?.runtime?.online) ? 'success' : 'default'" size="small">
            {{ activeBot?.runtime?.online ? t('bots.online') : t('bots.offline') }}
          </n-tag>
          <n-button quaternary size="small" @click="openRoutinesModal">
            <template #icon><n-icon><TimeOutline /></n-icon></template>
            {{ t('bots.routines') }} ({{ botsStore.routines.length }})
          </n-button>
          <n-button quaternary size="small" @click="openEditModal(activeBotObj)">
            <template #icon><n-icon><CreateOutline /></n-icon></template>
            {{ t('common.edit') }}
          </n-button>
        </n-space>
      </div>

      <div class="chat-body">
        <div ref="messagesEl" class="chat-messages" @click="handleCodeClick">
          <n-empty v-if="!botsStore.messages.length && !botsStore.chatLoading && !botsStore.sending" :description="t('bots.noMessages')" />
          <template v-else>
            <div v-for="(msgs, dateKey) in groupedMessages" :key="dateKey">
              <div v-if="dateKey" class="date-separator"><span>{{ dateKey }}</span></div>
              <div
                v-for="msg in msgs"
                :key="msg.id"
                class="message"
                :class="msg.role"
              >
                <div class="avatar" :class="msg.role === 'assistant' ? 'bot-avatar' : 'user-avatar'">
                  {{ msg.role === 'assistant' ? botAvatarText : 'U' }}
                </div>
                <div class="message-body" :class="{ 'bot-body': msg.role === 'assistant' }">
                  <div class="message-header">
                    <n-text strong class="sender-name">{{ msg.role === 'assistant' ? botDisplayName : t('bots.you') }}</n-text>
                    <n-tag v-if="msg.role === 'assistant'" size="tiny" type="success">AI</n-tag>
                    <span v-if="formatTime(msg.timestamp)" class="message-time">{{ formatTime(msg.timestamp) }}</span>
                  </div>
                  <div class="message-bubble" :class="[msg.role === 'assistant' ? 'agent-bubble' : 'user-bubble', { streaming: msg._streaming }]">
                    <n-spin v-if="msg._streaming && !msg.content" size="small" class="stream-spin" />
                    <template v-if="msg.role === 'assistant' && msg.content">
                      <ReasoningContent :content="msg.content" :streaming="msg._streaming" />
                    </template>
                    <div
                      v-else
                      class="bubble-content"
                      v-html="msg.content ? renderMarkdown(msg.content) : '<span class=\'placeholder\'>...</span>'"
                    ></div>
                  </div>
                </div>
              </div>
            </div>
          </template>
          <div v-if="botsStore.sending && !botsStore.messages.some(m => m._streaming)" class="message assistant">
            <div class="avatar bot-avatar">
              {{ botAvatarText }}
            </div>
            <div class="message-body bot-body">
              <div class="message-header">
                <n-text strong class="sender-name">{{ botDisplayName }}</n-text>
                <n-tag size="tiny" type="success">AI</n-tag>
              </div>
              <div class="message-bubble agent-bubble thinking">
                <div class="typing-indicator">
                  <span class="dot"></span>
                  <span class="dot"></span>
                  <span class="dot"></span>
                </div>
              </div>
            </div>
          </div>
          <div v-if="botsStore.chatLoading && !botsStore.messages.length" class="history-loading">
            <n-spin size="small" />
            <n-text depth="3" style="font-size: 13px; margin-left: 8px;">{{ t('bots.loadingHistory') }}</n-text>
          </div>
        </div>
      </div>

      <div class="input-area">
        <div class="input-wrapper">
          <n-input
            v-model:value="draft"
            type="textarea"
            :placeholder="t('bots.inputPlaceholder')"
            :autosize="{ minRows: 1, maxRows: 6 }"
            :disabled="botsStore.sending"
            class="chat-input"
            @keydown.enter.exact.prevent="handleSend"
          />
          <button
            class="send-btn-inline"
            :class="{ stopping: botsStore.sending }"
            :disabled="!botsStore.sending && !draft.trim()"
            @click="handleSend"
            @mousedown.prevent
            :title="botsStore.sending ? t('bots.sending') : t('bots.send')"
          >
            <svg v-if="!botsStore.sending" viewBox="0 0 24 24" width="18" height="18" fill="currentColor">
              <path d="M3.4 20.4l17.45-7.48a1 1 0 000-1.84L3.4 3.6a.993.993 0 00-1.39.91L2 9.12c0 .5.37.93.87.99L17 12 2.87 13.88c-.5.07-.87.5-.87 1l.01 4.61c0 .71.73 1.2 1.39.91z"/>
            </svg>
            <svg v-else viewBox="0 0 24 24" width="16" height="16" fill="currentColor">
              <rect x="6" y="6" width="12" height="12" rx="2"/>
            </svg>
          </button>
        </div>
      </div>
    </template>

    <!-- Bot list (default view) -->
    <template v-else>
      <n-space justify="space-between" style="margin-bottom: 16px;">
        <div>
          <h2>{{ t('bots.title') }}</h2>
          <n-text depth="3" style="font-size: 13px;">{{ t('bots.subtitle') }}</n-text>
        </div>
        <n-space>
          <n-button :loading="botsStore.loading" @click="botsStore.loadBots()">
            <template #icon><n-icon><RefreshOutline /></n-icon></template>
          </n-button>
          <n-button type="primary" :disabled="botModeDisabled" @click="openCreateModal">+ {{ t('bots.createBot') }}</n-button>
        </n-space>
      </n-space>

      <n-alert v-if="botModeDisabled" type="warning" style="margin-bottom: 16px;" :title="t('bots.modeDisabledTitle')">
        {{ t('bots.modeDisabledHint') }}
      </n-alert>

      <n-spin :show="botsStore.loading">
        <n-empty v-if="!botsStore.bots.length && !botsStore.loading" :description="t('bots.noBots')" style="padding: 60px 0;" />
        <n-grid :cols="3" :x-gap="16" :y-gap="16" responsive="screen" item-responsive>
          <n-gi v-for="b in botsStore.bots" :key="b.name" span="3 s:1 m:1 l:1">
            <n-card size="small" hoverable class="bot-card">
              <div class="bot-card-head">
                <n-avatar round size="medium" :style="{ backgroundColor: avatarColor(b.name) }">
                  {{ (b.mention_tag || b.name).slice(0, 2).toUpperCase() }}
                </n-avatar>
                <div class="bot-card-title">
                  <n-space align="center" :size="6">
                    <n-text strong>@{{ b.mention_tag || b.name }}</n-text>
                    <n-tag :type="b.runtime?.online ? 'success' : 'warning'" size="tiny">
                      {{ b.runtime?.online ? t('bots.online') : t('bots.offline') }}
                    </n-tag>
                  </n-space>
                  <n-text depth="3" style="font-size: 12px;">{{ b.title || b.description || '' }}</n-text>
                </div>
                <n-dropdown trigger="click" :options="cardMenuOptions(b)" @select="(key: string) => handleCardAction(key, b)">
                  <n-button quaternary size="tiny">
                    <template #icon><n-icon><EllipsisHorizontalOutline /></n-icon></template>
                  </n-button>
                </n-dropdown>
              </div>

              <n-text v-if="b.description" depth="3" style="font-size: 12px; display: block; margin-top: 8px;">
                {{ b.description }}
              </n-text>

              <n-space style="margin-top: 10px;" :size="6">
                <n-tag v-if="b.model" size="tiny" type="info">{{ b.model }}</n-tag>
                <n-tag v-if="b.provider" size="tiny">{{ b.provider }}</n-tag>
                <n-tag size="tiny" :bordered="false">
                  {{ t('bots.routineCount', { count: b.runtime?.active_routines ?? 0 }) }}
                </n-tag>
              </n-space>

              <template #footer>
                <n-space justify="end">
                  <n-button size="tiny" type="primary" @click="botsStore.openChat(b.name)">
                    <template #icon><n-icon><ChatbubbleEllipsesOutline /></n-icon></template>
                    {{ t('bots.openChat') }}
                  </n-button>
                </n-space>
              </template>
            </n-card>
          </n-gi>
        </n-grid>
      </n-spin>
    </template>

    <!-- Create/Edit Modal -->
    <n-modal v-model:show="showEditModal" :title="editingBot ? t('bots.editBot') : t('bots.createBot')" preset="card" style="width: 560px;">
      <n-form label-placement="top">
        <n-form-item :label="t('bots.name')" required>
          <n-input v-model:value="form.name" :disabled="!!editingBot" :placeholder="t('bots.namePlaceholder')" />
          <template #feedback>
            <n-text depth="3" style="font-size: 12px;">{{ t('bots.nameHint') }}</n-text>
          </template>
        </n-form-item>
        <n-form-item :label="t('bots.titleLabel')">
          <n-input v-model:value="form.title" :placeholder="t('bots.titlePlaceholder')" />
        </n-form-item>
        <n-form-item :label="t('common.description')">
          <n-input v-model:value="form.description" type="textarea" :rows="2" :placeholder="t('bots.descPlaceholder')" />
        </n-form-item>
        <n-form-item :label="t('bots.systemPrompt')">
          <n-input v-model:value="form.system_prompt" type="textarea" :rows="5" :placeholder="t('bots.systemPromptPlaceholder')" />
        </n-form-item>
        <n-form-item :label="t('bots.modelPin')">
          <n-select
            v-model:value="selectedModelId"
            :options="botModelSelectOptions"
            :placeholder="t('bots.inheritGlobal')"
            clearable
            filterable
            :consistent-menu-width="false"
            :render-label="renderBotModelLabel"
          />
        </n-form-item>
      </n-form>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showEditModal = false">{{ t('common.cancel') }}</n-button>
          <n-button type="primary" :loading="saving" @click="handleSaveBot">{{ t('common.save') }}</n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- Routines Modal -->
    <n-modal v-model:show="showRoutinesModal" :title="t('bots.routinesFor', { name: '@' + (activeBot?.mention_tag || '') })" preset="card" style="width: 660px;">
      <n-empty v-if="!botsStore.routines.length" :description="t('bots.noRoutines')" />

      <n-space vertical v-else>
        <n-card v-for="rt in botsStore.routines" :key="rt.id" size="small">
          <n-space justify="space-between" align="center">
            <n-space align="center" :size="8">
              <n-tag :type="rt.enabled ? 'success' : 'default'" size="small">
                {{ rt.enabled ? t('cron.stateActive') : t('cron.stateInactive') }}
              </n-tag>
              <n-text strong style="font-size: 14px;">{{ rt.name || rt.id }}</n-text>
            </n-space>
            <n-space :size="4">
              <n-popconfirm @positive-click="handleRemoveRoutine(rt.id)">
                <template #trigger>
                  <n-button size="tiny" type="error">{{ t('common.delete') }}</n-button>
                </template>
                {{ t('bots.confirmDeleteRoutine') }}
              </n-popconfirm>
            </n-space>
          </n-space>

          <n-grid :cols="3" :x-gap="16" style="margin-top: 10px;">
            <n-gi>
              <n-text depth="3" style="font-size: 12px;">{{ t('cron.cronExpression') }}</n-text>
              <div><n-text code>{{ rt.schedule }}</n-text></div>
            </n-gi>
            <n-gi>
              <n-text depth="3" style="font-size: 12px;">{{ t('cron.previousRun') }}</n-text>
              <div>{{ rt.last_run ? formatTime(rt.last_run) : '-' }}</div>
              <div v-if="rt.last_status">
                <n-tag :type="rt.last_status === 'success' ? 'success' : rt.last_status === 'failed' ? 'error' : 'warning'" size="tiny">
                  {{ rt.last_status }}
                </n-tag>
              </div>
            </n-gi>
            <n-gi>
              <n-text depth="3" style="font-size: 12px;">{{ t('bots.routinePrompt') }}</n-text>
              <n-text depth="3" style="font-size: 12px; display: block; max-width: 260px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">
                {{ rt.prompt }}
              </n-text>
            </n-gi>
          </n-grid>
        </n-card>
      </n-space>

      <n-divider />
      <n-h6 style="margin: 0 0 10px 0;">{{ t('bots.addRoutine') }}</n-h6>
      <n-form label-placement="top">
        <n-form-item :label="t('cron.jobName')">
          <n-input v-model:value="routineForm.name" :placeholder="t('bots.routineName')" />
        </n-form-item>
        <n-form-item :label="t('cron.cronExpression')" required>
          <n-input v-model:value="routineForm.schedule" placeholder="0 9 * * *" />
          <template #feedback>
            <n-text v-if="routineForm.schedule.trim()" :type="scheduleHint.type" style="font-size: 12px;">
              {{ scheduleHint.text }}
            </n-text>
          </template>
        </n-form-item>
        <n-form-item :label="t('cron.commonExpressions')">
          <n-space>
            <n-button size="tiny" @click="routineForm.schedule = '0 8 * * *'">{{ t('cron.daily8am') }}</n-button>
            <n-button size="tiny" @click="routineForm.schedule = '0 8 * * 1-5'">{{ t('cron.weekday8am') }}</n-button>
            <n-button size="tiny" @click="routineForm.schedule = '0 */2 * * *'">{{ t('cron.every2hours') }}</n-button>
            <n-button size="tiny" @click="routineForm.schedule = '0 0 * * *'">{{ t('cron.dailyMidnight') }}</n-button>
          </n-space>
        </n-form-item>
        <n-form-item :label="t('bots.routinePrompt')" required>
          <n-input v-model:value="routineForm.prompt" type="textarea" :rows="3" :placeholder="t('bots.routinePrompt')" />
        </n-form-item>
      </n-form>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showRoutinesModal = false">{{ t('common.cancel') }}</n-button>
          <n-button type="primary" :loading="addingRoutine" :disabled="!routineForm.schedule.trim() || !routineForm.prompt.trim()" @click="handleAddRoutine">
            {{ t('common.add') }}
          </n-button>
        </n-space>
      </template>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { computed, h, nextTick, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import {
  NAlert, NAvatar, NButton, NCard, NDivider, NDropdown, NEmpty, NForm,
  NFormItem, NGi, NGrid, NH6, NIcon, NInput, NList, NListItem, NModal,
  NPopconfirm, NSpace, NSelect, NSpin, NTag, NText, useMessage,
} from 'naive-ui'
import {
  ArrowBackOutline, ChatbubbleEllipsesOutline, CreateOutline,
  EllipsisHorizontalOutline, RefreshOutline, TimeOutline,
} from '@vicons/ionicons5'
import { useI18n } from 'vue-i18n'
import { useBotsStore } from '@/stores/bots'
import { useModelsStore } from '@/stores/models'
import type { Bot } from '@/api/bots'
import { marked } from 'marked'
import hljs from 'highlight.js'
import 'highlight.js/styles/github-dark.css'
import ReasoningContent from '@/components/ReasoningContent.vue'

const { t } = useI18n()
const message = useMessage()
const botsStore = useBotsStore()
const modelsStore = useModelsStore()

const showEditModal = ref(false)
const showRoutinesModal = ref(false)
const editingBot = ref<Bot | null>(null)
const saving = ref(false)
const addingRoutine = ref(false)
const draft = ref('')
const messagesEl = ref<HTMLElement | null>(null)

const form = reactive({
  name: '',
  title: '',
  description: '',
  system_prompt: '',
  model: '',
  provider: '',
})

const routineForm = reactive({ name: '', schedule: '', prompt: '' })

const scheduleHint = computed(() => {
  const s = routineForm.schedule.trim()
  if (!s) return { text: '', type: 'default' as const }
  const parts = s.split(/\s+/)
  if (parts.length < 5) return { text: t('cron.scheduleHint5fields'), type: 'error' as const }
  if (parts.length > 6) return { text: t('cron.scheduleHint6fields'), type: 'error' as const }
  if (s === '0 8 * * *') return { text: t('cron.scheduleHintDaily8'), type: 'success' as const }
  if (s === '0 8 * * 1-5') return { text: t('cron.scheduleHintWeekday9'), type: 'success' as const }
  if (s === '0 */2 * * *') return { text: t('cron.scheduleHint2hours'), type: 'success' as const }
  if (s === '0 0 * * *') return { text: t('cron.scheduleHintMidnight'), type: 'success' as const }
  return { text: t('cron.scheduleValid'), type: 'success' as const }
})

const activeBot = computed(() =>
  botsStore.bots.find(b => b.name === botsStore.activeBotName) || null
)
// The list endpoint already embeds runtime info; keep a non-null object for template use
const activeBotObj = computed<Bot | null>(() => activeBot.value)

// Driven by the store: set when GET /api/bots returns 503 (bot mode off).
const botModeDisabled = computed(() => botsStore.modeDisabled)

const botModelSelectOptions = computed(() => modelsStore.modelSelectOptions)

function renderBotModelLabel(option: { label: string; value: string }) {
  const parts = option.label.split(' / ')
  const provider = parts[0] || ''
  const model = parts[1] || ''
  return h('div', {
    style: 'display: flex; align-items: center; gap: 6px; padding: 4px 0;'
  }, [
    h('span', null, provider),
    h('span', null, `/ ${model}`),
  ])
}

// Unified "provider/model" select (matches Chat UI).
// value === "" means inherit global; otherwise "provider/model" id string.
const selectedModelId = computed<string>({
  get: () => {
    if (form.provider && form.model) return `${form.provider}/${form.model}`
    return ''
  },
  set: (v: string) => {
    if (!v) {
      form.provider = ''
      form.model = ''
      return
    }
    const idx = v.indexOf('/')
    if (idx < 0) {
      form.model = v
      form.provider = ''
    } else {
      form.provider = v.slice(0, idx)
      form.model = v.slice(idx + 1)
    }
  },
})

function cardMenuOptions(b: Bot) {
  return [
    { label: t('bots.openChat'), key: 'chat' },
    { label: t('common.edit'), key: 'edit' },
    { label: t('bots.manageRoutines'), key: 'routines' },
    { label: t('common.delete'), key: 'delete' },
  ]
}

function handleCardAction(key: string, b: Bot) {
  switch (key) {
    case 'chat':
      botsStore.openChat(b.name)
      break
    case 'edit':
      openEditModal(b)
      break
    case 'routines':
      botsStore.openChat(b.name)
      openRoutinesModal()
      break
    case 'delete':
      void handleDeleteBot(b)
      break
  }
}

function openCreateModal() {
  editingBot.value = null
  form.name = ''
  form.title = ''
  form.description = ''
  form.system_prompt = ''
  form.model = ''
  form.provider = ''
  showEditModal.value = true
}

function openEditModal(b: Bot | null) {
  if (!b) return
  editingBot.value = b
  form.name = b.name
  form.title = b.title || ''
  form.description = b.description || ''
  form.system_prompt = b.system_prompt || ''
  form.model = b.model || ''
  form.provider = b.provider || ''
  showEditModal.value = true
}

async function handleSaveBot() {
  if (!editingBot.value && !form.name.trim()) {
    message.warning(t('bots.enterName'))
    return
  }
  saving.value = true
  try {
    const payload = {
      title: form.title,
      description: form.description,
      system_prompt: form.system_prompt,
      model: form.model,
      provider: form.provider,
    }
    if (editingBot.value) {
      await botsStore.updateBot(editingBot.value.name, payload)
      message.success(t('bots.botUpdated'))
    } else {
      await botsStore.createBot({ name: form.name.trim(), ...payload })
      message.success(t('bots.botCreated'))
    }
    showEditModal.value = false
  } catch (e: any) {
    message.error(e.message || t('common.operationFailed'))
  } finally {
    saving.value = false
  }
}

async function handleDeleteBot(b: Bot) {
  try {
    await botsStore.deleteBot(b.name)
    message.success(t('bots.botDeleted'))
  } catch (e: any) {
    message.error(e.message || t('common.operationFailed'))
  }
}

function openRoutinesModal() {
  routineForm.name = ''
  routineForm.schedule = ''
  routineForm.prompt = ''
  showRoutinesModal.value = true
}

async function handleAddRoutine() {
  addingRoutine.value = true
  try {
    await botsStore.addRoutine({
      name: routineForm.name.trim(),
      schedule: routineForm.schedule.trim(),
      prompt: routineForm.prompt.trim(),
    })
    message.success(t('bots.routineAdded'))
    routineForm.name = ''
    routineForm.schedule = ''
    routineForm.prompt = ''
  } catch (e: any) {
    message.error(e.message || t('common.operationFailed'))
  } finally {
    addingRoutine.value = false
  }
}

async function handleRemoveRoutine(routineId: string) {
  try {
    await botsStore.removeRoutine(routineId)
    message.success(t('bots.routineRemoved'))
  } catch (e: any) {
    message.error(e.message || t('common.operationFailed'))
  }
}

async function handleSend() {
  const text = draft.value.trim()
  if (!text) return
  draft.value = ''
  try {
    await botsStore.sendMessage(text)
  } catch (e: any) {
    message.error(e.message || t('common.operationFailed'))
  }
}

watch(
  () => [botsStore.messages.length, botsStore.sending],
  async () => {
    await nextTick()
    const el = messagesEl.value
    if (el) el.scrollTop = el.scrollHeight
  }
)

function formatTime(ts: number) {
  if (!ts) return ''
  const d = new Date(ts)
  if (isNaN(d.getTime())) return ''
  const now = new Date()
  const isToday = d.toDateString() === now.toDateString()
  if (isToday) {
    return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  }
  return d.toLocaleDateString([], { month: 'short', day: 'numeric' }) + ' ' +
    d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

function formatDate(ts: number): string {
  if (!ts) return ''
  const d = new Date(ts)
  if (isNaN(d.getTime())) return ''
  const now = new Date()
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  const yesterday = new Date(today.getTime() - 86400000)
  const msgDate = new Date(d.getFullYear(), d.getMonth(), d.getDate())
  if (msgDate.getTime() === today.getTime()) return ''
  if (msgDate.getTime() === yesterday.getTime()) return 'Yesterday'
  return d.toLocaleDateString([], { month: 'short', day: 'numeric', year: 'numeric' })
}

const groupedMessages = computed(() => {
  const groups: Record<string, any[]> = {}
  for (const msg of botsStore.messages) {
    const key = formatDate(msg.timestamp)
    if (!groups[key]) groups[key] = []
    groups[key].push(msg)
  }
  return groups
})

const botDisplayName = computed(() => {
  const bot = activeBot.value
  return bot ? `@${bot.mention_tag || bot.name}` : 'Bot'
})

const botAvatarText = computed(() => {
  const bot = activeBot.value
  const tag = bot?.mention_tag || bot?.name || 'B'
  return tag.slice(0, 2).toUpperCase()
})

const codeRenderer = (code: string, lang?: string): string => {
  const language = lang && hljs.getLanguage(lang) ? lang : null
  const highlighted = language
    ? hljs.highlight(code, { language }).value
    : hljs.highlightAuto(code).value
  const copyBtn = `<button class="code-copy-btn" type="button">Copy</button>`
  return `<div class="code-block">${copyBtn}<pre><code class="hljs${language ? ` language-${language}` : ''}">${highlighted}</code></pre></div>`
}

marked.use({
  renderer: { code: codeRenderer },
  breaks: true,
  gfm: true,
})

const mdCache = new Map<string, string>()
const MD_CACHE_LIMIT = 200

function renderMarkdown(content: string): string {
  const cached = mdCache.get(content)
  if (cached !== undefined) return cached
  const html = marked.parse(content) as string
  if (mdCache.size >= MD_CACHE_LIMIT) {
    const keys = mdCache.keys()
    for (let i = 0; i < MD_CACHE_LIMIT / 2; i++) {
      const r = keys.next()
      if (r.done) break
      mdCache.delete(r.value)
    }
  }
  mdCache.set(content, html)
  return html
}

function handleCodeClick(e: MouseEvent) {
  const target = e.target as HTMLElement
  const btn = target.closest('.code-copy-btn') as HTMLElement | null
  if (btn) {
    const codeEl = btn.parentElement?.querySelector('code')
    const code = codeEl?.textContent || ''
    navigator.clipboard.writeText(code).catch(() => {})
    const original = btn.textContent
    btn.textContent = '✓'
    setTimeout(() => { btn.textContent = original }, 2000)
    return
  }
  const pre = target.closest('pre')
  if (pre) {
    const code = pre.querySelector('code')
    if (code) {
      navigator.clipboard.writeText(code.textContent || '').then(() => {
        message.success(t('groupchat.copied') || 'Copied')
      }).catch(() => {})
    }
  }
}

function handleDocumentCodeClick(e: MouseEvent) {
  const target = e.target as HTMLElement
  const btn = target.closest('.code-copy-btn') as HTMLElement | null
  if (!btn) return
  const codeEl = btn.parentElement?.querySelector('code')
  const code = codeEl?.textContent || ''
  navigator.clipboard.writeText(code).catch(() => {})
  const original = btn.textContent
  btn.textContent = '✓'
  setTimeout(() => { btn.textContent = original }, 2000)
}

onMounted(() => {
  void botsStore.loadBots()
  void modelsStore.loadModels()
  document.addEventListener('click', handleDocumentCodeClick)
})

onUnmounted(() => {
  document.removeEventListener('click', handleDocumentCodeClick)
})

const palette = ['#4f7cff', '#18a058', '#f0a020', '#d03050', '#722ed1', '#13c2c2']
function avatarColor(name: string) {
  let h = 0
  for (let i = 0; i < name.length; i++) h = (h * 31 + name.charCodeAt(i)) % 997
  return palette[h % palette.length]
}

onMounted(() => {
  void botsStore.loadBots()
  void modelsStore.loadModels()
})
</script>

<style scoped>
.bots-page {
  height: calc(100vh - 48px);
  display: flex;
  flex-direction: column;
}
/* List view */
.bots-page > n-space:first-child {
  flex-shrink: 0;
}
.bot-card-head {
  display: flex;
  align-items: center;
  gap: 10px;
}
.bot-card-title {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}
/* Chat view */
.chat-header {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 12px 16px;
  border-bottom: 1px solid #e8e8e8;
  flex-shrink: 0;
}
.chat-title {
  flex: 1;
  display: flex;
  align-items: baseline;
  gap: 8px;
}
.chat-body {
  flex: 1;
  min-height: 0;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}
.chat-messages {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 20px 24px;
  padding-bottom: 80px;
  display: flex;
  flex-direction: column;
  gap: 0;
}
.chat-messages > * {
  max-width: 960px;
  margin-left: auto;
  margin-right: auto;
  width: 100%;
}

/* ========== Message Layout ========== */
.message {
  display: flex;
  gap: 12px;
  margin-bottom: 20px;
  animation: fadeIn 0.3s ease;
}

@keyframes fadeIn {
  from { opacity: 0; transform: translateY(8px); }
  to { opacity: 1; transform: translateY(0); }
}

.message.user {
  flex-direction: row-reverse;
}

.message-body {
  max-width: 72%;
  min-width: 0;
  display: flex;
  flex-direction: column;
}

.message-body.bot-body {
  max-width: 80%;
}

/* ========== Avatars ========== */
.avatar {
  width: 34px;
  height: 34px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  font-size: 14px;
  color: #fff;
  font-weight: 600;
  user-select: none;
}

.user-avatar {
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
}

.bot-avatar {
  background: linear-gradient(135deg, #18a058 0%, #36ad6a 100%);
}

/* ========== Message Header ========== */
.message-header {
  display: flex;
  gap: 8px;
  align-items: center;
  margin-bottom: 4px;
  font-size: 13px;
}

.message.user .message-header {
  justify-content: flex-end;
  flex-direction: row-reverse;
}

.message.user .message-body {
  align-items: flex-end;
}

.sender-name {
  color: #333;
}

.message-time {
  font-size: 11px;
  color: #bbb;
}

.stream-spin {
  margin-right: 4px;
}

/* ========== Message Bubbles ========== */
.message-bubble {
  padding: 14px 18px;
  border-radius: 16px;
  line-height: 1.75;
  word-break: break-word;
  overflow-wrap: break-word;
  display: flex;
  align-items: flex-start;
  gap: 8px;
}

.user-bubble {
  background: linear-gradient(135deg, #4f7cff 0%, #3a5fd9 100%);
  color: #fff;
  border-bottom-right-radius: 4px;
}

.agent-bubble {
  background: #fff;
  color: #1f2937;
  border: 1px solid #e8e8e8;
  border-bottom-left-radius: 4px;
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.06);
}

.message-bubble.thinking {
  padding: 12px 16px;
}

.typing-indicator {
  display: flex;
  align-items: center;
  gap: 5px;
  padding: 2px 0;
}

.typing-indicator .dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: #18a058;
  animation: typing-bounce 1.4s ease-in-out infinite;
}

.typing-indicator .dot:nth-child(2) {
  animation-delay: 0.2s;
}

.typing-indicator .dot:nth-child(3) {
  animation-delay: 0.4s;
}

@keyframes typing-bounce {
  0%, 60%, 100% {
    transform: translateY(0);
    opacity: 0.4;
  }
  30% {
    transform: translateY(-6px);
    opacity: 1;
  }
}

.history-loading {
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 40px 0;
}

.message-bubble.streaming {
  border-style: dashed;
  animation: pulse-border 1.5s ease-in-out infinite;
}

@keyframes pulse-border {
  0%, 100% { border-color: #c8e6c9; }
  50% { border-color: #66bb6a; }
}

.bubble-content {
  word-break: break-word;
  overflow-wrap: break-word;
  flex: 1;
  min-width: 0;
}

.bubble-content :deep(.placeholder) {
  color: #999;
}

/* ========== Markdown Content ========== */
.message-bubble :deep(p) { margin: 0 0 10px 0; }
.message-bubble :deep(p:last-child) { margin-bottom: 0; }
.message-bubble :deep(ul), .message-bubble :deep(ol) { margin: 10px 0; padding-left: 28px; }
.message-bubble :deep(li) { margin: 5px 0; }

.message-bubble :deep(blockquote) {
  margin: 10px 0;
  padding: 8px 16px;
  border-left: 4px solid #d0d0d0;
  background: rgba(0, 0, 0, 0.03);
  color: inherit;
}

.message-bubble :deep(table) {
  border-collapse: collapse;
  margin: 10px 0;
  width: 100%;
}

.message-bubble :deep(th), .message-bubble :deep(td) {
  border: 1px solid #d0d0d0;
  padding: 6px 12px;
}

.message-bubble :deep(th) {
  background: rgba(0, 0, 0, 0.04);
}

.message-bubble :deep(h1),
.message-bubble :deep(h2),
.message-bubble :deep(h3),
.message-bubble :deep(h4) {
  margin: 14px 0 8px;
  font-weight: 600;
}

.message-bubble :deep(h1) { font-size: 20px; }
.message-bubble :deep(h2) { font-size: 18px; }
.message-bubble :deep(h3) { font-size: 16px; }
.message-bubble :deep(h4) { font-size: 15px; }

.message-bubble :deep(hr) {
  border: none;
  border-top: 1px solid #d0d0d0;
  margin: 14px 0;
}

.message-bubble :deep(pre) {
  background: #1e1e1e;
  color: #d4d4d4;
  padding: 12px 16px;
  border-radius: 8px;
  overflow-x: auto;
  max-width: 100%;
  margin: 10px 0;
}

.message-bubble :deep(.code-block) {
  position: relative;
  margin: 10px 0;
  border-radius: 8px;
  overflow: hidden;
  background: #1e1e1e;
}

.message-bubble :deep(.code-block pre) {
  margin: 0;
  padding: 12px 15px;
  overflow-x: auto;
}

.message-bubble :deep(.code-block code) {
  font-family: 'SF Mono', 'Consolas', monospace;
  font-size: 13px;
  color: #d4d4d4;
  line-height: 1.6;
}

.message-bubble :deep(.code-copy-btn) {
  position: absolute;
  top: 6px;
  right: 6px;
  background: rgba(255, 255, 255, 0.1);
  border: 1px solid rgba(255, 255, 255, 0.2);
  color: #ccc;
  padding: 2px 10px;
  border-radius: 4px;
  font-size: 11px;
  cursor: pointer;
  opacity: 0;
  transition: opacity 0.2s;
}

.message-bubble :deep(.code-block:hover .code-copy-btn) {
  opacity: 1;
}

.message-bubble :deep(code) {
  font-family: 'SF Mono', 'Fira Code', 'Consolas', monospace;
  font-size: 13px;
}

.message-bubble :deep(a) {
  color: inherit;
  text-decoration: underline;
  font-weight: 600;
}

.user-bubble :deep(a) {
  color: #e0f0ff;
  text-shadow: 0 1px 2px rgba(0, 0, 0, 0.3);
}

.user-bubble :deep(a:hover) {
  color: #fff;
}

/* ========== Date Separator ========== */
.date-separator {
  text-align: center;
  padding: 16px 0 8px;
  font-size: 12px;
  color: #999;
}

.date-separator span {
  background: #f5f5f5;
  padding: 2px 12px;
  border-radius: 10px;
}

/* ========== Code Block Click Hint ========== */
.chat-messages :deep(pre:hover) {
  outline: 2px solid #4f7cff;
  outline-offset: 2px;
  cursor: pointer;
  transition: outline 0.2s;
}

.chat-messages :deep(pre) {
  position: relative;
  border-radius: 6px;
}

.chat-messages :deep(pre:hover::after) {
  content: 'Click to copy';
  position: absolute;
  top: 4px;
  right: 8px;
  font-size: 11px;
  color: #4f7cff;
  background: rgba(255, 255, 255, 0.9);
  padding: 0 6px;
  border-radius: 3px;
  pointer-events: none;
}

/* ========== Input Area ========== */
.input-area {
  display: flex;
  gap: 8px;
  padding: 12px 24px 16px;
  border-top: 1px solid #e0e0e0;
  background: #fff;
  align-items: flex-end;
  flex-shrink: 0;
}

.input-area > * {
  max-width: 960px;
  margin-left: auto;
  margin-right: auto;
}

.input-wrapper {
  position: relative;
  flex: 1;
  min-width: 0;
  width: 100%;
  border: 1px solid #d9d9d9;
  border-radius: 12px;
  padding: 12px 16px;
  padding-right: 48px;
  background: #fff;
  transition: border-color 0.2s, box-shadow 0.2s;
}

.input-wrapper:focus-within {
  border-color: #18a058;
  box-shadow: 0 0 0 2px rgba(24, 160, 88, 0.15);
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

.send-btn-inline.stopping {
  background: #f0a020;
}

.send-btn-inline.stopping:hover {
  background: #d98610;
}

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

.chat-input.n-input :deep(.n-input__state-border),
.chat-input:deep(.n-input--focus .n-input__state-border) {
  border: none !important;
  box-shadow: none !important;
  display: none !important;
}

.chat-input :deep(.n-input__textarea-el) {
  resize: none;
}

@media (max-width: 768px) {
  .message-body {
    max-width: 88%;
  }
  .message-body.bot-body {
    max-width: 90%;
  }
}

/* ========== Dark Mode ========== */
@media (prefers-color-scheme: dark) {
  .chat-header {
    border-bottom-color: #374151;
  }
  .sender-name {
    color: #d1d5db;
  }
  .message-time {
    color: #6b7280;
  }
  .agent-bubble {
    background: #1f1f23;
    color: #e5e7eb;
    border-color: #374151;
  }
  .user-bubble {
    background: linear-gradient(135deg, #4f7cff 0%, #3a5fd9 100%);
  }
  .message-bubble :deep(blockquote) {
    border-left-color: #4b5563;
    background: rgba(255, 255, 255, 0.05);
  }
  .message-bubble :deep(th) {
    background: rgba(255, 255, 255, 0.05);
  }
  .message-bubble :deep(th),
  .message-bubble :deep(td) {
    border-color: #374151;
  }
  .date-separator span {
    background: #2c2c33;
  }
  .input-area {
    border-top-color: #374151;
    background: #1a1a1f;
  }
  .input-wrapper {
    background: #1a1a1f;
    border-color: #374151;
  }
  .input-wrapper:focus-within {
    border-color: #18a058;
    box-shadow: 0 0 0 2px rgba(24, 160, 88, 0.2);
  }
  .chat-messages :deep(pre:hover::after) {
    background: rgba(0, 0, 0, 0.8);
    color: #60a5fa;
  }
  .typing-indicator .dot {
    background: #36ad6a;
  }
}
</style>