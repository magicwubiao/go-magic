<template>
  <div class="models-providers-page">
    <div class="page-header">
      <h2>{{ t('modelsProviders.title') }}</h2>
      <n-button type="primary" @click="openAddProviderModal">
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
                type="warning"
                ghost
                @click.stop="openEditProviderModal(prov.name)"
              >
                {{ t('common.edit') }}
              </n-button>
              <n-popconfirm @positive-click="deleteProvider(prov.name)">
                <template #trigger>
                  <n-button
                    size="tiny"
                    type="error"
                    ghost
                    @click.stop
                  >
                    {{ t('common.delete') }}
                  </n-button>
                </template>
                {{ t('modelsProviders.confirmDeleteProvider', { name: prov.name }) }}
              </n-popconfirm>
            </div>
          </n-list-item>
        </n-list>
        <n-empty v-else :description="t('modelsProviders.noProviders')" />
      </div>

      <!-- 模型列表 -->
      <div class="models-panel">
        <template v-if="selectedProvider">
          <div class="panel-header">
            <h3>{{ providerModels.length > 0 ? selectedProvider + ' - ' + t('modelsProviders.models') : t('modelsProviders.models') }}</h3>
          </div>

          <n-empty v-if="!providerModels.length" :description="t('modelsProviders.noModels')" />

          <div v-else class="models-grid">
            <div
              v-for="(model, index) in providerModels"
              :key="model"
              class="model-card"
              :class="{ current: isCurrentProvider && index === 0 }"
            >
              <div class="model-info">
                <span class="model-name">{{ model }}</span>
                <n-tag v-if="isCurrentProvider && index === 0" size="small" type="success">
                  {{ t('modelsProviders.currentModel') }}
                </n-tag>
              </div>
              <div class="model-actions">
                <n-button
                  v-if="!isCurrentProvider || index !== 0"
                  size="tiny"
                  type="primary"
                  @click="setCurrentModel(model)"
                >
                  {{ t('modelsProviders.setAsCurrent') }}
                </n-button>
              </div>
            </div>
          </div>


        </template>
        <n-empty v-else :description="t('modelsProviders.selectProvider')" />
      </div>
    </div>

    <!-- 添加/编辑供应商弹窗 -->
    <n-modal v-model:show="showProviderModal" preset="card" class="modal-responsive modal-scroll" :title="isEditing ? t('modelsProviders.editProvider') : t('modelsProviders.addProvider')" style="width: 500px; max-width: 96vw;">
      <n-form :model="editingProvider" label-placement="top">
        <n-form-item :label="t('modelsProviders.providerName')" path="name">
          <n-select
            v-if="!isEditing"
            v-model:value="editingProvider.name"
            :options="availableProviders"
            filterable
            :placeholder="t('modelsProviders.selectProviderType')"
          />
          <n-input v-else :value="editingProvider.name" disabled />
        </n-form-item>
        <n-form-item :label="t('modelsProviders.models')">
          <n-dynamic-input
            v-model:value="editingProvider.models"
            :placeholder="t('modelsProviders.modelPlaceholder')"
          />
        </n-form-item>
        <n-form-item :label="t('modelsProviders.apiKey')">
          <n-input-group>
            <n-input
              v-model:value="editingProvider.apiKey"
              type="password"
              show-password-on="click"
              :placeholder="t('modelsProviders.apiKeyPlaceholder')"
            />
            <n-button
              :loading="testing"
              :disabled="!editingProvider.name"
              :title="testTitle"
              @click="handleTestConnection"
            >
              <span
                v-if="testResult && !testing"
                class="test-dot"
                :class="testResult.ok ? 'dot-ok' : 'dot-fail'"
              ></span>
              {{ testing ? t('modelsProviders.testing') : t('modelsProviders.testConnection') }}
            </n-button>
          </n-input-group>
        </n-form-item>
        <n-form-item :label="t('modelsProviders.baseUrl')">
          <n-input
            v-model:value="editingProvider.baseUrl"
            :placeholder="t('modelsProviders.baseUrlPlaceholder')"
          />
        </n-form-item>
        <n-form-item :label="t('modelsProviders.visionLabel')">
          <n-select
            v-model:value="editingProvider.vision"
            :options="visionOptions"
            :placeholder="t('modelsProviders.visionAuto')"
          />
        </n-form-item>
      </n-form>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showProviderModal = false">{{ t('common.cancel') }}</n-button>
          <n-button type="primary" @click="handleSaveProvider">{{ t('common.save') }}</n-button>
        </n-space>
      </template>
    </n-modal>

    <n-space v-if="loading" vertical class="loading">
      <n-spin size="large" />
    </n-space>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  NButton, NList, NListItem, NTag, NSelect, NModal, NForm, NFormItem,
  NInput, NInputGroup, NDynamicInput, NSpace, NEmpty, NSpin
} from 'naive-ui'
import { useModelsStore } from '@/stores/models'
import { useConfigStore } from '@/stores/config'
import * as providersApi from '@/api/providers'

const { t } = useI18n()
const modelsStore = useModelsStore()
const configStore = useConfigStore()

const loading = computed(() => modelsStore.loading)
const showProviderModal = ref(false)
const isEditing = ref(false)

const editingProvider = ref({
  name: '',
  models: [] as string[],
  apiKey: '',
  baseUrl: '',
  vision: null as boolean | null
})

// 视觉支持三态：null=按模型名自动检测，true/false=显式声明
const visionOptions = [
  { label: t('modelsProviders.visionAuto'), value: null },
  { label: t('modelsProviders.visionOn'), value: true },
  { label: t('modelsProviders.visionOff'), value: false }
]

// 测试连接：用表单当前值（未保存也行）向该 provider 发一条轻量请求
const testing = ref(false)
const testResult = ref<providersApi.ProviderTestResult | null>(null)

// 详细结果放在按钮 title 里，hover 即可看到延迟或失败原因
const testTitle = computed(() => {
  if (!testResult.value) return ''
  return testResult.value.ok
    ? t('modelsProviders.testOk', { ms: testResult.value.latencyMs ?? 0 })
    : t('modelsProviders.testFailed') + (testResult.value.error ? `: ${testResult.value.error}` : '')
})

function openAddProviderModal() {
  isEditing.value = false
  editingProvider.value = { name: '', models: [], apiKey: '', baseUrl: '', vision: null }
  testResult.value = null
  showProviderModal.value = true
}

function openEditProviderModal(name: string) {
  isEditing.value = true
  const providers = configStore.config?.providers || {}
  const prov = providers[name] || {}
  editingProvider.value = {
    name,
    models: prov.models || [],
    apiKey: prov.api_key || '',
    baseUrl: prov.base_url || '',
    vision: prov.vision === true || prov.vision === false ? prov.vision : null
  }
  testResult.value = null
  showProviderModal.value = true
}

async function handleTestConnection() {
  const name = editingProvider.value.name
  if (!name || testing.value) return
  testing.value = true
  testResult.value = null
  try {
    testResult.value = await providersApi.testProvider(name, {
      api_key: editingProvider.value.apiKey || undefined,
      base_url: editingProvider.value.baseUrl || undefined,
      model: editingProvider.value.models?.[0] || undefined
    })
  } catch (e: any) {
    testResult.value = { ok: false, error: e?.message || String(e) }
  } finally {
    testing.value = false
  }
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

const availableProviders = [
  { label: 'OpenAI', value: 'openai' },
  { label: 'Anthropic (Claude)', value: 'anthropic' },
  { label: 'DeepSeek', value: 'deepseek' },
  { label: 'Google Gemini', value: 'gemini' },
  { label: 'Groq', value: 'groq' },
  { label: '硅基流动 SiliconFlow', value: 'siliconflow' },
  { label: '智谱 GLM (Zhipu)', value: 'zhipu' },
  { label: '通义千问 (DashScope)', value: 'dashscope' },
  { label: '文心一言 (Wenxin)', value: 'wenxin' },
  { label: 'MiniMax', value: 'minimax' },
  { label: 'MiMo', value: 'mimo' },
  { label: '腾讯混元 (Hunyuan)', value: 'hunyuan' },
  { label: 'LongCat (美团龙猫)', value: 'longcat' },
  { label: 'Meta (Muse Spark)', value: 'meta' },
  { label: '火山引擎 (Huoshan)', value: 'huoshan' },
  { label: '月之暗面 (Moonshot)', value: 'moonshot' },
  { label: 'OpenRouter', value: 'openrouter' },
  { label: 'Together AI', value: 'together' },
  { label: 'Mistral AI', value: 'mistral' },
  { label: 'Cohere', value: 'cohere' },
  { label: 'Perplexity', value: 'perplexity' },
  { label: 'Ollama (本地)', value: 'ollama' },
  { label: 'vLLM (本地)', value: 'vllm' },
  { label: '自定义 (Custom)', value: 'custom' },
]

// 当前选中的供应商
const selectedProvider = ref('')

// 已知供应商默认配置（与后端 internal/provider/config.go 的默认值保持一致）
const providerPresets: Record<string, { baseUrl: string; model: string }> = {
  openai: { baseUrl: 'https://api.openai.com/v1', model: 'gpt-5.6' },
  anthropic: { baseUrl: 'https://api.anthropic.com', model: 'claude-sonnet-5' },
  deepseek: { baseUrl: 'https://api.deepseek.com', model: 'deepseek-chat' },
  gemini: { baseUrl: 'https://generativelanguage.googleapis.com/v1beta', model: 'gemini-3.7-flash' },
  groq: { baseUrl: 'https://api.groq.com/openai/v1', model: 'llama-3.3-70b-versatile' },
  zhipu: { baseUrl: 'https://open.bigmodel.cn/api/paas/v4', model: 'glm-4.6' },
  dashscope: { baseUrl: 'https://dashscope.aliyuncs.com/compatible-mode/v1', model: 'qwen-plus' },
  wenxin: { baseUrl: 'https://aip.baidubce.com/rpc/2/0/ai_qianfan_200/v1', model: 'ernie-4.0-8k' },
  minimax: { baseUrl: 'https://api.minimax.chat/v1', model: 'MiniMax-M2.5' },
  mimo: { baseUrl: 'https://api.mymimo.ai/v1', model: 'mimo-v2-flash' },
  hunyuan: { baseUrl: 'https://hunyuan.cloud.tencent.com/v1', model: 'hunyuan-turbos-latest' },
  longcat: { baseUrl: 'https://api.longcat.chat/openai/v1', model: 'LongCat-Flash-Chat' },
  meta: { baseUrl: 'https://api.meta.ai/v1', model: 'muse-spark-1.3' },
  huoshan: { baseUrl: 'https://ark.cn-beijing.volces.com/api/v3', model: 'doubao-1.5-pro-32k' },
  moonshot: { baseUrl: 'https://api.moonshot.cn/v1', model: 'kimi-k2-0905-preview' },
  openrouter: { baseUrl: 'https://openrouter.ai/api/v1', model: 'openai/gpt-5.6' },
  together: { baseUrl: 'https://api.together.xyz/v1', model: 'meta-llama/Llama-3.3-70B-Instruct-Turbo' },
  mistral: { baseUrl: 'https://api.mistral.ai/v1', model: 'mistral-large-latest' },
  cohere: { baseUrl: 'https://api.cohere.ai/v2', model: 'command-a-03-2025' },
  perplexity: { baseUrl: 'https://api.perplexity.ai', model: 'sonar-pro' },
  ollama: { baseUrl: 'http://localhost:11434', model: 'llama3.3' },
  vllm: { baseUrl: 'http://localhost:8000', model: 'llama3' }
}

// 新增供应商时，按已知预设自动填充 baseUrl 和默认模型
watch(() => editingProvider.value.name, (name) => {
  if (isEditing.value || !name) return
  const preset = providerPresets[name]
  if (!preset) return
  if (!editingProvider.value.baseUrl) {
    editingProvider.value.baseUrl = preset.baseUrl
  }
  if (editingProvider.value.models.length === 0) {
    editingProvider.value.models = [preset.model]
  }
})

// 当前选中的供应商是否是当前供应商
const isCurrentProvider = computed(() => {
  return selectedProvider.value === currentConfigProvider.value
})

// 当前选中供应商的模型列表
const providerModels = computed(() => {
  const providers = configStore.config?.providers || {}
  return providers[selectedProvider.value]?.models || []
})

function selectProvider(name: string) {
  selectedProvider.value = name
}

async function handleSaveProvider() {
  if (!editingProvider.value.name) return
  
  await configStore.saveProvider({
    name: editingProvider.value.name,
    apiKey: editingProvider.value.apiKey,
    baseUrl: editingProvider.value.baseUrl,
    models: editingProvider.value.models,
    vision: editingProvider.value.vision
  })
  showProviderModal.value = false
  await configStore.loadConfig()
  
  if (isEditing.value) {
    // 编辑后刷新当前选择
    await refreshModelsList()
  } else {
    // 添加后选中新供应商
    selectedProvider.value = editingProvider.value.name
  }
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

async function setCurrentModel(model: string) {
  // 如果不在当前供应商，先切换到该供应商
  if (selectedProvider.value !== currentConfigProvider.value) {
    const providers = configStore.config?.providers || {}
    const currentModels = providers[selectedProvider.value]?.models || []
    const newModels = [model, ...currentModels.filter(m => m !== model)]
    // 切换供应商并设置当前模型
    await configStore.updateConfig({
      provider: selectedProvider.value,
      model: model,
      providers: {
        ...providers,
        [selectedProvider.value]: {
          ...providers[selectedProvider.value],
          models: newModels
        }
      }
    })
  } else {
    // 在当前供应商，只需更新模型顺序
    const providers = { ...configStore.config?.providers }
    const currentModels = providers[selectedProvider.value]?.models || []
    const newModels = [model, ...currentModels.filter(m => m !== model)]
    await configStore.updateConfig({
      model: model,
      providers: {
        ...providers,
        [selectedProvider.value]: {
          ...providers[selectedProvider.value],
          models: newModels
        }
      }
    })
  }
  await configStore.loadConfig()
}

async function refreshModelsList() {
  // 重新获取配置
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
  width: 350px;
  flex-shrink: 0;
}

.providers-list .section-title {
  font-size: 14px;
  color: #666;
  margin-bottom: 10px;
  padding-left: 10px;
}

.provider-item-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.provider-item {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 4px;
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
  margin-bottom: 20px;
}

.panel-header h3 {
  margin: 0;
}

.models-grid {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.model-card {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 16px;
  background: #f5f5f5;
  border-radius: 8px;
  border: 2px solid transparent;
}

.model-card.current {
  border-color: #18a058;
  background: #f0fff0;
}

.model-info {
  display: flex;
  align-items: center;
  gap: 12px;
}

.model-name {
  font-weight: 500;
}

.model-actions {
  display: flex;
  gap: 8px;
}

.switch-provider {
  margin-top: 20px;
}

.loading {
  position: absolute;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
}

.test-dot {
  display: inline-block;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  margin-right: 6px;
  flex-shrink: 0;
}
.dot-ok {
  background: #18a058;
}
.dot-fail {
  background: #d03050;
}

/* 移动端:左右分栏改为上下堆叠 */
@media (max-width: 768px) {
  .page-header {
    flex-wrap: wrap;
    gap: 8px;
    margin-bottom: 12px;
  }
  .page-content {
    flex-direction: column;
    overflow: visible;
    gap: 14px;
  }
  .providers-list {
    width: 100%;
  }
}
</style>