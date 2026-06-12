<template>
  <div class="goal-list">
    <n-empty v-if="!goals.length" :description="t('goals.noGoals')" />
    <n-card
      v-for="goal in goals"
      :key="goal.id"
      size="small"
      style="margin-bottom: 12px;"
      :class="{ 'goal-completed': goal.status === 'completed' }"
    >
      <n-space vertical>
        <n-space justify="space-between" align="center">
          <n-space align="center" :size="8">
            <n-text strong style="font-size: 16px;">{{ goal.title }}</n-text>
            <n-tag :type="statusType(goal.status)" size="small">{{ t('goals.statusOptions.' + goal.status) }}</n-tag>
          </n-space>
          <n-space :size="4">
            <!-- Quick progress buttons -->
            <template v-if="goal.status !== 'completed'">
              <n-button-group size="tiny">
                <n-button @click="quickUpdate(goal, 25)">+25%</n-button>
                <n-button @click="quickUpdate(goal, 50)">50%</n-button>
                <n-button @click="quickUpdate(goal, 75)">75%</n-button>
                <n-button type="success" @click="$emit('complete', goal)">
                  <template #icon><n-icon :component="CheckmarkOutline" /></template>
                </n-button>
              </n-button-group>
            </template>
          </n-space>
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
          <n-button v-if="goal.status !== 'completed'" size="tiny" text @click="$emit('edit', goal)">
            <template #icon><n-icon :component="CreateOutline" /></template>
            {{ t('goals.adjust') }}
          </n-button>
          <n-button v-if="goal.status !== 'completed'" size="tiny" type="primary" quaternary @click="onDecompose(goal)" :loading="decomposing === goal.id">
            <template #icon><n-icon :component="SparklesOutline" /></template>
            {{ t('goals.decompose') }}
          </n-button>
        </n-space>

        <n-space v-if="goal.session_ids?.length">
          <n-text depth="3" style="font-size: 12px;">
            {{ t('goals.linkedSessions') }}: {{ goal.session_ids.length }}
          </n-text>
        </n-space>

        <n-space justify="end">
          <n-button size="tiny" @click="$emit('edit', goal)">{{ t('common.edit') }}</n-button>
          <n-button size="tiny" type="error" @click="$emit('delete', goal)">{{ t('common.delete') }}</n-button>
        </n-space>
      </n-space>
    </n-card>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useMessage } from 'naive-ui'
import { CheckmarkOutline, CreateOutline, SparklesOutline } from '@vicons/ionicons5'
import type { Goal } from '@/api/goals'
import { useGoalsStore } from '@/stores/goals'

const { t } = useI18n()
const message = useMessage()
const goalsStore = useGoalsStore()
const decomposing = ref<string | null>(null)

const props = defineProps<{
  goals: Goal[]
}>()

const emit = defineEmits<{
  edit: [goal: Goal]
  delete: [goal: Goal]
  complete: [goal: Goal]
  updateProgress: [goal: Goal, progress: number]
}>()

function statusType(status: string) {
  const map: Record<string, string> = {
    active: 'info',
    completed: 'success',
    abandoned: 'default',
  }
  return (map[status] || 'default') as any
}

function quickUpdate(goal: Goal, progress: number) {
  emit('updateProgress', goal, progress)
}

async function onDecompose(goal: Goal) {
  decomposing.value = goal.id
  try {
    const result = await goalsStore.decomposeGoal(goal.id)
    message.success(t('goals.decomposeSuccess', { count: result.count }))
  } catch (e: any) {
    message.error(e?.message || t('goals.decomposeFailed'))
  } finally {
    decomposing.value = null
  }
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