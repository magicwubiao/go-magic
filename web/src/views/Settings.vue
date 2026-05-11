<template>
  <div class="settings-view">
    <n-tabs type="line" animated>
      <!-- Display Settings -->
      <n-tab-pane name="display" tab="Display">
        <n-card title="Display Settings">
          <n-form :model="displaySettings" label-placement="left" label-width="180">
            <n-form-item label="Streaming Response">
              <n-switch v-model:value="displaySettings.streaming" />
            </n-form-item>
            <n-form-item label="Compact Mode">
              <n-space vertical>
                <n-switch v-model:value="displaySettings.compactMode" />
                <n-text depth="3">Show shorter UI elements</n-text>
              </n-space>
            </n-form-item>
            <n-form-item label="Show Reasoning">
              <n-space vertical>
                <n-switch v-model:value="displaySettings.showReasoning" />
                <n-text depth="3">Display reasoning/thinking content</n-text>
              </n-space>
            </n-form-item>
            <n-form-item label="Show Cost">
              <n-space vertical>
                <n-switch v-model:value="displaySettings.showCost" />
                <n-text depth="3">Display estimated cost per response</n-text>
              </n-space>
            </n-form-item>
            <n-form-item label="Theme">
              <n-select
                v-model:value="displaySettings.theme"
                :options="themeOptions"
                style="width: 200px"
              />
            </n-form-item>
            <n-form-item label="Language">
              <n-select
                v-model:value="displaySettings.language"
                :options="languageOptions"
                style="width: 200px"
              />
            </n-form-item>
            <n-form-item label="Active Skin">
              <n-select
                v-model:value="displaySettings.skin"
                :options="skinOptions"
                style="width: 200px"
              />
            </n-form-item>
          </n-form>
          <n-divider />
          <n-space justify="end">
            <n-button type="primary" @click="saveDisplaySettings">Save</n-button>
          </n-space>
        </n-card>
      </n-tab-pane>

      <!-- Agent Settings -->
      <n-tab-pane name="agent" tab="Agent">
        <n-card title="Agent Settings">
          <n-form :model="agentSettings" label-placement="left" label-width="180">
            <n-form-item label="Max Turns">
              <n-input-number
                v-model:value="agentSettings.maxTurns"
                :min="1"
                :max="100"
                style="width: 120px"
              />
              <n-text depth="3" style="margin-left: 12px">Maximum conversation turns</n-text>
            </n-form-item>
            <n-form-item label="Timeout (seconds)">
              <n-input-number
                v-model:value="agentSettings.timeout"
                :min="10"
                :max="600"
                :step="10"
                style="width: 120px"
              />
            </n-form-item>
            <n-form-item label="Temperature">
              <n-slider
                v-model:value="agentSettings.temperature"
                :min="0"
                :max="2"
                :step="0.1"
                style="width: 200px"
              />
              <n-text style="margin-left: 12px">{{ agentSettings.temperature }}</n-text>
            </n-form-item>
            <n-form-item label="Tool Enforcement">
              <n-space vertical>
                <n-switch v-model:value="agentSettings.toolEnforcement" />
                <n-text depth="3">Agent must use a tool on every turn</n-text>
              </n-space>
            </n-form-item>
            <n-form-item label="Default Model">
              <n-select
                v-model:value="agentSettings.defaultModel"
                :options="modelOptions"
                style="width: 300px"
                filterable
              />
            </n-form-item>
            <n-form-item label="System Prompt">
              <n-input
                v-model:value="agentSettings.systemPrompt"
                type="textarea"
                placeholder="System prompt..."
                :rows="4"
                style="width: 500px"
              />
            </n-form-item>
          </n-form>
          <n-divider />
          <n-space justify="end">
            <n-button @click="resetAgentSettings">Reset</n-button>
            <n-button type="primary" @click="saveAgentSettings">Save</n-button>
          </n-space>
        </n-card>
      </n-tab-pane>

      <!-- Memory Settings -->
      <n-tab-pane name="memory" tab="Memory">
        <n-card title="Memory Settings">
          <n-form :model="memorySettings" label-placement="left" label-width="180">
            <n-form-item label="Enable Memory">
              <n-switch v-model:value="memorySettings.enabled" />
            </n-form-item>
            <n-form-item label="Max Entries">
              <n-input-number
                v-model:value="memorySettings.maxEntries"
                :min="100"
                :max="10000"
                :step="100"
                style="width: 120px"
              />
            </n-form-item>
            <n-form-item label="Max Chars per Entry">
              <n-input-number
                v-model:value="memorySettings.maxCharsPerEntry"
                :min="100"
                :max="10000"
                :step="100"
                style="width: 120px"
              />
            </n-form-item>
            <n-form-item label="Auto-save Important">
              <n-space vertical>
                <n-switch v-model:value="memorySettings.autoSave" />
                <n-text depth="3">Automatically save important information</n-text>
              </n-space>
            </n-form-item>
            <n-form-item label="Learn from Mistakes">
              <n-space vertical>
                <n-switch v-model:value="memorySettings.learnFromMistakes" />
                <n-text depth="3">Remember and avoid past errors</n-text>
              </n-space>
            </n-form-item>
          </n-form>
          <n-divider />
          <n-space justify="end">
            <n-button @click="clearMemory">Clear Memory</n-button>
            <n-button type="primary" @click="saveMemorySettings">Save</n-button>
          </n-space>
        </n-card>
      </n-tab-pane>

      <!-- Session Settings -->
      <n-tab-pane name="session" tab="Session">
        <n-card title="Session Settings">
          <n-form :model="sessionSettings" label-placement="left" label-width="180">
            <n-form-item label="Idle Timeout (minutes)">
              <n-input-number
                v-model:value="sessionSettings.idleTimeout"
                :min="1"
                :max="1440"
                :step="5"
                style="width: 120px"
              />
            </n-form-item>
            <n-form-item label="Auto Reset">
              <n-space vertical>
                <n-switch v-model:value="sessionSettings.autoReset" />
                <n-text depth="3">Automatically reset session after timeout</n-text>
              </n-space>
            </n-form-item>
            <n-form-item label="Scheduled Reset">
              <n-select
                v-model:value="sessionSettings.scheduledReset"
                :options="scheduledResetOptions"
                style="width: 200px"
              />
            </n-form-item>
            <n-form-item label="Context Window">
              <n-input-number
                v-model:value="sessionSettings.contextWindow"
                :min="1000"
                :max="200000"
                :step="1000"
                style="width: 150px"
              />
              <n-text depth="3" style="margin-left: 12px">Max tokens in context</n-text>
            </n-form-item>
            <n-form-item label="Compress Threshold">
              <n-input-number
                v-model:value="sessionSettings.compressThreshold"
                :min="0.5"
                :max="0.95"
                :step="0.05"
                style="width: 120px"
              />
              <n-text depth="3" style="margin-left: 12px">
                {{ Math.round(sessionSettings.compressThreshold * 100) }}% of context window
              </n-text>
            </n-form-item>
          </n-form>
          <n-divider />
          <n-space justify="end">
            <n-button type="primary" @click="saveSessionSettings">Save</n-button>
          </n-space>
        </n-card>
      </n-tab-pane>

      <!-- Privacy Settings -->
      <n-tab-pane name="privacy" tab="Privacy">
        <n-card title="Privacy Settings">
          <n-form :model="privacySettings" label-placement="left" label-width="180">
            <n-form-item label="PII Redaction">
              <n-space vertical>
                <n-switch v-model:value="privacySettings.piiRedaction" />
                <n-text depth="3">Redact personally identifiable information</n-text>
              </n-space>
            </n-form-item>
            <n-form-item label="Log Tool Calls">
              <n-space vertical>
                <n-switch v-model:value="privacySettings.logToolCalls" />
                <n-text depth="3">Log all tool calls for debugging</n-text>
              </n-space>
            </n-form-item>
            <n-form-item label="Save Trajectories">
              <n-space vertical>
                <n-switch v-model:value="privacySettings.saveTrajectories" />
                <n-text depth="3">Save conversation trajectories for analysis</n-text>
              </n-space>
            </n-form-item>
            <n-form-item label="Share Analytics">
              <n-space vertical>
                <n-switch v-model:value="privacySettings.shareAnalytics" />
                <n-text depth="3">Share anonymous usage analytics</n-text>
              </n-space>
            </n-form-item>
          </n-form>
          <n-divider />
          <n-space justify="end">
            <n-button type="primary" @click="savePrivacySettings">Save</n-button>
          </n-space>
        </n-card>
      </n-tab-pane>

      <!-- API Server Settings -->
      <n-tab-pane name="server" tab="API Server">
        <n-card title="API Server Settings">
          <n-form :model="serverSettings" label-placement="left" label-width="180">
            <n-form-item label="Port">
              <n-input-number
                v-model:value="serverSettings.port"
                :min="1024"
                :max="65535"
                style="width: 120px"
              />
            </n-form-item>
            <n-form-item label="Host">
              <n-input v-model:value="serverSettings.host" style="width: 200px" />
            </n-form-item>
            <n-form-item label="Enable CORS">
              <n-switch v-model:value="serverSettings.cors" />
            </n-form-item>
            <n-form-item label="Authentication">
              <n-space vertical>
                <n-switch v-model:value="serverSettings.auth.enabled" />
                <n-text depth="3">Require authentication for API access</n-text>
              </n-space>
            </n-form-item>
            <n-form-item label="Auth Token" v-if="serverSettings.auth.enabled">
              <n-input
                v-model:value="serverSettings.auth.token"
                type="password"
                show-password-on="click"
                style="width: 300px"
              />
              <n-button size="small" style="margin-left: 8px" @click="regenerateToken">
                Regenerate
              </n-button>
            </n-form-item>
          </n-form>
          <n-divider />
          <n-space justify="end">
            <n-button @click="testConnection">Test Connection</n-button>
            <n-button type="primary" @click="saveServerSettings">Save</n-button>
          </n-space>
        </n-card>
      </n-tab-pane>
    </n-tabs>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import {
  NCard,
  NTabs,
  NTabPane,
  NForm,
  NFormItem,
  NSwitch,
  NSelect,
  NInput,
  NInputNumber,
  NSlider,
  NText,
  NSpace,
  NDivider,
  NButton,
} from 'naive-ui'

const displaySettings = reactive({
  streaming: true,
  compactMode: false,
  showReasoning: true,
  showCost: true,
  theme: 'light',
  language: 'en',
  skin: 'default',
})

const agentSettings = reactive({
  maxTurns: 50,
  timeout: 120,
  temperature: 0.7,
  toolEnforcement: false,
  defaultModel: '',
  systemPrompt: '',
})

const memorySettings = reactive({
  enabled: true,
  maxEntries: 1000,
  maxCharsPerEntry: 5000,
  autoSave: true,
  learnFromMistakes: true,
})

const sessionSettings = reactive({
  idleTimeout: 30,
  autoReset: false,
  scheduledReset: 'never',
  contextWindow: 100000,
  compressThreshold: 0.8,
})

const privacySettings = reactive({
  piiRedaction: false,
  logToolCalls: true,
  saveTrajectories: false,
  shareAnalytics: false,
})

const serverSettings = reactive({
  port: 8642,
  host: '0.0.0.0',
  cors: false,
  auth: {
    enabled: true,
    token: '',
  },
})

const themeOptions = [
  { label: 'Light', value: 'light' },
  { label: 'Dark', value: 'dark' },
  { label: 'System', value: 'system' },
]

const languageOptions = [
  { label: 'English', value: 'en' },
  { label: '中文', value: 'zh' },
  { label: '日本語', value: 'ja' },
  { label: '한국어', value: 'ko' },
]

const skinOptions = [
  { label: 'Default', value: 'default' },
  { label: 'Mono', value: 'mono' },
  { label: 'Slate', value: 'slate' },
  { label: 'Cyber', value: 'cyber' },
]

const modelOptions = ref<{ label: string; value: string }[]>([])
const scheduledResetOptions = [
  { label: 'Never', value: 'never' },
  { label: 'Daily', value: 'daily' },
  { label: 'Weekly', value: 'weekly' },
  { label: 'Monthly', value: 'monthly' },
]

async function loadSettings() {
  try {
    const res = await fetch('/api/config')
    if (res.ok) {
      const data = await res.json()
      Object.assign(displaySettings, data.display || {})
      Object.assign(agentSettings, data.agent || {})
      Object.assign(memorySettings, data.memory || {})
      Object.assign(sessionSettings, data.session || {})
      Object.assign(privacySettings, data.privacy || {})
      Object.assign(serverSettings, data.server || {})
    }
  } catch (e) {
    console.error('Failed to load settings:', e)
  }

  // Load models
  try {
    const res = await fetch('/api/models')
    if (res.ok) {
      const models = await res.json()
      modelOptions.value = models.map((m: { id: string; name: string }) => ({
        label: m.name,
        value: m.id,
      }))
    }
  } catch (e) {
    console.error('Failed to load models:', e)
  }
}

async function saveDisplaySettings() {
  await saveSettings('display', displaySettings)
}

async function saveAgentSettings() {
  await saveSettings('agent', agentSettings)
}

async function saveMemorySettings() {
  await saveSettings('memory', memorySettings)
}

async function saveSessionSettings() {
  await saveSettings('session', sessionSettings)
}

async function savePrivacySettings() {
  await saveSettings('privacy', privacySettings)
}

async function saveServerSettings() {
  await saveSettings('server', serverSettings)
}

async function saveSettings(section: string, data: Record<string, unknown>) {
  try {
    await fetch(`/api/config/${section}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(data),
    })
  } catch (e) {
    console.error('Failed to save settings:', e)
  }
}

async function clearMemory() {
  if (!confirm('Clear all memory entries?')) return
  try {
    await fetch('/api/memory/clear', { method: 'DELETE' })
  } catch (e) {
    console.error('Failed to clear memory:', e)
  }
}

function resetAgentSettings() {
  Object.assign(agentSettings, {
    maxTurns: 50,
    timeout: 120,
    temperature: 0.7,
    toolEnforcement: false,
    defaultModel: '',
    systemPrompt: '',
  })
}

async function regenerateToken() {
  try {
    const res = await fetch('/api/auth/regenerate-token', { method: 'POST' })
    if (res.ok) {
      const data = await res.json()
      serverSettings.auth.token = data.token
    }
  } catch (e) {
    console.error('Failed to regenerate token:', e)
  }
}

async function testConnection() {
  try {
    const res = await fetch('/api/health')
    if (res.ok) {
      alert('Connection successful!')
    } else {
      alert('Connection failed')
    }
  } catch (e) {
    alert('Connection failed')
  }
}

onMounted(() => {
  loadSettings()
})
</script>

<style lang="scss" scoped>
.settings-view {
  padding: 16px;
}
</style>
