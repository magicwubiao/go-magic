<template>
  <div class="goal-sidebar" :class="{ collapsed: isCollapsed }">
    <!-- Collapse toggle button -->
    <n-button 
      size="small" 
      quaternary 
      circle 
      class="collapse-toggle"
      @click="isCollapsed = !isCollapsed"
      :title="isCollapsed ? t('goals.expand') : t('goals.collapse')"
    >
      <template #icon>
        <n-icon :component="isCollapsed ? ChevronForwardOutline : ChevronBackOutline" :size="16" />
      </template>
    </n-button>

    <!-- Main content -->
    <div class="sidebar-content" v-show="!isCollapsed">
      <!-- Header -->
      <div class="sidebar-header">
        <n-space align="center">
          <n-icon :component="FlagOutline" :size="16" color="#2080f0" />
          <n-text strong>{{ t('goals.title') }}</n-text>
        </n-space>
        <n-button size="tiny" quaternary @click="openNewGoal">
          <template #icon><n-icon :component="AddOutline" :size="14" /></template>
        </n-button>
      </div>

      <!-- Goal list -->
      <div class="goal-list">
        <n-empty v-if="!activeGoals.length" :description="t('goals.noGoals')" size="small" />
        
        <div 
          v-for="goal in activeGoals" 
          :key="goal.id" 
          class="goal-card"
          :class="{ active: currentGoal?.id === goal.id }"
        >
          <!-- Goal header -->
          <div class="goal-card-header" @click="selectGoal(goal)">
            <n-space align="center" justify="space-between" style="width: 100%;">
              <n-text strong style="font-size: 14px; flex: 1; overflow: hidden; text-overflow: ellipsis; display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; word-break: break-all;">
                {{ goal.title }}
              </n-text>
              <n-tag :type="statusType(goal.status)" size="tiny">{{ t('goals.statusOptions.' + goal.status) }}</n-tag>
            </n-space>
          </div>

          <!-- Goal details -->
          <n-collapse-transition>
            <div v-if="expandedGoals.includes(goal.id)" class="goal-card-details">
              <!-- Description -->
              <n-text v-if="goal.description && goal.description !== goal.title" depth="3" style="font-size: 12px; display: block; margin-bottom: 8px;">
                {{ goal.description }}
              </n-text>

              <!-- Progress -->
              <div class="goal-progress">
                <n-progress
                  type="line"
                  :percentage="goal.progress"
                  :status="goal.progress === 100 ? 'success' : 'default'"
                  :show-indicator="false"
                  :height="6"
                />
                <n-text style="font-size: 11px; margin-top: 4px; display: block;">{{ goal.progress }}%</n-text>
              </div>

              <!-- Quick progress buttons -->
              <n-space :size="4" style="margin-top: 8px;" align="center">
                <n-button v-if="goal.status === 'active'" size="tiny" @click.stop="quickUpdate(goal, 25)">+25%</n-button>
                <n-button v-if="goal.status === 'active'" size="tiny" @click.stop="quickUpdate(goal, 50)">50%</n-button>
                <n-button v-if="goal.status === 'active'" size="tiny" @click.stop="quickUpdate(goal, 75)">75%</n-button>
                <n-button v-if="goal.status === 'active'" size="tiny" type="success" @click.stop="completeGoal(goal)">
                  {{ t('goals.complete') }}
                </n-button>
              </n-space>

              <!-- Linked sessions -->
              <div class="linked-sessions">
                <n-space align="center" :size="4" style="margin-bottom: 4px;">
                  <n-icon :component="ChatbubblesOutline" :size="12" />
                  <n-text depth="3" style="font-size: 11px;">
                    {{ t('goals.linkedSessions') }}: {{ goalSessions[goal.id]?.length || goal.session_ids?.length || 0 }}
                  </n-text>
                </n-space>
                
                <div v-if="sessionLoading[goal.id]" style="padding: 4px;">
                  <n-spin size="small" />
                </div>
                <div v-else-if="goalSessions[goal.id]?.length" class="session-items">
                  <div 
                    v-for="session in goalSessions[goal.id]" 
                    :key="session.id" 
                    class="session-item"
                    @click.stop="goToSession(session.id)"
                  >
                    <n-text style="font-size: 12px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; display: block;">
                      {{ session.title }}
                    </n-text>
                    <n-text depth="3" style="font-size: 10px;">{{ formatTime(session.updated_at) }}</n-text>
                  </div>
                </div>
                <div v-else class="session-items">
                  <n-text depth="3" style="font-size: 11px;">{{ t('goals.noLinkedSessions') }}</n-text>
                </div>

                <!-- Link current session button -->
                <div v-if="sessionId && !goal.session_ids?.includes(sessionId)" style="margin-top: 8px;">
                  <n-button size="tiny" type="primary" @click.stop="linkCurrentSession(goal)">
                    {{ t('goals.linkSession') }}
                  </n-button>
                </div>
              </div>

              <!-- Actions -->
              <n-space justify="end" :size="4" style="margin-top: 8px; padding-top: 8px; border-top: 1px solid #f0f0f0;">
                <n-button size="tiny" text @click.stop="goToGoalsPage">
                  {{ t('goals.details') }}
                </n-button>
                <n-button v-if="currentGoal?.id === goal.id && sessionId && goal.session_ids?.includes(sessionId)" size="tiny" text type="error" @click.stop="unlinkSession(goal)">
                  {{ t('goals.unlinkGoal') }}
                </n-button>
              </n-space>
            </div>
          </n-collapse-transition>

          <!-- Toggle button -->
          <n-button 
            size="tiny" 
            quaternary 
            circle 
            class="expand-toggle"
            @click="toggleExpand(goal.id)"
          >
            <template #icon>
              <n-icon 
                :component="expandedGoals.includes(goal.id) ? ChevronDownOutline : ChevronForwardOutline" 
                :size="12" 
              />
            </template>
          </n-button>
        </div>
      </div>
    </div>

    <!-- Collapsed indicator -->
    <div v-if="isCollapsed" class="collapsed-indicator">
      <n-icon :component="FlagOutline" :size="20" color="#2080f0" />
      <n-text style="font-size: 10px; writing-mode: vertical-rl; margin-top: 4px;">
        {{ t('goals.title') }}
      </n-text>
    </div>

    <!-- New goal modal -->
    <n-modal v-model:show="showNewGoalModal" :title="t('goals.newGoal')">
      <n-card style="width: 400px;">
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
import { ref, reactive, computed, onMounted, watch, nextTick } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useMessage } from 'naive-ui'
import { 
  FlagOutline, 
  AddOutline, 
  ChevronBackOutline, 
  ChevronForwardOutline, 
  ChevronDownOutline,
  ChatbubblesOutline
} from '@vicons/ionicons5'
import { useI18n } from 'vue-i18n'
import { useGoalsStore } from '@/stores/goals'
import { useChatStore } from '@/stores/chat'
import { useConfigStore } from '@/stores/config'
import type { Goal } from '@/api/goals'
import { getGoalSessions } from '@/api/goals'

const { t } = useI18n()
const router = useRouter()
const route = useRoute()
const message = useMessage()
const goalsStore = useGoalsStore()
const chatStore = useChatStore()
const configStore = useConfigStore()

const isCollapsed = ref(true)
const expandedGoals = ref<string[]>([])
const goalSessions = ref<Record<string, any[]>>({})
const sessionLoading = ref<Record<string, boolean>>({})

const showNewGoalModal = ref(false)
const creating = ref(false)
const newGoalForm = reactive({
  title: '',
  description: '',
})

const currentGoal = computed(() => goalsStore.currentGoal)
const activeGoals = computed(() => goalsStore.activeGoals)
const sessionId = computed(() => chatStore.activeSessionId || '')

function statusType(status: string) {
  const map: Record<string, string> = {
    active: 'info',
    completed: 'success',
    abandoned: 'default',
  }
  return (map[status] || 'default') as any
}

onMounted(() => {
  goalsStore.loadCurrentGoal()
  goalsStore.loadGoals('active')
  configStore.loadConfig()
})

// Auto-link session to current goal only if auto_link_goals is enabled
watch(sessionId, async (newSessionId) => {
  if (newSessionId && currentGoal.value && configStore.config?.auto_link_goals) {
    if (!currentGoal.value.session_ids?.includes(newSessionId)) {
      try {
        await goalsStore.linkSession(currentGoal.value.id, newSessionId)
      } catch (e) {}
    }
  }
}, { immediate: true })

async function selectGoal(goal: Goal) {
  goalsStore.currentGoal = goal
  if (sessionId.value && !goal.session_ids?.includes(sessionId.value)) {
    await goalsStore.linkSession(goal.id, sessionId.value)
  }
}

function toggleExpand(goalId: string) {
  const idx = expandedGoals.value.indexOf(goalId)
  if (idx >= 0) {
    expandedGoals.value.splice(idx, 1)
  } else {
    expandedGoals.value.push(goalId)
    loadSessionGoals(goalId)
  }
}

async function loadSessionGoals(goalId: string) {
  // 使用对象替换方式清空缓存后，等待下一个 tick 再检查
  await nextTick()
  if (goalSessions.value[goalId] !== undefined || sessionLoading.value[goalId]) return
  sessionLoading.value[goalId] = true
  try {
    const result = await getGoalSessions(goalId)
    goalSessions.value[goalId] = result.sessions || []
  } catch (e) {
    goalSessions.value[goalId] = []
  } finally {
    sessionLoading.value[goalId] = false
  }
}

async function quickUpdate(goal: Goal, progress: number) {
  try {
    await goalsStore.updateGoal(goal.id, { progress })
    message.success(t('goals.progressUpdated'))
  } catch (e) {
    message.error(t('common.operationFailed'))
  }
}

async function completeGoal(goal: Goal) {
  try {
    await goalsStore.completeGoal(goal.id)
    message.success(t('goals.goalCompleted'))
  } catch (e) {
    message.error(t('common.operationFailed'))
  }
}

function goToSession(sessionId: string) {
  router.push(`/chat?session=${sessionId}`)
}

function goToGoalsPage() {
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
    goalsStore.currentGoal = goal
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

async function unlinkSession(goal: Goal) {
  if (!sessionId.value) return
  const wasExpanded = expandedGoals.value.includes(goal.id)
  try {
    await goalsStore.unlinkSession(goal.id, sessionId.value)
    if (wasExpanded) {
      const newSessions = { ...goalSessions.value }
      delete newSessions[goal.id]
      goalSessions.value = newSessions
      await goalsStore.loadGoals('active')
      await loadSessionGoals(goal.id)
    } else {
      await goalsStore.loadGoals('active')
    }
    message.success(t('goals.unlinked'))
  } catch (e: any) {
    message.error(e?.message || t('common.operationFailed'))
  }
}

async function linkCurrentSession(goal: Goal) {
  if (!sessionId.value) return
  const wasExpanded = expandedGoals.value.includes(goal.id)
  try {
    await goalsStore.linkSession(goal.id, sessionId.value)
    // 如果目标已展开，需要刷新会话列表
    if (wasExpanded) {
      const newSessions = { ...goalSessions.value }
      delete newSessions[goal.id]
      goalSessions.value = newSessions
      await goalsStore.loadGoals('active')
      await loadSessionGoals(goal.id)
    } else {
      await goalsStore.loadGoals('active')
    }
    message.success(t('goals.sessionLinked'))
  } catch (e) {
    message.error(t('common.operationFailed'))
  }
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
.goal-sidebar {
  width: 280px;
  border-left: 1px solid #e0e0e0;
  background: #fff;
  display: flex;
  flex-direction: column;
  position: relative;
  transition: width 0.2s;
  flex-shrink: 0;
}

.goal-sidebar.collapsed {
  width: 48px;
}

.collapse-toggle {
  position: absolute;
  left: -14px;
  top: 50%;
  transform: translateY(-50%);
  z-index: 10;
  background: #fff !important;
  border: 1px solid #e0e0e0 !important;
  box-shadow: 0 2px 8px rgba(0,0,0,0.1);
}

.sidebar-content {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  padding-right: 10px;
}

.sidebar-header {
  padding: 12px;
  border-bottom: 1px solid #e0e0e0;
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.goal-list {
  flex: 1;
  overflow-y: auto;
  padding: 8px;
}

.goal-card {
  background: #fafafa;
  border-radius: 8px;
  margin-bottom: 8px;
  padding: 8px;
  cursor: pointer;
  transition: background 0.2s;
  position: relative;
  word-break: break-all;
  overflow-wrap: break-word;
}

.goal-card:hover {
  background: #f0f0f0;
}

.goal-card.active {
  background: #e8f4ff;
  border: 1px solid #2080f0;
}

.goal-card-header {
  padding: 4px;
}

.goal-card-details {
  padding: 8px 4px;
  border-top: 1px solid #e0e0e0;
  margin-top: 4px;
}

.goal-progress {
  margin-bottom: 8px;
}

.linked-sessions {
  margin-top: 8px;
  padding-top: 8px;
  border-top: 1px solid #e0e0e0;
}

.session-items {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.session-item {
  padding: 4px 6px;
  background: #fff;
  border-radius: 4px;
  cursor: pointer;
  transition: background 0.2s;
}

.session-item:hover {
  background: #e8f4ff;
}

.expand-toggle {
  position: absolute;
  right: 4px;
  top: 8px;
}

.collapsed-indicator {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 100%;
  padding: 8px;
}

@media (prefers-color-scheme: dark) {
  .goal-sidebar {
    background: #1e1e1e;
    border-left-color: #333;
  }
  
  .collapse-toggle {
    background: #1e1e1e !important;
    border-color: #333 !important;
  }
  
  .goal-card {
    background: #252525;
  }
  
  .goal-card:hover {
    background: #2d2d2d;
  }
  
  .goal-card.active {
    background: #1a3a5c;
    border-color: #2080f0;
  }
  
  .session-item {
    background: #1e1e1e;
  }
  
  .session-item:hover {
    background: #2a4a6c;
  }
  
  .sidebar-header {
    border-bottom-color: #333;
  }
  
  .goal-card-details {
    border-top-color: #333;
  }
  
  .linked-sessions {
    border-top-color: #333;
  }
}
</style>
