<template>
  <div>
    <n-space justify="space-between" style="margin-bottom: 16px;">
      <h2>Profiles</h2>
      <n-button type="primary" @click="showCreateModal = true">New Profile</n-button>
    </n-space>

    <n-spin v-if="loading" />
    <template v-else>
      <n-grid :cols="3" :x-gap="12" :y-gap="12">
        <n-gi v-for="profile in profiles" :key="profile.name">
          <n-card size="small">
            <template #header>
              <n-space align="center">
                <span>{{ profile.name }}</span>
                <n-tag v-if="profile.is_default" type="success" size="small">Active</n-tag>
              </n-space>
            </template>
            <n-space vertical>
              <n-text depth="3">Skills: {{ profile.skill_count || 0 }}</n-text>
              <n-text depth="3">Has .env: {{ profile.has_env ? 'Yes' : 'No' }}</n-text>
            </n-space>
            <template #action>
              <n-space>
                <n-button
                  v-if="!profile.is_default"
                  size="small"
                  type="primary"
                  @click="switchProfile(profile.name)"
                >
                  Switch
                </n-button>
                <n-button size="small" @click="editSoul(profile.name)">Soul</n-button>
                <n-popconfirm @positive-click="deleteProfile(profile.name)">
                  <template #trigger>
                    <n-button size="small" type="error" :disabled="profile.is_default">Delete</n-button>
                  </template>
                  Delete profile "{{ profile.name }}"?
                </n-popconfirm>
              </n-space>
            </template>
          </n-card>
        </n-gi>
      </n-grid>
    </template>

    <!-- Create Profile Modal -->
    <n-modal v-model:show="showCreateModal" preset="dialog" title="Create Profile">
      <n-form>
        <n-form-item label="Name">
          <n-input v-model:value="newProfileName" placeholder="Profile name" />
        </n-form-item>
        <n-form-item label="Clone from default">
          <n-switch v-model:value="cloneFromDefault" />
        </n-form-item>
      </n-form>
      <template #action>
        <n-button @click="showCreateModal = false">Cancel</n-button>
        <n-button type="primary" :loading="creating" @click="createProfile">Create</n-button>
      </template>
    </n-modal>

    <!-- Soul Editor Modal -->
    <n-modal v-model:show="showSoulModal" preset="dialog" :title="`Soul - ${editingProfile}`" style="width: 600px;">
      <n-input
        v-model:value="soulContent"
        type="textarea"
        :rows="15"
        placeholder="Define the AI's personality, behavior, and knowledge scope..."
      />
      <template #action>
        <n-button @click="showSoulModal = false">Cancel</n-button>
        <n-button type="primary" :loading="savingSoul" @click="saveSoul">Save</n-button>
      </template>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useMessage } from 'naive-ui'
import { request } from '@/api/client'
import { useConfigStore } from '@/stores/config'
import { useChatStore } from '@/stores/chat'

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
    message.error('Failed to load profiles')
  } finally {
    loading.value = false
  }
}

async function createProfile(): Promise<void> {
  if (!newProfileName.value.trim()) {
    message.warning('Please enter a profile name')
    return
  }
  creating.value = true
  try {
    await request('/profiles', {
      method: 'POST',
      body: JSON.stringify({ name: newProfileName.value, clone_from_default: cloneFromDefault.value }),
    })
    message.success(`Profile "${newProfileName.value}" created`)
    showCreateModal.value = false
    newProfileName.value = ''
    await loadProfiles()
  } catch (e) {
    message.error('Failed to create profile')
  } finally {
    creating.value = false
  }
}

async function switchProfile(name: string): Promise<void> {
  try {
    await request(`/profiles/${name}/switch`, { method: 'POST' })
    message.success(`Switched to profile "${name}"`)
    // Reload config and sessions after profile switch
    await configStore.loadConfig()
    await chatStore.loadSessions()
    await loadProfiles()
  } catch (e) {
    message.error('Failed to switch profile')
  }
}

async function deleteProfile(name: string): Promise<void> {
  try {
    await request(`/profiles/${name}`, { method: 'DELETE' })
    message.success(`Profile "${name}" deleted`)
    await loadProfiles()
  } catch (e) {
    message.error('Failed to delete profile')
  }
}

async function editSoul(name: string): Promise<void> {
  editingProfile.value = name
  try {
    const res = await request<{ content: string; exists: boolean }>(`/profiles/${name}/soul`)
    soulContent.value = res.content || ''
    showSoulModal.value = true
  } catch (e) {
    message.error('Failed to load soul')
  }
}

async function saveSoul(): Promise<void> {
  savingSoul.value = true
  try {
    await request(`/profiles/${editingProfile.value}/soul`, {
      method: 'PUT',
      body: JSON.stringify({ content: soulContent.value }),
    })
    message.success('Soul saved')
    showSoulModal.value = false
  } catch (e) {
    message.error('Failed to save soul')
  } finally {
    savingSoul.value = false
  }
}

onMounted(() => {
  loadProfiles()
})
</script>
