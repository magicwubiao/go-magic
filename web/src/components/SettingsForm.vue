<template>
  <n-form label-placement="left" label-width="100">
    <n-form-item label="Provider">
      <n-select v-model:value="settings.provider" :options="providerOptions" />
    </n-form-item>
    <n-form-item label="Model">
      <n-input v-model:value="settings.model" />
    </n-form-item>
    <n-form-item label="API Key">
      <n-input type="password" v-model:value="settings.apiKey" show-password-on="click" />
    </n-form-item>
    <n-form-item label="Theme">
      <n-radio-group v-model:value="settings.theme">
        <n-radio value="dark">Dark</n-radio>
        <n-radio value="light">Light</n-radio>
      </n-radio-group>
    </n-form-item>
    <n-form-item>
      <n-space>
        <n-button @click="testConnection">Test Connection</n-button>
        <n-button type="primary" @click="saveSettings">Save</n-button>
      </n-space>
    </n-form-item>
  </n-form>
</template>

<script setup>
import { reactive } from 'vue'

const settings = reactive({
  provider: 'openai',
  model: 'gpt-4',
  apiKey: '',
  theme: 'dark'
})

const providerOptions = [
  { label: 'OpenAI', value: 'openai' },
  { label: 'Anthropic', value: 'anthropic' },
  { label: 'DeepSeek', value: 'deepseek' }
]

const testConnection = () => console.log('Test connection')
const saveSettings = () => console.log('Save settings', settings)
</script>
