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
                <n-button size="small" @click="openProfileDetail(profile.name)">{{ t('profiles.edit') }}</n-button>
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

    <!-- Profile Detail Modal with Tabs -->
    <n-modal
      v-model:show="showDetailModal"
      preset="dialog"
      :title="`${t('profiles.edit')} - ${editingProfile}`"
      style="width: 700px; max-width: 96vw;"
    >
      <n-tabs type="line" animated>
        <!-- Environment Tab -->
        <n-tab-pane :name="t('profiles.tabs.env')" :tab="t('profiles.tabs.env')">
          <n-space vertical>
            <n-alert type="info" :title="t('profiles.envInfo')">
              {{ t('profiles.envInfoDesc') }}
            </n-alert>
            <n-button @click="editSoul(editingProfile)">{{ t('profiles.editSoul') }}</n-button>
          </n-space>
        </n-tab-pane>

        <!-- User Profile Tab -->
        <n-tab-pane :name="t('profiles.tabs.userProfile')" :tab="t('profiles.tabs.userProfile')">
          <n-spin v-if="loadingUser" />
          <n-form v-else label-placement="left" label-width="auto">
            <n-form-item :label="t('profiles.user.name')">
              <n-input v-model:value="userData.name" :placeholder="t('profiles.user.namePlaceholder')" />
            </n-form-item>
            <n-form-item :label="t('profiles.user.role')">
              <n-input v-model:value="userData.role" :placeholder="t('profiles.user.rolePlaceholder')" />
            </n-form-item>
            <n-form-item :label="t('profiles.user.communicationStyle')">
              <n-select
                v-model:value="userData.communication_style"
                :options="communicationStyleOptions"
                :placeholder="t('profiles.user.selectStyle')"
              />
            </n-form-item>
            <n-form-item :label="t('profiles.user.codeStyle')">
              <n-select
                v-model:value="userData.code_style"
                :options="codeStyleOptions"
                :placeholder="t('profiles.user.selectStyle')"
              />
            </n-form-item>
            <n-form-item :label="t('profiles.user.techStack')">
              <n-dynamic-tags v-model:value="userData.tech_stack" />
            </n-form-item>
            <n-form-item :label="t('profiles.user.interests')">
              <n-dynamic-tags v-model:value="userData.interests" />
            </n-form-item>
            <n-space justify="end">
              <n-button :loading="savingUser" type="primary" @click="saveUserProfile">
                {{ t('common.save') }}
              </n-button>
            </n-space>
          </n-form>
        </n-tab-pane>

        <!-- AI Soul Tab -->
        <n-tab-pane :name="t('profiles.tabs.soul')" :tab="t('profiles.tabs.soul')">
          <n-space vertical>
            <n-alert type="info">{{ t('profiles.soulDesc') }}</n-alert>
            <n-input
              v-model:value="soulContent"
              type="textarea"
              :rows="12"
              :placeholder="t('profiles.soulPlaceholder')"
            />
            <n-space justify="end">
              <n-button :loading="savingSoul" type="primary" @click="saveSoulFromDetail">
                {{ t('common.save') }}
              </n-button>
            </n-space>
          </n-space>
        </n-tab-pane>

        <!-- Learned Preferences Tab -->
        <n-tab-pane :name="t('profiles.tabs.preferences')" :tab="t('profiles.tabs.preferences')">
          <n-spin v-if="loadingPreferences" />
          <template v-else>
            <n-alert type="info" :title="t('profiles.preferencesInfo')">
              {{ t('profiles.preferencesInfoDesc') }}
            </n-alert>
            <n-list v-if="preferences.length > 0" bordered>
              <n-list-item v-for="pref in preferences" :key="pref.key">
                <n-thing>
                  <template #header>
                    <n-space align="center">
                      <n-tag v-if="pref.confidence >= 0.8" type="success">✓</n-tag>
                      <n-tag v-else-if="pref.confidence >= 0.5" type="warning">~</n-tag>
                      <n-tag v-else type="error">?</n-tag>
                      <span>{{ formatPreferenceKey(pref.key) }}: {{ pref.value }}</span>
                    </n-space>
                  </template>
                  <template #description>
                    <n-space vertical size="small">
                      <n-text depth="3">{{ t('profiles.preferences.confidence') }}: {{ Math.round(pref.confidence * 100) }}%</n-text>
                      <n-text depth="3">{{ t('profiles.preferences.source') }}: {{ pref.context }}</n-text>
                    </n-space>
                  </template>
                  <template #action>
                    <n-space>
                      <n-button size="small" @click="feedbackPreference(pref.key, true)">
                        {{ t('profiles.preferences.accurate') }}
                      </n-button>
                      <n-button size="small" type="error" @click="feedbackPreference(pref.key, false)">
                        {{ t('profiles.preferences.inaccurate') }}
                      </n-button>
                    </n-space>
                  </template>
                </n-thing>
              </n-list-item>
            </n-list>
            <n-empty v-else :description="t('profiles.preferences.empty')" />
          </template>
        </n-tab-pane>
      </n-tabs>
    </n-modal>

    <!-- Soul Editor Modal (legacy, for backward compatibility) -->
    <n-modal v-model:show="showSoulModal" preset="dialog" class="modal-responsive" :title="`${t('profiles.soul')} - ${editingProfile}`" style="width: 620px; max-width: 96vw;">
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
import { ref, onMounted, computed } from 'vue'
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

interface UserData {
  name: string
  role: string
  communication_style: string
  code_style: string
  tech_stack: string[]
  interests: string[]
}

interface Preference {
  key: string
  value: string
  context: string
  confidence: number
  source: string
}

const message = useMessage()
const configStore = useConfigStore()
const chatStore = useChatStore()
const profiles = ref<Profile[]>([])
const loading = ref(false)
const creating = ref(false)
const savingSoul = ref(false)
const savingUser = ref(false)
const showCreateModal = ref(false)
const showSoulModal = ref(false)
const showDetailModal = ref(false)
const newProfileName = ref('')
const cloneFromDefault = ref(true)
const editingProfile = ref('')
const soulContent = ref('')

// User profile data
const loadingUser = ref(false)
const userData = ref<UserData>({
  name: '',
  role: '',
  communication_style: '',
  code_style: '',
  tech_stack: [],
  interests: []
})

// Preferences data
const loadingPreferences = ref(false)
const preferences = ref<Preference[]>([])

const communicationStyleOptions = computed(() => [
  { label: t('profiles.user.styles.concise'), value: 'concise' },
  { label: t('profiles.user.styles.detailed'), value: 'detailed' },
  { label: t('profiles.user.styles.technical'), value: 'technical' },
  { label: t('profiles.user.styles.casual'), value: 'casual' }
])

const codeStyleOptions = computed(() => [
  { label: t('profiles.user.styles.clean'), value: 'clean' },
  { label: t('profiles.user.styles.documented'), value: 'documented' },
  { label: t('profiles.user.styles.efficient'), value: 'efficient' },
  { label: t('profiles.user.styles.verbose'), value: 'verbose' }
])

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
    // 后 3 个无依赖可并行
    await Promise.all([
      configStore.loadConfig(),
      chatStore.loadSessions(),
      loadProfiles(),
    ])
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

// Open profile detail modal with tabs
async function openProfileDetail(name: string): Promise<void> {
  editingProfile.value = name
  showDetailModal.value = true

  // Load all data
  const results = await Promise.allSettled([
    loadUserProfile(name),
    loadSoul(name),
    loadPreferences(name)
  ])
  results.forEach((r, i) => {
    if (r.status === 'rejected') {
      console.error(`openProfileDetail [${i}] failed:`, r.reason)
    }
  })
}

async function loadUserProfile(name: string): Promise<void> {
  loadingUser.value = true
  try {
    const res = await request<{ content: string; exists: boolean; data: UserData }>(`/profiles/${name}/user`)
    if (res.data) {
      userData.value = {
        name: res.data.name || '',
        role: res.data.role || '',
        communication_style: res.data.communication_style || '',
        code_style: res.data.code_style || '',
        tech_stack: res.data.tech_stack || [],
        interests: res.data.interests || []
      }
    }
  } catch (e) {
    message.error(t('profiles.failedToLoadUser'))
  } finally {
    loadingUser.value = false
  }
}

async function saveUserProfile(): Promise<void> {
  savingUser.value = true
  try {
    await request(`/profiles/${editingProfile.value}/user`, {
      method: 'PUT',
      body: JSON.stringify({ data: userData.value }),
    })
    message.success(t('profiles.saved'))
  } catch (e) {
    message.error(t('profiles.failedToSaveUser'))
  } finally {
    savingUser.value = false
  }
}

async function loadSoul(name: string): Promise<void> {
  try {
    const res = await request<{ content: string; exists: boolean }>(`/profiles/${name}/soul`)
    soulContent.value = res.content || ''
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

async function saveSoulFromDetail(): Promise<void> {
  savingSoul.value = true
  try {
    await request(`/profiles/${editingProfile.value}/soul`, {
      method: 'PUT',
      body: JSON.stringify({ content: soulContent.value }),
    })
    message.success(t('profiles.saved'))
  } catch (e) {
    message.error(t('profiles.failedToSaveSoul'))
  } finally {
    savingSoul.value = false
  }
}

async function loadPreferences(name: string): Promise<void> {
  loadingPreferences.value = true
  try {
    const res = await request<{ preferences: Preference[] }>(`/profiles/${name}/preferences`)
    preferences.value = res.preferences || []
  } catch (e) {
    message.error(t('profiles.failedToLoadPreferences'))
  } finally {
    loadingPreferences.value = false
  }
}

async function feedbackPreference(key: string, accurate: boolean): Promise<void> {
  try {
    await request(`/profiles/${editingProfile.value}/preferences/${key}/feedback`, {
      method: 'POST',
      body: JSON.stringify({ accurate }),
    })
    message.success(accurate ? t('profiles.preferences.thanksAccurate') : t('profiles.preferences.thanksInaccurate'))
    // Reload preferences
    await loadPreferences(editingProfile.value)
  } catch (e) {
    message.error(t('profiles.failedToFeedback'))
  }
}

function formatPreferenceKey(key: string): string {
  // Convert snake_case to readable text
  return key.replace(/_/g, ' ').replace(/\b\w/g, l => l.toUpperCase())
}

// Legacy edit soul function (for backward compatibility)
async function editSoul(name: string): Promise<void> {
  editingProfile.value = name
  await loadSoul(name)
  showSoulModal.value = true
}

onMounted(() => {
  loadProfiles()
})
</script>
