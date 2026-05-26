<template>
  <div>
    <h2 style="margin-bottom: 24px;">{{ t('config.title') }}</h2>
    <n-spin v-if="configStore.loading" />
    <n-tabs v-else type="line" animated>
      <!-- General Tab -->
      <n-tab-pane name="general" :tab="t('config.general')">
        <n-form label-placement="left" label-width="200" style="max-width: 600px; margin-top: 16px;">
          <n-form-item :label="t('config.secretRedaction')">
            <n-switch v-model:value="generalForm.secret_redaction" />
          </n-form-item>
          <n-form-item :label="t('config.workingDirectory')">
            <n-input v-model:value="generalForm.working_dir" :placeholder="t('config.workingDirectory')" />
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
  </div>
</template>

<script setup lang="ts">
import { reactive, ref, onMounted } from 'vue'
import { useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { useConfigStore } from '@/stores/config'
import { useAuthStore } from '@/stores/auth'
import { request } from '@/api/client'

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
})

const agentForm = reactive({
  goal_max_turns: 60,
})

const memoryForm = reactive({
  enabled: true,
})

function populateFromConfig(cfg: any) {
  generalForm.working_dir = cfg.working_dir || ''
  generalForm.secret_redaction = cfg.secret_redaction || false

  const agent = cfg.agent || {}
  agentForm.goal_max_turns = agent.goal_max_turns || 60

  const mem = cfg.memory || {}
  memoryForm.enabled = mem.enabled !== false
}

async function saveGeneral() {
  saving.value = true
  try {
    await configStore.updateConfig({
      working_dir: generalForm.working_dir,
      secret_redaction: generalForm.secret_redaction,
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
