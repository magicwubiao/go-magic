<template>
  <div>
    <h2 style="margin-bottom: 24px;">{{ t('config.title') }}</h2>

    <!-- Language Settings -->
    <n-card :title="t('common.language')" style="margin-bottom: 24px;">
      <locale-switch />
    </n-card>

    <n-spin v-if="configStore.loading" />
    <n-tabs v-else type="line" animated>
      <!-- General Tab -->
      <n-tab-pane name="general" :tab="t('config.general')">
        <n-form label-placement="left" label-width="200" style="max-width: 600px; margin-top: 16px;">
          <n-form-item :label="t('config.secretRedaction')">
            <n-switch v-model:value="generalForm.secret_redaction" />
          </n-form-item>
          <n-form-item :label="t('config.workingDirectory')">
            <n-space>
              <n-input v-model:value="generalForm.working_dir" :placeholder="t('config.workingDirectory')" style="flex: 1;" />
              <n-button @click="openDirPicker">
                <template #icon><n-icon><FolderOpenOutline /></n-icon></template>
              </n-button>
            </n-space>
          </n-form-item>
          <n-form-item :label="t('config.chatMode')">
            <n-select v-model:value="generalForm.chat_mode" :options="chatModeOptions" />
          </n-form-item>
          <n-form-item>
            <n-button type="primary" :loading="saving" @click="saveGeneral">{{ t('common.save') }}</n-button>
          </n-form-item>
        </n-form>
      </n-tab-pane>

      <!-- Agent Tab -->
      <n-tab-pane name="agent" :tab="t('config.agent')">
        <n-form label-placement="left" label-width="200" style="max-width: 600px; margin-top: 16px;">
          <n-form-item :label="t('config.goalMaxTurns')">
            <n-input-number v-model:value="agentForm.goal_max_turns" :min="1" :max="200" />
          </n-form-item>
          <n-form-item :label="t('config.maxTurns')">
            <n-input-number v-model:value="agentForm.max_turns" :min="1" :max="500" />
            <span style="margin-left: 12px; color: #999;">{{ t('config.maxTurnsHint') }}</span>
          </n-form-item>
          <n-divider style="margin: 8px 0 24px;" />
          <h3 style="margin: 0 0 16px 0;">{{ t('config.botMode') }}</h3>
          <n-form-item :label="t('config.botModeEnabled')">
            <n-switch v-model:value="botModeForm.enabled" />
            <span style="margin-left: 12px; color: #999;">{{ t('config.botModeEnabledHint') }}</span>
          </n-form-item>
          <n-form-item :label="t('config.botModeHistoryWindow')">
            <n-input-number v-model:value="botModeForm.history_window" :min="20" :max="2000" />
            <span style="margin-left: 12px; color: #999;">{{ t('config.botModeHistoryWindowHint') }}</span>
          </n-form-item>
          <n-form-item :label="t('config.botModeInjectProtocol')">
            <n-select
              v-model:value="botModeForm.inject_bot_protocol"
              :options="injectProtocolOptions"
              style="width: 160px;"
            />
            <span style="margin-left: 12px; color: #999;">{{ t('config.botModeInjectProtocolHint') }}</span>
          </n-form-item>
          <n-alert v-if="botModeNeedsRestart" type="warning" style="margin-bottom: 12px;">
            {{ t('config.botModeRestartHint') }}
          </n-alert>
          <n-form-item>
            <n-button type="primary" :loading="saving" @click="saveAgent">{{ t('common.save') }}</n-button>
          </n-form-item>
        </n-form>
      </n-tab-pane>

      <!-- Memory Tab -->
      <n-tab-pane name="memory" :tab="t('config.memory')">
        <n-form label-placement="left" label-width="200" style="max-width: 600px; margin-top: 16px;">
          <n-form-item :label="t('config.enableMemory')">
            <n-switch v-model:value="memoryForm.enabled" />
          </n-form-item>
          <n-form-item>
            <n-button type="primary" :loading="saving" @click="saveMemory">{{ t('common.save') }}</n-button>
          </n-form-item>
        </n-form>
      </n-tab-pane>

      <!-- Cortex Tab -->
      <n-tab-pane name="cortex" :tab="t('config.cortex')">
        <n-form label-placement="left" label-width="200" style="max-width: 600px; margin-top: 16px;">
          <n-form-item :label="t('config.cortexEnabled')">
            <n-switch v-model:value="cortexForm.enabled" />
            <span style="margin-left: 12px; color: #999;">{{ t('config.cortexEnabledHint') }}</span>
          </n-form-item>
          <n-form-item :label="t('config.cortexSkillPatternFreq')">
            <n-input-number v-model:value="cortexForm.skill_min_pattern_freq" :min="1" :max="10" />
            <span style="margin-left: 12px; color: #999;">{{ t('config.cortexSkillPatternFreqHint') }}</span>
          </n-form-item>
          <n-form-item>
            <n-button type="primary" :loading="saving" @click="saveCortex">{{ t('common.save') }}</n-button>
          </n-form-item>
        </n-form>
      </n-tab-pane>

      <!-- Server Tab -->
      <n-tab-pane name="server" :tab="t('config.server')">
        <n-form label-placement="left" label-width="200" style="max-width: 600px; margin-top: 16px;">
          <n-form-item :label="t('config.uploadUrlPrefix')">
            <n-input
              v-model:value="serverForm.upload_url_prefix"
              :placeholder="t('config.uploadUrlPrefixPlaceholder')"
              style="width: 400px;"
            />
            <template #feedback>
              <span style="color: #999; font-size: 12px;">{{ t('config.uploadUrlPrefixHint') }}</span>
            </template>
          </n-form-item>
          <n-form-item :label="t('config.fileStrategy')">
            <n-select
              v-model:value="serverForm.file_strategy"
              :options="fileStrategyOptions"
              style="width: 200px;"
            />
            <template #feedback>
              <span style="color: #999; font-size: 12px;">{{ t('config.fileStrategyHint') }}</span>
            </template>
          </n-form-item>
          <n-form-item>
            <n-button type="primary" :loading="saving" @click="saveServer">{{ t('common.save') }}</n-button>
          </n-form-item>
        </n-form>
      </n-tab-pane>

      <!-- Security Tab -->
      <n-tab-pane name="security" :tab="t('config.security')">
        <n-form label-placement="left" label-width="200" style="max-width: 600px; margin-top: 16px;">
          <n-form-item :label="t('config.authStatus')">
            <n-tag :type="authConfigured ? 'success' : 'warning'">
              {{ authConfigured ? t('config.passwordSet') : t('config.notConfigured') }}
            </n-tag>
          </n-form-item>
          <n-form-item :label="t('config.resetPassword')">
            <n-popconfirm @positive-click="resetPassword">
              <template #trigger>
                <n-button type="warning">{{ t('config.resetPassword') }}</n-button>
              </template>
              {{ t('config.resetPasswordConfirm') }}
            </n-popconfirm>
          </n-form-item>
        </n-form>

        <n-divider />

        <!-- 审批设置已移至「审批」页面统一管理，避免双入口导致状态不一致 -->
      </n-tab-pane>

      <!-- Privacy / PII 脱敏 Tab -->
      <n-tab-pane name="privacy" :tab="t('config.privacy')">
        <n-form label-placement="left" label-width="220" style="max-width: 640px; margin-top: 16px;">
          <n-alert type="info" :show-icon="true" style="margin-bottom: 16px;">
            {{ t('config.privacyHint') }}
          </n-alert>
          <n-form-item :label="t('config.privacyEnabled')">
            <n-switch v-model:value="privacyForm.enabled" />
            <span style="margin-left: 12px; color: #999;">{{ t('config.privacyEnabledHint') }}</span>
          </n-form-item>
          <n-form-item :label="t('config.privacyRedactPhone')">
            <n-switch v-model:value="privacyForm.redact_phone" />
          </n-form-item>
          <n-form-item :label="t('config.privacyRedactEmail')">
            <n-switch v-model:value="privacyForm.redact_email" />
          </n-form-item>
          <n-form-item :label="t('config.privacyRedactIDCard')">
            <n-switch v-model:value="privacyForm.redact_id_card" />
          </n-form-item>
          <n-form-item :label="t('config.privacyRedactBankCard')">
            <n-switch v-model:value="privacyForm.redact_bank_card" />
          </n-form-item>
          <n-form-item :label="t('config.privacyRedactIP')">
            <n-switch v-model:value="privacyForm.redact_ip" />
          </n-form-item>
          <n-form-item :label="t('config.privacyRedactAddress')">
            <n-switch v-model:value="privacyForm.redact_address" />
          </n-form-item>
          <n-form-item>
            <n-button type="primary" :loading="saving" @click="savePrivacy">{{ t('common.save') }}</n-button>
          </n-form-item>
        </n-form>
      </n-tab-pane>

      <!-- Raw JSON Tab -->
      <n-tab-pane name="raw" :tab="t('config.rawJson')">
        <div style="margin-top: 16px;">
          <n-alert v-if="rawError" type="error" style="margin-bottom: 12px;" closable @close="rawError = null">
            {{ rawError }}
          </n-alert>
          <n-input
            v-model:value="rawJson"
            type="textarea"
            :rows="20"
            :placeholder="t('config.rawJson')"
            style="font-family: monospace;"
          />
          <n-space style="margin-top: 12px;">
            <n-button type="primary" :loading="saving" @click="saveRaw">{{ t('common.save') }}</n-button>
            <n-button @click="formatJson">{{ t('common.format') }}</n-button>
            <n-button @click="loadRaw">{{ t('common.refresh') }}</n-button>
          </n-space>
        </div>
      </n-tab-pane>
    </n-tabs>

    <!-- Directory picker modal -->
    <n-modal v-model:show="showDirPicker" preset="card" :title="t('chat.workDirSelect')" style="max-width: 560px;">
      <div class="dir-picker">
        <div class="dir-breadcrumb">
          <n-button size="tiny" quaternary :disabled="!dirParent" @click="navigateDir(dirParent)">
            <template #icon><n-icon><FolderOpenOutline /></n-icon></template>
            ..
          </n-button>
          <n-text class="dir-current" :title="dirCurrentPath">{{ dirCurrentPath }}</n-text>
          <div class="dir-actions">
            <n-input
              v-if="showNewFolderInput"
              v-model:value="newFolderName"
              size="tiny"
              placeholder="文件夹名"
              style="width: 140px;"
              @keyup.enter="createNewFolder"
              @blur="cancelNewFolder"
              ref="newFolderInputRef"
            />
            <n-button v-else size="tiny" quaternary :title="t('chat.newFolder')" @click="startNewFolder">
              <template #icon><n-icon><AddOutline /></n-icon></template>
            </n-button>
          </div>
        </div>
        <div class="dir-list">
          <div v-if="dirLoading" class="dir-loading">
            <n-spin size="small" /> <span style="margin-left: 8px;">{{ t('chat.workDirLoading') }}</span>
          </div>
          <div v-else-if="dirEntries.length === 0" class="dir-empty">
            {{ t('chat.workDirEmpty') }}
          </div>
          <div
            v-for="entry in dirEntries"
            v-else
            :key="entry.path"
            class="dir-item"
            @click="navigateDir(entry.path)"
          >
            <n-icon size="16"><FolderOutline /></n-icon>
            <span>{{ entry.name }}</span>
          </div>
        </div>
      </div>
      <template #footer>
        <n-space>
          <n-button @click="showDirPicker = false">{{ t('common.cancel') }}</n-button>
          <n-button type="primary" @click="selectWorkDir">{{ t('chat.workDirSet') }}</n-button>
        </n-space>
      </template>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { reactive, ref, computed, onMounted, nextTick } from 'vue'
import { useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { useConfigStore } from '@/stores/config'
import { useAuthStore } from '@/stores/auth'
import { request } from '@/api/client'
import LocaleSwitch from '@/components/LocaleSwitch.vue'
import { FolderOpenOutline, FolderOutline, AddOutline } from '@vicons/ionicons5'
import { listDirs, createDir } from '@/api/sessions'

const { t } = useI18n()

const message = useMessage()
const configStore = useConfigStore()
const authStore = useAuthStore()
const saving = ref(false)
const rawJson = ref('')
const rawError = ref<string | null>(null)
const authConfigured = ref(false)

const generalForm = reactive({
  working_dir: '',
  secret_redaction: false,
  chat_mode: 'chat',
})

const chatModeOptions = computed(() => [
  { label: t('config.chatModes.chat'), value: 'chat' },
  { label: t('config.chatModes.coding'), value: 'coding' },
])

const injectProtocolOptions = computed(() => [
  { label: t('config.botModeInjectDefault'), value: '' },
  { label: t('common.enabled'), value: 'on' },
  { label: t('common.disabled'), value: 'off' },
])

// Bot Mode enabled flag changed since page load -> manager must restart.
const botModeNeedsRestart = computed(
  () => botModeForm.enabled !== botModeEnabledAtLoad
)

const agentForm = reactive({
  goal_max_turns: 60,
  max_turns: 60,
})

// Bot Mode section on the Agent tab (config.bot_mode.*)
const botModeForm = reactive({
  enabled: false,
  history_window: 200,
  // tri-state: null/'' = follow default, 'on'/'off' explicit
  inject_bot_protocol: '' as '' | 'on' | 'off',
})
// Track the originally loaded enabled flag to warn when a restart is needed.
let botModeEnabledAtLoad = false

const memoryForm = reactive({
  enabled: true,
})

const cortexForm = reactive({
  enabled: true,
  skill_min_pattern_freq: 3,
})

const serverForm = reactive({
  upload_url_prefix: '',
  file_strategy: 'auto',
})

const privacyForm = reactive({
  enabled: true,
  redact_phone: true,
  redact_email: true,
  redact_id_card: true,
  redact_bank_card: true,
  redact_ip: true,
  redact_address: true,
})

const fileStrategyOptions = computed(() => [
  { label: t('config.fileStrategies.auto'), value: 'auto' },
  { label: t('config.fileStrategies.url'), value: 'url' },
  { label: t('config.fileStrategies.base64'), value: 'base64' },
])

const showDirPicker = ref(false)
const dirCurrentPath = ref('')
const dirEntries = ref<any[]>([])
const dirLoading = ref(false)
const showNewFolderInput = ref(false)
const newFolderName = ref('')
const newFolderInputRef = ref<{ focus: () => void } | null>(null)

const dirParent = computed(() => dirEntries.value.find(e => e.name === '..')?.path || '')

function populateFromConfig(cfg: any) {
  generalForm.working_dir = cfg.working_dir || ''
  generalForm.secret_redaction = cfg.secret_redaction || false
  generalForm.chat_mode = cfg.chat_mode || 'chat'

  const agent = cfg.agent || {}
  agentForm.goal_max_turns = agent.goal_max_turns || 60
  // 0 means "use built-in default (60)"; show the effective value in UI.
  const maxTurns = Number(agent.max_turns) || 0
  agentForm.max_turns = maxTurns > 0 ? maxTurns : 60

  const botMode = cfg.bot_mode || {}
  botModeForm.enabled = botMode.enabled === true
  botModeEnabledAtLoad = botModeForm.enabled
  const hw = Number(botMode.history_window) || 0
  botModeForm.history_window = hw > 0 ? hw : 200
  // Backend stores a JSON bool pointer; map to tri-state select value.
  if (botMode.inject_bot_protocol === true) botModeForm.inject_bot_protocol = 'on'
  else if (botMode.inject_bot_protocol === false) botModeForm.inject_bot_protocol = 'off'
  else botModeForm.inject_bot_protocol = ''

  const mem = cfg.memory || {}
  memoryForm.enabled = mem.enabled !== false

  const cortex = cfg.cortex || {}
  cortexForm.enabled = cortex.enabled !== false
  cortexForm.skill_min_pattern_freq = cortex.skill_min_pattern_freq || 3

  const server = cfg.server || {}
  serverForm.upload_url_prefix = server.upload_url_prefix || ''
  serverForm.file_strategy = server.file_strategy || 'auto'

  const privacy = cfg.privacy || {}
  privacyForm.enabled = privacy.enabled !== false
  privacyForm.redact_phone = privacy.redact_phone !== false
  privacyForm.redact_email = privacy.redact_email !== false
  privacyForm.redact_id_card = privacy.redact_id_card !== false
  privacyForm.redact_bank_card = privacy.redact_bank_card !== false
  privacyForm.redact_ip = privacy.redact_ip !== false
  privacyForm.redact_address = privacy.redact_address !== false
}

async function saveGeneral() {
  saving.value = true
  try {
    await configStore.updateConfig({
      working_dir: generalForm.working_dir,
      secret_redaction: generalForm.secret_redaction,
      chat_mode: generalForm.chat_mode,
    })
    // Reload to ensure sync with server
    await configStore.loadConfig()
    message.success(t('config.generalSaved'))
  } catch (e) {
    message.error(t('common.error') + ': ' + (e instanceof Error ? e.message : 'Unknown error'))
  } finally {
    saving.value = false
  }
}

async function saveAgent() {
  saving.value = true
  try {
    await configStore.updateConfig({
      agent: {
        goal_max_turns: agentForm.goal_max_turns,
        max_turns: agentForm.max_turns,
      },
      bot_mode: {
        enabled: botModeForm.enabled,
        history_window: botModeForm.history_window,
        // '' means "follow default"; send explicit bool otherwise.
        inject_bot_protocol:
          botModeForm.inject_bot_protocol === 'on' ? true :
          botModeForm.inject_bot_protocol === 'off' ? false : undefined,
      },
    })
    await configStore.loadConfig()
    message.success(t('config.agentSaved'))
  } catch (e) {
    message.error(t('common.error') + ': ' + (e instanceof Error ? e.message : 'Unknown error'))
  } finally {
    saving.value = false
  }
}

async function saveMemory() {
  saving.value = true
  try {
    await configStore.updateConfig({
      memory: { enabled: memoryForm.enabled }
    })
    await configStore.loadConfig()
    message.success(t('config.memorySaved'))
  } catch (e) {
    message.error(t('common.error') + ': ' + (e instanceof Error ? e.message : 'Unknown error'))
  } finally {
    saving.value = false
  }
}

async function saveCortex() {
  saving.value = true
  try {
    await configStore.updateConfig({
      cortex: {
        enabled: cortexForm.enabled,
        skill_min_pattern_freq: cortexForm.skill_min_pattern_freq,
      }
    })
    await configStore.loadConfig()
    message.success(t('config.cortexSaved'))
  } catch (e) {
    message.error(t('common.error') + ': ' + (e instanceof Error ? e.message : 'Unknown error'))
  } finally {
    saving.value = false
  }
}

async function saveServer() {
  saving.value = true
  try {
    await configStore.updateConfig({
      server: {
        upload_url_prefix: serverForm.upload_url_prefix,
        file_strategy: serverForm.file_strategy,
      }
    })
    await configStore.loadConfig()
    message.success(t('config.serverSaved'))
  } catch (e) {
    message.error(t('common.error') + ': ' + (e instanceof Error ? e.message : 'Unknown error'))
  } finally {
    saving.value = false
  }
}

async function savePrivacy() {
  saving.value = true
  try {
    await configStore.updateConfig({
      privacy: {
        enabled: privacyForm.enabled,
        redact_phone: privacyForm.redact_phone,
        redact_email: privacyForm.redact_email,
        redact_id_card: privacyForm.redact_id_card,
        redact_bank_card: privacyForm.redact_bank_card,
        redact_ip: privacyForm.redact_ip,
        redact_address: privacyForm.redact_address,
      }
    })
    await configStore.loadConfig()
    message.success(t('config.privacySaved'))
  } catch (e) {
    message.error(t('common.error') + ': ' + (e instanceof Error ? e.message : 'Unknown error'))
  } finally {
    saving.value = false
  }
}

async function resetPassword() {
  try {
    await request('/auth/reset', { method: 'POST' })
    authConfigured.value = false
    authStore.logout()
    message.success(t('config.passwordReset'))
  } catch (e) {
    message.error(t('config.failedToReset'))
  }
}

function formatJson(): void {
  try {
    const parsed = JSON.parse(rawJson.value)
    rawJson.value = JSON.stringify(parsed, null, 2)
    rawError.value = null
  } catch (e) {
    rawError.value = t('config.invalidJson') + ': ' + (e instanceof Error ? e.message : 'Parse error')
  }
}

async function saveRaw() {
  saving.value = true
  rawError.value = null
  try {
    JSON.parse(rawJson.value)
  } catch (e) {
    rawError.value = t('config.invalidJson') + ': ' + (e instanceof Error ? e.message : 'Parse error')
    saving.value = false
    return
  }
  try {
    await request('/config', {
      method: 'PUT',
      body: rawJson.value,
    })
    message.success(t('config.rawConfigSaved'))
  } catch (e) {
    message.error(t('common.error') + ': ' + (e instanceof Error ? e.message : 'Unknown error'))
  } finally {
    saving.value = false
  }
}

async function loadRaw() {
  try {
    const res: any = await request('/config')
    rawJson.value = JSON.stringify(res, null, 2)
    rawError.value = null
  } catch (e) {
    rawJson.value = '{}'
  }
}

async function loadDirs(path?: string) {
  dirLoading.value = true
  try {
    const res = await listDirs(path)
    dirCurrentPath.value = res.current
    dirEntries.value = res.dirs || []
  } catch (e) {
    console.error('Failed to list directories:', e)
    dirEntries.value = []
  } finally {
    dirLoading.value = false
  }
}

function openDirPicker() {
  loadDirs(generalForm.working_dir || undefined)
  showDirPicker.value = true
}

function navigateDir(path: string) {
  if (!path) return
  showNewFolderInput.value = false
  newFolderName.value = ''
  loadDirs(path)
}

function startNewFolder() {
  showNewFolderInput.value = true
  newFolderName.value = ''
  nextTick(() => {
    newFolderInputRef.value?.focus()
  })
}

function cancelNewFolder() {
  setTimeout(() => {
    showNewFolderInput.value = false
    newFolderName.value = ''
  }, 150)
}

async function createNewFolder() {
  const name = newFolderName.value.trim()
  if (!name) {
    showNewFolderInput.value = false
    return
  }
  try {
    await createDir(dirCurrentPath.value, name)
    newFolderName.value = ''
    showNewFolderInput.value = false
    loadDirs(dirCurrentPath.value)
  } catch (e: any) {
    message.error(e?.message || t('common.operationFailed'))
  }
}

function selectWorkDir() {
  if (!dirCurrentPath.value) return
  generalForm.working_dir = dirCurrentPath.value
  showDirPicker.value = false
}

onMounted(async () => {
  await configStore.loadConfig()
  if (configStore.config) {
    populateFromConfig(configStore.config)
  }
  await authStore.checkStatus()
  authConfigured.value = authStore.configured
  await loadRaw()
})
</script>

<style scoped>
.dir-picker {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.dir-breadcrumb {
  display: flex;
  align-items: center;
  gap: 8px;
  padding-bottom: 8px;
  border-bottom: 1px solid var(--n-border-color, #eee);
}

.dir-current {
  font-size: 13px;
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: monospace;
}

.dir-actions {
  display: flex;
  align-items: center;
  gap: 4px;
}

.dir-list {
  max-height: 320px;
  overflow-y: auto;
  min-height: 120px;
}

.dir-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 8px;
  border-radius: 4px;
  cursor: pointer;
  font-size: 13px;
}

.dir-item:hover {
  background: var(--n-color-hover, #f5f5f5);
}

.dir-empty,
.dir-loading {
  display: flex;
  align-items: center;
  padding: 24px;
  color: #999;
  justify-content: center;
}
</style>