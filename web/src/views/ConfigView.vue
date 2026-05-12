<template>
  <div class="config-view">
    <div class="view-header">
      <h2>Configuration</h2>
      <n-button type="primary" @click="saveConfig" :loading="saving">
        Save Changes
      </n-button>
    </div>

    <div class="config-sections">
      <!-- Model Settings -->
      <div class="config-section">
        <h3>Model Settings</h3>
        <div class="config-grid">
          <div class="config-item">
            <label>Provider</label>
            <n-select v-model:value="config.provider" :options="providerOptions" />
          </div>
          <div class="config-item">
            <label>Model</label>
            <n-select v-model:value="config.model" :options="modelOptions" />
          </div>
          <div class="config-item">
            <label>Temperature</label>
            <n-slider v-model:value="config.temperature" :min="0" :max="2" :step="0.1" />
            <span class="value-label">{{ config.temperature }}</span>
          </div>
          <div class="config-item">
            <label>Max Tokens</label>
            <n-input-number v-model:value="config.max_tokens" :min="100" :max="32000" />
          </div>
        </div>
      </div>

      <!-- Interface Settings -->
      <div class="config-section">
        <h3>Interface</h3>
        <div class="config-grid">
          <div class="config-item">
            <label>Theme</label>
            <n-select v-model:value="config.theme" :options="themeOptions" />
          </div>
          <div class="config-item">
            <label>Language</label>
            <n-select v-model:value="config.language" :options="languageOptions" />
          </div>
          <div class="config-item checkbox">
            <n-switch v-model:value="config.streaming" />
            <label>Enable Streaming Responses</label>
          </div>
        </div>
      </div>

      <!-- System Prompt -->
      <div class="config-section">
        <h3>System Prompt</h3>
        <n-input
          v-model:value="config.system_prompt"
          type="textarea"
          :rows="6"
          placeholder="Custom instructions for the AI..."
        />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { NButton, NSelect, NSlider, NInputNumber, NSwitch, NInput, useMessage } from 'naive-ui'
import { apiService, Config } from '../api'

const message = useMessage()
const saving = ref(false)
const config = ref<Config>({
  provider: 'openai',
  model: 'gpt-4',
  temperature: 0.7,
  max_tokens: 4096,
  theme: 'dark',
  language: 'en',
  streaming: true,
  system_prompt: 'You are go-magic, a helpful AI assistant.',
})

const providerOptions = [
  { label: 'OpenAI', value: 'openai' },
  { label: 'DeepSeek', value: 'deepseek' },
  { label: 'Anthropic', value: 'anthropic' },
  { label: 'Google', value: 'google' },
]

const modelOptions = [
  { label: 'GPT-4', value: 'gpt-4' },
  { label: 'GPT-4 Turbo', value: 'gpt-4-turbo' },
  { label: 'GPT-3.5 Turbo', value: 'gpt-3.5-turbo' },
  { label: 'Claude 3', value: 'claude-3' },
]

const themeOptions = [
  { label: 'Dark', value: 'dark' },
  { label: 'Light', value: 'light' },
  { label: 'System', value: 'system' },
]

const languageOptions = [
  { label: 'English', value: 'en' },
  { label: '中文', value: 'zh' },
  { label: '日本語', value: 'ja' },
]

onMounted(loadConfig)

async function loadConfig() {
  try {
    const response = await apiService.config.get()
    config.value = { ...config.value, ...response.data }
  } catch (err) {
    message.error('Failed to load config')
  }
}

async function saveConfig() {
  saving.value = true
  try {
    await apiService.config.update(config.value)
    message.success('Configuration saved')
  } catch (err) {
    message.error('Failed to save config')
  } finally {
    saving.value = false
  }
}
</script>

<style scoped>
.config-view {
  padding: 20px;
  height: 100%;
  overflow-y: auto;
}

.view-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 24px;
}

.view-header h2 {
  margin: 0;
}

.config-sections {
  display: flex;
  flex-direction: column;
  gap: 24px;
}

.config-section {
  background: var(--bg-secondary);
  border-radius: 12px;
  padding: 20px;
}

.config-section h3 {
  font-size: 16px;
  margin: 0 0 16px;
  color: var(--text-secondary);
}

.config-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(250px, 1fr));
  gap: 16px;
}

.config-item {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.config-item label {
  font-size: 14px;
  font-weight: 500;
}

.config-item.checkbox {
  flex-direction: row;
  align-items: center;
}

.config-item.checkbox label {
  font-weight: normal;
}

.value-label {
  font-size: 12px;
  color: var(--text-secondary);
}
</style>
