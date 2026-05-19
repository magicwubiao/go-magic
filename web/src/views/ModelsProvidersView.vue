<template>
  <div>
    <h2 style="margin-bottom: 24px;">Models & Providers</h2>

    <n-tabs type="line" animated>
      <!-- Providers Tab -->
      <n-tab-pane name="providers" tab="Providers">
        <n-space style="margin-bottom: 16px;">
          <n-button type="primary" @click="showAddProviderModal = true">
            Add Provider
          </n-button>
        </n-space>

        <n-spin v-if="providersStore.loading" />
        <n-list v-else>
          <n-list-item v-for="provider in providersStore.providers" :key="provider.id">
            <n-thing :title="provider.name">
              <template #description>
                <n-space>
                  <n-tag :type="provider.enabled ? 'success' : 'default'">
                    {{ provider.enabled ? 'Enabled' : 'Disabled' }}
                  </n-tag>
                  <n-text depth="3">{{ provider.type }}</n-text>
                </n-space>
              </template>
              <template #action>
                <n-space>
                  <n-button size="small" @click="editProvider(provider)">Edit</n-button>
                  <n-button size="small" type="error" @click="deleteProvider(provider.id)">Delete</n-button>
                </n-space>
              </template>
            </n-thing>
          </n-list-item>
        </n-list>

        <!-- Add/Edit Provider Modal -->
        <n-modal v-model:show="showAddProviderModal" title="Provider">
          <n-card style="width: 500px;">
            <n-form>
              <n-form-item label="Name">
                <n-input v-model:value="providerForm.name" />
              </n-form-item>
              <n-form-item label="Type">
                <n-select v-model:value="providerForm.type" :options="typeOptions" />
              </n-form-item>
              <n-form-item label="API Key">
                <n-input v-model:value="providerForm.api_key" type="password" show-password-on="click" placeholder="Your API key" />
              </n-form-item>
              <n-form-item label="Base URL">
                <n-input v-model:value="providerForm.base_url" placeholder="Optional custom base URL" />
              </n-form-item>
              <n-form-item label="Model">
                <n-input v-model:value="providerForm.model" placeholder="Default model for this provider" />
              </n-form-item>
              <n-form-item label="Enabled">
                <n-switch v-model:value="providerForm.enabled" />
              </n-form-item>
            </n-form>
            <template #footer>
              <n-space justify="end">
                <n-button @click="showAddProviderModal = false">Cancel</n-button>
                <n-button type="primary" @click="saveProvider">Save</n-button>
              </n-space>
            </template>
          </n-card>
        </n-modal>
      </n-tab-pane>

      <!-- Models Tab -->
      <n-tab-pane name="models" tab="Models">
        <n-spin v-if="modelsStore.loading" />
        <div v-else>
          <n-card title="Current Model" style="margin-bottom: 16px;">
            <n-space align="center">
              <n-tag type="success" size="large">
                {{ modelsStore.currentModel?.name || 'Not set' }}
              </n-tag>
              <n-text depth="3">
                Provider: {{ modelsStore.currentModel?.provider || 'Unknown' }}
              </n-text>
            </n-space>
          </n-card>

          <n-card title="Available Models">
            <n-list>
              <n-list-item v-for="model in modelsStore.models" :key="model.id">
                <n-thing :title="model.name">
                  <template #description>
                    <n-space>
                      <n-tag size="small">{{ model.provider }}</n-tag>
                      <n-text depth="3">{{ model.description }}</n-text>
                    </n-space>
                  </template>
                  <template #action>
                    <n-button
                      size="small"
                      :type="isCurrent(model.id) ? 'primary' : 'default'"
                      :disabled="isCurrent(model.id)"
                      @click="selectModel(model.id)"
                    >
                      {{ isCurrent(model.id) ? 'Active' : 'Select' }}
                    </n-button>
                  </template>
                </n-thing>
              </n-list-item>
            </n-list>
          </n-card>
        </div>
      </n-tab-pane>
    </n-tabs>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted, computed } from 'vue'
import { useMessage } from 'naive-ui'
import { useProvidersStore } from '@/stores/providers'
import { useModelsStore } from '@/stores/models'
import { useConfigStore } from '@/stores/config'
import type { Provider } from '@/api/providers'

const message = useMessage()
const providersStore = useProvidersStore()
const modelsStore = useModelsStore()
const configStore = useConfigStore()

// --- Providers ---
const showAddProviderModal = ref(false)
const editingProviderId = ref<string | null>(null)

const providerForm = reactive({
  name: '',
  type: 'openai',
  api_key: '',
  base_url: '',
  model: '',
  enabled: true,
})

const typeOptions = [
  { label: 'OpenAI', value: 'openai' },
  { label: 'DeepSeek', value: 'deepseek' },
  { label: 'Anthropic', value: 'anthropic' },
  { label: 'Google (Gemini)', value: 'google' },
  { label: 'Zhipu (智谱)', value: 'zhipu' },
  { label: 'Qwen (通义千问)', value: 'qwen' },
  { label: 'Kimi (Moonshot)', value: 'kimi' },
  { label: 'Ollama (Local)', value: 'ollama' },
  { label: 'Groq', value: 'groq' },
  { label: 'Mistral', value: 'mistral' },
  { label: 'Cohere', value: 'cohere' },
  { label: 'Custom (OpenAI Compatible)', value: 'custom' },
]

function editProvider(provider: Provider) {
  editingProviderId.value = provider.id
  providerForm.name = provider.name
  providerForm.type = provider.type
  providerForm.enabled = provider.enabled
  message.info('Edit provider: ' + provider.name)
}

async function saveProvider() {
  if (editingProviderId.value) {
    await providersStore.updateProvider(editingProviderId.value, { ...providerForm })
  } else {
    await providersStore.createProvider({ ...providerForm })
  }
  // Also save to config if api_key provided
  if (providerForm.api_key) {
    const payload: any = {
      providers: {
        [providerForm.type]: {
          provider: providerForm.type,
          api_key: providerForm.api_key,
          base_url: providerForm.base_url,
          model: providerForm.model,
        },
      },
    }
    if (providerForm.model) {
      payload.model = providerForm.model
    }
    await configStore.updateConfig(payload)
  }
  showAddProviderModal.value = false
  editingProviderId.value = null
  message.success('Provider saved')
}

async function deleteProvider(id: string) {
  await providersStore.deleteProvider(id)
  message.success('Provider deleted')
}

// --- Models ---
const isCurrent = computed(() => (id: string) => {
  return modelsStore.currentModel?.id === id
})

async function selectModel(id: string) {
  await modelsStore.setModel(id)
  message.success('Model updated')
}

onMounted(() => {
  providersStore.loadProviders()
  modelsStore.loadModels()
  modelsStore.loadCurrentModel()
})
</script>
