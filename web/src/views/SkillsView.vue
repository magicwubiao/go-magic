<template>
  <div class="skills-view">
    <n-card title="Skills">
      <template #header-extra>
        <n-space>
          <n-button @click="refreshSkills">Refresh</n-button>
          <n-button type="primary" @click="showInstall = true">Install from Hub</n-button>
        </n-space>
      </template>

      <n-grid :cols="3" :x-gap="16" :y-gap="16">
        <n-gi v-for="skill in skills" :key="skill.name">
          <n-card hoverable @click="viewSkill(skill)">
            <template #header>
              <n-space justify="space-between">
                <span>{{ skill.name }}</span>
                <n-tag v-if="skill.official" size="small" type="success">Official</n-tag>
              </n-space>
            </template>
            <p>{{ skill.description }}</p>
            <template #footer>
              <n-space>
                <n-tag size="small">{{ skill.category }}</n-tag>
                <n-tag size="small" type="info">{{ skill.tools.length }} tools</n-tag>
              </n-space>
            </template>
          </n-card>
        </n-gi>
      </n-grid>
    </n-card>
  </div>
</template>

<script setup>
import { ref } from 'vue'

const showInstall = ref(false)

const skills = [
  { name: 'git-workflow', description: 'Git operations and workflow automation', category: 'development', official: true, tools: ['git_commit', 'git_branch'] },
  { name: 'docker-helper', description: 'Docker container management', category: 'devops', official: true, tools: ['docker_build', 'docker_run'] },
  { name: 'code-review', description: 'Automated code review', category: 'development', official: false, tools: ['review_code', 'suggest_changes'] }
]

const refreshSkills = () => console.log('Refresh skills')
const viewSkill = (skill) => console.log('View skill', skill)
</script>
