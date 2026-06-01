<template>
  <div v-if="currentGoal" class="current-goal">
    <n-card size="small" :class="{ 'goal-completed': currentGoal.status === 'completed' }">
      <n-space align="center" justify="space-between">
        <n-space align="center" :size="8">
          <n-icon :component="FlagOutline" :color="currentGoal.status === 'completed' ? '#18a058' : '#2080f0'" />
          <n-text strong style="font-size: 14px;">{{ currentGoal.title }}</n-text>
          <n-tag :type="statusType(currentGoal.status)" size="tiny">{{ currentGoal.status }}</n-tag>
        </n-space>
        <n-space :size="4">
          <n-button v-if="currentGoal.status !== 'completed'" size="tiny" type="success" @click="completeGoal">
            <template #icon><n-icon :component="CheckmarkOutline" /></template>
            {{ t('goals.completeGoal') }}
          </n-button>
          <n-button size="tiny" @click="goToGoals">
            <template #icon><n-icon :component="OpenOutline" /></template>
            {{ t('goals.details') }}
          </n-button>
        </n-space>
      </n-space>
      <n-progress
        v-if="currentGoal.status !== 'completed'"
        type="line"
        :percentage="currentGoal.progress"
        :status="currentGoal.progress === 100 ? 'success' : 'default'"
        :show-indicator="false"
        style="margin-top: 8px;"
        :height="4"
      />
    </n-card>
  </div>
  <div v-else class="current-goal-empty">
    <n-card size="small">
      <n-space align="center" justify="center" :size="8">
        <n-icon :component="FlagOutline" color="#999" />
        <n-text depth="3" style="font-size: 13px;">{{ t('goals.noActiveGoal') }}</n-text>
        <n-button size="tiny" text type="primary" @click="goToGoals">{{ t('goals.createGoal') }}</n-button>
      </n-space>
    </n-card>
  </div>
</template>

<script setup lang="ts">
import { onMounted, computed } from 'vue'
import { useRouter } from 'vue-router'
import { useMessage } from 'naive-ui'
import { FlagOutline, CheckmarkOutline, OpenOutline } from '@vicons/ionicons5'
import { useI18n } from 'vue-i18n'
import { useGoalsStore } from '@/stores/goals'

const { t } = useI18n()
const router = useRouter()
const message = useMessage()
const goalsStore = useGoalsStore()

const currentGoal = computed(() => goalsStore.currentGoal)

onMounted(() => {
  goalsStore.loadCurrentGoal()
})

function statusType(status: string) {
  const map: Record<string, string> = {
    active: 'info',
    completed: 'success',
    abandoned: 'default',
  }
  return (map[status] || 'default') as any
}

async function completeGoal() {
  if (!currentGoal.value) return
  try {
    await goalsStore.completeGoal(currentGoal.value.id)
    message.success(t('goals.goalCompleted'))
  } catch (e) {
    message.error(t('common.operationFailed'))
  }
}

function goToGoals() {
  router.push('/goals')
}
</script>

<style scoped>
.current-goal {
  margin-bottom: 12px;
}

.current-goal-empty {
  margin-bottom: 12px;
}

.goal-completed {
  opacity: 0.9;
}

/* Dark mode */
@media (prefers-color-scheme: dark) {
  .current-goal-empty .n-card {
    background: #2a2a2a !important;
  }
}
</style>
