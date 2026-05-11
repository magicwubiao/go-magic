<template>
  <div class="settings-view">
    <n-tabs type="line" animated>
      <!-- Profiles -->
      <n-tab-pane name="profiles" tab="Profiles">
        <n-space vertical :size="16">
          <n-space justify="space-between" align="center">
            <h3>{{ $t('settings.profiles.title') }}</h3>
            <n-button type="primary" @click="createProfile">
              <template #icon>
                <n-icon :component="Add" />
              </template>
              {{ $t('settings.profiles.create') }}
            </n-button>
          </n-space>

          <n-list hoverable clickable>
            <n-list-item v-for="profile in profiles" :key="profile.name">
              <n-space justify="space-between" align="center">
                <n-space align="center">
                  <n-icon :component="Person" size="20" />
                  <span>{{ profile.name }}</span>
                  <n-tag v-if="profile.active" size="small" type="success">
                    {{ $t('settings.profiles.active') }}
                  </n-tag>
                </n-space>
                <n-space>
                  <n-button
                    v-if="!profile.active"
                    size="small"
                    @click="switchProfile(profile.name)"
                  >
                    {{ $t('settings.profiles.switch') }}
                  </n-button>
                  <n-button
                    v-if="profile.name !== 'default'"
                    size="small"
                    type="error"
                    @click="deleteProfile(profile.name)"
                  >
                    {{ $t('settings.profiles.delete') }}
                  </n-button>
                </n-space>
              </n-space>
            </n-list-item>
          </n-list>
        </n-space>
      </n-tab-pane>

      <!-- Provider -->
      <n-tab-pane name="provider" tab="Provider">
        <n-form label-placement="left" label-width="120">
          <n-form-item :label="$t('settings.provider.select')">
            <n-select
              v-model:value="config.provider"
              :options="providerOptions"
              placeholder="Select provider"
            />
          </n-form-item>

          <n-form-item :label="$t('settings.provider.model')">
            <n-select
              v-model:value="config.model"
              :options="modelOptions"
              placeholder="Select model"
              filterable
            />
          </n-form-item>

          <n-form-item :label="$t('settings.provider.apiKey')">
            <n-input
              v-model:value="config.apiKey"
              type="password"
              show-password-on="click"
              placeholder="API Key"
            />
          </n-form-item>

          <n-form-item :label="$t('settings.provider.baseURL')">
            <n-input
              v-model:value="config.baseURL"
              placeholder="https://api.openai.com/v1"
            />
          </n-form-item>

          <n-button type="primary" @click="saveProvider">
            {{ $t('common.save') }}
          </n-button>
        </n-form>
      </n-tab-pane>

      <!-- Display -->
      <n-tab-pane name="display" tab="Display">
        <n-form label-placement="left" label-width="150">
          <n-form-item :label="$t('settings.display.streaming')">
            <n-switch v-model:value="config.streaming" />
          </n-form-item>

          <n-form-item :label="$t('settings.display.compactMode')">
            <n-switch v-model:value="config.compactMode" />
          </n-form-item>

          <n-form-item :label="$t('settings.display.reasoning')">
            <n-switch v-model:value="config.showReasoning" />
          </n-form-item>

          <n-form-item :label="$t('settings.display.cost')">
            <n-switch v-model:value="config.showCost" />
          </n-form-item>

          <n-form-item :label="$t('settings.display.theme')">
            <n-select
              v-model:value="config.theme"
              :options="[
                { label: 'Light', value: 'light' },
                { label: 'Dark', value: 'dark' },
                { label: 'System', value: 'system' },
              ]"
            />
          </n-form-item>

          <n-button type="primary" @click="saveDisplay">
            {{ $t('common.save') }}
          </n-button>
        </n-form>
      </n-tab-pane>

      <!-- Agent -->
      <n-tab-pane name="agent" tab="Agent">
        <n-form label-placement="left" label-width="150">
          <n-form-item :label="$t('settings.agent.maxTurns')">
            <n-input-number v-model:value="config.maxTurns" :min="1" :max="100" />
          </n-form-item>

          <n-form-item :label="$t('settings.agent.timeout')">
            <n-input-number v-model:value="config.timeout" :min="10" :max="600" />
          </n-form-item>

          <n-form-item :label="$t('settings.agent.toolEnforcement')">
            <n-select
              v-model:value="config.toolEnforcement"
              :options="[
                { label: 'Auto', value: 'auto' },
                { label: 'Required', value: 'required' },
                { label: 'Disabled', value: 'disabled' },
              ]"
            />
          </n-form-item>

          <n-button type="primary" @click="saveAgent">
            {{ $t('common.save') }}
          </n-button>
        </n-form>
      </n-tab-pane>
    </n-tabs>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { NIcon } from 'naive-ui'
import { Person, Add } from '@vicons/ionicons5'
import { profileApi, configApi } from '@/api'

const profiles = ref<any[]>([])
const config = ref({
  provider: 'openai',
  model: '',
  apiKey: '',
  baseURL: '',
  streaming: true,
  compactMode: false,
  showReasoning: true,
  showCost: true,
  theme: 'dark',
  maxTurns: 50,
  timeout: 120,
  toolEnforcement: 'auto',
})

const providerOptions = [
  { label: 'OpenAI', value: 'openai' },
  { label: 'DeepSeek', value: 'deepseek' },
  { label: 'Huoshan', value: 'huoshan' },
  { label: 'Anthropic', value: 'anthropic' },
]

const modelOptions = ref<any[]>([])

async function loadProfiles() {
  try {
    const response = await profileApi.list()
    profiles.value = response.data
  } catch (e) {
    console.error('Failed to load profiles:', e)
  }
}

async function loadConfig() {
  try {
    const response = await configApi.get()
    Object.assign(config.value, response.data)
  } catch (e) {
    console.error('Failed to load config:', e)
  }
}

async function createProfile() {
  const name = prompt('Profile name:')
  if (name) {
    await profileApi.create(name)
    loadProfiles()
  }
}

async function switchProfile(name: string) {
  await profileApi.switch(name)
  loadProfiles()
}

async function deleteProfile(name: string) {
  await profileApi.delete(name)
  loadProfiles()
}

async function saveProvider() {
  await configApi.save({ section: 'provider', data: config.value })
}

async function saveDisplay() {
  await configApi.save({ section: 'display', data: config.value })
}

async function saveAgent() {
  await configApi.save({ section: 'agent', data: config.value })
}

onMounted(() => {
  loadProfiles()
  loadConfig()
})
</script>

<style lang="scss" scoped>
.settings-view {
  h3 {
    margin: 0;
  }
}
</style>
