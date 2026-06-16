<template>
  <div>
    <n-space justify="space-between" style="margin-bottom: 16px;">
      <h2>{{ t('skills.title') }}</h2>
      <n-space>
        <n-button type="primary" @click="openHubModal">{{ t('skills.browseHub') }}</n-button>
      </n-space>
    </n-space>

    <n-spin v-if="skillsStore.loading" />
    <template v-else>
      <!-- Drag & Drop Zone -->
      <n-card :title="t('skills.dragDropInstall')" style="margin-bottom: 24px;">
        <n-space vertical>
          <n-upload
            ref="uploadRef"
            multiple
            directory-dnd
            :max="5"
            accept=".yaml,.yml,.md,.json,.zip"
            :custom-request="handleCustomUpload"
          >
            <n-upload-dragger>
              <div style="padding: 40px 0">
                <n-icon size="48" :depth="3">
                  <upload-icon />
                </n-icon>
                <n-text depth="3" style="display: block; margin-top: 16px;">
                  {{ t('skills.dragDropDesc') }}
                </n-text>
                <n-text depth="3" style="display: block; font-size: 12px; margin-top: 8px;">
                  {{ t('skills.supportedFormats') }}
                </n-text>
              </div>
            </n-upload-dragger>
          </n-upload>
        </n-space>
      </n-card>

      <!-- Skills by Source (Collapsible Panels) -->
      <n-collapse v-model:expanded-names="expandedSources">
        <n-collapse-item v-for="source in displaySources" :key="source" :name="source">
          <template #header>
            <n-space align="center">
              <span style="font-weight: 600;">{{ t(`skills.sourceOptions.${source}`) || source }}</span>
              <n-tag size="small" :type="getSourceType(source)">
                {{ skillsBySource[source]?.length || 0 }}
              </n-tag>
              <!-- 对于 auto 技能展示待审核数量 -->
              <n-tag v-if="source === 'auto' && pendingAutoCount > 0" size="small" type="warning">
                {{ t('skills.pendingReview') }}: {{ pendingAutoCount }}
              </n-tag>
            </n-space>
          </template>
          <n-grid :cols="3" :x-gap="12" :y-gap="12">
            <n-gi v-for="skill in skillsBySource[source]" :key="skill.id">
              <n-card size="small" hoverable @click="showSkillDetail(skill)">
                <template #header>
                  <div style="display: flex; align-items: center; justify-content: space-between; width: 100%;">
                    <n-space align="center">
                      <span style="font-weight: 500;">{{ skill.name }}</span>
                      <!-- Status tag for auto skills -->
                      <n-tag v-if="skill.source === 'auto' && skill.status" size="small" :type="getStatusTagType(skill.status)">
                        {{ getStatusLabel(skill.status) }}
                      </n-tag>
                      <n-tag v-else :type="skill.enabled ? 'success' : 'default'" size="small">
                        {{ skill.enabled ? t('tools.enabled') : t('tools.disabled') }}
                      </n-tag>
                    </n-space>
                    <div style="display: flex; align-items: center; gap: 8px;" @click.stop>
                      <n-tag v-if="getSkillStat(skill.name)" size="tiny" :type="getSuccessRateType(getSkillStat(skill.name)!.success_rate)">
                        {{ (getSkillStat(skill.name)!.success_rate * 100).toFixed(0) }}%
                      </n-tag>
                      <!-- Auto-skill action buttons -->
                      <template v-if="skill.source === 'auto'">
                        <n-popconfirm v-if="skill.status === 'pending'" @positive-click="performAction(skill, 'reject')" @click.stop>
                          <template #trigger>
                            <n-button size="small" quaternary circle>
                              <template #icon><n-icon><close-circle-icon /></n-icon></template>
                            </n-button>
                          </template>
                          {{ t('skills.rejectConfirm', { name: skill.name }) }}
                        </n-popconfirm>
                        <n-popconfirm v-if="skill.status === 'pending'" @positive-click="performAction(skill, 'approve')" @click.stop>
                          <template #trigger>
                            <n-button size="small" type="success" quaternary circle>
                              <template #icon><n-icon><checkmark-circle-icon /></n-icon></template>
                            </n-button>
                          </template>
                          {{ t('skills.approveConfirm', { name: skill.name }) }}
                        </n-popconfirm>
                        <n-popconfirm v-if="skill.status === 'approved'" @positive-click="performAction(skill, 'archive')" @click.stop>
                          <template #trigger>
                            <n-button size="small" quaternary circle>
                              <template #icon><n-icon><archive-icon /></n-icon></template>
                            </n-button>
                          </template>
                          {{ t('skills.archiveConfirm', { name: skill.name }) }}
                        </n-popconfirm>
                        <n-popconfirm v-if="skill.status === 'archived'" @positive-click="performAction(skill, 'restore')" @click.stop>
                          <template #trigger>
                            <n-button size="small" type="primary" quaternary circle>
                              <template #icon><n-icon><refresh-icon /></n-icon></template>
                            </n-button>
                          </template>
                          {{ t('skills.restoreConfirm', { name: skill.name }) }}
                        </n-popconfirm>
                        <n-popconfirm @positive-click="performAction(skill, 'delete')" @click.stop>
                          <template #trigger>
                            <n-button size="small" type="error" quaternary circle>
                              <template #icon><n-icon><delete-icon /></n-icon></template>
                            </n-button>
                          </template>
                          {{ t('skills.deleteAutoConfirm', { name: skill.name }) }}
                        </n-popconfirm>
                      </template>
                      <!-- Regular skill delete + toggle -->
                      <template v-else>
                        <n-popconfirm v-if="skill.source !== 'builtin'" @positive-click="deleteSkillConfirm(skill.id, skill.name)" @click.stop>
                          <template #trigger>
                            <n-button size="small" type="error" quaternary circle @click.stop>
                              <template #icon>
                                <n-icon><delete-icon /></n-icon>
                              </template>
                            </n-button>
                          </template>
                          {{ t('skills.deleteConfirm', { name: skill.name }) }}
                        </n-popconfirm>
                        <n-switch v-model:value="skill.enabled" size="small" @update:value="toggleSkill(skill.name, $event)" @click.stop />
                      </template>
                    </div>
                  </div>
                </template>
                <n-space vertical size="small">
                  <n-text depth="3" style="display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; text-overflow: ellipsis;">
                    {{ skill.description || t('skills.noDescription') }}
                  </n-text>
                  <n-space v-if="skill.tags && skill.tags.length > 0" size="small">
                    <n-tag v-for="tag in skill.tags" :key="tag" size="tiny">{{ tag }}</n-tag>
                  </n-space>
                </n-space>
              </n-card>
            </n-gi>
          </n-grid>
          <n-empty v-if="!skillsBySource[source]?.length" :description="t('skills.noSkills')" />
        </n-collapse-item>
      </n-collapse>
    </template>

    <!-- Skill Detail Modal -->
    <n-modal
      v-model:show="showDetailModal"
      :title="selectedSkill?.name"
      preset="card"
      style="width: 700px; max-height: 80vh;"
    >
      <n-space vertical v-if="selectedSkill">
        <n-space justify="end" style="margin-bottom: 8px;">
          <n-button v-if="selectedSkill?.source === 'auto' && selectedSkill?.status === 'pending'" type="success" @click="performAction(selectedSkill, 'approve')">
            {{ t('skills.approve') }}
          </n-button>
          <n-button v-if="selectedSkill?.source === 'auto' && selectedSkill?.status === 'pending'" type="warning" @click="performAction(selectedSkill, 'reject')">
            {{ t('skills.reject') }}
          </n-button>
          <n-button v-if="selectedSkill?.source === 'auto' && selectedSkill?.status === 'approved'" type="default" @click="performAction(selectedSkill, 'archive')">
            {{ t('skills.archive') }}
          </n-button>
          <n-button v-if="selectedSkill?.source === 'auto' && selectedSkill?.status === 'archived'" type="primary" @click="performAction(selectedSkill, 'restore')">
            {{ t('skills.restore') }}
          </n-button>
          <n-button v-if="selectedSkill?.source === 'auto'" type="error" @click="performAction(selectedSkill, 'delete')">
            {{ t('skills.delete') }}
          </n-button>
          <n-button v-if="selectedSkill?.source !== 'builtin' && selectedSkill?.source !== 'auto'" size="small" @click="openEditModal(selectedSkill)">
            <template #icon><EditIcon /></template>
            {{ t('skills.edit') }}
          </n-button>
        </n-space>
        <n-descriptions bordered>
          <n-descriptions-item :label="t('skills.description')">
            {{ selectedSkill.description || t('skills.noDescription') }}
          </n-descriptions-item>
          <n-descriptions-item v-if="selectedSkill.status" :label="t('skills.status')">
            <n-tag :type="getStatusTagType(selectedSkill.status)">{{ getStatusLabel(selectedSkill.status) }}</n-tag>
          </n-descriptions-item>
          <n-descriptions-item :label="t('skills.tags')">
            <n-space>
              <n-tag v-for="tag in selectedSkill.tags" :key="tag" size="small">{{ tag }}</n-tag>
            </n-space>
          </n-descriptions-item>
        </n-descriptions>

        <!-- Statistics -->
        <n-card v-if="getSkillStat(selectedSkill.name)" :title="t('skills.statistics')" size="small">
          <n-grid :cols="3" :x-gap="12">
            <n-gi>
              <n-statistic :label="t('skills.invocations')" :value="getSkillStat(selectedSkill.name)!.total_invocations" />
            </n-gi>
            <n-gi>
              <n-statistic :label="t('skills.successRate')" :value="(getSkillStat(selectedSkill.name)!.success_rate * 100).toFixed(1)" suffix="%" />
            </n-gi>
            <n-gi>
              <n-statistic :label="t('skills.avgQuality')" :value="getSkillStat(selectedSkill.name)!.avg_quality.toFixed(1)" />
            </n-gi>
          </n-grid>
        </n-card>
      </n-space>
    </n-modal>

    <!-- Edit Skill Modal -->
    <n-modal
      v-model:show="showEditModal"
      :title="t('skills.editSkill')"
      preset="card"
      style="width: 600px;"
    >
      <n-form v-if="editingSkill">
        <n-form-item :label="t('skills.name')">
          <n-input v-model:value="editingSkill.name" :disabled="true" />
        </n-form-item>
        <n-form-item :label="t('skills.description')">
          <n-input v-model:value="editingSkill.description" type="textarea" :rows="3" />
        </n-form-item>
        <n-form-item :label="t('skills.tags')">
          <n-dynamic-tags v-model:value="editingSkill.tags" />
        </n-form-item>
        <n-space justify="end">
          <n-button @click="showEditModal = false">{{ t('common.cancel') }}</n-button>
          <n-button type="primary" :loading="savingEdit" @click="saveEdit">{{ t('common.save') }}</n-button>
        </n-space>
      </n-form>
    </n-modal>

    <!-- Hub Modal -->
    <n-modal
      v-model:show="showHubModal"
      :title="t('skills.browseHub')"
      preset="card"
      style="width: 900px; max-height: 80vh;"
    >
      <n-space vertical>
        <n-space align="center">
          <n-input
            v-model:value="hubSearchKeyword"
            :placeholder="t('skills.searchHub')"
            @keyup.enter="searchHub"
            style="flex: 1;"
          >
            <template #prefix>
              <SearchIcon />
            </template>
          </n-input>
          <n-button type="primary" :loading="hubLoading" @click="searchHub">
            {{ t('skills.search') }}
          </n-button>
        </n-space>

        <n-spin :show="hubLoading">
          <div v-if="hubSkills.length > 0" style="max-height: 500px; overflow-y: auto;">
            <n-space vertical>
              <n-card
                v-for="item in hubSkills"
                :key="item.source_id || item.name"
                size="small"
                hoverable
                style="margin-bottom: 8px;"
              >
                <n-space justify="space-between" align="center">
                  <n-space vertical size="small" style="flex: 1; min-width: 0;">
                    <n-space align="center">
                      <span style="font-weight: 600;">{{ item.name }}</span>
                      <n-tag size="small" type="info">{{ t(`skills.hubSources.${item.source}`) || item.source }}</n-tag>
                      <n-tag v-if="item.verified" size="small" type="success">Verified</n-tag>
                      <n-tag v-if="item.stars > 0" size="small">⭐ {{ item.stars }}</n-tag>
                      <n-tag v-if="item.installs > 0" size="small">⬇️ {{ item.installs }}</n-tag>
                    </n-space>
                    <n-text depth="2" style="font-size: 13px;">{{ item.description }}</n-text>
                    <n-space v-if="item.tags && item.tags.length > 0">
                      <n-tag v-for="tag in item.tags" :key="tag" size="tiny" type="default">{{ tag }}</n-tag>
                    </n-space>
                  </n-space>
                  <n-button
                    size="small"
                    type="primary"
                    :loading="installingHubSkill === item.source_id"
                    @click="installHubSkill(item.source, item.source_id)"
                  >
                    {{ t('skills.install') }}
                  </n-button>
                </n-space>
              </n-card>
            </n-space>
          </div>
          <n-empty v-else-if="!hubLoading" :description="t('skills.noHubSkills')" />
        </n-spin>
      </n-space>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import { useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import {
  CloudUploadOutline as UploadIcon,
  Trash as DeleteIcon,
  Search as SearchIcon,
  CreateOutline as EditIcon,
  CheckmarkCircleOutline as CheckmarkCircleIcon,
  CloseCircleOutline as CloseCircleIcon,
  ArchiveOutline as ArchiveIcon,
  RefreshOutline as RefreshIcon,
} from '@vicons/ionicons5'
import { useSkillsStore } from '@/stores/skills'
import { uploadSkill, deleteSkill, getSkillStatistics, performAutoSkillAction } from '@/api/skills'
import type { UploadFileInfo } from 'naive-ui'

const { t } = useI18n()
const message = useMessage()
const skillsStore = useSkillsStore()

// UI State
const showDetailModal = ref(false)
const showEditModal = ref(false)
const showHubModal = ref(false)
const selectedSkill = ref<Skill | null>(null)
const performingAction = ref<string | null>(null)
const expandedSources = ref(['builtin', 'local', 'global', 'registry', 'auto'])

// Edit State
const editingSkill = ref<Skill | null>(null)
const savingEdit = ref(false)

// Hub State
const hubSearchKeyword = ref('')
const hubLoading = ref(false)
const hubSkills = ref<HubSkill[]>([])
const installingHubSkill = ref<string | null>(null)

// Data
const skillStats = ref<SkillStatistics[]>([])

// Types
interface Skill {
  id: string
  name: string
  description: string
  tags: string[]
  enabled: boolean
  source: 'builtin' | 'local' | 'global' | 'registry' | 'auto' | string
  status?: string  // 'pending' | 'approved' | 'archived' | 'rejected' (仅 auto 技能)
}

interface SkillStatistics {
  skill_name: string
  total_invocations: number
  success_rate: number
  avg_quality: number
  trend: string
}

interface HubSkill {
  name: string
  description: string
  tags: string[]
  source: string
  source_id: string
  url: string
  author: string
  stars: number
  installs: number
  verified: boolean
}

// Computed
const displaySources = computed(() => {
  const allSources = new Set(skillsStore.skills.map(s => s.source))
  const ordered = ['builtin', 'local', 'global', 'registry', 'auto']
  return ordered.filter(s => allSources.has(s))
})

const skillsBySource = computed(() => {
  const grouped: Record<string, Skill[]> = {}
  for (const skill of skillsStore.skills) {
    if (!grouped[skill.source]) {
      grouped[skill.source] = []
    }
    grouped[skill.source].push(skill)
  }
  return grouped
})

const pendingAutoCount = computed(() => {
  return skillsStore.skills.filter(s => s.source === 'auto' && s.status === 'pending').length
})

function getSuccessRateType(rate: number): 'success' | 'warning' | 'error' {
  if (rate >= 0.8) return 'success'
  if (rate >= 0.5) return 'warning'
  return 'error'
}

function getSourceType(source: string): 'success' | 'warning' | 'error' | 'info' | 'primary' | 'default' {
  switch (source) {
    case 'builtin': return 'success'
    case 'local': return 'info'
    case 'global': return 'primary'
    case 'registry': return 'warning'
    case 'auto': return 'default'
    default: return 'default'
  }
}

function getStatusTagType(status: string): 'success' | 'warning' | 'error' | 'info' | 'default' {
  switch (status) {
    case 'approved': return 'success'
    case 'pending': return 'warning'
    case 'archived': return 'default'
    case 'rejected': return 'error'
    default: return 'info'
  }
}

function getStatusLabel(status: string): string {
  const map: Record<string, string> = {
    pending: t('skills.statusPending'),
    approved: t('skills.statusApproved'),
    archived: t('skills.statusArchived'),
    rejected: t('skills.statusRejected'),
  }
  return map[status] || status
}

function getSkillStat(skillName: string): SkillStatistics | undefined {
  return skillStats.value.find(s => s.skill_name === skillName)
}

async function showSkillDetail(skill: Skill) {
  selectedSkill.value = skill
  showDetailModal.value = true
}

function openEditModal(skill: Skill) {
  editingSkill.value = {
    ...skill,
    tags: skill.tags ? [...skill.tags] : []
  }
  showEditModal.value = true
}

async function saveEdit() {
  if (!editingSkill.value) return
  savingEdit.value = true
  try {
    await skillsStore.updateSkill(editingSkill.value.id, {
      name: editingSkill.value.name,
      description: editingSkill.value.description,
      tags: editingSkill.value.tags,
    })
    message.success(t('skills.updated'))
    showEditModal.value = false
    if (selectedSkill.value && selectedSkill.value.id === editingSkill.value.id) {
      selectedSkill.value = { ...selectedSkill.value, ...editingSkill.value }
    }
  } catch (e) {
    message.error(t('skills.failedToUpdate'))
  } finally {
    savingEdit.value = false
  }
}

// Auto-skill lifecycle action
async function performAction(skill: Skill, action: string) {
  performingAction.value = action
  try {
    await performAutoSkillAction(skill.name, action)
    const actionMessages: Record<string, string> = {
      approve: t('skills.approved', { name: skill.name }),
      reject: t('skills.rejected', { name: skill.name }),
      archive: t('skills.archived', { name: skill.name }),
      restore: t('skills.restored', { name: skill.name }),
      delete: t('skills.deleted'),
    }
    message.success(actionMessages[action] || actionMessages.delete)

    // Update local UI state
    if (action === 'delete') {
      // Remove from list
      await skillsStore.loadSkills()
      showDetailModal.value = false
    } else {
      // Update status in-place
      const statusMap: Record<string, string> = {
        approve: 'approved',
        reject: 'rejected',
        archive: 'archived',
        restore: 'approved',
      }
      const newStatus = statusMap[action]
      // Update local skill object
      const idx = skillsStore.skills.findIndex(s => s.id === skill.id)
      if (idx >= 0) {
        (skillsStore.skills as any)[idx] = { ...skillsStore.skills[idx], status: newStatus }
      }
      if (selectedSkill.value && selectedSkill.value.id === skill.id) {
        selectedSkill.value = { ...selectedSkill.value, status: newStatus }
      }
    }
  } catch (e: any) {
    message.error(e?.message || t('skills.failedToAction'))
  } finally {
    performingAction.value = null
  }
}

// Hub functions
async function searchHub(): Promise<void> {
  hubLoading.value = true
  try {
    hubSkills.value = (await skillsStore.searchHubSkills(hubSearchKeyword.value)) || []
  } catch (e) {
    message.error(t('skills.failedToSearchHub'))
    hubSkills.value = []
  } finally {
    hubLoading.value = false
  }
}

async function openHubModal(): Promise<void> {
  showHubModal.value = true
  hubSearchKeyword.value = ''
  await searchHub()
}

async function installHubSkill(source: string, sourceID: string): Promise<void> {
  installingHubSkill.value = sourceID
  try {
    const result = await skillsStore.installHubSkill(source, sourceID)
    if (!result) {
      message.error(t('skills.failedToInstall'))
    } else if (!result.ok) {
      message.error(result.error || t('skills.failedToInstall'))
    } else {
      message.success(t('skills.installed'))
      showHubModal.value = false
      hubSearchKeyword.value = ''
      hubSkills.value = []
      await skillsStore.loadSkills()
    }
  } catch (e) {
    message.error(t('skills.failedToInstall'))
  } finally {
    installingHubSkill.value = null
  }
}

async function toggleSkill(id: string, enabled: boolean): Promise<void> {
  try {
    await skillsStore.toggleSkill(id, enabled)
    message.success(enabled ? t('skills.skillEnabled') : t('skills.skillDisabled'))
  } catch (e) {
    message.error(t('skills.failedToToggle'))
  }
}

async function deleteSkillConfirm(id: string, name: string): Promise<void> {
  try {
    await deleteSkill(id)
    message.success(t('skills.deleted'))
    await skillsStore.loadSkills()
  } catch (e) {
    message.error(t('skills.failedToDelete'))
  }
}

async function handleCustomUpload({ file, onFinish, onError }: { file: UploadFileInfo; onFinish: () => void; onError: () => void }) {
  const rawFile = file.file
  if (!rawFile) {
    onError()
    return
  }

  const ext = rawFile.name.split('.').pop()?.toLowerCase()
  const allowedExts = ['yaml', 'yml', 'md', 'json', 'zip']

  if (!ext || !allowedExts.includes(ext)) {
    message.warning(t('skills.fileTypeNotSupported', { ext: ext || 'unknown', allowed: allowedExts.join(', ') }))
    onError()
    return
  }

  try {
    let skillName = rawFile.name.replace(/\.(yaml|yml|md|json|zip)$/i, '')
    // @ts-ignore
    const relativePath = rawFile.webkitRelativePath || ''

    if (relativePath && relativePath.includes('/')) {
      const parts = relativePath.split('/')
      if (parts.length > 1) {
        skillName = parts[0]
      }
    }

    await uploadSkill(rawFile, skillName, relativePath)
    onFinish()
    message.success(t('skills.installed'))
    await skillsStore.loadSkills()
  } catch (e) {
    message.error(t('skills.failedToInstallFile', { name: rawFile.name }))
    onError()
  }
}

// Load data on mount
onMounted(async () => {
  await skillsStore.loadSkills()

  try {
    const stats = await getSkillStatistics()
    skillStats.value = stats
  } catch (e) {
    console.error('Failed to load skill data:', e)
  }
})
</script>
