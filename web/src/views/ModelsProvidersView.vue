<template>
  <div class="models-providers-page">
    <div class="page-header">
      <h2>{{ t('modelsProviders.title') }}</h2>
      <n-button type="primary" @click="showAddProviderModal = true">
        {{ t('modelsProviders.addProvider') }}
      </n-button>
    </div>

    <div class="page-content">
      <!-- 供应商列表（只显示已添加的） -->
      <div class="providers-list">
        <div class="section-title">{{ t('modelsProviders.providers') }}</div>
        <n-list hoverable clickable v-if="configProviders.length > 0">
          <n-list-item
            v-for="prov in configProviders"
            :key="prov.name"
            :class="{ active: selectedProvider === prov.name }"
            @click="selectProvider(prov.name)"
          >
            <div class="provider-item-row">
              <div class="provider-item">
                <div class="provider-info">
                  <span class="provider-name">{{ prov.name }}</span>
                  <n-tag v-if="prov.isCurrent" size="small" type="success">{{ t('modelsProviders.current') }}</n-tag>
                </div>
                <span class="model-count">{{ prov.models.length }} {{ t('modelsProviders.models') }}</span>
              </div>
              <n-button
                size="tiny"
                type="error"
                ghost
                @click.stop="deleteProvider(prov.name)"
              >
                {{ t('common.delete') }}
              </n-button>
            </div>
          </n-list-item>
        </n-list>
        <n-empty v-else :description="t('modelsProviders.noProviders')" />
      </div>

      <!-- 模型列表 -->
      <div class="models-panel">
        <template v-if="selectedProvider">
          <div class="panel-header">
            <h3>{{ selectedProvider }} - {{ t('modelsProviders.models') }}</h3>
            <n-button size="small" @click="showAddModelModal = true">
              {{ t('modelsProviders.addModel') }}
            </n-button>
          </div>

          <n-empty v-if="!providerModels.length" :description="t('modelsProviders.noModels')" />

          <div v-else class="models-grid">
            <div
              v-for="(model, index) in providerModels"
              :key="model"
              class="model-card"
              :class="{ current: index === 0 }"
            >
              <div class="model-info">
                <span class="model-name">{{ model }}</span>
                <n-tag v-if="index === 0" size="small" type="success">
                  {{ t('modelsProviders.currentModel') }}
                </n-tag>
              </div>
              <div class="model-actions">
                <n-button
                  v-if="index !== 0"
                  size="tiny"
                  type="primary"
                  @click="setCurrentModel(model)"
                >
                  {{ t('modelsProviders.setAsCurrent') }}
                </n-button>
                <n-button
                  size="tiny"
                  type="error"
                  ghost
                  @click="removeModel(model)"
                >
                  {{ t('common.delete') }}
                </n-button>
              </div>
            </div>
          </div>

          <!-- 切换为当前供应商按钮 -->
          <div v-if="selectedProvider !== currentConfigProvider" class="switch-provider">
            <n-button type="primary" @click="switchToProvider(selectedProvider)">
              {{ t('modelsProviders.switchToThisProvider') }}
            </n-button>
          </div>
        </template>
        <n-empty v-else :description="t('modelsProviders.selectProvider')" />
      </div>
    </div>

    <!-- 添加供应商弹窗 -->
    <n-modal v-model:show="showAddProviderModal" preset="card" :title="t('modelsProviders.addProvider')" style="width: 500px">
      <n-form :model="newProvider" label-placement="top" :rules="providerFormRules">
        <n-form-item :label="t('modelsProviders.providerName')" path="name">
          <n-select
            v-model:value="newProvider.name"
            :options="availableProviders"
            filterable
            :placeholder="t('modelsProviders.selectProviderType')"
          />
        </n-form-item>
        <n-form-item :label="t('modelsProviders.models')" path="models">
          <n-dynamic-input
            v-model:value="newProvider.models"
            :placeholder="t('modelsProviders.modelPlaceholder')"
          />
        </n-form-item>
        <n-form-item :label="t('modelsProviders.apiKey')">
          <n-input
            v-model:value="newProvider.apiKey"
            type="password"
            show-password-on="click"
            :placeholder="t('modelsProviders.apiKeyPlaceholder')"
          />
        </n-form-item>
        <n-form-item :label="t('modelsProviders.baseUrl')">
          <n-input
            v-model:value="newProvider.baseUrl"
            :placeholder="t('modelsProviders.baseUrlPlaceholder')"
          />
        </n-form-item>
      </n-form>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showAddProviderModal = false">{{ t('common.cancel') }}</n-button>
          <n-button type="primary" @click="handleAddProvider">{{ t('common.save') }}</n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- 添加模型弹窗 -->
    <n-modal v-model:show="showAddModelModal" preset="card" :title="t('modelsProviders.addModel')" style="width: 400px">
      <n-form-item :label="t('modelsProviders.modelName')">
        <n-input
          v-model:value="newModelName"
          :placeholder="t('modelsProviders.modelPlaceholder')"
        />
      </n-form-item>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showAddModelModal = false">{{ t('common.cancel') }}</n-button>
          <n-button type="primary" @click="handleAddModel">{{ t('common.add') }}</n-button>
        </n-space>
      </template>
    </n-modal>

    <n-space v-if="loading" vertical class="loading">
      <n-spin size="large" />
    </n-space>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  NButton, NList, NListItem, NTag, NSelect, NModal, NForm, NFormItem,
  NInput, NDynamicInput, NSpace, NEmpty, NSpin
} from 'naive-ui'
import { useModelsStore } from '@/stores/models'
import { useConfigStore } from '@/stores/config'
import * as modelsApi from '@/api/models'
import * as providersApi from '@/api/providers'

const { t } = useI18n()
const modelsStore = useModelsStore()
const configStore = useConfigStore()

const loading = computed(() => modelsStore.loading)
const showAddProviderModal = ref(false)
const showAddModelModal = ref(false)
const newModelName = ref('')

const newProvider = ref({
  name: '',
  models: [] as string[],
  apiKey: '',
  baseUrl: ''
})

const providerFormRules = {
  name: { required: true, message: '请选择供应商', trigger: 'blur' },
  models: { required: true, message: '请至少添加一个模型', trigger: 'blur' },
}

// 从配置中获取已添加的供应商列表
const configProviders = computed(() => {
  const providers = configStore.config?.providers || {}
  const currentProvider = configStore.config?.provider || ''
  return Object.entries(providers).map(([name, prov]: [string, any]) => ({
    name,
    models: prov.models || [],
    isCurrent: name === currentProvider
  }))
})

// 当前使用的供应商
const currentConfigProvider = computed(() => configStore.config?.provider || '')

// 当前使用的模型
const currentConfigModel = computed(() => {
  const providers = configStore.config?.providers || {}
  const currentProv = providers[currentConfigProvider.value]
  if (currentProv?.models?.length > 0) {
    return currentProv.models[0]
  }
  return configStore.config?.model || ''
})

const availableProviders = [
  { label: 'OpenAI', value: 'openai' },
  { label: 'Anthropic (Claude)', value: 'anthropic' },
  { label: 'DeepSeek', value: 'deepseek' },
  { label: 'Google Gemini', value: 'gemini' },
  { label: 'Groq', value: 'groq' },
  { label: 'Ollama (本地)', value: 'ollama' },
  { label: '硅基流动 SiliconFlow', value: 'siliconflow' },
  { label: 'Kimi (Moonshot)', value: 'kimi' },
  { label: '智谱 GLM (Zhipu)', value: 'zhipu' },
  { label: '通义千问 (DashScope)', value: 'dashscope' },
  { label: '文心一言 (Wenxin)', value: 'wenxin' },
  { label: 'MiniMax', value: 'minimax' },
  { label: '腾讯混元 (Hunyuan)', value: 'hunyuan' },
  { label: '豆包 (Doubao)', value: 'doubao' },
  { label: '月之暗面 (Moonshot)', value: 'moonshot' },
  { label: 'OpenRouter', value: 'openrouter' },
  { label: 'Together AI', value: 'together' },
  { label: 'Mistral AI', value: 'mistral' },
  { label: 'Cohere', value: 'cohere' },
  { label: 'Perplexity', value: 'perplexity' },
  { label: 'vLLM (本地)', value: 'vllm' },
  { label: '自定义 (Custom)', value: 'custom' },
]

// 当前选中的供应商
const selectedProvider = ref('')

// 当前选中供应商的模型列表
const providerModels = computed(() => {
  const providers = configStore.config?.providers || {}
  return providers[selectedProvider.value]?.models || []
})

function selectProvider(name: string) {
  selectedProvider.value = name
}

function getProviderApiKey(name: string): string {
  return configStore.config?.providers?.[name]?.api_key || ''
}

async function handleAddProvider() {
  if (!newProvider.value.name || !newProvider.value.models.length) return
  const providerName = newProvider.value.name
  await configStore.saveProvider({
    name: providerName,
    apiKey: newProvider.value.apiKey,
    baseUrl: newProvider.value.baseUrl,
    models: newProvider.value.models
  })
  showAddProviderModal.value = false
  newProvider.value = { name: '', models: [], apiKey: '', baseUrl: '' }
  await configStore.loadConfig()
  selectedProvider.value = providerName
}

async function deleteProvider(name: string) {
  await providersApi.deleteProvider(name)
  await configStore.loadConfig()
  // Update selected provider
  const providers = configStore.config?.providers || {}
  if (selectedProvider.value === name) {
    selectedProvider.value = Object.keys(providers)[0] || ''
  }
}

async function handleAddModel() {
  if (!newModelName.value || !selectedProvider.value) return
  const providers = { ...configStore.config?.providers }
  const currentModels = providers[selectedProvider.value]?.models || []
  providers[selectedProvider.value] = {
    ...providers[selectedProvider.value],
    models: [...currentModels, newModelName.value]
  }
  await configStore.updateConfig({ providers })
  showAddModelModal.value = false
  newModelName.value = ''
  await configStore.loadConfig()
}

async function setCurrentModel(model: string) {
  // 将模型移到数组第一位
  const providers = { ...configStore.config?.providers }
  const currentModels = providers[selectedProvider.value]?.models || []
  const newModels = [model, ...currentModels.filter(m => m !== model)]
  providers[selectedProvider.value] = {
    ...providers[selectedProvider.value],
    models: newModels
  }
  // 如果这是当前供应商，也更新顶层的 model 字段
  const updates: any = { providers }
  if (selectedProvider.value === currentConfigProvider.value) {
    updates.model = model
  }
  await configStore.updateConfig(updates)
  await configStore.loadConfig()
}

async function removeModel(model: string) {
  const providers = { ...configStore.config?.providers }
  const currentModels = providers[selectedProvider.value]?.models || []
  const newModels = currentModels.filter(m => m !== model)
  providers[selectedProvider.value] = {
    ...providers[selectedProvider.value],
    models: newModels
  }
  // 如果这是当前供应商，且删除的是当前模型，需要更新顶层的 model
  const updates: any = { providers }
  if (selectedProvider.value === currentConfigProvider.value && currentModels[0] === model) {
    updates.model = newModels[0] || ''
  }
  await configStore.updateConfig(updates)
  await configStore.loadConfig()
}

async function switchToProvider(providerName: string) {
  const providers = configStore.config?.providers || {}
  const models = providers[providerName]?.models || []
  if (models.length === 0) return
  
  // 切换供应商和模型
  await configStore.updateConfig({
    provider: providerName,
    model: models[0]
  })
  await configStore.loadConfig()
}

onMounted(async () => {
  await configStore.loadConfig()
  // 默认选中当前供应商
  if (currentConfigProvider.value) {
    selectedProvider.value = currentConfigProvider.value
  } else if (configProviders.value.length > 0) {
    selectedProvider.value = configProviders.value[0].name
  }
})
</script>

<style scoped>
.models-providers-page {
  height: 100%;
  display: flex;
  flex-direction: column;
}

.page-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
}

.page-header h2 {
  margin: 0;
}

.page-content {
  display: flex;
  gap: 20px;
  flex: 1;
  overflow: hidden;
}

.providers-list {
  width: 280px;
  flex-shrink: 0;
}

.providers-list .section-title {
  font-size: 14px;
  color: #666;
  margin-bottom: 10px;
  padding-left: 10px;
}

.provider-item {
  display: flex;
  justify-content: space-between;
  align-items: center;
  width: 100%;
}

.provider-item-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
}

.provider-info {
  display: flex;
  align-items: center;
  gap: 8px;
}

.provider-name {
  font-weight: 500;
}

.model-count {
  font-size: 12px;
  color: #999;
}

.models-panel {
  flex: 1;
  overflow: auto;
}

.panel-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
}

.panel-header h3 {
  margin: 0;
}

.models-grid {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.model-card {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 12px 16px;
  border: 1px solid #e8e8e8;
  border-radius: 8px;
  background: #fff;
}

.model-card.current {
  border-color: #18a058;
  background: #f0fdf4;
}

.model-info {
  display: flex;
  align-items: center;
  gap: 8px;
}

.model-name {
  font-weight: 500;
}

.model-actions {
  display: flex;
  gap: 8px;
}

.switch-provider {
  margin-top: 16px;
  padding-top: 16px;
  border-top: 1px solid #e8e8e8;
}

.loading {
  position: fixed;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
}
</style>
