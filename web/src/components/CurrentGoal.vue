<template>
  <div class="current-goal">
    <!-- Goal selector with dropdown arrow -->
    <div class="goal-header">
      <div class="goal-main">
        <n-icon :component="FlagOutline" :size="14" :color="currentGoal?.status === 'completed' ? '#18a058' : '#2080f0'" />
        
        <!-- Goal dropdown selector -->
        <n-dropdown 
          v-if="goalsStore.activeGoals.length > 0"
          :options="goalOptions"
          :value="currentGoal?.id"
          @select="onSelectGoal"
          trigger="click"
          size="small"
        >
          <n-button :type="currentGoal ? 'primary' : 'default'" size="small" quaternary class="goal-select-btn">
            <template #default>
              <span class="goal-title">{{ currentGoal?.title || t('goals.selectGoal') }}</span>
            </template>
            <template #suffix>
              <n-icon :component="ChevronDownOutline" :size="14" />
            </template>
          </n-button>
        </n-dropdown>
        
        <!-- No active goal case -->
        <n-text v-else depth="3" style="font-size: 13px;">
          {{ t('goals.noActiveGoal') }}
        </n-text>
      </div>

      <!-- Progress bar and percentage -->
      <div v-if="currentGoal && currentGoal.status !== 'completed'" class="goal-progress">
        <n-progress
          type="line"
          :percentage="currentGoal.progress"
          :status="currentGoal.progress === 100 ? 'success' : 'default'"
          :show-indicator="false"
          :height="4"
          class="goal-progress-bar"
        />
        <n-text class="goal-percentage">{{ currentGoal.progress }}%</n-text>
      </div>

      <!-- Action buttons -->
      <div class="goal-actions">
        <n-tag v-if="currentGoal?.status === 'completed'" type="success" size="tiny">{{ t('goals.complete') }}</n-tag>
        
        <n-button 
          v-if="currentGoal && currentGoal.status !== 'completed'" 
          size="tiny" 
          type="success" 
          quaternary 
          @click="completeGoal"
          class="action-btn"
        >
          {{ t('goals.completeGoal') }}
        </n-button>
        
        <n-button size="tiny" quaternary @click="openNewGoal" class="action-btn" title="New Goal">
          <template #icon><n-icon :component="AddOutline" :size="14" /></template>
        </n-button>
        
        <n-button size="tiny" quaternary @click="goToGoals" class="action-btn">
          {{ t('goals.details') }}
        </n-button>
        
        <n-button 
          v-if="currentGoal && sessionId" 
          size="tiny" 
          quaternary 
          @click="unlinkSession" 
          class="action-btn"
          title="Unlink"
        >
          <template #icon><n-icon :component="CloseOutline" :size="14" /></template>
        </n-button>
      </div>
    </div>
    
    <!-- New goal modal -->
    <n-modal v-model:show="showNewGoalModal" :title="t('goals.newGoal')">
      <n-card style="width: 400px; max-width: 96vw;">
        <n-form>
          <n-form-item :label="t('goals.goalTitle')">
            <n-input v-model:value="newGoalForm.title" :placeholder="t('goals.goalTitle')" />
          </n-form-item>
          <n-form-item :label="t('goals.goalDescription')">
            <n-input v-model:value="newGoalForm.description" type="textarea" :rows="3" :placeholder="t('goals.goalDescription')" />
          </n-form-item>
        </n-form>
        <template #footer>
          <n-space justify="end">
            <n-button @click="showNewGoalModal = false">{{ t('common.cancel') }}</n-button>
            <n-button type="primary" @click="createGoal" :loading="creating">{{ t('common.create') }}</n-button>
          </n-space>
        </template>
      </n-card>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useMessage } from 'naive-ui'
import { FlagOutline, AddOutline, CloseOutline, ChevronDownOutline } from '@vicons/ionicons5'
import { useI18n } from 'vue-i18n'
import { useGoalsStore } from '@/stores/goals'
import type { Goal } from '@/api/goals'

const { t } = useI18n()
const router = useRouter()
const route = useRoute()
const message = useMessage()
const goalsStore = useGoalsStore()

const showNewGoalModal = ref(false)
const creating = ref(false)
const newGoalForm = reactive({
  title: '',
  description: '',
})

const currentGoal = computed(() => goalsStore.currentGoal)
const sessionId = computed(() => route.query.session as string || '')

const goalOptions = computed(() => {
  return goalsStore.activeGoals.map((g: Goal) => ({
    label: g.title,
    key: g.id,
    progress: g.progress,
  }))
})

onMounted(() => {
  goalsStore.loadCurrentGoal()
  goalsStore.loadGoals('active')
})

watch(sessionId, async (newSessionId) => {
  if (newSessionId && currentGoal.value) {
    if (!currentGoal.value.session_ids?.includes(newSessionId)) {
      try {
        await goalsStore.linkSession(currentGoal.value.id, newSessionId)
      } catch (e) {}
    }
  }
}, { immediate: true })

async function onSelectGoal(goalId: string) {
  try {
    const goal = goalsStore.goals.find(g => g.id === goalId)
    if (goal) {
      goalsStore.setCurrentGoal(goal)
      if (sessionId.value && !goal.session_ids?.includes(sessionId.value)) {
        await goalsStore.linkSession(goalId, sessionId.value)
      }
    }
  } catch (e) {
    message.error(t('common.operationFailed'))
  }
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

function openNewGoal() {
  newGoalForm.title = ''
  newGoalForm.description = ''
  showNewGoalModal.value = true
}

async function createGoal() {
  if (!newGoalForm.title.trim()) {
    message.warning(t('goals.goalTitle'))
    return
  }
  
  creating.value = true
  try {
    const goal = await goalsStore.createGoal(newGoalForm.title, newGoalForm.description)
    goalsStore.setCurrentGoal(goal)
    if (sessionId.value) {
      await goalsStore.linkSession(goal.id, sessionId.value)
    }
    showNewGoalModal.value = false
    message.success(t('goals.created'))
  } catch (e) {
    message.error(t('common.error'))
  } finally {
    creating.value = false
  }
}

async function unlinkSession() {
  if (!currentGoal.value || !sessionId.value) return
  try {
    await goalsStore.unlinkSession(currentGoal.value.id, sessionId.value)
    message.success(t('goals.unlinkGoal'))
  } catch (e) {
    message.error(t('common.operationFailed'))
  }
}
</script>

<style scoped>
.current-goal {
  padding: 8px 16px;
  background: #fafafa;
  border-bottom: 1px solid #e8e8e8;
  display: flex;
  align-items: center;
}

.goal-header {
  display: flex;
  align-items: center;
  gap: 12px;
  width: 100%;
}

.goal-main {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

.goal-select-btn {
  padding: 4px 12px;
  min-width: 160px;
  justify-content: space-between;
}

.goal-title {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-weight: 500;
  font-size: 13px;
}

.goal-progress {
  flex: 1;
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 120px;
}

.goal-progress-bar {
  flex: 1;
  min-width: 80px;
}

.goal-percentage {
  font-size: 12px;
  font-weight: 500;
  color: #666;
  min-width: 32px;
  text-align: right;
}

.goal-actions {
  display: flex;
  align-items: center;
  gap: 4px;
  flex-shrink: 0;
}

.action-btn {
  padding: 4px 8px;
}

@media (prefers-color-scheme: dark) {
  .current-goal {
    background: #1e1e1e;
    border-bottom-color: #333;
  }
  
  .goal-percentage {
    color: #aaa;
  }
}
</style>
