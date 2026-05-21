<template>
  <div>
    <n-space justify="space-between" style="margin-bottom: 16px;">
      <h2>Skills</h2>
      <n-space>
        <n-button @click="showInstallModal = true">Install from URL</n-button>
      </n-space>
    </n-space>

    <n-spin v-if="skillsStore.loading" />
    <template v-else>
      <!-- Categories -->
      <n-card title="Categories" style="margin-bottom: 24px;" v-if="skillsStore.categories.length">
        <n-space>
          <n-tag v-for="cat in skillsStore.categories" :key="cat" size="large">
            {{ cat }}
          </n-tag>
        </n-space>
      </n-card>

      <!-- Drag & Drop Zone -->
      <n-card title="Drag & Drop Install" style="margin-bottom: 24px;">
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
                Click or drag skill files here to install
              </n-text>
              <n-text depth="3" style="display: block; font-size: 12px; margin-top: 8px;">
                Supported: .yaml, .yml, .md, .json, .zip (SKILL.md, skill.yaml)
              </n-text>
            </div>
          </n-upload-dragger>
        </n-upload>
      </n-card>

      <!-- Skills Grid -->
      <n-card title="All Skills">
        <n-grid :cols="3" :x-gap="12" :y-gap="12">
          <n-gi v-for="skill in skillsStore.skills" :key="skill.id">
            <n-card size="small">
              <template #header>
                <n-space align="center">
                  <span style="font-weight: 500;">{{ skill.name }}</span>
                  <n-tag :type="skill.enabled ? 'success' : 'default'" size="small">
                    {{ skill.enabled ? 'Enabled' : 'Disabled' }}
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
                    Are you sure you want to delete skill "{{ skill.name }}"?
                  </n-popconfirm>
                </n-space>
              </template>
              <n-space vertical size="small">
                <n-text depth="3" style="display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; text-overflow: ellipsis;">{{ skill.description || 'No description' }}</n-text>
                <n-text depth="3" style="font-size: 12px;">Category: {{ skill.category || 'General' }}</n-text>
              </n-space>
            </n-card>
          </n-gi>
        </n-grid>
        <n-empty v-if="!skillsStore.skills.length" description="No skills available" />
      </n-card>
    </template>

    <!-- Install Modal -->
    <n-modal v-model:show="showInstallModal" title="Install Skill from URL" preset="dialog">
      <n-form>
        <n-form-item label="Git URL">
          <n-input v-model:value="installUrl" placeholder="https://github.com/user/skill-repo.git" />
        </n-form-item>
      </n-form>
      <template #action>
        <n-space justify="end">
          <n-button @click="showInstallModal = false">Cancel</n-button>
          <n-button type="primary" :loading="installing" @click="installSkill">Install</n-button>
        </n-space>
      </template>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useMessage } from 'naive-ui'
import { CloudUploadOutline as UploadIcon, Trash as DeleteIcon } from '@vicons/ionicons5'
import { useSkillsStore } from '@/stores/skills'
import { uploadSkill, deleteSkill } from '@/api/skills'
import type { UploadFile, UploadFileInfo } from 'naive-ui'

const message = useMessage()
const skillsStore = useSkillsStore()
const showInstallModal = ref(false)
const installUrl = ref('')
const installing = ref(false)
const uploadingCount = ref(0)

async function toggleSkill(id: string, enabled: boolean): Promise<void> {
  try {
    await skillsStore.toggleSkill(id, enabled)
    message.success(enabled ? 'Skill enabled' : 'Skill disabled')
  } catch (e) {
    message.error('Failed to toggle skill')
  }
}

async function installSkill(): Promise<void> {
  if (!installUrl.value.trim()) {
    message.warning('Please enter a URL')
    return
  }
  installing.value = true
  try {
    await skillsStore.installSkill(installUrl.value)
    message.success('Skill installed successfully')
    showInstallModal.value = false
    installUrl.value = ''
  } catch (e) {
    message.error('Failed to install skill')
  } finally {
    installing.value = false
  }
}

async function deleteSkillConfirm(id: string, name: string): Promise<void> {
  try {
    await deleteSkill(id)
    message.success(`Skill "${name}" deleted`)
    await skillsStore.loadSkills()
    await skillsStore.loadCategories()
  } catch (e) {
    message.error(`Failed to delete skill "${name}"`)
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
    message.warning(`File type .${ext || 'unknown'} is not supported. Use: ${allowedExts.join(', ')}`)
    onError()
    return
  }

  uploadingCount.value++

  try {
    const skillName = rawFile.name.replace(/\.(yaml|yml|md|json|zip)$/i, '')
    await uploadSkill(rawFile, skillName)
    message.success(`Installed "${rawFile.name}" successfully`)
    onFinish()
    await skillsStore.loadSkills()
    await skillsStore.loadCategories()
  } catch (e) {
    message.error(`Failed to install "${rawFile.name}"`)
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

onMounted(() => {
  skillsStore.loadSkills()
  skillsStore.loadCategories()
})
</script>
