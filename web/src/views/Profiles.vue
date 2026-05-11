<template>
  <div class="profiles-view">
    <n-grid :cols="3" :x-gap="16" :y-gap="16">
      <!-- Profiles List -->
      <n-gi :span="1">
        <n-card title="Profiles" class="profiles-card">
          <template #header-extra>
            <n-button size="small" type="primary" @click="showCreateProfile = true">
              <template #icon>
                <n-icon :component="Add" />
              </template>
              New
            </n-button>
          </template>

          <n-list hoverable clickable @click="selectProfile(profile)" v-if="profiles.length > 0">
            <n-list-item
              v-for="profile in profiles"
              :key="profile.id"
              :class="{ active: selectedProfile?.id === profile.id }"
            >
              <n-thing>
                <template #avatar>
                  <n-avatar round>
                    <n-icon :component="Profile" />
                  </n-avatar>
                </template>
                <template #header>
                  {{ profile.name }}
                  <n-tag v-if="profile.active" type="success" size="tiny" style="margin-left: 8px">
                    Active
                  </n-tag>
                </template>
                <template #description>
                  <n-text depth="3">{{ profile.description || 'No description' }}</n-text>
                </template>
              </n-thing>
            </n-list-item>
          </n-list>
          <n-empty v-else description="No profiles yet" />
        </n-card>
      </n-gi>

      <!-- Profile Details -->
      <n-gi :span="2">
        <n-card v-if="selectedProfile" :title="`Profile: ${selectedProfile.name}`">
          <n-descriptions :column="1" label-placement="top">
            <n-descriptions-item label="Name">
              {{ selectedProfile.name }}
            </n-descriptions-item>
            <n-descriptions-item label="Description">
              {{ selectedProfile.description || 'No description' }}
            </n-descriptions-item>
            <n-descriptions-item label="Created">
              {{ formatTime(selectedProfile.createdAt) }}
            </n-descriptions-item>
            <n-descriptions-item label="Provider">
              <n-tag size="small">{{ selectedProfile.provider || 'Not set' }}</n-tag>
            </n-descriptions-item>
            <n-descriptions-item label="Model">
              <n-tag size="small">{{ selectedProfile.model || 'Not set' }}</n-tag>
            </n-descriptions-item>
          </n-descriptions>

          <n-divider />

          <!-- Actions -->
          <n-space>
            <n-button @click="switchToProfile" :loading="switching" :disabled="selectedProfile.active">
              <template #icon>
                <n-icon :component="SwapHorizontal" />
              </template>
              Switch to Profile
            </n-button>
            <n-button @click="editProfile">
              <template #icon>
                <n-icon :component="Create" />
              </template>
              Edit
            </n-button>
            <n-button @click="duplicateProfile">
              <template #icon>
                <n-icon :component="Copy" />
              </template>
              Clone
            </n-button>
            <n-button type="warning" @click="exportProfile">
              <template #icon>
                <n-icon :component="CloudDownload" />
              </template>
              Export
            </n-button>
            <n-button type="error" @click="deleteProfile" :disabled="profiles.length <= 1">
              <template #icon>
                <n-icon :component="Trash" />
              </template>
              Delete
            </n-button>
          </n-space>
        </n-card>

        <n-card v-else title="Profile Details">
          <n-empty description="Select a profile to view details" />
        </n-card>
      </n-gi>
    </n-grid>

    <!-- Create/Edit Profile Modal -->
    <n-modal
      v-model:show="showCreateProfile"
      preset="card"
      :title="editingProfile ? 'Edit Profile' : 'Create Profile'"
      style="width: 500px"
    >
      <n-form :model="profileForm" label-placement="top">
        <n-form-item label="Profile Name" required>
          <n-input v-model:value="profileForm.name" placeholder="My Profile" />
        </n-form-item>

        <n-form-item label="Description">
          <n-input
            v-model:value="profileForm.description"
            type="textarea"
            placeholder="Profile description"
            :rows="2"
          />
        </n-form-item>

        <n-form-item label="Provider">
          <n-select
            v-model:value="profileForm.provider"
            :options="providerOptions"
            placeholder="Select provider"
            clearable
          />
        </n-form-item>

        <n-form-item label="Model">
          <n-select
            v-model:value="profileForm.model"
            :options="modelOptions"
            placeholder="Select model"
            clearable
            filterable
          />
        </n-form-item>

        <n-form-item label="Enabled Toolsets">
          <n-checkbox-group v-model:value="profileForm.enabledToolsets">
            <n-space>
              <n-checkbox v-for="toolset in availableToolsets" :key="toolset" :value="toolset" :label="toolset" />
            </n-space>
          </n-checkbox-group>
        </n-form-item>
      </n-form>

      <template #footer>
        <n-space justify="end">
          <n-button @click="showCreateProfile = false">Cancel</n-button>
          <n-button type="primary" @click="saveProfile" :loading="saving">
            {{ editingProfile ? 'Update' : 'Create' }}
          </n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- Import Profile Modal -->
    <n-modal v-model:show="showImportProfile" preset="card" title="Import Profile" style="width: 500px">
      <n-upload
        accept=".tar.gz"
        :max="1"
        @change="handleImportFile"
        @before-upload="handleImportBefore"
      >
        <n-button>Select Archive</n-button>
      </n-upload>
      <n-text depth="3" style="margin-top: 12px">
        Select a .tar.gz archive exported from another profile.
      </n-text>
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
  NEmpty,
  NDescriptions,
  NDescriptionsItem,
  NDivider,
  NModal,
  NForm,
  NFormItem,
  NInput,
  NSelect,
  NCheckbox,
  NCheckboxGroup,
  NUpload,
  useMessage,
} from 'naive-ui'
import {
  Add,
  Profile,
  SwapHorizontal,
  Create,
  Copy,
  CloudDownload,
  CloudUpload,
  Trash,
} from '@vicons/ionicons5'

interface ProfileData {
  id: string
  name: string
  description?: string
  provider?: string
  model?: string
  enabledToolsets: string[]
  active: boolean
  createdAt: number
}

const profiles = ref<ProfileData[]>([])
const selectedProfile = ref<ProfileData | null>(null)
const showCreateProfile = ref(false)
const showImportProfile = ref(false)
const editingProfile = ref<ProfileData | null>(null)
const switching = ref(false)
const saving = ref(false)

const message = useMessage()

const profileForm = reactive({
  name: '',
  description: '',
  provider: null as string | null,
  model: null as string | null,
  enabledToolsets: [] as string[],
})

const availableToolsets = [
  'web',
  'terminal',
  'file',
  'browser',
  'memory',
  'todo',
  'session',
  'skills',
  'cron',
  'delegation',
  'code_execution',
  'homeassistant',
  'utility',
  'mcp',
]

const providerOptions = [
  { label: 'OpenAI', value: 'openai' },
  { label: 'DeepSeek', value: 'deepseek' },
  { label: 'Anthropic', value: 'anthropic' },
  { label: 'Google', value: 'google' },
  { label: 'Azure OpenAI', value: 'azure' },
  { label: 'OpenRouter', value: 'openrouter' },
]

const modelOptions = ref<{ label: string; value: string }[]>([])

function formatTime(timestamp: number): string {
  return new Date(timestamp).toLocaleString()
}

function selectProfile(profile: ProfileData) {
  selectedProfile.value = profile
}

async function loadProfiles() {
  try {
    const res = await fetch('/api/profiles')
    if (res.ok) {
      profiles.value = await res.json()
      if (profiles.value.length > 0 && !selectedProfile.value) {
        selectedProfile.value = profiles.value.find((p) => p.active) || profiles.value[0]
      }
    }
  } catch (e) {
    console.error('Failed to load profiles:', e)
  }
}

function editProfile() {
  if (!selectedProfile.value) return
  editingProfile.value = selectedProfile.value
  Object.assign(profileForm, {
    name: selectedProfile.value.name,
    description: selectedProfile.value.description || '',
    provider: selectedProfile.value.provider || null,
    model: selectedProfile.value.model || null,
    enabledToolsets: [...selectedProfile.value.enabledToolsets],
  })
  showCreateProfile.value = true
}

function duplicateProfile() {
  if (!selectedProfile.value) return
  editingProfile.value = null
  Object.assign(profileForm, {
    name: `${selectedProfile.value.name} (Copy)`,
    description: selectedProfile.value.description || '',
    provider: selectedProfile.value.provider || null,
    model: selectedProfile.value.model || null,
    enabledToolsets: [...selectedProfile.value.enabledToolsets],
  })
  showCreateProfile.value = true
}

async function saveProfile() {
  if (!profileForm.name) return
  saving.value = true

  try {
    const method = editingProfile.value ? 'PUT' : 'POST'
    const url = editingProfile.value ? `/api/profiles/${editingProfile.value.id}` : '/api/profiles'

    const res = await fetch(url, {
      method,
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(profileForm),
    })

    if (res.ok) {
      showCreateProfile.value = false
      editingProfile.value = null
      resetForm()
      loadProfiles()
      message.success(editingProfile.value ? 'Profile updated' : 'Profile created')
    }
  } catch (e) {
    console.error('Failed to save profile:', e)
  } finally {
    saving.value = false
  }
}

async function switchToProfile() {
  if (!selectedProfile.value) return
  switching.value = true

  try {
    const res = await fetch(`/api/profiles/${selectedProfile.value.id}/switch`, {
      method: 'POST',
    })

    if (res.ok) {
      profiles.value.forEach((p) => (p.active = p.id === selectedProfile.value?.id))
      message.success(`Switched to ${selectedProfile.value.name}`)
    }
  } catch (e) {
    console.error('Failed to switch profile:', e)
  } finally {
    switching.value = false
  }
}

async function deleteProfile() {
  if (!selectedProfile.value || profiles.value.length <= 1) return
  if (!confirm(`Delete profile "${selectedProfile.value.name}"?`)) return

  try {
    await fetch(`/api/profiles/${selectedProfile.value.id}`, {
      method: 'DELETE',
    })
    selectedProfile.value = null
    loadProfiles()
    message.success('Profile deleted')
  } catch (e) {
    console.error('Failed to delete profile:', e)
  }
}

async function exportProfile() {
  if (!selectedProfile.value) return

  try {
    const res = await fetch(`/api/profiles/${selectedProfile.value.id}/export`)
    if (res.ok) {
      const blob = await res.blob()
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = `${selectedProfile.value.name}.tar.gz`
      a.click()
      URL.revokeObjectURL(url)
    }
  } catch (e) {
    console.error('Failed to export profile:', e)
  }
}

function handleImportFile(options: any) {
  // Handle import logic
}

function handleImportBefore(options: any) {
  return true
}

function resetForm() {
  profileForm.name = ''
  profileForm.description = ''
  profileForm.provider = null
  profileForm.model = null
  profileForm.enabledToolsets = []
}

onMounted(() => {
  loadProfiles()
})
</script>

<style lang="scss" scoped>
.profiles-view {
  padding: 16px;
  height: calc(100vh - 84px);
}

.profiles-card {
  height: 100%;
}

.n-list-item {
  &.active {
    background: var(--selected-color, #e8f5e9);
  }
}
</style>
