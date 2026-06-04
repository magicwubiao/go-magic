<template>
  <div>
    <n-space justify="space-between" style="margin-bottom: 16px;">
      <h2>{{ t('skills.title') }}</h2>
      <n-space>
        <n-button @click="showInstallModal = true">{{ t('skills.installFromUrl') }}</n-button>
        <n-button type="primary" @click="openHubModal">{{ t('skills.browseHub') }}</n-button>
      </n-space>
    </n-space>

    <n-spin v-if="skillsStore.loading" />
    <template v-else>
      <!-- Skill Statistics Overview -->
      <n-card :title="t('skills.statistics')" style="margin-bottom: 24px;" v-if="skillStats.length > 0">
        <n-grid :cols="4" :x-gap="12">
          <n-gi>
            <n-statistic :label="t('skills.totalInvocations')" :value="totalInvocations" />
          </n-gi>
          <n-gi>
            <n-statistic :label="t('skills.avgSuccessRate')" :value="avgSuccessRate.toFixed(1)" suffix="%" />
          </n-gi>
          <n-gi>
            <n-statistic :label="t('skills.topSkill')" :value="topSkillName" />
          </n-gi>
          <n-gi>
            <n-statistic :label="t('skills.improvingSkills')" :value="improvingSkillsCount" />
          </n-gi>
        </n-grid>
      </n-card>

      <!-- Categories & Sources Filter -->
      <n-card title="筛选" style="margin-bottom: 24px;">
        <n-space vertical size="medium">
          <!-- Categories -->
          <div>
            <n-space align="center">
              <n-text depth="3" style="font-size: 14px; font-weight: 500; margin-right: 8px;">{{ t('skills.categories') }}</n-text>
              <n-button size="tiny" quaternary @click="showAddCategoryModal = true">
                <template #icon><AddIcon /></template>
              </n-button>
            </n-space>
            <n-space style="margin-top: 4px;">
              <n-tag 
                size="large"
                :checked="selectedCategory === ''"
                checkable
                @update:checked="selectedCategory = selectedCategory === '' ? '' : ''"
              >
                {{ t('skills.categoryNames.all') || 'All' }}
              </n-tag>
              <n-tag 
                size="large"
                :checked="selectedCategory === 'general'"
                checkable
                @update:checked="selectedCategory = selectedCategory === 'general' ? '' : 'general'"
              >
                {{ t('skills.general') || 'General' }}
              </n-tag>
              <n-tag 
                v-for="cat in filteredCategories" 
                :key="cat" 
                size="large"
                :checked="selectedCategory === cat"
                checkable
                @update:checked="selectedCategory = selectedCategory === cat ? '' : cat"
              >
                {{ t(`skills.categoryNames.${cat}`) || cat }}
              </n-tag>
            </n-space>
          </div>
          
          <!-- Sources -->
          <div>
            <n-text depth="3" style="font-size: 14px; font-weight: 500; margin-right: 8px;">{{ t('skills.sources') }}</n-text>
            <n-space>
              <n-tag 
                v-for="source in skillSources" 
                :key="source" 
                size="large"
                :checked="selectedSource === source"
                checkable
                :type="getSourceType(source)"
                @update:checked="selectedSource = selectedSource === source ? '' : source"
              >
                {{ t(`skills.sourceOptions.${source}`) || source }}
              </n-tag>
            </n-space>
          </div>
        </n-space>
      </n-card>

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
            @finish="handleUploadFinish"
            @error="handleUploadError"
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

      <!-- Skills Grid with Stats -->
      <n-card :title="t('skills.allSkills')">
        <n-grid :cols="3" :x-gap="12" :y-gap="12">
          <n-gi v-for="skill in filteredSkills" :key="skill.id">
            <n-card size="small" hoverable @click="showSkillDetail(skill)">
              <template #header>
                <n-space align="center" justify="space-between" style="width: 100%;">
                  <n-space align="center">
                    <span style="font-weight: 500;">{{ skill.name }}</span>
                    <n-tag :type="skill.enabled ? 'success' : 'default'" size="small">
                      {{ skill.enabled ? t('tools.enabled') : t('tools.disabled') }}
                    </n-tag>
                    <n-tag :type="getSourceTagType(skill.source)" size="tiny">
                      {{ t(`skills.sourceOptions.${skill.source}`) || skill.source }}
                    </n-tag>
                  </n-space>
                  <n-space align="center" size="small">
                    <n-tag v-if="getSkillStat(skill.name)" size="tiny" :type="getSuccessRateType(getSkillStat(skill.name)!.success_rate)">
                      {{ (getSkillStat(skill.name)!.success_rate * 100).toFixed(0) }}%
                    </n-tag>
                  </n-space>
                </n-space>
              </template>
              <template #header-extra>
                <n-space align="center" size="small">
                  <n-switch v-model:value="skill.enabled" size="small" @update:value="toggleSkill(skill.name, $event)" @click.stop />
                  <n-popconfirm @positive-click="deleteSkillConfirm(skill.id, skill.name)" @click.stop>
                    <template #trigger>
                      <n-button size="small" type="error" quaternary circle @click.stop>
                        <template #icon>
                          <n-icon><delete-icon /></n-icon>
                        </template>
                      </n-button>
                    </template>
                    {{ t('skills.deleteConfirm', { name: skill.name }) }}
                  </n-popconfirm>
                </n-space>
              </template>
              <n-space vertical size="small">
                <n-text depth="3" style="display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; text-overflow: ellipsis;">
                  {{ skill.description || t('skills.noDescription') }}
                </n-text>
                <n-space justify="space-between" align="center">
                  <n-text depth="3" style="font-size: 12px;">
                    {{ t('skills.category') }}: {{ skill.category ? t(`skills.categoryNames.${skill.category}`) || skill.category : t('skills.general') }}
                  </n-text>
                  <n-tag v-if="getSkillTrend(skill.name)" size="tiny" :type="getTrendType(getSkillTrend(skill.name)!)">
                    {{ t(`skills.trends.${getSkillTrend(skill.name)}`) }}
                  </n-tag>
                </n-space>
              </n-space>
            </n-card>
          </n-gi>
        </n-grid>
        <n-empty v-if="!filteredSkills.length" :description="t('skills.noSkills')" />
      </n-card>
    </template>

    <!-- Install Modal -->
    <n-modal v-model:show="showInstallModal" :title="t('skills.installFromUrl')" preset="dialog">
      <n-form>
        <n-form-item :label="t('skills.gitUrl')">
          <n-input v-model:value="installUrl" placeholder="https://github.com/user/skill-repo.git" />
        </n-form-item>
      </n-form>
      <template #action>
        <n-space justify="end">
          <n-button @click="showInstallModal = false">{{ t('common.cancel') }}</n-button>
          <n-button type="primary" :loading="installing" @click="installSkill">{{ t('skills.install') }}</n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- Skill Detail Modal -->
    <n-modal 
      v-model:show="showDetailModal" 
      :title="selectedSkill?.name" 
      preset="card" 
      style="width: 800px; max-height: 80vh;"
      @update:show="handleModalShowChange"
    >
      <n-tabs v-if="selectedSkill">
        <n-tab-pane :name="t('skills.tabs.overview')" :tab="t('skills.tabs.overview')">
          <n-space vertical>
            <n-space justify="end" style="margin-bottom: 12px;">
              <n-button size="small" style="display: flex; align-items: center;" @click="openEditModal(selectedSkill)">
                <template #icon><EditIcon /></template>
                {{ t('skills.edit') }}
              </n-button>
            </n-space>
            <n-descriptions bordered>
              <n-descriptions-item :label="t('skills.description')">
                {{ selectedSkill.description || t('skills.noDescription') }}
              </n-descriptions-item>
              <n-descriptions-item :label="t('skills.category')">
                {{ selectedSkill.category ? t(`skills.categoryNames.${selectedSkill.category}`) || selectedSkill.category : t('skills.general') }}
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
        </n-tab-pane>
        
        <n-tab-pane :name="t('skills.tabs.versions')" :tab="t('skills.tabs.versions')">
          <n-empty v-if="!skillVersions.length" :description="t('skills.noVersions')" />
          <n-timeline v-else>
            <n-timeline-item 
              v-for="version in skillVersions" 
              :key="version.version"
              :type="version.is_current ? 'success' : 'default'"
              :title="version.version + (version.is_current ? ' (' + t('skills.current') + ')' : '')"
              :content="version.description"
              :time="formatTime(version.created_at)"
            />
          </n-timeline>
        </n-tab-pane>
        
        <n-tab-pane :name="t('skills.tabs.evolution')" :tab="t('skills.tabs.evolution')">
          <n-empty v-if="!evolutionHistory.length" :description="t('skills.noEvolution')" />
          <n-timeline v-else>
            <n-timeline-item 
              v-for="record in evolutionHistory" 
              :key="record.id"
              :type="record.status === 'validated' ? 'success' : record.status === 'reverted' ? 'error' : 'warning'"
              :title="t('skills.generation') + ' ' + record.generation"
              :content="record.reason"
              :time="formatTime(record.timestamp)"
            />
          </n-timeline>
        </n-tab-pane>
      </n-tabs>
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
        <n-form-item :label="t('skills.category')">
          <n-select v-model:value="editingSkill.category" :options="categoryOptions" />
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

    <!-- Add Category Modal -->
    <n-modal
      v-model:show="showAddCategoryModal"
      :title="t('skills.addCategory')"
      preset="card"
      style="width: 400px;"
    >
      <n-form>
        <n-form-item :label="t('skills.name')">
          <n-input v-model:value="newCategoryName" :placeholder="t('skills.categoryNamePlaceholder')" />
        </n-form-item>
        <n-space justify="end">
          <n-button @click="showAddCategoryModal = false">{{ t('common.cancel') }}</n-button>
          <n-button type="primary" :loading="addingCategory" @click="saveNewCategory">{{ t('common.save') }}</n-button>
        </n-space>
      </n-form>
    </n-modal>

    <!-- Hub / Skill Market Modal -->
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
            <n-list bordered>
              <n-list-item v-for="item in hubSkills" :key="item.source_id || item.name">
                <template #header>
                  <n-space align="center">
                    <span style="font-weight: 600;">{{ item.name }}</span>
                    <n-tag size="small" type="info">{{ item.source }}</n-tag>
                    <n-tag v-if="item.verified" size="small" type="success">Verified</n-tag>
                    <n-tag v-if="item.stars > 0" size="small">{{ item.stars }}</n-tag>
                  </n-space>
                </template>
                <template #description>
                  <n-space vertical size="small">
                    <n-text depth="2">{{ item.description }}</n-text>
                    <n-space v-if="item.tags && item.tags.length > 0">
                      <n-tag v-for="tag in item.tags" :key="tag" size="tiny">{{ tag }}</n-tag>
                    </n-space>
                  </n-space>
                </template>
                <template #extra>
                  <n-button
                    size="small"
                    type="primary"
                    :loading="installingHubSkill === item.source_id"
                    @click="installHubSkill(item.source, item.source_id)"
                  >
                    {{ t('skills.install') }}
                  </n-button>
                </template>
              </n-list-item>
            </n-list>
          </div>
          <n-empty v-else :description="t('skills.noHubSkills')" />
        </n-spin>
      </n-space>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import { useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { CloudUploadOutline as UploadIcon, Trash as DeleteIcon, Search as SearchIcon, CreateOutline as EditIcon, AddOutline as AddIcon } from '@vicons/ionicons5'
import { useSkillsStore } from '@/stores/skills'
import { uploadSkill, deleteSkill, getSkillStatistics, getSkillVersions, getSkillEvolutionHistory, createCategory } from '@/api/skills'
import type { UploadFileInfo } from 'naive-ui'

const { t } = useI18n()
const message = useMessage()
const skillsStore = useSkillsStore()

// UI State
const showInstallModal = ref(false)
const showDetailModal = ref(false)
const showEditModal = ref(false)
const showAddCategoryModal = ref(false)
const installUrl = ref('')
const installing = ref(false)
const uploadingCount = ref(0)
const selectedCategory = ref('')
const selectedSource = ref('')
const selectedSkill = ref<Skill | null>(null)

// Edit State
const editingSkill = ref<Skill | null>(null)
const savingEdit = ref(false)

// Category State
const newCategoryName = ref('')
const addingCategory = ref(false)

// Hub State
const showHubModal = ref(false)
const hubSearchKeyword = ref('')
const hubLoading = ref(false)
const hubSkills = ref<HubSkill[]>([])
const installingHubSkill = ref<string | null>(null)

// Data
const skillStats = ref<SkillStatistics[]>([])
const skillSources = ref<string[]>(['all', 'local', 'global', 'registry', 'auto'])
const skillVersions = ref<SkillVersion[]>([])
const evolutionHistory = ref<EvolutionRecord[]>([])

// Types
interface Skill {
  id: string
  name: string
  description: string
  category: string
  tags: string[]
  enabled: boolean
  source: 'builtin' | 'local' | 'global' | 'registry' | 'auto' | string
}

interface SkillStatistics {
  skill_name: string
  total_invocations: number
  success_rate: number
  avg_quality: number
  trend: string
}

interface SkillVersion {
  version: string
  description: string
  created_at: string
  is_current: boolean
  quality_score: number
}

interface EvolutionRecord {
  id: string
  generation: number
  reason: string
  status: string
  timestamp: string
}

interface HubSkill {
  name: string
  description: string
  category: string
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
const filteredSkills = computed(() => {
  let result = skillsStore.skills
  
  if (selectedCategory.value && selectedCategory.value !== 'all') {
    if (selectedCategory.value === 'general') {
      result = result.filter(s => !s.category || s.category === '' || s.category === 'general')
    } else {
      result = result.filter(s => s.category === selectedCategory.value)
    }
  }
  
  if (selectedSource.value && selectedSource.value !== 'all') {
    result = result.filter(s => s.source === selectedSource.value)
  }
  
  return result
})

const totalInvocations = computed(() => {
  return skillStats.value.reduce((sum, s) => sum + s.total_invocations, 0)
})

const avgSuccessRate = computed(() => {
  if (skillStats.value.length === 0) return 0
  const total = skillStats.value.reduce((sum, s) => sum + s.success_rate, 0)
  return (total / skillStats.value.length) * 100
})

const topSkillName = computed(() => {
  if (skillStats.value.length === 0) return '-'
  const top = skillStats.value.reduce((max, s) => s.total_invocations > max.total_invocations ? s : max, skillStats.value[0])
  return top.skill_name
})

const improvingSkillsCount = computed(() => {
  return skillStats.value.filter(s => s.trend === 'improving').length
})

const categoryOptions = computed(() => {
  return [
    { label: t('skills.general'), value: 'general' },
    ...skillsStore.categories
      .filter(c => c !== 'general')
      .map(c => ({
        label: t(`skills.categoryNames.${c}`) || c,
        value: c
      }))
  ]
})

// 过滤掉 "general" 和空字符串，因为已单独显示
const filteredCategories = computed(() => {
  return skillsStore.categories.filter(c => c && c !== 'general')
})

function getSuccessRateType(rate: number): 'success' | 'warning' | 'error' {
  if (rate >= 0.8) return 'success'
  if (rate >= 0.5) return 'warning'
  return 'error'
}

function getTrendType(trend: string): 'success' | 'warning' | 'error' | 'default' {
  switch (trend) {
    case 'improving': return 'success'
    case 'declining': return 'error'
    case 'stable': return 'default'
    default: return 'default'
  }
}

function getSourceType(source: string): 'success' | 'warning' | 'error' | 'info' | 'primary' | 'default' {
  switch (source) {
    case 'all': return 'primary'
    case 'builtin': return 'success'
    case 'local': return 'info'
    case 'global': return 'primary'
    case 'registry': return 'warning'
    case 'auto': return 'default'
    default: return 'default'
  }
}

function getSourceTagType(source: string): 'success' | 'warning' | 'error' | 'info' | 'primary' | 'default' {
  switch (source) {
    case 'builtin': return 'success'
    case 'local': return 'info'
    case 'global': return 'primary'
    case 'registry': return 'warning'
    case 'auto': return 'default'
    default: return 'default'
  }
}

function getSkillStat(skillName: string): SkillStatistics | undefined {
  return skillStats.value.find(s => s.skill_name === skillName)
}

function getSkillTrend(skillName: string): string | undefined {
  const stat = getSkillStat(skillName)
  return stat?.trend
}

function formatTime(timeStr: string): string {
  return new Date(timeStr).toLocaleString()
}

async function selectSkill(skill: Skill) {
  selectedSkill.value = skill
  await loadSkillDetail(skill.name)
  showDetailModal.value = true
}

async function showSkillDetail(skill: Skill, event?: Event) {
  // Prevent focus from staying on the card
  if (event && event.target instanceof HTMLElement) {
    event.target.blur()
  }
  selectedSkill.value = skill
  await loadSkillDetail(skill.name)
  showDetailModal.value = true
}

function handleModalShowChange(visible: boolean) {
  if (visible) {
    // When modal opens, focus should move inside the modal
    setTimeout(() => {
      const modalContent = document.querySelector('.n-modal-content')
      if (modalContent) {
        const focusable = modalContent.querySelector('button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])') as HTMLElement
        if (focusable) {
          focusable.focus()
        }
      }
    }, 100)
  }
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
      category: editingSkill.value.category,
      tags: editingSkill.value.tags,
    })
    message.success(t('skills.updated'))
    showEditModal.value = false
    // Refresh detail modal if open
    if (selectedSkill.value && selectedSkill.value.id === editingSkill.value.id) {
      selectedSkill.value = { ...selectedSkill.value, ...editingSkill.value }
    }
  } catch (e) {
    message.error(t('skills.failedToUpdate'))
  } finally {
    savingEdit.value = false
  }
}

async function saveNewCategory() {
  if (!newCategoryName.value.trim()) {
    message.warning(t('skills.pleaseEnterCategoryName'))
    return
  }
  addingCategory.value = true
  try {
    await createCategory(newCategoryName.value.trim())
    message.success(t('skills.categoryAdded'))
    showAddCategoryModal.value = false
    newCategoryName.value = ''
    await skillsStore.loadCategories()
  } catch (e) {
    message.error(t('skills.failedToAddCategory'))
  } finally {
    addingCategory.value = false
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
    await skillsStore.installHubSkill(source, sourceID)
    message.success(t('skills.installed'))
    await skillsStore.loadSkills()
    await skillsStore.loadCategories()
    showHubModal.value = false
    hubSearchKeyword.value = ''
    hubSkills.value = []
  } catch (e) {
    message.error(t('skills.failedToInstall'))
  } finally {
    installingHubSkill.value = null
  }
}

async function loadSkillDetail(skillName: string) {
  try {
    const [versions, evolution] = await Promise.all([
      getSkillVersions(skillName),
      getSkillEvolutionHistory(skillName)
    ])
    skillVersions.value = versions
    evolutionHistory.value = evolution
  } catch (e) {
    console.error('Failed to load skill detail:', e)
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

async function installSkill(): Promise<void> {
  if (!installUrl.value.trim()) {
    message.warning(t('skills.pleaseEnterUrl'))
    return
  }
  installing.value = true
  try {
    await skillsStore.installSkill(installUrl.value)
    message.success(t('skills.installed'))
    showInstallModal.value = false
    installUrl.value = ''
  } catch (e) {
    message.error(t('skills.failedToInstall'))
  } finally {
    installing.value = false
  }
}

async function deleteSkillConfirm(id: string, name: string): Promise<void> {
  try {
    await deleteSkill(id)
    message.success(t('skills.deleted'))
    await skillsStore.loadSkills()
    await skillsStore.loadCategories()
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

  uploadingCount.value++

  try {
    let skillName = rawFile.name.replace(/\.(yaml|yml|md|json|zip)$/i, '')
    // @ts-ignore
    let relativePath = rawFile.webkitRelativePath || ''
    
    if (relativePath && relativePath.includes('/')) {
      const parts = relativePath.split('/')
      if (parts.length > 1) {
        skillName = parts[0]
      }
    }
    
    await uploadSkill(rawFile, skillName, relativePath)
    message.success(t('skills.installedFile', { name: rawFile.name }))
    onFinish()
    await skillsStore.loadSkills()
    await skillsStore.loadCategories()
  } catch (e) {
    message.error(t('skills.failedToInstallFile', { name: rawFile.name }))
    onError()
  } finally {
    uploadingCount.value--
  }
}

function handleUploadFinish() {
  // File uploaded successfully
}

function handleUploadError() {
  // Upload failed
}

// Load data on mount
onMounted(async () => {
  await Promise.all([
    skillsStore.loadSkills(),
    skillsStore.loadCategories()
  ])
  
  // Load statistics
  try {
    const stats = await getSkillStatistics()
    skillStats.value = stats
  } catch (e) {
    console.error('Failed to load skill data:', e)
  }
})
</script>