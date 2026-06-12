<template>
  <div v-if="currentGoal" class="current-goal">
    <n-space align="center" :size="8" wrap="false">
      <n-icon :component="FlagOutline" :size="14" :color="currentGoal.status === 'completed' ? '#18a058' : '#2080f0'" />
      <n-text strong style="font-size: 13px; max-width: 500px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">{{ currentGoal.title }}</n-text>
      <n-tag v-if="currentGoal.status === 'completed'" type="success" size="tiny">{{ t('goals.complete') }}</n-tag>
      <n-progress
        v-if="currentGoal.status !== 'completed'"
        type="line"
        :percentage="currentGoal.progress"
        :status="currentGoal.progress === 100 ? 'success' : 'default'"
        :show-indicator="false"
        :height="3"
        style="flex: 1; min-width: 80px;"
      />
      <n-button v-if="currentGoal.status !== 'completed'" size="tiny" type="success" quaternary @click="completeGoal">
        {{ t('goals.completeGoal') }}
      </n-button>
      <n-button size="tiny" quaternary @click="goToGoals">{{ t('goals.details') }}</n-button>
    </n-space>
  </div>
</template>

<script setup lang="ts">
import { onMounted, computed } from 'vue'
import { useRouter } from 'vue-router'
import { useMessage } from 'naive-ui'
import { FlagOutline } from '@vicons/ionicons5'
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
  padding: 6px 12px;
  background: #f9f9f9;
  border-bottom: 1px solid #eee;
  font-size: 13px;
}

@media (prefers-color-scheme: dark) {
  .current-goal {
    background: #1e1e1e;
    border-bottom-color: #333;
  }
}
</style>