<template>
  <div>
    <n-space justify="space-between" style="margin-bottom: 16px;">
      <h2>{{ t('goals.title') }}</h2>
      <n-space>
        <n-button :loading="loading" @click="loadGoals()"><template #icon><n-icon><RefreshOutline /></n-icon></template></n-button>
        <n-button type="primary" @click="openAddGoal">+ {{ t('goals.newGoal') }}</n-button>
      </n-space>
    </n-space>

    <n-tabs type="line" animated>
      <n-tab-pane name="active" :tab="t('common.active')">
        <GoalList :goals="goalsStore.activeGoals" @edit="openEditGoal" @delete="deleteGoal" @complete="completeGoal" @abandon="abandonGoal" @update-progress="updateProgress" />
      </n-tab-pane>
      <n-tab-pane name="completed" :tab="t('common.completed')">
        <GoalList :goals="goalsStore.completedGoals" @edit="openEditGoal" @delete="deleteGoal" />
      </n-tab-pane>
      <n-tab-pane name="abandoned" :tab="t('goals.abandoned')">
        <GoalList :goals="goalsStore.abandonedGoals" @edit="openEditGoal" @delete="deleteGoal" @reactivate="reactivateGoal" />
      </n-tab-pane>
      <n-tab-pane name="all" :tab="t('common.all')">
        <GoalList :goals="goalsStore.goals" @edit="openEditGoal" @delete="deleteGoal" @complete="completeGoal" @abandon="abandonGoal" @update-progress="updateProgress" />
      </n-tab-pane>
    </n-tabs>

    <!-- Add/Edit Goal Modal -->
    <n-modal v-model:show="showGoalModal" :title="editingGoal ? t('goals.editGoal') : t('goals.newGoal')">
      <n-card style="width: 500px;">
        <n-form>
          <n-form-item :label="t('goals.goalTitle')">
            <n-input v-model:value="goalForm.title" :placeholder="t('goals.goalTitle')" />
          </n-form-item>
          <n-form-item :label="t('goals.goalDescription')">
            <n-input v-model:value="goalForm.description" type="textarea" :rows="4" :placeholder="t('goals.goalDescription')" />
          </n-form-item>
          <n-form-item :label="t('goals.progress')" v-if="editingGoal">
            <n-slider v-model:value="goalForm.progress" :min="0" :max="100" :step="5" />
            <n-text>{{ goalForm.progress }}%</n-text>
          </n-form-item>
        </n-form>
        <template #footer>
          <n-space justify="end">
            <n-button @click="showGoalModal = false">{{ t('common.cancel') }}</n-button>
            <n-button type="primary" @click="saveGoal">{{ editingGoal ? t('common.save') : t('common.create') }}</n-button>
          </n-space>
        </template>
      </n-card>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { useMessage } from 'naive-ui'
import { RefreshOutline } from '@vicons/ionicons5'
import { useI18n } from 'vue-i18n'
import { useGoalsStore } from '@/stores/goals'
import GoalList from '@/components/GoalList.vue'
import type { Goal } from '@/api/goals'

const { t } = useI18n()
const message = useMessage()
const goalsStore = useGoalsStore()
const showGoalModal = ref(false)
const editingGoal = ref<Goal | null>(null)

const goalForm = reactive({
  title: '',
  description: '',
  progress: 0,
})

function openAddGoal() {
  editingGoal.value = null
  goalForm.title = ''
  goalForm.description = ''
  goalForm.progress = 0
  showGoalModal.value = true
}

function openEditGoal(goal: Goal) {
  editingGoal.value = goal
  goalForm.title = goal.title
  goalForm.description = goal.description
  goalForm.progress = goal.progress
  showGoalModal.value = true
}

async function saveGoal() {
  if (!goalForm.title.trim()) {
    message.warning(t('kanban.enterTitle'))
    return
  }

  try {
    if (editingGoal.value) {
      await goalsStore.updateGoal(editingGoal.value.id, {
        title: goalForm.title,
        description: goalForm.description,
        progress: goalForm.progress,
      })
      message.success(t('goals.updated'))
    } else {
      await goalsStore.createGoal(goalForm.title, goalForm.description)
      message.success(t('goals.created'))
    }
    showGoalModal.value = false
  } catch (e) {
    message.error(t('common.error'))
  }
}

async function deleteGoal(goal: Goal) {
  try {
    await goalsStore.deleteGoal(goal.id)
    message.success(t('goals.deleted'))
  } catch (e) {
    message.error(t('common.error'))
  }
}

async function completeGoal(goal: Goal) {
  try {
    await goalsStore.completeGoal(goal.id)
    message.success(t('goals.completed'))
  } catch (e) {
    message.error(t('common.error'))
  }
}

async function abandonGoal(goal: Goal) {
  try {
    await goalsStore.abandonGoal(goal.id)
    message.success(t('goals.abandoned'))
  } catch (e) {
    message.error(t('common.error'))
  }
}

async function reactivateGoal(goal: Goal) {
  try {
    await goalsStore.reactivateGoal(goal.id)
    message.success(t('goals.reactivated'))
  } catch (e) {
    message.error(t('common.error'))
  }
}

async function updateProgress(goal: Goal, progress: number) {
  try {
    await goalsStore.updateGoal(goal.id, { progress })
    message.success(t('goals.progressUpdated', { progress }))
  } catch (e) {
    message.error(t('common.error'))
  }
}

onMounted(() => goalsStore.loadGoals())
</script>