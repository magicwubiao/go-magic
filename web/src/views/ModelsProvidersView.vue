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
        <n-modal v-model:show="showModal" :title="editingId ? t('models.editProvider') : t('models.addProvider')" preset="dialog">
          <n-form label-placement="left" label-width="120">
            <n-form-item :label="t('models.providerName')">
              <n-select v-model:value="form.name" :options="providerOptions" :disabled="!!editingId" />
            </n-form-item>
            <n-form-item :label="t('models.apiKey')">
              <n-input v-model:value="form.api_key" type="password" show-password-on="click" :placeholder="t('models.apiKey')" />
            </n-form-item>
            <n-form-item :label="t('models.baseUrl')">
              <n-input v-model:value="form.base_url" :placeholder="t('config.serverUrl')" />
            </n-form-item>
            <n-form-item :label="t('models.model')">
              <n-input v-model:value="form.model" :placeholder="t('models.currentModelPlaceholder')" />
            </n-form-item>
            <n-form-item :label="t('models.supportedModels')">
              <n-dynamic-input v-model:value="form.models" :placeholder="t('models.modelPlaceholder')" />
            </n-form-item>
          </n-form>
          <template #action>
            <n-space justify="end">
              <n-button v-if="editingId" type="error" @click="deleteProvider">{{ t('common.delete') }}</n-button>
              <n-button @click="showModal = false">{{ t('common.cancel') }}</n-button>
              <n-button type="primary" @click="saveProvider">{{ t('common.save') }}</n-button>
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
import { ref, reactive, h, onMounted } from 'vue'
import { useMessage, NButton, NTag, NPopconfirm } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { useProvidersStore } from '@/stores/providers'
import { useModelsStore } from '@/stores/models'
import { useConfigStore } from '@/stores/config'
import type { Provider } from '@/api/providers'

const { t } = useI18n()
const message = useMessage()
const providersStore = useProvidersStore()
const modelsStore = useModelsStore()
const configStore = useConfigStore()

const showModal = ref(false)
const editingId = ref<string | null>(null)

const form = reactive({
  name: '',
  api_key: '',
  base_url: '',
  model: '',
  models: [] as string[],
  enabled: true,
})

const providerOptions = [
  { label: 'OpenAI', value: 'openai' },
  { label: 'Anthropic (Claude)', value: 'anthropic' },
  { label: 'DeepSeek', value: 'deepseek' },
  { label: 'Google (Gemini)', value: 'gemini' },
  { label: 'Kimi (Moonshot)', value: 'kimi' },
  { label: 'Doubao (Volcano/ByteDance)', value: 'doubao' },
  { label: t('models.zhipu') || '智谱 AI', value: 'zhipu' },
  { label: t('models.dashscope') || 'DashScope', value: 'dashscope' },
  { label: 'MiniMax', value: 'minimax' },
  { label: t('models.wenxin') || '文心一言', value: 'wenxin' },
  { label: t('models.hunyuan') || '腾讯混元', value: 'hunyuan' },
  { label: 'Moonshot', value: 'moonshot' },
  { label: 'MiMo', value: 'mimo' },
  { label: 'OpenRouter', value: 'openrouter' },
  { label: 'Groq', value: 'groq' },
  { label: 'Mistral', value: 'mistral' },
  { label: 'Cohere', value: 'cohere' },
  { label: 'Perplexity', value: 'perplexity' },
  { label: 'Together AI', value: 'together' },
  { label: 'Ollama (Local)', value: 'ollama' },
  { label: 'vLLM (Local)', value: 'vllm' },
  { label: 'Custom (OpenAI Compatible)', value: 'custom' },
]

const providerColumns = [
  { title: t('common.name'), key: 'name' },
  { title: t('models.model'), key: 'model' },
  {
    title: t('models.supportedModels'),
    key: 'models',
    render: (row: Provider) => {
      if (!row.models || row.models.length === 0) {
        return h(NTag, { size: 'small', type: 'default' }, () => '-')
      }
      return h('div', { style: 'display: flex; flex-wrap: wrap; gap: 4px;' },
        row.models.map((model: string) => h(NTag, { size: 'small', type: model === row.model ? 'success' : 'info' }, () => model))
      )
    },
  },
  {
    title: t('models.apiKey'),
    key: 'api_key',
    render: (row: Provider) => h(NTag, { size: 'small', type: row.api_key ? 'success' : 'default' }, () => row.api_key ? t('models.set') : t('models.notSet')),
  },
  {
    title: t('common.actions'),
    key: 'actions',
    render: (row: Provider) => h('div', { style: 'display: flex; gap: 4px;' }, [
      h(NButton, { size: 'tiny', onClick: () => editProvider(row) }, () => t('common.edit')),
      h(NPopconfirm, { onPositiveClick: () => deleteProviderById(row.id) }, {
        trigger: () => h(NButton, { size: 'tiny', type: 'error' }, () => t('common.delete')),
        default: () => t('models.deleteConfirm'),
      }),
    ]),
  },
]

const modelColumns = [
  { title: t('common.name'), key: 'name' },
  { title: t('models.provider'), key: 'provider' },
  {
    title: t('common.actions'),
    key: 'action',
    render: (row: any) => h(NButton, {
      size: 'tiny',
      type: modelsStore.currentModel?.id === row.id ? 'primary' : 'default',
      disabled: modelsStore.currentModel?.id === row.id,
      onClick: () => selectModel(row),
    }, () => modelsStore.currentModel?.id === row.id ? t('models.active') : t('models.select')),
  },
]

function openAddModal() {
  editingId.value = null
  form.name = ''
  form.api_key = ''
  form.base_url = ''
  form.model = ''
  form.models = []
  showModal.value = true
}

function editProvider(provider: Provider) {
  editingId.value = provider.id
  form.name = provider.name
  form.api_key = provider.api_key || ''
  form.base_url = provider.base_url || ''
  form.model = provider.model || ''
  form.models = provider.models || []
  form.enabled = provider.enabled ?? true
  showModal.value = true
}

async function saveProvider() {
  if (!form.name.trim()) {
    message.warning(t('models.pleaseEnterName'))
    return
  }

  try {
    if (editingId.value) {
      await providersStore.updateProvider(editingId.value, { ...form })
    } else {
      await providersStore.createProvider({ ...form })
    }

    // Also update config for api_key
    if (form.api_key) {
      await configStore.updateConfig({
        provider_config: {
          [form.name]: {
            api_key: form.api_key,
            base_url: form.base_url,
            model: form.model,
            models: form.models,
          },
        },
      })
    }

    showModal.value = false
    await providersStore.loadProviders()
    await modelsStore.loadModels() // Reload models after saving provider
    message.success(t('models.saved'))
  } catch (e) {
    message.error(t('models.failedToSave') + ': ' + (e instanceof Error ? e.message : 'Unknown error'))
  }
}

async function deleteProvider() {
  if (!editingId.value) return
  await providersStore.deleteProvider(editingId.value)
  showModal.value = false
  await providersStore.loadProviders()
  await modelsStore.loadModels() // Reload models after deleting provider
  message.success(t('models.deleted'))
}

async function deleteProviderById(id: string) {
  await providersStore.deleteProvider(id)
  await providersStore.loadProviders()
  await modelsStore.loadModels() // Reload models after deleting provider
  message.success(t('models.deleted'))
}

async function selectModel(row: any) {
  // Use row.id directly which is "provider/model" format
  const modelId = row.id || `${row.provider}/${row.name}`

  try {
    await modelsStore.setModel(modelId)
    message.success(`Model set to ${row.name} (${row.provider})`)
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
