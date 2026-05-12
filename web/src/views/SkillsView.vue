<template>
  <div class="skills-view">
    <div class="view-header">
      <h2>Skills</h2>
      <n-button type="primary" @click="showCreateModal = true">
        + Add Skill
      </n-button>
    </div>

    <div class="skills-grid" v-if="skills.length > 0">
      <div
        v-for="skill in skills"
        :key="skill.name"
        class="skill-card"
        :class="{ disabled: !skill.enabled }"
      >
        <div class="skill-header">
          <div class="skill-icon">📚</div>
          <div class="skill-info">
            <div class="skill-name">{{ skill.name }}</div>
            <n-tag size="tiny" type="info">{{ skill.category }}</n-tag>
          </div>
          <n-switch
            :value="skill.enabled"
            @update:value="toggleSkill(skill, $event)"
          />
        </div>
        <div class="skill-desc">{{ skill.description }}</div>
        <div class="skill-actions">
          <n-button size="small" quaternary @click="deleteSkill(skill.name)">
            Delete
          </n-button>
        </div>
      </div>
    </div>

    <div v-else class="empty-state">
      <div class="empty-icon">📚</div>
      <p>No skills configured</p>
      <n-button @click="showCreateModal = true">Add your first skill</n-button>
    </div>

    <!-- Create Modal -->
    <n-modal v-model:show="showCreateModal" preset="card" title="Add Skill">
      <n-form>
        <n-form-item label="Name">
          <n-input v-model:value="newSkill.name" placeholder="e.g., git-workflow" />
        </n-form-item>
        <n-form-item label="Description">
          <n-input v-model:value="newSkill.description" placeholder="Skill description" />
        </n-form-item>
        <n-form-item label="Category">
          <n-input v-model:value="newSkill.category" placeholder="e.g., development" />
        </n-form-item>
      </n-form>
      <template #footer>
        <n-button @click="showCreateModal = false">Cancel</n-button>
        <n-button type="primary" @click="createSkill">Create</n-button>
      </template>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { NButton, NSwitch, NTag, NModal, NForm, NFormItem, NInput, useMessage } from 'naive-ui'
import { apiService, Skill } from '../api'

const message = useMessage()
const skills = ref<Skill[]>([])
const showCreateModal = ref(false)
const newSkill = ref({ name: '', description: '', category: '' })

onMounted(loadSkills)

async function loadSkills() {
  try {
    const response = await apiService.skills.list()
    skills.value = response.data
  } catch (err) {
    message.error('Failed to load skills')
  }
}

async function createSkill() {
  if (!newSkill.value.name || !newSkill.value.description) {
    message.warning('Please fill in required fields')
    return
  }
  
  try {
    const response = await apiService.skills.create(newSkill.value)
    skills.value.push(response.data)
    showCreateModal.value = false
    newSkill.value = { name: '', description: '', category: '' }
    message.success('Skill created')
  } catch (err) {
    message.error('Failed to create skill')
  }
}

async function toggleSkill(skill: Skill, enabled: boolean) {
  skill.enabled = enabled
  message.info(`Skill ${enabled ? 'enabled' : 'disabled'}`)
}

async function deleteSkill(name: string) {
  if (!confirm(`Delete skill "${name}"?`)) return
  
  try {
    await apiService.skills.delete(name)
    skills.value = skills.value.filter(s => s.name !== name)
    message.success('Skill deleted')
  } catch (err) {
    message.error('Failed to delete skill')
  }
}
</script>

<style scoped>
.skills-view {
  padding: 20px;
  height: 100%;
  overflow-y: auto;
}

.view-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
}

.view-header h2 {
  margin: 0;
}

.skills-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
  gap: 16px;
}

.skill-card {
  background: var(--bg-secondary);
  border-radius: 12px;
  padding: 16px;
}

.skill-card.disabled {
  opacity: 0.6;
}

.skill-header {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  margin-bottom: 12px;
}

.skill-icon {
  font-size: 24px;
}

.skill-info {
  flex: 1;
}

.skill-name {
  font-weight: 600;
  margin-bottom: 4px;
}

.skill-desc {
  font-size: 13px;
  color: var(--text-secondary);
  margin-bottom: 12px;
}

.skill-actions {
  display: flex;
  justify-content: flex-end;
}

.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 300px;
  color: var(--text-secondary);
}

.empty-icon {
  font-size: 48px;
  margin-bottom: 16px;
}
</style>
