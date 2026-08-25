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
        <n-space :size="8">
          <n-tag :type="(activeBot?.runtime?.online) ? 'success' : 'default'" size="small">
            {{ activeBot?.runtime?.online ? t('bots.online') : t('bots.offline') }}
          </n-tag>
          <n-button size="small" @click="openRoutinesModal">
            <template #icon><n-icon><TimeOutline /></n-icon></template>
            {{ t('bots.routines') }} ({{ botsStore.routines.length }})
          </n-button>
          <n-button size="small" @click="openEditModal(activeBotObj)">
            <template #icon><n-icon><CreateOutline /></n-icon></template>
            {{ t('common.edit') }}
          </n-button>
        </n-space>
      </div>

      <n-spin :show="botsStore.chatLoading" class="chat-body">
        <div ref="messagesEl" class="chat-messages">
          <n-empty v-if="!botsStore.messages.length && !botsStore.chatLoading" :description="t('bots.noMessages')" />
          <div v-for="msg in botsStore.messages" :key="msg.id" class="chat-row" :class="msg.role">
            <div class="bubble" :class="msg.role">
              <div class="bubble-content">{{ msg.content }}</div>
              <div class="bubble-time">{{ formatTime(msg.timestamp) }}</div>
            </div>
          </div>
          <div v-if="botsStore.sending" class="chat-row assistant">
            <div class="bubble assistant thinking">
              <n-spin size="small" />
            </div>
          </div>
        </div>
      </n-spin>

      <div class="chat-input-area">
        <n-input
          v-model:value="draft"
          type="textarea"
          :placeholder="t('bots.inputPlaceholder')"
          :autosize="{ minRows: 1, maxRows: 4 }"
          :disabled="botsStore.sending"
          @keydown.enter.exact.prevent="handleSend"
        />
        <n-button type="primary" :loading="botsStore.sending" :disabled="!draft.trim()" @click="handleSend">
          {{ t('bots.send') }}
        </n-button>
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
            :options="modelsStore.modelSelectOptions"
            :placeholder="t('bots.inheritGlobal')"
            clearable
            filterable
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
    <n-modal v-model:show="showRoutinesModal" :title="t('bots.routinesFor', { name: '@' + (activeBot?.mention_tag || '') })" preset="card" style="width: 620px;">
      <n-empty v-if="!botsStore.routines.length" :description="t('bots.noRoutines')" />
      <n-list v-else hoverable clickable>
        <n-list-item v-for="rt in botsStore.routines" :key="rt.id">
          <div class="routine-row">
            <div class="routine-info">
              <n-space align="center" :size="8">
                <n-text strong>{{ rt.name || rt.id }}</n-text>
                <n-tag size="tiny" :type="rt.enabled ? 'success' : 'default'">
                  {{ rt.enabled ? t('cron.stateActive') : t('cron.stateInactive') }}
                </n-tag>
              </n-space>
              <n-text code depth="3" style="font-size: 12px;">{{ rt.schedule }}</n-text>
              <n-text depth="3" style="font-size: 12px; display: block; max-width: 420px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">
                {{ rt.prompt }}
              </n-text>
            </div>
            <n-popconfirm @positive-click="handleRemoveRoutine(rt.id)">
              <template #trigger>
                <n-button size="tiny" type="error" quaternary>{{ t('common.delete') }}</n-button>
              </template>
              {{ t('bots.confirmDeleteRoutine') }}
            </n-popconfirm>
          </div>
        </n-list-item>
      </n-list>

      <n-divider />
      <n-h6 style="margin: 0 0 10px 0;">{{ t('bots.addRoutine') }}</n-h6>
      <n-form label-placement="top" inline>
        <n-space :size="10" vertical>
          <n-input v-model:value="routineForm.name" :placeholder="t('bots.routineName')" style="width: 200px;" />
          <n-input v-model:value="routineForm.schedule" placeholder="0 9 * * *" style="width: 160px;" />
          <n-input v-model:value="routineForm.prompt" type="textarea" :rows="2" :placeholder="t('bots.routinePrompt')" style="flex: 1;" />
          <n-button type="primary" :loading="addingRoutine" :disabled="!routineForm.schedule.trim() || !routineForm.prompt.trim()" @click="handleAddRoutine">
            {{ t('common.add') }}
          </n-button>
        </n-space>
      </n-form>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref, watch } from 'vue'
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

const activeBot = computed(() =>
  botsStore.bots.find(b => b.name === botsStore.activeBotName) || null
)
// The list endpoint already embeds runtime info; keep a non-null object for template use
const activeBotObj = computed<Bot | null>(() => activeBot.value)

// Driven by the store: set when GET /api/bots returns 503 (bot mode off).
const botModeDisabled = computed(() => botsStore.modeDisabled)

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
  return d.toLocaleString(undefined, {
    month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit',
  })
}

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
  padding-bottom: 12px;
  border-bottom: 1px solid #eee;
  margin-bottom: 12px;
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
  overflow: hidden;
}
.chat-messages {
  height: 100%;
  overflow-y: auto;
  padding: 4px 8px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.chat-row {
  display: flex;
}
.chat-row.user {
  justify-content: flex-end;
}
.chat-row.assistant {
  justify-content: flex-start;
}
.bubble {
  max-width: 72%;
  border-radius: 12px;
  padding: 8px 12px;
  font-size: 14px;
  line-height: 1.5;
}
.bubble.user {
  background: #4f7cff;
  color: #fff;
  border-bottom-right-radius: 4px;
}
.bubble.assistant {
  background: #f3f4f6;
  color: #1f2937;
  border-bottom-left-radius: 4px;
}
.bubble.thinking {
  padding: 12px 16px;
}
.bubble-content {
  white-space: pre-wrap;
  word-break: break-word;
}
.bubble-time {
  margin-top: 4px;
  font-size: 11px;
  opacity: 0.65;
  text-align: right;
}
.chat-input-area {
  display: flex;
  gap: 10px;
  align-items: flex-end;
  padding-top: 12px;
  border-top: 1px solid #eee;
  flex-shrink: 0;
}
.routine-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.routine-info {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
}
@media (max-width: 768px) {
  .bubble {
    max-width: 88%;
  }
}
</style>