<template>
  <div>
    <h2 style="margin-bottom: 24px;">{{ t('models.title') }}</h2>

    <n-tabs type="line" animated>
      <!-- Models Tab -->
      <n-tab-pane name="models" :tab="t('models.models')">
        <n-spin v-if="modelsStore.loading" />
        <div v-else>
          <!-- Current Model Card -->
          <n-card :title="t('models.currentModel')" size="small" style="margin-bottom: 16px;" embedded>
            <n-result
              v-if="!modelsStore.currentModel"
              status="warning"
              :title="t('models.notSet')"
              :description="t('models.selectPrompt')"
            />
            <n-space v-else align="center">
              <n-tag type="success" size="large" :bordered="false">
                {{ modelsStore.currentModel.name }}
              </n-tag>
              <n-tag size="small" type="info">
                {{ modelsStore.currentModel.provider }}
              </n-tag>
            </n-space>
          </n-card>

          <!-- Models Grouped by Provider -->
          <n-space vertical size="large">
            <n-collapse>
              <n-collapse-item
                v-for="(models, provider) in groupedModels"
                :key="provider"
                :title="provider"
                :name="provider"
              >
                <template #header>
                  <n-space>
                    <n-tag type="info">{{ provider }}</n-tag>
                    <n-tag size="small">{{ models.length }} {{ t('models.models') }}</n-tag>
                  </n-space>
                </template>
                <n-grid :cols="3" :x-gap="12" :y-gap="12">
                  <n-gi
                    v-for="model in models"
                    :key="model.id"
                  >
                    <n-card
                      size="small"
                      hoverable
                      :class="{ 'model-card-active': isCurrentModel(model) }"
                      @click="selectModel(model)"
                    >
                      <template #header>
                        <n-space justify="space-between" align="center">
                          <n-text strong>{{ model.name }}</n-text>
                          <n-badge
                            v-if="isCurrentModel(model)"
                            type="success"
                            :value="t('models.active')"
                          />
                        </n-space>
                      </template>
                      <n-space vertical size="small">
                        <n-tag size="small" :type="isCurrentModel(model) ? 'success' : 'default'">
                          {{ isCurrentModel(model) ? t('models.current') : t('models.clickToUse') }}
                        </n-tag>
                      </n-space>
                    </n-card>
                  </n-gi>
                </n-grid>
              </n-collapse-item>
            </n-collapse>
          </n-space>
        </div>
      </n-tab-pane>

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
            <n-form-item :label="t('models.models')">
              <n-dynamic-input
                v-model:value="form.models"
                :placeholder="t('models.modelPlaceholder')"
                preset="card"
                #default="{ value }"
              >
                <n-input v-model:value="value.value" :placeholder="t('models.modelPlaceholder')" />
              </n-dynamic-input>
              <template #feedback>
                <n-text depth="3" style="font-size: 12px;">
                  {{ t('models.firstAsCurrent') }}
                </n-text>
              </template>
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
    </n-tabs>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import { useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { useProvidersStore } from '@/stores/providers'
import { useModelsStore } from '@/stores/models'
import { useConfigStore } from '@/stores/config'
import type { Provider } from '@/api/providers'
import type { Model } from '@/api/models'

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
  models: [] as string[],
  enabled: true,
})

// Group models by provider
const groupedModels = computed(() => {
  const groups: Record<string, Model[]> = {}
  for (const model of modelsStore.models) {
    if (!groups[model.provider]) {
      groups[model.provider] = []
    }
    groups[model.provider].push(model)
  }
  return groups
})

// Check if model is current
function isCurrentModel(model: Model): boolean {
  return modelsStore.currentModel?.id === model.id
}

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
  { title: t('common.name'), key: 'name', width: 150 },
  {
    title: t('models.models'),
    key: 'models',
    render: (row: Provider) => {
      if (!row.models || row.models.length === 0) {
        return h('span', { style: 'color: #999;' }, '-')
      }
      return h('div', { style: 'display: flex; flex-wrap: wrap; gap: 4px; max-width: 300px;' },
        row.models.map((model: string, index: number) =>
          h('span', {
            style: `
              display: inline-block;
              padding: 2px 8px;
              border-radius: 4px;
              font-size: 12px;
              background: ${index === 0 ? '#d4edda' : '#e9ecef'};
              color: ${index === 0 ? '#155724' : '#495057'};
              margin: 2px;
            `
          }, model)
        )
      )
    },
  },
  {
    title: t('models.apiKey'),
    key: 'api_key',
    width: 100,
    render: (row: Provider) => h('span', {
      style: `color: ${row.api_key ? '#52c41a' : '#999;'}`
    }, row.api_key ? '✓' : '-'),
  },
  {
    title: t('common.actions'),
    key: 'actions',
    width: 150,
    render: (row: Provider) => h('div', { style: 'display: flex; gap: 8px;' }, [
      h('button', {
        onClick: () => editProvider(row),
        style: 'border: none; background: #1890ff; color: white; padding: 4px 12px; border-radius: 4px; cursor: pointer;'
      }, t('common.edit')),
      h('button', {
        onClick: () => deleteProviderById(row.id),
        style: 'border: none; background: #ff4d4f; color: white; padding: 4px 12px; border-radius: 4px; cursor: pointer;'
      }, t('common.delete')),
    ]),
  },
]

function openAddModal() {
  editingId.value = null
  form.name = ''
  form.api_key = ''
  form.base_url = ''
  form.models = []
  showModal.value = true
}

function editProvider(provider: Provider) {
  editingId.value = provider.id
  form.name = provider.name
  form.api_key = provider.api_key || ''
  form.base_url = provider.base_url || ''
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

    showModal.value = false
    await providersStore.loadProviders()
    await modelsStore.loadModels()
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
  await modelsStore.loadModels()
  message.success(t('models.deleted'))
}

async function deleteProviderById(id: string) {
  await providersStore.deleteProvider(id)
  await providersStore.loadProviders()
  await modelsStore.loadModels()
  message.success(t('models.deleted'))
}

async function selectModel(model: Model) {
  try {
    // Pass provider and model id to setModel
    await modelsStore.setModel(model.provider, model.id)
    message.success(`${t('models.modelSet')}: ${model.name}`)
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

<style scoped>
.model-card-active {
  border-color: #52c41a !important;
  background: #f6ffed;
}

.model-card-active:hover {
  background: #f6ffed !important;
}
</style>
