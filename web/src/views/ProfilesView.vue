<template>
  <div class="profiles-container">
    <!-- 页头：标题 + 一句话说明 + 刷新 / 新建 -->
    <div class="page-head">
      <div class="page-head-text">
        <h2 class="page-title">{{ t('profiles.title') }}</h2>
        <p class="page-sub">{{ t('profiles.subtitle') }}</p>
      </div>
      <n-space :size="8" align="center">
        <n-button secondary :loading="refreshing" :disabled="refreshing" @click="handleRefresh">
          <template #icon><n-icon><RefreshOutline /></n-icon></template>
          {{ t('common.refresh') }}
        </n-button>
        <n-button type="primary" @click="openCreate">
          <template #icon><n-icon><AddOutline /></n-icon></template>
          {{ t('profiles.newProfile') }}
        </n-button>
      </n-space>
    </div>

    <n-spin :show="loading">
      <n-grid
        v-if="profiles.length > 0"
        responsive="screen"
        cols="1 s:2 m:2 l:3 xl:4"
        :x-gap="14"
        :y-gap="14"
      >
        <n-gi v-for="profile in profiles" :key="profile.name">
          <n-card
            size="small"
            class="profile-card"
            :class="{ 'is-active': profile.is_applied }"
            :segmented="{ footer: true }"
          >
            <div class="pf-top">
              <div class="pf-avatar" :class="{ 'is-active': profile.is_applied }">
                {{ initialOf(profile.name) }}
              </div>
              <div class="pf-ident">
                <div class="pf-name">
                  <span class="pf-name-text" :title="profile.name">{{ profile.name }}</span>
                  <n-tag v-if="profile.is_default" size="tiny" :bordered="false">
                    {{ t('profiles.builtin') }}
                  </n-tag>
                  <n-tag v-if="profile.is_applied" size="tiny" type="success" :bordered="false">
                    {{ t('profiles.inUse') }}
                  </n-tag>
                </div>
                <div class="pf-desc" :title="describeProfile(profile)">{{ describeProfile(profile) }}</div>
              </div>
            </div>

            <div class="pf-facts">
              <span class="fact" :class="{ muted: !profile.has_soul }">
                <n-icon :size="14"><SparklesOutline /></n-icon>
                {{ profile.has_soul ? t('profiles.soulSet') : t('profiles.soulMissing') }}
              </span>
              <span class="fact" :class="{ muted: !profile.has_user }">
                <n-icon :size="14"><PersonOutline /></n-icon>
                {{ profile.has_user ? t('profiles.tabs.userProfile') : t('profiles.userProfileMissing') }}
              </span>
            </div>

            <div class="pf-path" :title="profile.path">{{ profile.path }}</div>

            <template #footer>
              <div class="pf-footer">
                <n-space :size="6">
                  <n-button
                    v-if="!profile.is_default && !profile.is_applied"
                    size="tiny"
                    type="primary"
                    :loading="applyingName === profile.name"
                    :disabled="!!applyingName && applyingName !== profile.name"
                    @click="applyProfile(profile.name)"
                  >
                    {{ t('profiles.apply') }}
                  </n-button>
                  <n-button size="tiny" secondary @click="openProfileDetail(profile)">
                    {{ t('profiles.manage') }}
                  </n-button>
                </n-space>
                <n-dropdown
                  trigger="click"
                  placement="bottom-end"
                  :options="menuOptions(profile)"
                  @select="(key: string | number) => handleMenu(String(key), profile)"
                >
                  <n-button size="tiny" quaternary circle>
                    <template #icon><n-icon><EllipsisHorizontal /></n-icon></template>
                  </n-button>
                </n-dropdown>
              </div>
            </template>
          </n-card>
        </n-gi>
      </n-grid>

      <n-empty v-else-if="!loading" class="empty-block" :description="t('profiles.empty')">
        <template #extra>
          <n-button size="small" type="primary" @click="openCreate">
            {{ t('profiles.newProfile') }}
          </n-button>
        </template>
      </n-empty>
    </n-spin>

    <!-- 新建预设 -->
    <n-modal
      v-model:show="showCreateModal"
      preset="card"
      class="form-modal"
      :title="t('profiles.newProfile')"
      :style="{ width: '460px', maxWidth: '96vw' }"
      :bordered="false"
    >
      <n-form label-placement="top">
        <n-form-item :label="t('profiles.profileName')" :show-feedback="false">
          <n-input
            v-model:value="newProfileName"
            :placeholder="t('profiles.profileNamePlaceholder')"
            :status="newProfileName && !isValidProfileName(newProfileName.trim()) ? 'error' : undefined"
            @keyup.enter="createProfile"
          />
        </n-form-item>
        <div class="field-hint" :class="{ 'is-error': !!newProfileName && !isValidProfileName(newProfileName.trim()) }">
          {{ t('profiles.nameRule') }}
        </div>
        <n-form-item :label="t('profiles.cloneFromDefault')" class="switch-field">
          <n-switch v-model:value="cloneFromDefault" />
        </n-form-item>
        <div class="field-hint">{{ t('profiles.cloneHint') }}</div>
      </n-form>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showCreateModal = false">{{ t('common.cancel') }}</n-button>
          <n-button type="primary" :loading="creating" @click="createProfile">{{ t('common.create') }}</n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- 重命名 -->
    <n-modal
      v-model:show="showRenameModal"
      preset="card"
      class="form-modal"
      :title="t('profiles.renameTitle')"
      :style="{ width: '460px', maxWidth: '96vw' }"
      :bordered="false"
    >
      <n-form label-placement="top">
        <n-form-item :label="t('profiles.newName')" :show-feedback="false">
          <n-input
            v-model:value="renameTarget"
            :status="renameTarget && !isValidProfileName(renameTarget.trim()) ? 'error' : undefined"
            @keyup.enter="renameProfile"
          />
        </n-form-item>
        <div class="field-hint" :class="{ 'is-error': !!renameTarget && !isValidProfileName(renameTarget.trim()) }">
          {{ t('profiles.renameHint') }}
        </div>
      </n-form>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showRenameModal = false">{{ t('common.cancel') }}</n-button>
          <n-button type="primary" :loading="renaming" @click="renameProfile">{{ t('common.save') }}</n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- 预设详情：概览 / 用户画像 / AI 人格 / 学习偏好 -->
    <n-modal
      :show="showDetailModal"
      preset="card"
      class="detail-modal"
      :bordered="false"
      :closable="false"
      :mask-closable="false"
      :close-on-esc="false"
      :segmented="{ content: true, footer: true }"
      :style="{ width: '820px', maxWidth: '96vw' }"
    >
      <template #header>
        <div class="detail-head">
          <div class="pf-avatar" :class="{ 'is-active': detailProfile?.is_applied }">
            {{ initialOf(editingProfile) }}
          </div>
          <div class="detail-ident">
            <div class="detail-title">
              <span>{{ editingProfile }}</span>
              <n-tag v-if="detailProfile?.is_default" size="tiny" :bordered="false">
                {{ t('profiles.builtin') }}
              </n-tag>
              <n-tag v-if="detailProfile?.is_applied" size="tiny" type="success" :bordered="false">
                {{ t('profiles.inUse') }}
              </n-tag>
            </div>
            <div class="detail-sub">{{ describeProfile(detailProfile) }}</div>
          </div>
          <n-button class="detail-close" quaternary circle size="small" @click="closeDetail">
            <template #icon><n-icon><CloseOutline /></n-icon></template>
          </n-button>
        </div>
      </template>

      <n-tabs v-model:value="activeTab" type="line" animated>
        <!-- 概览：把"这个预设落在磁盘哪里、缺什么、是否已应用"讲清楚 -->
        <n-tab-pane name="overview" :tab="t('profiles.tabs.overview')">
          <n-descriptions :column="1" label-placement="left" size="small" bordered class="overview-desc">
            <n-descriptions-item :label="t('profiles.overview.dir')">
              <div class="kv">
                <code class="kv-value">{{ detailProfile?.path || '-' }}</code>
                <n-button v-if="detailProfile?.path" text size="tiny" type="primary" @click="copyText(detailProfile?.path || '', t('profiles.overview.pathCopied'))">
                  {{ t('profiles.overview.copyPath') }}
                </n-button>
              </div>
            </n-descriptions-item>
            <n-descriptions-item :label="t('profiles.overview.skeleton')">
              <n-space :size="6">
                <n-tag size="tiny" :type="detailProfile?.has_soul ? 'success' : 'warning'" :bordered="false">
                  SOUL.md · {{ detailProfile?.has_soul ? t('profiles.overview.present') : t('profiles.overview.absent') }}
                </n-tag>
                <n-tag size="tiny" :type="detailProfile?.has_user ? 'success' : 'warning'" :bordered="false">
                  USER.json · {{ detailProfile?.has_user ? t('profiles.overview.present') : t('profiles.overview.absent') }}
                </n-tag>
              </n-space>
            </n-descriptions-item>
            <n-descriptions-item :label="t('profiles.overview.cli')">
              <div class="kv">
                <code class="kv-value">{{ setupCommand || '-' }}</code>
                <n-button v-if="setupCommand" text size="tiny" type="primary" @click="copyText(setupCommand, t('profiles.commandCopied'))">
                  {{ t('profiles.overview.copyCommand') }}
                </n-button>
              </div>
            </n-descriptions-item>
          </n-descriptions>
          <n-alert type="info" :bordered="false" class="overview-tip">
            {{ t('profiles.overview.syncHint') }}
          </n-alert>
          <n-alert
            v-if="detailProfile && !detailProfile.is_default"
            type="warning"
            :bordered="false"
            class="overview-tip"
          >
            {{ t('profiles.templateHint') }}
          </n-alert>
          <n-alert v-else-if="detailProfile" type="success" :bordered="false" class="overview-tip">
            {{ t('profiles.liveHint') }}
          </n-alert>
        </n-tab-pane>

        <!-- 用户画像 -->
        <n-tab-pane name="user" :tab="t('profiles.tabs.userProfile')">
          <n-spin :show="loadingUser">
            <n-form class="user-profile-form" label-placement="left" label-width="96" :show-feedback="false">
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
                  clearable
                />
              </n-form-item>
              <n-form-item :label="t('profiles.user.codeStyle')">
                <n-select
                  v-model:value="userData.code_style"
                  :options="codeStyleOptions"
                  :placeholder="t('profiles.user.selectStyle')"
                  clearable
                />
              </n-form-item>
              <n-form-item :label="t('profiles.user.techStack')">
                <n-dynamic-tags v-model:value="userData.tech_stack" />
              </n-form-item>
              <n-form-item :label="t('profiles.user.interests')">
                <n-dynamic-tags v-model:value="userData.interests" />
              </n-form-item>
            </n-form>
            <n-text depth="3" class="tab-footnote">{{ t('profiles.user.saveHint') }}</n-text>
          </n-spin>
        </n-tab-pane>

        <!-- AI 灵魂 -->
        <n-tab-pane name="soul" :tab="t('profiles.tabs.soul')">
          <n-space vertical :size="10">
            <n-alert type="info" :bordered="false">{{ t('profiles.soulDesc') }}</n-alert>
            <n-input
              v-model:value="soulContent"
              type="textarea"
              :rows="16"
              :placeholder="t('profiles.soulPlaceholder')"
              class="soul-input"
            />
            <div class="soul-meta">
              <span>{{ t('profiles.charCount', { count: soulContent.length }) }}</span>
              <span v-if="soulDirty" class="dirty-badge">{{ t('profiles.unsaved') }}</span>
            </div>
          </n-space>
        </n-tab-pane>

        <!-- 学习偏好 -->
        <n-tab-pane name="preferences" :tab="t('profiles.tabs.preferences')">
          <n-spin :show="loadingPreferences">
            <n-alert type="info" :bordered="false" class="overview-tip">{{ t('profiles.preferencesInfoDesc') }}</n-alert>
            <n-list v-if="sortedPreferences.length > 0" bordered class="pref-list">
              <n-list-item v-for="pref in sortedPreferences" :key="pref.key">
                <div class="pref-row">
                  <div class="pref-main">
                    <div class="pref-key">{{ humanizeKey(pref.key) }}</div>
                    <div class="pref-value">{{ pref.value }}</div>
                    <div class="pref-meta">
                      <n-progress
                        type="line"
                        :percentage="Math.round(pref.confidence * 100)"
                        :height="4"
                        :show-indicator="false"
                        :status="confidenceStatus(pref.confidence)"
                        class="pref-bar"
                      />
                      <span>{{ t('profiles.preferences.confidence') }} {{ Math.round(pref.confidence * 100) }}%</span>
                      <span v-if="pref.context" class="pref-ctx" :title="pref.context">· {{ pref.context }}</span>
                    </div>
                  </div>
                  <n-space :size="6">
                    <n-button size="tiny" secondary type="success" @click="feedbackPreference(pref.key, true)">
                      {{ t('profiles.preferences.accurate') }}
                    </n-button>
                    <n-button size="tiny" secondary type="error" @click="feedbackPreference(pref.key, false)">
                      {{ t('profiles.preferences.inaccurate') }}
                    </n-button>
                  </n-space>
                </div>
              </n-list-item>
            </n-list>
            <n-empty v-else :description="t('profiles.preferences.empty')" class="empty-inline" />
          </n-spin>
        </n-tab-pane>
      </n-tabs>

      <template #footer>
        <div class="detail-footer">
          <span v-if="dirty" class="dirty-badge">{{ t('profiles.unsavedBadge') }}</span>
          <span v-else />
          <n-space :size="8">
            <n-button @click="closeDetail">{{ t('common.close') }}</n-button>
            <n-button
              v-if="activeTab === 'user'"
              type="primary"
              :loading="savingUser"
              :disabled="!userDirty"
              @click="saveUserProfile"
            >
              {{ t('common.save') }}
            </n-button>
            <n-button
              v-if="activeTab === 'soul'"
              type="primary"
              :loading="savingSoul"
              :disabled="!soulDirty"
              @click="saveSoul"
            >
              {{ t('common.save') }}
            </n-button>
            <n-button
              v-if="activeTab === 'overview' && detailProfile && !detailProfile.is_default && !detailProfile.is_applied"
              type="primary"
              :loading="applyingName === editingProfile"
              @click="applyProfile(editingProfile)"
            >
              {{ t('profiles.apply') }}
            </n-button>
          </n-space>
        </div>
      </template>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, h } from 'vue'
import {
  NAlert,
  NButton,
  NCard,
  NDescriptions,
  NDescriptionsItem,
  NDropdown,
  NDynamicTags,
  NEmpty,
  NForm,
  NFormItem,
  NGi,
  NGrid,
  NIcon,
  NInput,
  NList,
  NListItem,
  NModal,
  NProgress,
  NSelect,
  NSpace,
  NSpin,
  NSwitch,
  NTabPane,
  NTabs,
  NTag,
  NText,
  useDialog,
  useMessage,
} from 'naive-ui'
import type { DropdownOption } from 'naive-ui'
import {
  AddOutline,
  CloseOutline,
  CreateOutline,
  EllipsisHorizontal,
  PersonOutline,
  RefreshOutline,
  SparklesOutline,
  TerminalOutline,
  TrashOutline,
} from '@vicons/ionicons5'
import { useI18n } from 'vue-i18n'
import { request } from '@/api/client'
import { useConfigStore } from '@/stores/config'

interface Profile {
  name: string
  path: string
  is_default: boolean
  // 当前活动人格就是这套预设（应用后内容已抄进 cortex）
  is_applied: boolean
  has_soul: boolean
  has_user: boolean
  user_name: string
  user_role: string
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

const { t } = useI18n()
const message = useMessage()
const dialog = useDialog()
const configStore = useConfigStore()

const profiles = ref<Profile[]>([])
const loading = ref(false)
const refreshing = ref(false)
const creating = ref(false)
const renaming = ref(false)
const applyingName = ref('')

const showCreateModal = ref(false)
const showRenameModal = ref(false)
const showDetailModal = ref(false)
const newProfileName = ref('')
const cloneFromDefault = ref(true)
const renameSource = ref('')
const renameTarget = ref('')

const editingProfile = ref('')
const detailProfile = computed(() => profiles.value.find(p => p.name === editingProfile.value))
const activeTab = ref<'overview' | 'user' | 'soul' | 'preferences'>('overview')
const setupCommand = ref('')

const loadingUser = ref(false)
const savingUser = ref(false)
const userData = ref<UserData>(emptyUserData())
// 快照必须是响应式的：保存后只改 snapshot，userData 本身没变，
// 非响应式的变量不会让 userDirty 重算，按钮会一直停在"未保存"。
const userSnapshot = ref('')

const soulContent = ref('')
const savingSoul = ref(false)
const soulSnapshot = ref('')

const loadingPreferences = ref(false)
const preferences = ref<Preference[]>([])

const userDirty = computed(() => JSON.stringify(userData.value) !== userSnapshot.value)
const soulDirty = computed(() => soulContent.value !== soulSnapshot.value)
const dirty = computed(() => userDirty.value || soulDirty.value)

const sortedPreferences = computed(() =>
  [...preferences.value].sort((a, b) => b.confidence - a.confidence)
)

const communicationStyleOptions = computed(() => [
  { label: t('profiles.user.styles.concise'), value: 'concise' },
  { label: t('profiles.user.styles.detailed'), value: 'detailed' },
  { label: t('profiles.user.styles.technical'), value: 'technical' },
  { label: t('profiles.user.styles.casual'), value: 'casual' },
])

const codeStyleOptions = computed(() => [
  { label: t('profiles.user.styles.clean'), value: 'clean' },
  { label: t('profiles.user.styles.documented'), value: 'documented' },
  { label: t('profiles.user.styles.efficient'), value: 'efficient' },
  { label: t('profiles.user.styles.verbose'), value: 'verbose' },
])

function emptyUserData(): UserData {
  return {
    name: '',
    role: '',
    communication_style: '',
    code_style: '',
    tech_stack: [],
    interests: [],
  }
}

function initialOf(name: string): string {
  if (!name) return '?'
  // 取首个字符即可：中英文都成立，比取首字母更稳（中文名没有"首字母"概念）。
  return name.trim().charAt(0).toUpperCase()
}

// 卡片副标题：有画像就显示"姓名 · 角色"，没有就明确说没填，而不是留空。
function describeProfile(profile?: Profile): string {
  if (!profile) return ''
  const name = (profile.user_name || '').trim()
  const role = (profile.user_role || '').trim()
  if (name && role) return `${name} · ${role}`
  if (name) return name
  if (role) return role
  return t('profiles.userProfileMissing')
}

// 与后端 appconfig.ValidProfileName 对齐：单一路径段，不含空格/分隔符。
const PROFILE_NAME_RE = /^[\p{L}\p{N}._-]{1,64}$/u

function isValidProfileName(name: string): boolean {
  return PROFILE_NAME_RE.test(name)
    && name !== '.' && name !== '..'
    && !/^(con|prn|aux|nul|com[1-9]|lpt[1-9])$/i.test(name)
}

function humanizeKey(key: string): string {
  // 后端偏好键是 "Communication style" 这种空格分词，也兼容历史 snake_case。
  const spaced = key.replace(/_/g, ' ').trim()
  return spaced.charAt(0).toUpperCase() + spaced.slice(1)
}

function confidenceStatus(confidence: number): 'success' | 'warning' | 'error' {
  if (confidence >= 0.8) return 'success'
  if (confidence >= 0.5) return 'warning'
  return 'error'
}

async function copyText(text: string, okMessage: string): Promise<void> {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text)
    } else {
      // 非安全上下文（http + 非 localhost）没有 clipboard API，退回 textarea。
      const el = document.createElement('textarea')
      el.value = text
      el.style.position = 'fixed'
      el.style.opacity = '0'
      document.body.appendChild(el)
      el.select()
      document.execCommand('copy')
      document.body.removeChild(el)
    }
    message.success(okMessage)
  } catch {
    message.error(t('common.copyFailed'))
  }
}

async function loadProfiles(): Promise<void> {
  loading.value = true
  try {
    const res = await request<{ profiles: Profile[] }>('/profiles')
    profiles.value = res.profiles || []
  } catch {
    message.error(t('profiles.failedToLoad'))
  } finally {
    loading.value = false
  }
}

function openCreate(): void {
  newProfileName.value = ''
  cloneFromDefault.value = true
  showCreateModal.value = true
}

async function createProfile(): Promise<void> {
  const name = newProfileName.value.trim()
  if (!name) {
    message.warning(t('profiles.pleaseEnterName'))
    return
  }
  if (!isValidProfileName(name)) {
    message.warning(t('profiles.invalidName'))
    return
  }
  creating.value = true
  try {
    await request('/profiles', {
      method: 'POST',
      body: JSON.stringify({ name, clone_from_default: cloneFromDefault.value }),
    })
    message.success(t('profiles.created', { name }))
    showCreateModal.value = false
    await loadProfiles()
  } catch (e: unknown) {
    message.error(errorText(e) || t('profiles.failedToCreate'))
  } finally {
    creating.value = false
  }
}

async function applyProfile(name: string): Promise<void> {
  if (applyingName.value) return
  applyingName.value = name
  try {
    // 应用 = 把人设与画像写进活动 cortex，立即生效（没有"待重启"这一说）。
    await request(`/profiles/${name}/apply`, { method: 'POST' })
    message.success(t('profiles.appliedMsg', { name }))
    await Promise.all([configStore.loadConfig(), loadProfiles()])
  } catch (e: unknown) {
    message.error(errorText(e) || t('profiles.failedToApply'))
  } finally {
    applyingName.value = ''
  }
}

function deleteProfile(profile: Profile): void {
  dialog.warning({
    title: t('profiles.deleteProfile'),
    content: t('profiles.deleteConfirm', { name: profile.name }),
    positiveText: t('common.delete'),
    negativeText: t('common.cancel'),
    onPositiveClick: async () => {
      try {
        await request(`/profiles/${profile.name}`, { method: 'DELETE' })
        message.success(t('profiles.deleted', { name: profile.name }))
        if (editingProfile.value === profile.name) showDetailModal.value = false
        await loadProfiles()
      } catch (e: unknown) {
        message.error(errorText(e) || t('profiles.failedToDelete'))
      }
    },
  })
}

function openRename(profile: Profile): void {
  renameSource.value = profile.name
  renameTarget.value = profile.name
  showRenameModal.value = true
}

async function renameProfile(): Promise<void> {
  const newName = renameTarget.value.trim()
  if (!isValidProfileName(newName)) {
    message.warning(t('profiles.invalidName'))
    return
  }
  if (newName === renameSource.value) {
    showRenameModal.value = false
    return
  }
  renaming.value = true
  try {
    await request(`/profiles/${renameSource.value}`, {
      method: 'PATCH',
      body: JSON.stringify({ name: renameSource.value, new_name: newName }),
    })
    message.success(t('profiles.renamed', { name: newName }))
    if (editingProfile.value === renameSource.value) editingProfile.value = newName
    showRenameModal.value = false
    await loadProfiles()
  } catch (e: unknown) {
    message.error(errorText(e) || t('profiles.failedToRename'))
  } finally {
    renaming.value = false
  }
}

async function copySetupCommand(name: string): Promise<void> {
  try {
    const res = await request<{ command: string }>(`/profiles/${name}/setup-command`)
    await copyText(res.command, t('profiles.commandCopied'))
  } catch (e: unknown) {
    message.error(errorText(e) || t('profiles.failedToLoadCommand'))
  }
}

function menuOptions(profile: Profile): DropdownOption[] {
  const deleteBlocked = profile.is_default
  return [
    {
      label: t('profiles.rename'),
      key: 'rename',
      disabled: profile.is_default,
      // 内置 default 就是运行目录，改名等于搬走运行时数据，后端也会拒。
      icon: () => h(NIcon, null, { default: () => h(CreateOutline) }),
    },
    {
      label: t('profiles.copyCommand'),
      key: 'command',
      icon: () => h(NIcon, null, { default: () => h(TerminalOutline) }),
    },
    { key: 'd1', type: 'divider' },
    {
      label: t('common.delete'),
      key: 'delete',
      disabled: deleteBlocked,
      // disabled 项没有点击事件，用 title 说明为什么不能点。
      props: deleteBlocked ? { title: t('profiles.deleteBuiltinBlocked') } : undefined,
      icon: () => h(NIcon, null, { default: () => h(TrashOutline) }),
    },
  ]
}

function handleMenu(key: string, profile: Profile): void {
  switch (key) {
    case 'rename':
      openRename(profile)
      break
    case 'command':
      copySetupCommand(profile.name)
      break
    case 'delete':
      deleteProfile(profile)
      break
  }
}

async function openProfileDetail(profile: Profile): Promise<void> {
  editingProfile.value = profile.name
  activeTab.value = 'overview'
  setupCommand.value = ''
  userData.value = emptyUserData()
  userSnapshot.value = JSON.stringify(userData.value)
  soulContent.value = ''
  soulSnapshot.value = ''
  preferences.value = []
  showDetailModal.value = true

  const results = await Promise.allSettled([
    loadUserProfile(profile.name),
    loadSoul(profile.name),
    loadPreferences(profile.name),
    loadSetupCommand(profile.name),
  ])
  results.forEach((r, i) => {
    if (r.status === 'rejected') console.error(`openProfileDetail [${i}] failed:`, r.reason)
  })
}

// 关闭前先看有没有未保存的编辑：有就确认一次，避免点一下关闭就丢掉改动。
function closeDetail(): void {
  if (!dirty.value) {
    showDetailModal.value = false
    return
  }
  dialog.warning({
    title: t('profiles.unsavedTitle'),
    content: t('profiles.unsavedHint'),
    positiveText: t('profiles.discard'),
    negativeText: t('common.cancel'),
    onPositiveClick: () => {
      showDetailModal.value = false
      userSnapshot.value = JSON.stringify(userData.value)
      soulSnapshot.value = soulContent.value
    },
  })
}

async function loadSetupCommand(name: string): Promise<void> {
  const res = await request<{ command: string }>(`/profiles/${name}/setup-command`)
  setupCommand.value = res.command || ''
}

async function loadUserProfile(name: string): Promise<void> {
  loadingUser.value = true
  try {
    const res = await request<{ content: string; exists: boolean; data: UserData }>(`/profiles/${name}/user`)
    const data = res.data
    userData.value = {
      name: data?.name || '',
      role: data?.role || '',
      communication_style: data?.communication_style || '',
      code_style: data?.code_style || '',
      tech_stack: data?.tech_stack || [],
      interests: data?.interests || [],
    }
    userSnapshot.value = JSON.stringify(userData.value)
  } catch (e: unknown) {
    message.error(errorText(e) || t('profiles.failedToLoadUser'))
  } finally {
    loadingUser.value = false
  }
}

async function saveUserProfile(): Promise<void> {
  if (!userDirty.value) return
  savingUser.value = true
  try {
    await request(`/profiles/${editingProfile.value}/user`, {
      method: 'PUT',
      body: JSON.stringify({ data: userData.value }),
    })
    userSnapshot.value = JSON.stringify(userData.value)
    message.success(t('profiles.saved'))
    // 卡片上的"姓名 · 角色"来自 USER.json，存完顺手刷一下列表。
    await loadProfiles()
  } catch (e: unknown) {
    message.error(errorText(e) || t('profiles.failedToSaveUser'))
  } finally {
    savingUser.value = false
  }
}

async function loadSoul(name: string): Promise<void> {
  try {
    const res = await request<{ content: string; exists: boolean }>(`/profiles/${name}/soul`)
    soulContent.value = res.content || ''
    soulSnapshot.value = soulContent.value
  } catch (e: unknown) {
    message.error(errorText(e) || t('profiles.failedToLoadSoul'))
  }
}

async function saveSoul(): Promise<void> {
  if (!soulDirty.value) return
  savingSoul.value = true
  try {
    await request(`/profiles/${editingProfile.value}/soul`, {
      method: 'PUT',
      body: JSON.stringify({ content: soulContent.value }),
    })
    soulSnapshot.value = soulContent.value
    message.success(t('profiles.saved'))
    await loadProfiles()
  } catch (e: unknown) {
    message.error(errorText(e) || t('profiles.failedToSaveSoul'))
  } finally {
    savingSoul.value = false
  }
}

async function loadPreferences(name: string): Promise<void> {
  loadingPreferences.value = true
  try {
    const res = await request<{ preferences: Preference[] }>(`/profiles/${name}/preferences`)
    preferences.value = res.preferences || []
  } catch (e: unknown) {
    message.error(errorText(e) || t('profiles.failedToLoadPreferences'))
  } finally {
    loadingPreferences.value = false
  }
}

async function feedbackPreference(key: string, accurate: boolean): Promise<void> {
  try {
    await request(`/profiles/${editingProfile.value}/preferences/${encodeURIComponent(key)}/feedback`, {
      method: 'POST',
      body: JSON.stringify({ accurate }),
    })
    message.success(accurate ? t('profiles.preferences.thanksAccurate') : t('profiles.preferences.thanksInaccurate'))
    await loadPreferences(editingProfile.value)
  } catch (e: unknown) {
    message.error(errorText(e) || t('profiles.failedToFeedback'))
  }
}

// 后端把错误写在响应体里（"HTTP 409: Profile already exists: work"），
// 直接给用户看状态码没有意义，剥掉前缀只留后端那句。
function errorText(e: unknown): string {
  const raw = e instanceof Error ? e.message : String(e || '')
  const match = raw.match(/^HTTP \d{3}:\s*([\s\S]*)$/)
  return (match ? match[1] : raw).trim()
}

onMounted(() => {
  loadProfiles()
})

// 刷新按钮：列表 + 当前已打开的详情一起重拉。
// 详情面板的数据（SOUL / 用户画像 / 偏好）各自独立请求，只刷列表的话
// 面板里还是旧内容；这里按 showDetailModal 的状态把三者一并刷新。
async function handleRefresh(): Promise<void> {
  if (refreshing.value) return
  refreshing.value = true
  try {
    await loadProfiles()
    const name = editingProfile.value
    if (name && showDetailModal.value) {
      await Promise.all([
        loadUserProfile(name),
        loadSoul(name),
        loadPreferences(name),
        loadSetupCommand(name),
      ])
    }
    message.success(t('common.refreshed'))
  } catch (e: unknown) {
    message.error(errorText(e) || t('common.refreshFailed'))
  } finally {
    refreshing.value = false
  }
}
</script>

<style scoped>
.profiles-container {
  padding: 0;
}

.page-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 16px;
  flex-wrap: wrap;
}

.page-title {
  margin: 0;
  font-size: 20px;
  font-weight: 600;
  line-height: 1.4;
}

.page-sub {
  margin: 4px 0 0;
  font-size: 13px;
  opacity: 0.6;
  line-height: 1.5;
}

.pending-alert {
  margin-bottom: 16px;
}

.profile-card {
  height: 100%;
  transition: transform 0.18s ease, box-shadow 0.18s ease;
}

.profile-card:hover {
  transform: translateY(-2px);
  box-shadow: 0 6px 18px rgba(0, 0, 0, 0.08);
}

.profile-card.is-active {
  --n-border-color: #18a058;
  --n-border-color-hover: #18a058;
  box-shadow: 0 0 0 1px rgba(24, 160, 88, 0.25);
}

.pf-top {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}

.pf-avatar {
  width: 38px;
  height: 38px;
  border-radius: 10px;
  flex: none;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 16px;
  font-weight: 600;
  background: rgba(128, 128, 128, 0.14);
  color: inherit;
}

.pf-avatar.is-active {
  background: rgba(24, 160, 88, 0.14);
  color: #18a058;
}

.pf-ident {
  min-width: 0;
  flex: 1;
}

.pf-name {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
}

.pf-name-text {
  font-size: 15px;
  font-weight: 600;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.pf-desc {
  margin-top: 2px;
  font-size: 12px;
  opacity: 0.55;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.pf-facts {
  display: flex;
  flex-wrap: wrap;
  gap: 6px 14px;
  margin: 14px 0 8px;
  font-size: 12px;
  opacity: 0.85;
}

.fact {
  display: inline-flex;
  align-items: center;
  gap: 4px;
}

.fact.muted {
  opacity: 0.5;
}

.pf-path {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 11px;
  opacity: 0.45;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.pf-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.empty-block {
  padding: 48px 0;
}

.field-hint {
  font-size: 12px;
  opacity: 0.55;
  margin: 6px 0 14px;
  line-height: 1.6;
}

.field-hint.is-error {
  opacity: 1;
  color: #d03050;
}

.switch-field {
  margin-top: 4px;
}

.detail-head {
  display: flex;
  align-items: center;
  gap: 10px;
}

.detail-close {
  margin-left: auto;
}

.detail-ident {
  min-width: 0;
}

.detail-title {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 16px;
  font-weight: 600;
}

.detail-sub {
  font-size: 12px;
  opacity: 0.55;
  margin-top: 2px;
}

.overview-desc :deep(.n-descriptions-table-content) {
  word-break: break-all;
}

.kv {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.kv-value {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12px;
  padding: 1px 6px;
  border-radius: 4px;
  background: rgba(128, 128, 128, 0.12);
}

.overview-tip {
  margin-top: 12px;
  font-size: 12px;
}

.soul-input :deep(textarea) {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 13px;
  line-height: 1.6;
}

.soul-meta {
  display: flex;
  justify-content: space-between;
  font-size: 12px;
  opacity: 0.55;
}

.pref-list {
  margin-top: 12px;
}

.pref-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.pref-main {
  min-width: 0;
}

.pref-key {
  font-size: 13px;
  font-weight: 600;
}

.pref-value {
  margin-top: 2px;
  font-size: 13px;
  opacity: 0.8;
  word-break: break-word;
}

.pref-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 6px;
  font-size: 11px;
  opacity: 0.55;
}

.pref-bar {
  width: 72px;
}

.pref-ctx {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 260px;
}

.empty-inline {
  margin-top: 28px;
}

.detail-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.dirty-badge {
  font-size: 12px;
  color: #f0a020;
}

.tab-footnote {
  display: block;
  font-size: 12px;
  margin-top: 4px;
}

/* 用户画像表单：上下与 tab 边缘留出呼吸空间 */
.user-profile-form {
  padding: 16px 4px;
}

/* 行间稍微松一点（show-feedback=false 时 naive-ui 默认行距偏挤） */
.user-profile-form :deep(.n-form-item) {
  margin-bottom: 10px;
}
</style>
