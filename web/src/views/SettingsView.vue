<template>
  <div>
    <h2 style="margin-bottom: 24px;">Settings</h2>
    <n-form label-placement="left" label-width="120">
      <n-form-item label="Provider">
        <n-select v-model:value="settings.provider" :options="providerOptions" />
      </n-form-item>
      <n-form-item label="Model">
        <n-input v-model:value="settings.model" placeholder="e.g. gpt-4" />
      </n-form-item>
      <n-form-item label="API Key">
        <n-input v-model:value="settings.apiKey" type="password" placeholder="Your API key" />
      </n-form-item>
      <n-form-item>
        <n-button type="primary" @click="save">Save</n-button>
      </n-form-item>
    </n-form>
  </div>
</template>

<script setup lang="ts">
import { reactive } from 'vue'
import { useMessage } from 'naive-ui'

const message = useMessage()

const settings = reactive({
  provider: 'openai',
  model: 'gpt-4',
  apiKey: '',
})

const providerOptions = [
  { label: 'OpenAI', value: 'openai' },
  { label: 'DeepSeek', value: 'deepseek' },
  { label: 'Anthropic', value: 'anthropic' },
]

function save() {
  // TODO: Save to backend
  message.success('Settings saved')
}
</script>
