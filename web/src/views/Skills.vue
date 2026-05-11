<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { NInput, NSpin, NButton, NTag, useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const message = useMessage()

interface Skill {
  name: string
  description: string
  category: string
  source: 'builtin' | 'hub' | 'local'
  tags?: string[]
}

const loading = ref(false)
const skills = ref<Skill[]>([])
const searchQuery = ref('')
const selectedSkill = ref<Skill | null>(null)
const sourceFilter = ref<string | null>(null)

const filteredSkills = computed(() => {
  const query = searchQuery.value.trim().toLowerCase()
  return skills.value.filter(skill => {
    if (sourceFilter.value && skill.source !== sourceFilter.value) return false
    if (!query) return true
    return skill.name.toLowerCase().includes(query) ||
           skill.description.toLowerCase().includes(query)
  })
})

const categories = computed(() => {
  const cats = new Map<string, Skill[]>()
  for (const skill of filteredSkills.value) {
    const cat = skill.category || 'General'
    if (!cats.has(cat)) cats.set(cat, [])
    cats.get(cat)!.push(skill)
  }
  return cats
})

async function loadSkills() {
  loading.value = true
  try {
    const res = await fetch('/api/skills')
    if (res.ok) {
      const data = await res.json()
      skills.value = data.skills || []
    } else {
      // CLI fallback
      const cliRes = await fetch('/api/cli', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ command: 'skills', args: ['list', '--json'] })
      })
      const cliData = await cliRes.json()
      if (cliData.output) {
        skills.value = parseSkillsList(cliData.output)
      }
    }
  } catch (err: any) {
    console.error('Failed to load skills:', err)
    message.error(t('skills.loadFailed'))
  } finally {
    loading.value = false
  }
}

function parseSkillsList(output: string): Skill[] {
  try {
    // Try JSON parse first
    return JSON.parse(output)
  } catch {
    // Parse text format
    const lines = output.split('\n').filter(l => l.trim())
    const skills: Skill[] = []
    for (const line of lines) {
      const parts = line.split(/\s{2,}/)
      if (parts.length >= 2) {
        skills.push({
          name: parts[0].trim(),
          description: parts[1].trim(),
          category: 'General',
          source: 'local'
        })
      }
    }
    return skills
  }
}

function selectSkill(skill: Skill) {
  selectedSkill.value = skill
}

function getSourceColor(source: string): 'success' | 'info' | 'warning' {
  switch (source) {
    case 'builtin': return 'success'
    case 'hub': return 'info'
    default: return 'warning'
  }
}

function getSourceLabel(source: string): string {
  switch (source) {
    case 'builtin': return t('skills.source.builtin')
    case 'hub': return t('skills.source.hub')
    default: return t('skills.source.local')
  }
}

onMounted(loadSkills)
</script>

<template>
  <div class="skills-view">
    <header class="page-header">
      <h2 class="header-title">{{ t('skills.title') }}</h2>
      <div class="header-actions">
        <NButton size="small" @click="loadSkills" :loading="loading">
          {{ t('skills.refresh') }}
        </NButton>
      </div>
    </header>

    <div class="skills-content">
      <div class="skills-toolbar">
        <NInput
          v-model:value="searchQuery"
          :placeholder="t('skills.searchPlaceholder')"
          size="small"
          clearable
          style="width: 200px"
        />
        <div class="source-legend">
          <NTag
            v-if="sourceFilter === null || sourceFilter === 'builtin'"
            :type="sourceFilter === 'builtin' ? 'success' : 'default'"
            size="small"
            checkable
            :checked="sourceFilter === 'builtin'"
            @click="sourceFilter = sourceFilter === 'builtin' ? null : 'builtin'"
          >
            {{ t('skills.source.builtin') }}
          </NTag>
          <NTag
            v-if="sourceFilter === null || sourceFilter === 'hub'"
            :type="sourceFilter === 'hub' ? 'info' : 'default'"
            size="small"
            checkable
            :checked="sourceFilter === 'hub'"
            @click="sourceFilter = sourceFilter === 'hub' ? null : 'hub'"
          >
            {{ t('skills.source.hub') }}
          </NTag>
          <NTag
            v-if="sourceFilter === null || sourceFilter === 'local'"
            :type="sourceFilter === 'local' ? 'warning' : 'default'"
            size="small"
            checkable
            :checked="sourceFilter === 'local'"
            @click="sourceFilter = sourceFilter === 'local' ? null : 'local'"
          >
            {{ t('skills.source.local') }}
          </NTag>
        </div>
      </div>

      <NSpin :show="loading" size="large">
        <div v-if="filteredSkills.length === 0 && !loading" class="empty-state">
          {{ t('skills.noMatch') }}
        </div>

        <div v-else class="skills-layout">
          <div class="skills-list">
            <div
              v-for="[category, categorySkills] in categories"
              :key="category"
              class="skill-category"
            >
              <div class="category-header">{{ category }}</div>
              <div
                v-for="skill in categorySkills"
                :key="skill.name"
                class="skill-item"
                :class="{ active: selectedSkill?.name === skill.name }"
                @click="selectSkill(skill)"
              >
                <div class="skill-info">
                  <span class="skill-name">{{ skill.name }}</span>
                  <span class="skill-desc">{{ skill.description }}</span>
                </div>
                <NTag :type="getSourceColor(skill.source)" size="tiny">
                  {{ getSourceLabel(skill.source) }}
                </NTag>
              </div>
            </div>
          </div>

          <div class="skill-detail">
            <template v-if="selectedSkill">
              <div class="detail-header">
                <h3>{{ selectedSkill.name }}</h3>
                <NTag :type="getSourceColor(selectedSkill.source)" size="small">
                  {{ getSourceLabel(selectedSkill.source) }}
                </NTag>
              </div>
              <div class="detail-content">
                <p class="detail-desc">{{ selectedSkill.description }}</p>
                <div v-if="selectedSkill.tags?.length" class="detail-tags">
                  <NTag
                    v-for="tag in selectedSkill.tags"
                    :key="tag"
                    size="small"
                    type="info"
                  >
                    {{ tag }}
                  </NTag>
                </div>
              </div>
            </template>
            <div v-else class="empty-detail">
              <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1" opacity="0.2">
                <polygon points="12 2 2 7 12 12 22 7 12 2" />
                <polyline points="2 17 12 22 22 17" />
                <polyline points="2 12 12 17 22 12" />
              </svg>
              <span>{{ t('skills.noMatch') }}</span>
            </div>
          </div>
        </div>
      </NSpin>
    </div>
  </div>
</template>

<style scoped lang="scss">
.skills-view {
  height: calc(100 * var(--vh));
  display: flex;
  flex-direction: column;
}

.skills-content {
  flex: 1;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

.skills-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 20px;
  border-bottom: 1px solid #303133;
}

.source-legend {
  display: flex;
  gap: 8px;
}

.empty-state {
  text-align: center;
  color: #909399;
  padding: 40px 0;
}

.skills-layout {
  flex: 1;
  display: flex;
  overflow: hidden;
}

.skills-list {
  width: 300px;
  border-right: 1px solid #303133;
  overflow-y: auto;
  padding: 12px;
}

.skill-category {
  margin-bottom: 16px;
}

.category-header {
  font-size: 11px;
  font-weight: 600;
  color: #909399;
  text-transform: uppercase;
  padding: 8px 8px 4px;
  letter-spacing: 0.5px;
}

.skill-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 8px;
  border-radius: 6px;
  cursor: pointer;
  transition: background 0.2s;

  &:hover {
    background: rgba(255, 255, 255, 0.05);
  }

  &.active {
    background: rgba(255, 215, 0, 0.1);
    border: 1px solid rgba(255, 215, 0, 0.3);
  }
}

.skill-info {
  display: flex;
  flex-direction: column;
  gap: 2px;
  overflow: hidden;
}

.skill-name {
  font-size: 13px;
  font-weight: 500;
  color: #e6e6e6;
}

.skill-desc {
  font-size: 11px;
  color: #909399;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.skill-detail {
  flex: 1;
  padding: 20px;
  overflow-y: auto;
}

.detail-header {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 16px;

  h3 {
    margin: 0;
    font-size: 18px;
    font-weight: 600;
    color: #e6e6e6;
  }
}

.detail-content {
  color: #c0c4cc;
  line-height: 1.6;
}

.detail-desc {
  margin: 0 0 16px;
}

.detail-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.empty-detail {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 100%;
  color: #606266;
  gap: 12px;
}
</style>
