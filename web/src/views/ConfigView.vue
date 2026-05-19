<template>
  <div>
    <h2 style="margin-bottom: 24px;">Configuration</h2>
    <n-spin v-if="configStore.loading" />
    <n-tabs v-else type="line" animated>
      <!-- Agent Tab -->
      <n-tab-pane name="agent" tab="Agent">
        <n-form label-placement="left" label-width="200" style="max-width: 600px; margin-top: 16px;">
          <n-form-item label="Working Directory">
            <n-input-group>
              <n-input v-model:value="agentForm.working_dir" placeholder="Select or type a directory path" />
              <n-button @click="pickDirectory">📂 Browse</n-button>
            </n-input-group>
          </n-form-item>
          <n-form-item label="Goal Max Turns">
            <n-input-number v-model:value="agentForm.goal_max_turns" :min="1" :max="200" />
          </n-form-item>
          <n-form-item label="Max Turns">
            <n-input-number v-model:value="agentForm.max_turns" :min="1" :max="200" />
          </n-form-item>
          <n-form-item label="Max Iterations">
            <n-input-number v-model:value="agentForm.max_iterations" :min="1" :max="200" />
          </n-form-item>
          <n-form-item label="Context Window">
            <n-input-number v-model:value="agentForm.context_window" :min="1000" :max="1000000" :step="1000" />
          </n-form-item>
          <n-form-item label="Compression Enabled">
            <n-switch v-model:value="agentForm.compression_enabled" />
          </n-form-item>
          <n-form-item label="Compression Ratio">
            <n-slider v-model:value="agentForm.compression_ratio" :min="0.1" :max="1" :step="0.05" />
          </n-form-item>
          <n-form-item>
            <n-space>
              <n-button type="primary" :loading="saving" @click="saveAgent">Save Agent Config</n-button>
            </n-space>
          </n-form-item>
        </n-form>
      </n-tab-pane>

      <!-- Memory Tab -->
      <n-tab-pane name="memory" tab="Memory">
        <n-form label-placement="left" label-width="200" style="max-width: 600px; margin-top: 16px;">
          <n-form-item label="Enable Memory">
            <n-switch v-model:value="memoryForm.enabled" />
          </n-form-item>
          <n-form-item label="Auto Recall">
            <n-switch v-model:value="memoryForm.auto_recall" />
          </n-form-item>
          <n-form-item label="Recall Limit">
            <n-input-number v-model:value="memoryForm.recall_limit" :min="1" :max="50" />
          </n-form-item>
          <n-form-item>
            <n-space>
              <n-button type="primary" :loading="saving" @click="saveMemory">Save Memory Config</n-button>
            </n-space>
          </n-form-item>
        </n-form>
      </n-tab-pane>

      <!-- Security Tab -->
      <n-tab-pane name="security" tab="Security">
        <n-form label-placement="left" label-width="200" style="max-width: 600px; margin-top: 16px;">
          <n-form-item label="Auth Status">
            <n-tag :type="authConfigured ? 'success' : 'warning'">
              {{ authConfigured ? 'Password Set' : 'Not Configured' }}
            </n-tag>
          </n-form-item>
          <n-form-item label="Reset Password">
            <n-popconfirm @positive-click="resetPassword">
              <template #trigger>
                <n-button type="warning">Reset Password</n-button>
              </template>
              This will delete the current password. You will need to set a new one on next login.
            </n-popconfirm>
          </n-form-item>
        </n-form>
      </n-tab-pane>

      <!-- Raw JSON Tab -->
      <n-tab-pane name="raw" tab="Raw JSON">
        <div style="margin-top: 16px;">
          <n-alert v-if="rawError" type="error" style="margin-bottom: 12px;" closable @close="rawError = null">
            {{ rawError }}
          </n-alert>
          <n-input
            v-model:value="rawJson"
            type="textarea"
            :rows="20"
            placeholder="Raw JSON configuration"
            style="font-family: monospace;"
          />
          <n-space style="margin-top: 12px;">
            <n-button type="primary" :loading="saving" @click="saveRaw">Save Raw Config</n-button>
            <n-button @click="formatJson">Format</n-button>
            <n-button @click="loadRaw">Reload</n-button>
          </n-space>
        </div>
      </n-tab-pane>
    </n-tabs>
  </div>
</template>

<script setup lang="ts">
import { reactive, ref, onMounted } from 'vue'
import { useMessage } from 'naive-ui'
import { useConfigStore } from '@/stores/config'
import { useAuthStore } from '@/stores/auth'
import { request } from '@/api/client'

const message = useMessage()
const configStore = useConfigStore()
const authStore = useAuthStore()
const saving = ref(false)
const rawJson = ref('')
const rawError = ref<string | null>(null)
const authConfigured = ref(false)

const agentForm = reactive({
  working_dir: '',
  goal_max_turns: 60,
  max_turns: 60,
  max_iterations: 80,
  context_window: 200000,
  compression_enabled: true,
  compression_ratio: 0.7,
})

const memoryForm = reactive({
  enabled: true,
  auto_recall: true,
  recall_limit: 5,
})

function populateFromConfig(cfg: any) {
  const agent = cfg.agent || {}
  agentForm.working_dir = agent.working_dir || ''
  agentForm.goal_max_turns = agent.goal_max_turns || 60
  agentForm.max_turns = agent.max_turns || 60
  agentForm.max_iterations = agent.max_iterations || 80
  agentForm.context_window = agent.context_window || 200000
  agentForm.compression_enabled = agent.compression_enabled !== false
  agentForm.compression_ratio = agent.compression_ratio || 0.7

  const mem = cfg.memory || {}
  memoryForm.enabled = mem.enabled !== false
  memoryForm.auto_recall = mem.auto_recall !== false
  memoryForm.recall_limit = mem.recall_limit || 5
}

function pickDirectory(): void {
  const input = document.createElement('input')
  input.type = 'file'
  input.webkitdirectory = true
  input.onchange = () => {
    if (input.files && input.files.length > 0) {
      agentForm.working_dir = input.files[0].webkitRelativePath.split('/')[0]
      // Try to get full path from the file
      const path = (input.files[0] as any).path
      if (path) {
        agentForm.working_dir = path.replace(/\\/g, '/').replace(/\/[^/]*$/, '')
      }
    }
  }
  input.click()
}

async function saveAgent() {
  saving.value = true
  try {
    await configStore.updateConfig({ agent: { ...agentForm } })
    message.success('Agent config saved')
  } catch (e) {
    message.error('Failed to save: ' + (e instanceof Error ? e.message : 'Unknown error'))
  } finally {
    saving.value = false
  }
}

async function saveMemory() {
  saving.value = true
  try {
    await configStore.updateConfig({ memory: { ...memoryForm } })
    message.success('Memory config saved')
  } catch (e) {
    message.error('Failed to save: ' + (e instanceof Error ? e.message : 'Unknown error'))
  } finally {
    saving.value = false
  }
}

async function resetPassword() {
  try {
    await request('/auth/reset', { method: 'POST' })
    authConfigured.value = false
    authStore.logout()
    message.success('Password reset. Please set a new password on next login.')
  } catch (e) {
    message.error('Failed to reset password')
  }
}

function formatJson(): void {
  try {
    const parsed = JSON.parse(rawJson.value)
    rawJson.value = JSON.stringify(parsed, null, 2)
    rawError.value = null
  } catch (e) {
    rawError.value = 'Invalid JSON: ' + (e instanceof Error ? e.message : 'Parse error')
  }
}

async function saveRaw() {
  saving.value = true
  rawError.value = null
  try {
    JSON.parse(rawJson.value)
  } catch (e) {
    rawError.value = 'Invalid JSON: ' + (e instanceof Error ? e.message : 'Parse error')
    saving.value = false
    return
  }
  try {
    await request('/config', {
      method: 'PUT',
      body: rawJson.value,
    })
    message.success('Raw config saved')
  } catch (e) {
    message.error('Failed to save: ' + (e instanceof Error ? e.message : 'Unknown error'))
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
