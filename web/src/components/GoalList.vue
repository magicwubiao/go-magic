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
            <template v-if="goal.status === 'active'">
              <n-button-group size="tiny">
                <n-button @click="quickUpdate(goal, 25)">+25%</n-button>
                <n-button @click="quickUpdate(goal, 50)">50%</n-button>
                <n-button @click="quickUpdate(goal, 75)">75%</n-button>
                <n-button type="warning" @click="$emit('abandon', goal)">
                  {{ t('goals.abandon') }}
                </n-button>
                <n-button type="success" @click="$emit('complete', goal)">
                  <template #icon><n-icon :component="CheckmarkOutline" /></template>
                </n-button>
              </n-button-group>
            </template>
            <template v-else-if="goal.status === 'abandoned'">
              <n-button type="success" size="tiny" @click="$emit('reactivate', goal)">
                {{ t('goals.reactivate') }}
              </n-button>
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

        <!-- Linked Sessions with details -->
        <div v-if="goal.session_ids?.length" class="linked-sessions">
          <n-space align="center" :size="4" style="margin-bottom: 8px;">
            <n-icon :component="ChatbubblesOutline" :size="14" />
            <n-text depth="3" style="font-size: 12px;">
              {{ t('goals.linkedSessions') }}: {{ goal.session_ids.length }}
            </n-text>
            <n-button size="tiny" quaternary @click="toggleSessions(goal.id)">
              {{ expandedGoals.includes(goal.id) ? t('common.hide') : t('common.show') }}
            </n-button>
          </n-space>
          
          <n-collapse-transition :show="expandedGoals.includes(goal.id)">
            <div v-if="sessionsLoading[goal.id]" style="padding: 8px;">
              <n-spin size="small" />
            </div>
            <div v-else-if="goalSessions[goal.id]?.length" class="session-list">
              <div 
                v-for="session in goalSessions[goal.id]" 
                :key="session.id" 
                class="session-item"
                @click="goToSession(session.id)"
              >
                <n-space align="center" justify="space-between">
                  <n-space align="center" :size="8">
                    <n-tag v-if="session.is_active" type="success" size="tiny">{{ t('common.active') }}</n-tag>
                    <n-text style="font-size: 13px; max-width: 200px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">
                      {{ session.title }}
                    </n-text>
                  </n-space>
                  <n-text depth="3" style="font-size: 11px;">
                    {{ formatTime(session.updated_at) }}
                  </n-text>
                </n-space>
                <n-text depth="3" style="font-size: 12px; margin-top: 4px; display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 100%;">
                  {{ session.preview || t('goals.noPreview') }}
                </n-text>
              </div>
            </div>
            <n-empty v-else size="small" :description="t('goals.noLinkedSessions')" />
          </n-collapse-transition>
        </div>

        <n-space justify="end">
          <n-button size="tiny" @click="$emit('edit', goal)">{{ t('common.edit') }}</n-button>
          <n-button size="tiny" type="error" @click="$emit('delete', goal)">{{ t('common.delete') }}</n-button>
        </n-space>
      </n-space>
    </n-card>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { useMessage } from 'naive-ui'
import { CheckmarkOutline, CreateOutline, SparklesOutline, ChatbubblesOutline } from '@vicons/ionicons5'
import type { Goal, GoalSession } from '@/api/goals'
import { getGoalSessions } from '@/api/goals'
import { useGoalsStore } from '@/stores/goals'

const { t } = useI18n()
const router = useRouter()
const message = useMessage()
const goalsStore = useGoalsStore()
const decomposing = ref<string | null>(null)
const expandedGoals = ref<string[]>([])
const goalSessions = ref<Record<string, GoalSession[]>>({})
const sessionsLoading = ref<Record<string, boolean>>({})

const props = defineProps<{
  goals: Goal[]
}>()

const emit = defineEmits<{
  edit: [goal: Goal]
  delete: [goal: Goal]
  complete: [goal: Goal]
  abandon: [goal: Goal]
  reactivate: [goal: Goal]
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
    if (e?.name === 'AbortError' || e?.message?.includes('aborted')) return
    message.error(e?.message || t('goals.decomposeFailed'))
  } finally {
    decomposing.value = null
  }
}

async function toggleSessions(goalId: string) {
  const idx = expandedGoals.value.indexOf(goalId)
  if (idx >= 0) {
    expandedGoals.value.splice(idx, 1)
  } else {
    expandedGoals.value.push(goalId)
    if (!goalSessions.value[goalId] && !sessionsLoading.value[goalId]) {
      sessionsLoading.value[goalId] = true
      try {
        const result = await getGoalSessions(goalId)
        goalSessions.value[goalId] = result.sessions || []
      } catch (e) {
        goalSessions.value[goalId] = []
      } finally {
        sessionsLoading.value[goalId] = false
      }
    }
  }
}

function goToSession(sessionId: string) {
  router.push(`/chat?session=${sessionId}`)
}

function formatTime(timestamp: number): string {
  const date = new Date(timestamp * 1000)
  const now = new Date()
  const diffMs = now.getTime() - date.getTime()
  const diffDays = Math.floor(diffMs / (1000 * 60 * 60 * 24))
  
  if (diffDays === 0) {
    return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  } else if (diffDays === 1) {
    return t('common.yesterday')
  } else if (diffDays < 7) {
    return t('common.daysAgo', { count: diffDays })
  } else {
    return date.toLocaleDateString([], { month: 'short', day: 'numeric' })
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

.linked-sessions {
  margin-top: 8px;
  padding: 8px 12px;
  background: #f5f5f5;
  border-radius: 6px;
}

.session-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.session-item {
  padding: 8px;
  background: #fff;
  border-radius: 4px;
  cursor: pointer;
  transition: background 0.2s;
}

.session-item:hover {
  background: #e8f4ff;
}

@media (prefers-color-scheme: dark) {
  .linked-sessions {
    background: #252525;
  }
  
  .session-item {
    background: #333;
  }
  
  .session-item:hover {
    background: #3a3a4a;
  }
}
</style>