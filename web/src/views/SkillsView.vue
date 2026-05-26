<template>
  <div>
    <n-space justify="space-between" style="margin-bottom: 16px;">
      <h2>{{ t('skills.title') }}</h2>
      <n-space>
        <n-button @click="showInstallModal = true">{{ t('skills.installFromUrl') }}</n-button>
      </n-space>
    </n-space>

    <n-spin v-if="skillsStore.loading" />
    <template v-else>
      <!-- Categories -->
      <n-card :title="t('skills.categories')" style="margin-bottom: 24px;" v-if="skillsStore.categories.length">
        <n-space>
          <n-tag v-for="cat in skillsStore.categories" :key="cat" size="large">
            {{ cat }}
          </n-tag>
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
          
          <!-- Directory Upload Button -->
          <n-space justify="center">
            <input
              ref="dirInputRef"
              type="file"
              webkitdirectory
              directory
              multiple
              style="display: none"
              @change="handleDirectorySelect"
            />
            <n-button @click="dirInputRef?.click()">
              <template #icon>
                <n-icon><folder-icon /></n-icon>
              </template>
              {{ t('skills.selectSkillFolder') }}
            </n-button>
          </n-space>
        </n-space>
      </n-card>

      <!-- Skills Grid -->
      <n-card :title="t('skills.allSkills')">
        <n-grid :cols="3" :x-gap="12" :y-gap="12">
          <n-gi v-for="skill in skillsStore.skills" :key="skill.id">
            <n-card size="small">
              <template #header>
                <n-space align="center">
                  <span style="font-weight: 500;">{{ skill.name }}</span>
                  <n-tag :type="skill.enabled ? 'success' : 'default'" size="small">
                    {{ skill.enabled ? t('tools.enabled') : t('tools.disabled') }}
                  </n-tag>
                </n-space>
              </template>
              <template #header-extra>
                <n-space align="center" size="small">
                  <n-switch v-model:value="skill.enabled" size="small" @update:value="toggleSkill(skill.name, $event)" />
                  <n-popconfirm @positive-click="deleteSkillConfirm(skill.id, skill.name)">
                    <template #trigger>
                      <n-button size="small" type="error" quaternary circle>
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
                <n-text depth="3" style="display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; text-overflow: ellipsis;">{{ skill.description || t('skills.noDescription') }}</n-text>
                <n-text depth="3" style="font-size: 12px;">{{ t('skills.category') }}: {{ skill.category || t('skills.general') }}</n-text>
              </n-space>
            </n-card>
          </n-gi>
        </n-grid>
        <n-empty v-if="!skillsStore.skills.length" :description="t('skills.noSkills')" />
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
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { CloudUploadOutline as UploadIcon, Trash as DeleteIcon, Folder as FolderIcon } from '@vicons/ionicons5'
import { useSkillsStore } from '@/stores/skills'
import { uploadSkill, deleteSkill } from '@/api/skills'
import type { UploadFile, UploadFileInfo } from 'naive-ui'

const { t } = useI18n()
const message = useMessage()
const skillsStore = useSkillsStore()
const showInstallModal = ref(false)
const installUrl = ref('')
const installing = ref(false)
const uploadingCount = ref(0)
const dirInputRef = ref<HTMLInputElement | null>(null)

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

  // Validate file type
  const ext = rawFile.name.split('.').pop()?.toLowerCase()
  const allowedExts = ['yaml', 'yml', 'md', 'json', 'zip']

  if (!ext || !allowedExts.includes(ext)) {
    message.warning(t('skills.fileTypeNotSupported', { ext: ext || 'unknown', allowed: allowedExts.join(', ') }))
    onError()
    return
  }

  uploadingCount.value++

  try {
    // Get skill name from folder path if available (for directory uploads)
    let skillName = rawFile.name.replace(/\.(yaml|yml|md|json|zip)$/i, '')
    let relativePath = ''
    
    // Check for webkitRelativePath (directory upload) - extract folder name
    // @ts-ignore
    relativePath = rawFile.webkitRelativePath || ''
    console.log('[Upload] File:', rawFile.name, 'webkitRelativePath:', relativePath)
    
    if (relativePath && relativePath.includes('/')) {
      // Use the first folder name as skill name
      const parts = relativePath.split('/')
      if (parts.length > 1) {
        skillName = parts[0]
        console.log('[Upload] Using folder name as skill:', skillName)
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

function handleUploadFinish({ file }: { file: UploadFileInfo }) {
  // File uploaded successfully
}

function handleUploadError({ file }: { file: UploadFileInfo }) {
  // Upload failed, error already shown in handleCustomUpload
}

async function handleDirectorySelect(event: Event) {
  const input = event.target as HTMLInputElement
  const files = input.files
  if (!files || files.length === 0) return

  uploadingCount.value += files.length

  // Get folder name from first file's path
  const firstFile = files[0]
  // @ts-ignore
  const relativePath = firstFile.webkitRelativePath || ''
  const folderName = relativePath.split('/')[0] || 'skill'
  console.log('[Directory Upload] Folder:', folderName, 'Files:', files.length)

  // Upload all files
  const uploadPromises = Array.from(files).map(async (file) => {
    // @ts-ignore
    const filePath = file.webkitRelativePath || file.name
    console.log('[Directory Upload] Uploading:', filePath)

    try {
      await uploadSkill(file, folderName, filePath)
    } catch (e) {
      console.error('[Directory Upload] Failed:', file.name, e)
      throw e
    }
  })

  try {
    await Promise.all(uploadPromises)
    message.success(t('skills.installedSkill', { name: folderName, count: files.length }))
    await skillsStore.loadSkills()
    await skillsStore.loadCategories()
  } catch (e) {
    message.error(t('skills.failedToInstall'))
  } finally {
    uploadingCount.value -= files.length
    // Reset input
    input.value = ''
  }
}

onMounted(() => {
  skillsStore.loadSkills()
  skillsStore.loadCategories()
})
</script>
