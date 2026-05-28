<template>
  <div>
    <h2 style="margin-bottom: 24px;">{{ t('models.title') }}</h2>

    <n-tabs type="line" animated>
      <!-- Providers Tab -->
      <n-tab-pane name="providers" :tab="t('models.providers')">
        <n-space style="margin-bottom: 16px;">
          <n-button type="primary" @click="openAddModal">{{ t('models.addProvider') }}</n-button>
        </n-space>

        <n-spin v-if="providersStore.loading" />
        <n-data-table
          v-else
          :columns="providerColumns"
          :data="providersStore.providers"
          :bordered="false"
          size="small"
        />

        <!-- Add/Edit Modal -->
        <n-modal v-model:show="showModal" :title="editingId ? t('models.editProvider') : t('models.addProvider')" preset="dialog" style="width: 600px;">
          <n-form label-placement="left" label-width="140">
            <n-form-item :label="t('models.providerName')" required>
              <n-input 
                v-model:value="form.name" 
                :placeholder="t('models.providerNamePlaceholder')" 
                :disabled="!!editingId"
              />
            </n-form-item>
            <n-form-item :label="t('models.apiKey')">
              <n-input 
                v-model:value="form.api_key" 
                type="password" 
                show-password-on="click" 
                :placeholder="t('models.apiKeyPlaceholder')" 
              />
            </n-form-item>
            <n-form-item :label="t('models.baseUrl')">
              <n-input 
                v-model:value="form.base_url" 
                :placeholder="t('models.baseUrlPlaceholder')" 
              />
            </n-form-item>
            <n-form-item :label="t('models.model')">
              <n-input 
                v-model:value="form.model" 
                :placeholder="t('models.modelPlaceholder')" 
              />
            </n-form-item>
          </n-form>
          <template #action>
            <n-space justify="end">
              <n-button @click="showModal = false">{{ t('common.cancel') }}</n-button>
              <n-button type="primary" @click="saveProvider" :loading="saving">{{ t('common.save') }}</n-button>
            </n-space>
          </template>
        </n-modal>
      </n-tab-pane>

      <!-- Models Tab -->
      <n-tab-pane name="models" :tab="t('models.models')">
        <n-spin v-if="modelsStore.loading" />
        <div v-else>
          <n-card :title="t('models.currentModel')" size="small" style="margin-bottom: 16px;">
            <n-space align="center">
              <n-tag type="success" size="large">
                {{ modelsStore.currentModel?.name || t('models.notSet') }}
              </n-tag>
              <n-text depth="3">
                {{ t('models.provider') }}: {{ modelsStore.currentModel?.provider || t('models.unknown') }}
              </n-text>
            </n-space>
          </n-card>
          <n-data-table
            :columns="modelColumns"
            :data="modelsStore.models"
            :bordered="false"
            size="small"
          />
        </div>
      </n-tab-pane>
    </n-tabs>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, h, onMounted, computed } from 'vue'
import { useMessage, NButton, NTag, NSwitch } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { useProvidersStore } from '@/stores/providers'
import { useModelsStore } from '@/stores/models'
import type { Provider } from '@/api/providers'

const { t } = useI18n()
const message = useMessage()
const providersStore = useProvidersStore()
const modelsStore = useModelsStore()

const showModal = ref(false)
const editingId = ref<string | null>(null)
const saving = ref(false)

const form = reactive({
  name: '',
  api_key: '',
  base_url: '',
  model: '',
})

// Provider name -> display label mapping
const providerLabels: Record<string, string> = {
  openai: 'OpenAI',
  anthropic: 'Anthropic (Claude)',
  deepseek: 'DeepSeek',
  gemini: 'Google (Gemini)',
  kimi: 'Kimi (Moonshot)',
  doubao: 'Doubao (Volcano)',
  zhipu: '智谱 GLM',
  dashscope: '通义千问',
  minimax: 'MiniMax',
  wenxin: '文心一言',
  hunyuan: '腾讯混元',
  moonshot: 'Moonshot',
  mimo: 'MiMo',
  openrouter: 'OpenRouter',
  groq: 'Groq',
  mistral: 'Mistral',
  cohere: 'Cohere',
  perplexity: 'Perplexity',
  together: 'Together AI',
  ollama: 'Ollama (Local)',
  vllm: 'vLLM (Local)',
  custom: 'Custom (OpenAI Compatible)',
}

const providerColumns = [
  { title: t('common.name'), key: 'name', width: 150 },
  { 
    title: 'Base URL', 
    key: 'base_url', 
    ellipsis: { tooltip: true },
    render: (row: Provider) => row.base_url || '-',
  },
  { 
    title: t('models.model'), 
    key: 'model',
    render: (row: Provider) => row.model || '-',
  },
  { 
    title: 'API Key', 
    key: 'api_key',
    width: 100,
    render: (row: Provider) => h(NTag, { size: 'small', type: row.api_key ? 'success' : 'default' }, () => row.api_key ? t('models.set') : t('models.notSet')),
  },
  {
    title: t('common.actions'),
    key: 'actions',
    width: 120,
    render: (row: Provider) => h(NButton, { size: 'small', onClick: () => editProvider(row) }, () => t('common.edit')),
  },
]

const modelColumns = [
  { title: t('models.model'), key: 'name', width: 200 },
  { title: t('models.provider'), key: 'provider', width: 150 },
  {
    title: t('common.actions'),
    key: 'action',
    width: 120,
    render: (row: any) => h(NButton, {
      size: 'small',
      type: modelsStore.currentModel?.id === row.id ? 'primary' : 'default',
      onClick: () => selectModel(row),
    }, () => modelsStore.currentModel?.id === row.id ? t('models.active') : t('models.select')),
  },
]

function getProviderLabel(name: string): string {
  return providerLabels[name] || name
}

function openAddModal() {
  editingId.value = null
  form.name = ''
  form.api_key = ''
  form.base_url = ''
  form.model = ''
  showModal.value = true
}

function editProvider(provider: Provider) {
  editingId.value = provider.id
  form.name = provider.name || provider.id
  form.api_key = provider.api_key || ''
  form.base_url = provider.base_url || ''
  form.model = provider.model || ''
  showModal.value = true
}

async function saveProvider() {
  if (!form.name.trim()) {
    message.warning(t('models.pleaseEnterName'))
    return
  }

  saving.value = true
  try {
    const payload = {
      name: form.name,
      api_key: form.api_key,
      base_url: form.base_url,
      model: form.model,
    }

    if (editingId.value) {
      // Update existing provider
      const id = editingId.value
      await providersStore.updateProvider(id, payload)
      message.success(t('models.saved'))
    } else {
      // Create new provider
      await providersStore.createProvider(payload)
      message.success(t('models.added'))
    }

    showModal.value = false
    await providersStore.loadProviders()
  } catch (e) {
    message.error(t('models.failedToSave') + ': ' + (e instanceof Error ? e.message : 'Unknown error'))
  } finally {
    saving.value = false
  }
}

async function selectModel(row: any) {
  try {
    await modelsStore.setModel(row.id)
    message.success(t('models.modelSet') + ': ' + row.name)
    await modelsStore.loadCurrentModel()
  } catch (e) {
    message.error(t('models.failedToSet') + ': ' + (e instanceof Error ? e.message : 'Unknown error'))
  }
}

onMounted(() => {
  providersStore.loadProviders()
  modelsStore.loadModels()
  modelsStore.loadCurrentModel()
})
</script>
