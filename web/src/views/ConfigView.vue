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
          <n-form-item :label="t('config.autoLinkGoals')">
            <n-switch v-model:value="generalForm.auto_link_goals" />
            <template #feedback>{{ t('config.autoLinkGoalsHint') }}</template>
          </n-form-item>
          <n-form-item :label="t('config.workingDirectory')">
            <n-space>
              <n-input v-model:value="generalForm.working_dir" :placeholder="t('config.workingDirectory')" style="flex: 1;" />
              <n-button size="small" @click="openDirPicker">
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

        <!-- Approval Settings -->
        <n-card size="small" :title="t('approval.title')">
          <n-form label-placement="left" label-width="200" style="max-width: 600px;">
            <n-form-item :label="t('approval.settings.strategy')">
              <n-select
                v-model:value="approvalForm.strategy"
                :options="strategyOptions"
                style="width: 320px;"
              />
            </n-form-item>
            <n-form-item :label="t('approval.settings.trustThreshold')">
              <n-input-number
                v-model:value="approvalForm.trust_threshold"
                :min="1"
                :max="100"
                style="width: 160px;"
              />
            </n-form-item>
            <n-form-item :label="t('approval.settings.enableLearning')">
              <n-switch v-model:value="approvalForm.enable_learning" />
            </n-form-item>
            <n-form-item :label="t('approval.settings.cliConfirm')">
              <n-switch v-model:value="approvalForm.enable_cli_confirm" />
            </n-form-item>
            <n-form-item>
              <n-button type="primary" :loading="saving" @click="saveApproval">{{ t('common.save') }}</n-button>
            </n-form-item>
          </n-form>
        </n-card>
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
import { reactive, ref, computed, onMounted } from 'vue'
import { useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { useConfigStore } from '@/stores/config'
import { useAuthStore } from '@/stores/auth'
import { request } from '@/api/client'
import LocaleSwitch from '@/components/LocaleSwitch.vue'
import { FolderOpenOutline, FolderOutline } from '@vicons/ionicons5'
import { listDirs } from '@/api/sessions'

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
  auto_link_goals: false,
  chat_mode: 'chat',
})

const chatModeOptions = computed(() => [
  { label: t('config.chatModes.chat'), value: 'chat' },
  { label: t('config.chatModes.coding'), value: 'coding' },
])

const agentForm = reactive({
  goal_max_turns: 60,
})

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

const fileStrategyOptions = computed(() => [
  { label: t('config.fileStrategies.auto'), value: 'auto' },
  { label: t('config.fileStrategies.url'), value: 'url' },
  { label: t('config.fileStrategies.base64'), value: 'base64' },
])

const approvalForm = reactive({
  strategy: 'smart',
  trust_threshold: 1,
  enable_learning: true,
  enable_cli_confirm: false,
})

const showDirPicker = ref(false)
const dirCurrentPath = ref('')
const dirEntries = ref<any[]>([])
const dirLoading = ref(false)

const dirParent = computed(() => dirEntries.value.find(e => e.name === '..')?.path || '')

const strategyOptions = computed(() => [
  { label: t('approval.settings.strategies.smart'), value: 'smart' },
  { label: t('approval.settings.strategies.manual'), value: 'manual' },
  { label: t('approval.settings.strategies.auto'), value: 'auto' },
])

function populateFromConfig(cfg: any) {
  generalForm.working_dir = cfg.working_dir || ''
  generalForm.secret_redaction = cfg.secret_redaction || false
  generalForm.auto_link_goals = cfg.auto_link_goals || false
  generalForm.chat_mode = cfg.chat_mode || 'chat'

  const agent = cfg.agent || {}
  agentForm.goal_max_turns = agent.goal_max_turns || 60

  const mem = cfg.memory || {}
  memoryForm.enabled = mem.enabled !== false

  const cortex = cfg.cortex || {}
  cortexForm.enabled = cortex.enabled !== false
  cortexForm.skill_min_pattern_freq = cortex.skill_min_pattern_freq || 3

  const server = cfg.server || {}
  serverForm.upload_url_prefix = server.upload_url_prefix || ''
  serverForm.file_strategy = server.file_strategy || 'auto'

  const ac = cfg.approval || {}
  approvalForm.strategy = ac.strategy || 'smart'
  approvalForm.trust_threshold = ac.trust_threshold || 1
  approvalForm.enable_learning = ac.enable_learning !== false
  approvalForm.enable_cli_confirm = ac.enable_cli_confirm || false
}

async function saveGeneral() {
  saving.value = true
  try {
    await configStore.updateConfig({
      working_dir: generalForm.working_dir,
      secret_redaction: generalForm.secret_redaction,
      auto_link_goals: generalForm.auto_link_goals,
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
      agent: { goal_max_turns: agentForm.goal_max_turns }
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

async function saveApproval() {
  saving.value = true
  try {
    await configStore.updateConfig({
      approval: {
        strategy: approvalForm.strategy,
        trust_threshold: approvalForm.trust_threshold,
        enable_learning: approvalForm.enable_learning,
        enable_cli_confirm: approvalForm.enable_cli_confirm,
      }
    })
    await configStore.loadConfig()
    message.success(t('approval.settings.settingsSaved'))
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
  loadDirs(path)
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