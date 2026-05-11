<template>
  <div class="skills-view">
    <n-space vertical :size="16">
      <n-space justify="space-between" align="center">
        <h2>{{ $t('skills.title') }}</h2>
        <n-space>
          <n-button @click="showBrowse = true">
            <template #icon>
              <n-icon :component="Search" />
            </template>
            {{ $t('skills.browse') }}
          </n-button>
        </n-space>
      </n-space>

      <n-tabs type="line">
        <n-tab-pane name="installed" tab="Installed">
          <n-grid :cols="3" :x-gap="16" :y-gap="16">
            <n-gi v-for="skill in skills" :key="skill.name">
              <n-card hoverable @click="viewSkill(skill)">
                <template #header>
                  <n-space align="center">
                    <n-icon :component="Book" size="20" />
                    <span class="skill-name">{{ skill.name }}</span>
                  </n-space>
                </template>
                <template #default>
                  <p class="skill-description">{{ skill.description }}</p>
                  <n-space>
                    <n-tag size="small" type="info">{{ skill.category }}</n-tag>
                    <n-tag
                      v-for="tag in skill.tags.slice(0, 2)"
                      :key="tag"
                      size="small"
                    >
                      {{ tag }}
                    </n-tag>
                  </n-space>
                </template>
              </n-card>
            </n-gi>
          </n-grid>
        </n-tab-pane>
      </n-tabs>
    </n-space>

    <!-- Browse Modal -->
    <n-modal v-model:show="showBrowse" preset="card" title="Browse Skills" style="width: 800px">
      <n-input v-model:value="searchQuery" placeholder="Search skills...">
        <template #prefix>
          <n-icon :component="Search" />
        </template>
      </n-input>
      <n-space vertical :size="12" style="margin-top: 16px">
        <n-card v-for="hubSkill in hubSkills" :key="hubSkill.name" hoverable>
          <n-space justify="space-between" align="center">
            <div>
              <n-text strong>{{ hubSkill.name }}</n-text>
              <n-text depth="3" style="display: block">{{ hubSkill.description }}</n-text>
            </div>
            <n-button size="small" @click="installSkill(hubSkill.source)">
              {{ $t('skills.install') }}
            </n-button>
          </n-space>
        </n-card>
      </n-space>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { NIcon } from 'naive-ui'
import { Book, Search } from '@vicons/ionicons5'
import { skillApi } from '@/api'
import type { Skill } from '@/types'

interface HubSkill extends Skill {
  source: string
  installs?: number
}

const skills = ref<Skill[]>([])
const showBrowse = ref(false)
const searchQuery = ref('')
const hubSkills = ref<HubSkill[]>([])

async function loadSkills() {
  try {
    const response = await skillApi.list()
    skills.value = response.data
  } catch (e) {
    console.error('Failed to load skills:', e)
  }
}

async function browseSkills() {
  try {
    const response = await skillApi.browse()
    hubSkills.value = response.data
  } catch (e) {
    console.error('Failed to browse skills:', e)
  }
}

async function installSkill(source: string) {
  try {
    await skillApi.install(source)
    loadSkills()
  } catch (e) {
    console.error('Failed to install skill:', e)
  }
}

function viewSkill(skill: Skill) {
  // Navigate to skill detail or show modal
  console.log('View skill:', skill)
}

onMounted(() => {
  loadSkills()
  browseSkills()
})
</script>

<style lang="scss" scoped>
.skill-name {
  font-weight: 600;
}

.skill-description {
  margin: 0 0 12px;
  color: var(--text-color-3);
}
</style>
