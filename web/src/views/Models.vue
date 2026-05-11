<template>
  <div class="models-view">
    <n-grid :cols="3" :x-gap="16" :y-gap="16">
      <!-- Providers List -->
      <n-gi :span="1">
        <n-card title="Providers" class="providers-card">
          <template #header-extra>
            <n-button size="small" @click="showAddProvider = true">
              <template #icon>
                <n-icon :component="Add" />
              </template>
              Add
            </n-button>
          </template>

          <n-list hoverable clickable @click="selectProvider(provider)">
            <n-list-item
              v-for="provider in providers"
              :key="provider.id"
              :class="{ active: selectedProvider?.id === provider.id }"
            >
              <n-thing>
                <template #avatar>
                  <n-avatar round>
                    <n-icon :component="getProviderIcon(provider.name)" />
                  </n-avatar>
                </template>
                <template #header>
                  {{ provider.name }}
                </template>
                <template #description>
                  <n-text depth="3">{{ provider.models.length }} models</n-text>
                </template>
                <template #header-extra>
                  <n-tag v-if="provider.default" size="small" type="success">Default</n-tag>
                </template>
              </n-thing>
            </n-list-item>
          </n-list>
        </n-card>
      </n-gi>

      <!-- Models List -->
      <n-gi :span="2">
        <n-card :title="selectedProvider?.name ? `${selectedProvider.name} Models` : 'Select a Provider'">
          <template #header-extra>
            <n-button size="small" @click="refreshModels" :loading="loading">
              <template #icon>
                <n-icon :component="Refresh" />
              </template>
            </n-button>
          </template>

          <n-data-table
            v-if="selectedProvider"
            :columns="modelColumns"
            :data="selectedProvider.models"
            :bordered="false"
          />

          <n-empty v-else description="Select a provider to view models" />
        </n-card>
      </n-gi>
    </n-grid>

    <!-- Add Provider Modal -->
    <n-modal v-model:show="showAddProvider" preset="card" title="Add Provider" style="width: 500px">
      <n-form :model="providerForm" label-placement="top">
        <n-form-item label="Provider Type">
          <n-select
            v-model:value="providerForm.type"
            :options="providerTypeOptions"
            placeholder="Select provider type"
          />
        </n-form-item>

        <n-form-item label="Provider Name" required>
          <n-input v-model:value="providerForm.name" placeholder="My Provider" />
        </n-form-item>

        <n-form-item label="API Base URL" required>
          <n-input
            v-model:value="providerForm.baseURL"
            placeholder="https://api.openai.com/v1"
          />
        </n-form-item>

        <n-form-item label="API Key">
          <n-input
            v-model:value="providerForm.apiKey"
            type="password"
            placeholder="sk-..."
            show-password-on="click"
          />
        </n-form-item>

        <n-form-item label="Organization ID" v-if="providerForm.type === 'openai'">
          <n-input v-model:value="providerForm.orgId" placeholder="org-..." />
        </n-form-item>

        <n-form-item>
          <n-space>
            <n-switch v-model:value="providerForm.isDefault" />
            <n-text>Set as default provider</n-text>
          </n-space>
        </n-form-item>
      </n-form>

      <template #footer>
        <n-space justify="end">
          <n-button @click="showAddProvider = false">Cancel</n-button>
          <n-button type="primary" @click="addProvider" :loading="saving">
            Add Provider
          </n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- OAuth Login Modal -->
    <n-modal v-model:show="showOAuth" preset="card" title="OAuth Login" style="width: 400px">
      <n-space vertical align="center">
        <n-text>Click the button below to authorize with {{ oauthProvider }}.</n-text>
        <n-button type="primary" tag="a" :href="oauthUrl" target="_blank">
          <template #icon>
            <n-icon :component="LogIn" />
          </template>
          Authorize
        </n-button>
      </n-space>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import {
  NCard,
  NGrid,
  NGi,
  NList,
  NListItem,
  NThing,
  NAvatar,
  NTag,
  NText,
  NButton,
  NIcon,
  NSpace,
  NDataTable,
  NEmpty,
  NModal,
  NForm,
  NFormItem,
  NInput,
  NSelect,
  NSwitch,
  NDynamicTags,
} from 'naive-ui'
import {
  Add,
  Refresh,
  LogIn,
  LogoOpenAI,
  LogoGoogle,
  LogoGithub,
  CodeSlash,
  Cloud,
} from '@vicons/ionicons5'

interface Model {
  id: string
  name: string
  contextLength: number
  inputPrice?: number
  outputPrice?: number
  enabled: boolean
}

interface Provider {
  id: string
  name: string
  type: string
  baseURL: string
  apiKey?: string
  models: Model[]
  default: boolean
}

const providers = ref<Provider[]>([])
const selectedProvider = ref<Provider | null>(null)
const showAddProvider = ref(false)
const showOAuth = ref(false)
const oauthProvider = ref('')
const oauthUrl = ref('')
const loading = ref(false)
const saving = ref(false)

const providerForm = reactive({
  type: '',
  name: '',
  baseURL: '',
  apiKey: '',
  orgId: '',
  isDefault: false,
})

const providerTypeOptions = [
  { label: 'OpenAI', value: 'openai' },
  { label: 'Anthropic', value: 'anthropic' },
  { label: 'Google', value: 'google' },
  { label: 'DeepSeek', value: 'deepseek' },
  { label: 'Azure OpenAI', value: 'azure' },
  { label: 'OpenRouter', value: 'openrouter' },
  { label: 'Custom (OpenAI Compatible)', value: 'custom' },
]

const modelColumns = [
  { title: 'Model', key: 'name' },
  {
    title: 'Context',
    key: 'contextLength',
    render: (row: Model) => `${(row.contextLength / 1000).toFixed(0)}K`,
  },
  {
    title: 'Input ($/1M)',
    key: 'inputPrice',
    render: (row: Model) => row.inputPrice ? `$${row.inputPrice}` : '-',
  },
  {
    title: 'Output ($/1M)',
    key: 'outputPrice',
    render: (row: Model) => row.outputPrice ? `$${row.outputPrice}` : '-',
  },
  {
    title: 'Status',
    key: 'enabled',
    render: (row: Model) =>
      h(
        NTag,
        { type: row.enabled ? 'success' : 'default', size: 'small' },
        { default: () => (row.enabled ? 'Enabled' : 'Disabled') }
      ),
  },
]

function getProviderIcon(name: string) {
  const lower = name.toLowerCase()
  if (lower.includes('openai')) return LogoOpenAI
  if (lower.includes('google') || lower.includes('gemini')) return LogoGoogle
  if (lower.includes('github')) return LogoGithub
  if (lower.includes('azure')) return Cloud
  return CodeSlash
}

function selectProvider(provider: Provider) {
  selectedProvider.value = provider
}

async function loadProviders() {
  try {
    const res = await fetch('/api/models/providers')
    if (res.ok) {
      providers.value = await res.json()
    }
  } catch (e) {
    console.error('Failed to load providers:', e)
  }
}

async function refreshModels() {
  if (!selectedProvider.value) return
  loading.value = true
  try {
    const res = await fetch(`/api/models/providers/${selectedProvider.value.id}/refresh`, {
      method: 'POST',
    })
    if (res.ok) {
      const models = await res.json()
      selectedProvider.value.models = models
    }
  } catch (e) {
    console.error('Failed to refresh models:', e)
  } finally {
    loading.value = false
  }
}

async function addProvider() {
  if (!providerForm.name || !providerForm.baseURL) return
  saving.value = true

  try {
    const res = await fetch('/api/models/providers', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(providerForm),
    })

    if (res.ok) {
      showAddProvider.value = false
      resetProviderForm()
      loadProviders()
    }
  } catch (e) {
    console.error('Failed to add provider:', e)
  } finally {
    saving.value = false
  }
}

function resetProviderForm() {
  providerForm.type = ''
  providerForm.name = ''
  providerForm.baseURL = ''
  providerForm.apiKey = ''
  providerForm.orgId = ''
  providerForm.isDefault = false
}

onMounted(() => {
  loadProviders()
})
</script>

<style lang="scss" scoped>
.models-view {
  padding: 16px;
  height: calc(100vh - 84px);
}

.providers-card {
  height: 100%;
}

.n-list-item {
  &.active {
    background: var(--selected-color, #e8f5e9);
  }
}
</style>
