<template>
  <div>
    <n-space justify="space-between" style="margin-bottom: 16px;">
      <h2>{{ t('profiles.title') }}</h2>
      <n-button type="primary" @click="showCreateModal = true">{{ t('profiles.newProfile') }}</n-button>
    </n-space>

    <n-spin v-if="loading" />
    <template v-else>
      <n-grid :cols="3" :x-gap="12" :y-gap="12">
        <n-gi v-for="profile in profiles" :key="profile.name">
          <n-card size="small">
            <template #header>
              <n-space align="center">
                <span>{{ profile.name }}</span>
                <n-tag v-if="profile.is_default" type="success" size="small">{{ t('profiles.active') }}</n-tag>
              </n-space>
            </template>
            <n-space vertical>
              <n-text depth="3">{{ t('profiles.skills') }}: {{ profile.skill_count || 0 }}</n-text>
              <n-text depth="3">{{ t('profiles.hasEnv') }}: {{ profile.has_env ? t('profiles.yes') : t('profiles.no') }}</n-text>
            </n-space>
            <template #action>
              <n-space>
                <n-button
                  v-if="!profile.is_default"
                  size="small"
                  type="primary"
                  @click="switchProfile(profile.name)"
                >
                  {{ t('profiles.switch') }}
                </n-button>
                <n-button size="small" @click="editSoul(profile.name)">{{ t('profiles.soul') }}</n-button>
                <n-popconfirm @positive-click="deleteProfile(profile.name)">
                  <template #trigger>
                    <n-button size="small" type="error" :disabled="profile.is_default">{{ t('common.delete') }}</n-button>
                  </template>
                  {{ t('profiles.deleteConfirm', { name: profile.name }) }}
                </n-popconfirm>
              </n-space>
            </template>
          </n-card>
        </n-gi>
      </n-grid>
    </template>

    <!-- Create Profile Modal -->
    <n-modal v-model:show="showCreateModal" preset="dialog" :title="t('profiles.newProfile')">
      <n-form>
        <n-form-item :label="t('profiles.profileName')">
          <n-input v-model:value="newProfileName" :placeholder="t('profiles.profileName')" />
        </n-form-item>
        <n-form-item :label="t('profiles.cloneFromDefault')">
          <n-switch v-model:value="cloneFromDefault" />
        </n-form-item>
      </n-form>
      <template #action>
        <n-button @click="showCreateModal = false">{{ t('common.cancel') }}</n-button>
        <n-button type="primary" :loading="creating" @click="createProfile">{{ t('common.create') }}</n-button>
      </template>
    </n-modal>

    <!-- Soul Editor Modal -->
    <n-modal v-model:show="showSoulModal" preset="dialog" :title="`${t('profiles.soul')} - ${editingProfile}`" style="width: 600px;">
      <n-input
        v-model:value="soulContent"
        type="textarea"
        :rows="15"
        :placeholder="t('profiles.soulPlaceholder')"
      />
      <template #action>
        <n-button @click="showSoulModal = false">{{ t('common.cancel') }}</n-button>
        <n-button type="primary" :loading="savingSoul" @click="saveSoul">{{ t('common.save') }}</n-button>
      </template>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { request } from '@/api/client'
import { useConfigStore } from '@/stores/config'
import { useChatStore } from '@/stores/chat'

const { t } = useI18n()

interface Profile {
  name: string
  path: string
  is_default: boolean
  model: string | null
  provider: string | null
  has_env: boolean
  skill_count: number
}

const message = useMessage()
const configStore = useConfigStore()
const chatStore = useChatStore()
const profiles = ref<Profile[]>([])
const loading = ref(false)
const creating = ref(false)
const savingSoul = ref(false)
const showCreateModal = ref(false)
const showSoulModal = ref(false)
const newProfileName = ref('')
const cloneFromDefault = ref(true)
const editingProfile = ref('')
const soulContent = ref('')

async function loadProfiles(): Promise<void> {
  loading.value = true
  try {
    const res = await request<{ profiles: Profile[] }>('/profiles')
    profiles.value = res.profiles || []
  } catch (e) {
    message.error(t('profiles.failedToLoad'))
  } finally {
    loading.value = false
  }
}

async function createProfile(): Promise<void> {
  if (!newProfileName.value.trim()) {
    message.warning(t('profiles.pleaseEnterName'))
    return
  }
  creating.value = true
  try {
    await request('/profiles', {
      method: 'POST',
      body: JSON.stringify({ name: newProfileName.value, clone_from_default: cloneFromDefault.value }),
    })
    message.success(t('profiles.created', { name: newProfileName.value }))
    showCreateModal.value = false
    newProfileName.value = ''
    await loadProfiles()
  } catch (e) {
    message.error(t('profiles.failedToCreate'))
  } finally {
    creating.value = false
  }
}

async function switchProfile(name: string): Promise<void> {
  try {
    await request(`/profiles/${name}/switch`, { method: 'POST' })
    message.success(t('profiles.switched', { name }))
    // Reload config and sessions after profile switch
    await configStore.loadConfig()
    await chatStore.loadSessions()
    await loadProfiles()
  } catch (e) {
    message.error(t('profiles.failedToSwitch'))
  }
}

async function deleteProfile(name: string): Promise<void> {
  try {
    await request(`/profiles/${name}`, { method: 'DELETE' })
    message.success(t('profiles.deleted', { name }))
    await loadProfiles()
  } catch (e) {
    message.error(t('profiles.failedToDelete'))
  }
}

async function editSoul(name: string): Promise<void> {
  editingProfile.value = name
  try {
    const res = await request<{ content: string; exists: boolean }>(`/profiles/${name}/soul`)
    soulContent.value = res.content || ''
    showSoulModal.value = true
  } catch (e) {
    message.error(t('profiles.failedToLoadSoul'))
  }
}

async function saveSoul(): Promise<void> {
  savingSoul.value = true
  try {
    await request(`/profiles/${editingProfile.value}/soul`, {
      method: 'PUT',
      body: JSON.stringify({ content: soulContent.value }),
    })
    message.success(t('profiles.saved'))
    showSoulModal.value = false
  } catch (e) {
    message.error(t('profiles.failedToSaveSoul'))
  } finally {
    savingSoul.value = false
  }
}

onMounted(() => {
  loadProfiles()
})
</script>
