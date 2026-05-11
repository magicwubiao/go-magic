<template>
  <div class="config-view">
    <n-tabs type="line">
      <n-tab-pane name="provider" tab="Provider">
        <n-card title="AI Provider Configuration">
          <n-form label-placement="left" label-width="120">
            <n-form-item label="Provider">
              <n-select v-model:value="config.provider" :options="providerOptions" />
            </n-form-item>
            <n-form-item label="Model">
              <n-select v-model:value="config.model" :options="modelOptions" />
            </n-form-item>
            <n-form-item label="API Key">
              <n-input type="password" v-model:value="config.apiKey" placeholder="sk-..." show-password-on="click" />
            </n-form-item>
            <n-form-item label="Temperature">
              <n-slider v-model:value="config.temperature" :min="0" :max="2" :step="0.1" />
              <span style="margin-left: 12px">{{ config.temperature }}</span>
            </n-form-item>
          </n-form>
          <template #footer>
            <n-space justify="end">
              <n-button @click="resetConfig">Reset</n-button>
              <n-button type="primary" @click="saveConfig">Save</n-button>
            </n-space>
          </template>
        </n-card>
      </n-tab-pane>

      <n-tab-pane name="display" tab="Display">
        <n-card title="Display Settings">
          <n-form label-placement="left" label-width="120">
            <n-form-item label="Theme">
              <n-radio-group v-model:value="config.theme">
                <n-radio value="dark">Dark</n-radio>
                <n-radio value="light">Light</n-radio>
                <n-radio value="auto">Auto</n-radio>
              </n-radio-group>
            </n-form-item>
            <n-form-item label="Language">
              <n-select v-model:value="config.language" :options="languageOptions" />
            </n-form-item>
            <n-form-item label="Streaming">
              <n-switch v-model:value="config.streaming" />
            </n-form-item>
          </n-form>
        </n-card>
      </n-tab-pane>

      <n-tab-pane name="gateway" tab="Gateway">
        <n-card title="Messaging Gateway">
          <n-form label-placement="left" label-width="120">
            <n-form-item label="Telegram">
              <n-switch v-model:value="config.telegram" />
            </n-form-item>
            <n-form-item v-if="config.telegram" label="Bot Token">
              <n-input v-model:value="config.telegramToken" placeholder="123456:ABC..." />
            </n-form-item>
            <n-form-item label="Discord">
              <n-switch v-model:value="config.discord" />
            </n-form-item>
            <n-form-item v-if="config.discord" label="Bot Token">
              <n-input v-model:value="config.discordToken" type="password" />
            </n-form-item>
          </n-form>
        </n-card>
      </n-tab-pane>
    </n-tabs>
  </div>
</template>

<script setup>
import { ref, reactive } from 'vue'

const config = reactive({
  provider: 'openai',
  model: 'gpt-4',
  apiKey: '',
  temperature: 0.7,
  theme: 'dark',
  language: 'en',
  streaming: true,
  telegram: false,
  telegramToken: '',
  discord: false,
  discordToken: ''
})

const providerOptions = [
  { label: 'OpenAI', value: 'openai' },
  { label: 'Anthropic', value: 'anthropic' },
  { label: 'DeepSeek', value: 'deepseek' },
  { label: 'Ollama', value: 'ollama' },
  { label: 'OpenRouter', value: 'openrouter' }
]

const modelOptions = [
  { label: 'GPT-4', value: 'gpt-4' },
  { label: 'GPT-4 Turbo', value: 'gpt-4-turbo' },
  { label: 'GPT-3.5 Turbo', value: 'gpt-3.5-turbo' }
]

const languageOptions = [
  { label: 'English', value: 'en' },
  { label: '中文', value: 'zh' },
  { label: '日本語', value: 'ja' }
]

const saveConfig = () => console.log('Save config', config)
const resetConfig = () => console.log('Reset config')
</script>
