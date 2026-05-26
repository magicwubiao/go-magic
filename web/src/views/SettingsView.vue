<template>
  <div>
    <h2 style="margin-bottom: 24px;">{{ t('nav.config') }}</h2>
    <n-spin v-if="loading" />
    <n-form v-else label-placement="left" label-width="120">
      <n-form-item :label="t('models.provider')">
        <n-select v-model:value="settings.provider" :options="providerOptions" />
      </n-form-item>
      <n-form-item :label="t('models.model')">
        <n-input v-model:value="settings.model" :placeholder="'e.g. gpt-4'" />
      </n-form-item>
      <n-form-item :label="t('models.apiKey')">
        <n-input v-model:value="settings.apiKey" type="password" show-password-on="click" :placeholder="t('models.apiKey')" />
      </n-form-item>
      <n-form-item :label="t('models.baseUrl')">
        <n-input v-model:value="settings.baseUrl" :placeholder="t('config.serverUrl')" />
      </n-form-item>
      <n-form-item>
        <n-button type="primary" :loading="saving" @click="save">{{ t('common.save') }}</n-button>
      </n-form-item>
    </n-form>
  </div>
</template>

<script setup lang="ts">
import { reactive, ref, onMounted } from 'vue'
import { useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { useConfigStore } from '@/stores/config'

const { t } = useI18n()

const message = useMessage()
const configStore = useConfigStore()
const loading = ref(true)
const saving = ref(false)

const settings = reactive({
  provider: 'openai',
  model: 'gpt-4',
  apiKey: '',
  baseUrl: '',
})

const providerOptions = [
  { label: 'OpenAI', value: 'openai' },
  { label: 'DeepSeek', value: 'deepseek' },
  { label: 'Anthropic', value: 'anthropic' },
  { label: 'Google', value: 'google' },
  { label: 'Zhipu', value: 'zhipu' },
  { label: 'Qwen', value: 'qwen' },
  { label: 'Kimi', value: 'kimi' },
  { label: 'Ollama', value: 'ollama' },
]

async function save() {
  saving.value = true
  try {
    await configStore.updateConfig({
      provider: settings.provider,
      model: settings.model,
      api_key: settings.apiKey,
      base_url: settings.baseUrl,
    })
    // Reload to ensure sync
    await configStore.loadConfig()
    message.success(t('config.saveSuccess'))
  } catch (e) {
    message.error(t('common.error') + ': ' + (e instanceof Error ? e.message : 'Unknown error'))
  } finally {
    saving.value = false
  }
}

onMounted(async () => {
  await configStore.loadConfig()
  if (configStore.config) {
    settings.provider = configStore.config.provider || 'openai'
    settings.model = configStore.config.model || 'gpt-4'
    settings.apiKey = configStore.config.api_key || ''
    settings.baseUrl = configStore.config.base_url || ''
  }
  loading.value = false
})
</script>
