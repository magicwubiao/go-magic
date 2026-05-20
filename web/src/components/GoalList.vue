<template>
  <div class="goal-list">
    <n-empty v-if="!goals.length" description="No goals yet" />
    <n-card
      v-for="goal in goals"
      :key="goal.id"
      size="small"
      style="margin-bottom: 12px;"
      :class="{ 'goal-completed': goal.status === 'completed' }"
    >
      <n-space vertical>
        <n-space justify="space-between" align="center">
          <n-text strong style="font-size: 16px;">{{ goal.title }}</n-text>
          <n-tag :type="statusType(goal.status)" size="small">{{ goal.status }}</n-tag>
        </n-space>
        
        <n-text depth="3">{{ goal.description }}</n-text>
        
        <n-space align="center">
          <n-progress
            type="line"
            :percentage="goal.progress"
            :status="goal.status === 'completed' ? 'success' : 'default'"
            style="width: 200px;"
          />
          <n-text style="font-size: 12px;">{{ goal.progress }}%</n-text>
        </n-space>
        
        <n-space v-if="goal.session_ids?.length">
          <n-text depth="3" style="font-size: 12px;">
            Linked to {{ goal.session_ids.length }} session(s)
          </n-text>
        </n-space>
        
        <n-space justify="end">
          <n-button v-if="goal.status !== 'completed'" size="tiny" type="success" @click="$emit('complete', goal)">
            Complete
          </n-button>
          <n-button size="tiny" @click="$emit('edit', goal)">Edit</n-button>
          <n-button size="tiny" type="error" @click="$emit('delete', goal)">Delete</n-button>
        </n-space>
      </n-space>
    </n-card>
  </div>
</template>

<script setup lang="ts">
import type { Goal } from '@/api/goals'

defineProps<{
  goals: Goal[]
}>()

defineEmits<{
  edit: [goal: Goal]
  delete: [goal: Goal]
  complete: [goal: Goal]
}>()

function statusType(status: string) {
  const map: Record<string, string> = {
    active: 'info',
    completed: 'success',
    abandoned: 'default',
  }
  return (map[status] || 'default') as any
}
</script>

<style scoped>
.goal-list {
  padding: 8px 0;
}

.goal-completed {
  opacity: 0.8;
}
</style>
