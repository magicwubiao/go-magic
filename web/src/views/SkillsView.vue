<template>
  <div>
    <n-space justify="space-between" style="margin-bottom: 16px;">
      <h2>Skills</h2>
      <n-button type="primary" @click="showInstallModal = true">Install from URL</n-button>
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
                <n-switch v-model:value="skill.enabled" size="small" @update:value="toggleSkill(skill.name, $event)" />
              </template>
              <n-space vertical size="small">
                <n-text depth="3">{{ skill.description || 'No description' }}</n-text>
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
import { useSkillsStore } from '@/stores/skills'

const message = useMessage()
const skillsStore = useSkillsStore()
const showInstallModal = ref(false)
const installUrl = ref('')
const installing = ref(false)

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

onMounted(() => {
  skillsStore.loadSkills()
  skillsStore.loadCategories()
})
</script>
