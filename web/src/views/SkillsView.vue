<template>
  <div>
    <h2 style="margin-bottom: 24px;">Skills</h2>
    <n-input
      v-model:value="searchQuery"
      placeholder="Search skills..."
      style="margin-bottom: 16px;"
      @keyup.enter="doSearch"
    >
      <template #suffix>
        <n-button text @click="doSearch">
          <n-icon><search-outline /></n-icon>
        </n-button>
      </template>
    </n-input>

    <n-spin v-if="skillsStore.loading" />
    <n-list v-else>
      <n-list-item v-for="skill in skillsStore.filteredSkills" :key="skill.id">
        <n-thing :title="skill.name">
          <template #description>
            <n-space vertical>
              <n-text depth="3">{{ skill.description }}</n-text>
              <n-space>
                <n-tag size="small">{{ skill.category }}</n-tag>
                <n-tag v-for="tag in skill.tags" :key="tag" size="small" type="info">
                  {{ tag }}
                </n-tag>
              </n-space>
            </n-space>
          </template>
          <template #action>
            <n-tag :type="skill.enabled ? 'success' : 'default'">
              {{ skill.enabled ? 'Enabled' : 'Disabled' }}
            </n-tag>
          </template>
        </n-thing>
      </n-list-item>
    </n-list>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { SearchOutline } from '@vicons/ionicons5'
import { useSkillsStore } from '@/stores/skills'

const skillsStore = useSkillsStore()
const searchQuery = ref('')

function doSearch() {
  skillsStore.search(searchQuery.value)
}

onMounted(() => {
  skillsStore.loadSkills()
})
</script>
