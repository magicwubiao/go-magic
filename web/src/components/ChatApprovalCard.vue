<template>
  <div class="chat-approval-card" :class="cardClass">
    <!-- 单行：风险点 + 命令 + 倒计时 + 详情按钮 + 操作按钮 -->
    <div class="card-main">
      <span
        class="risk-dot"
        :class="`risk-${riskLevelKey(approval.riskLevel)}`"
        :title="t(`approval.riskLevels.${riskLevelKey(approval.riskLevel)}`)"
      />
      <pre class="command-text" :title="approval.command">{{ sanitizeCommand(approval.command) }}</pre>

      <div class="card-right">
        <!-- 倒计时 -->
        <span
          v-if="approval.status === 'pending' && remainingMs > 0"
          class="countdown"
          :class="{ urgent: remainingMs < 30000 }"
        >{{ remainingDisplay }}</span>
        <!-- 终态标签 -->
        <span v-else-if="approval.status === 'approved'" class="status-tag status-approved">✓</span>
        <span v-else-if="approval.status === 'denied'" class="status-tag status-denied">✗</span>
        <span v-else-if="approval.status === 'expired'" class="status-tag status-expired">⏱</span>

        <!-- 详情按钮：仅当有详情时显示 -->
        <button
          v-if="hasDetails && approval.status === 'pending'"
          type="button"
          class="icon-btn detail-btn"
          :class="{ active: showDetails }"
          :title="t('approval.pending.context')"
          @click="showDetails = !showDetails"
        >ⓘ</button>

        <!-- 操作按钮（仅 pending） -->
        <template v-if="approval.status === 'pending'">
          <button
            type="button"
            class="icon-btn deny-btn"
            :disabled="submitting"
            :title="t('approval.pending.deny')"
            @click="handleResolve(false)"
          >✗</button>
          <button
            type="button"
            class="icon-btn approve-btn"
            :disabled="submitting"
            :title="t('approval.pending.approve')"
            @click="handleResolve(true)"
          >✓</button>
        </template>
        <!-- 处理中 -->
        <n-spin v-else-if="approval.status === 'approving' || approval.status === 'denying'" size="tiny" />
      </div>
    </div>

    <!-- 详情：点击图标按钮才显示，默认完全不占位 -->
    <div v-if="showDetails && hasDetails" class="card-details">
      <div v-if="approval.reason" class="detail-row">
        <span class="detail-label">{{ t('approval.pending.reason') }}</span>
        <span class="detail-value">{{ approval.reason }}</span>
      </div>
      <div v-if="approval.workDir" class="detail-row">
        <span class="detail-label">{{ t('approval.pending.workDir') }}</span>
        <code class="detail-value">{{ approval.workDir }}</code>
      </div>
      <pre v-if="approval.context" class="context-text">{{ approval.context }}</pre>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useChatStore, type PendingApprovalCard } from '@/stores/chat'

const props = defineProps<{
  approval: PendingApprovalCard
  sessionId: string
}>()

const { t } = useI18n()
const chatStore = useChatStore()
const submitting = ref(false)
const showDetails = ref(false)

// 倒计时：每秒刷新 remainingMs
const now = ref(Date.now())
let timer: ReturnType<typeof setInterval> | null = null

function startTimer() {
  stopTimer()
  timer = setInterval(() => {
    now.value = Date.now()
    if (props.approval.status === 'pending' && remainingMs.value <= 0) {
      chatStore.markApprovalExpired(props.sessionId, props.approval.id)
      stopTimer()
    }
  }, 1000)
}

function stopTimer() {
  if (timer) {
    clearInterval(timer)
    timer = null
  }
}

watch(
  () => props.approval.status,
  (status) => {
    if (status === 'pending') {
      startTimer()
    } else {
      stopTimer()
      // 终态时收起详情，节省空间
      showDetails.value = false
    }
  },
  { immediate: true },
)

onMounted(() => {
  if (props.approval.status === 'pending') {
    startTimer()
  }
})

onUnmounted(() => {
  stopTimer()
})

const remainingMs = computed(() => {
  void now.value
  const exp = props.approval.expiresAt
  if (!exp) return Infinity
  return exp * 1000 - now.value
})

const remainingDisplay = computed(() => {
  const ms = remainingMs.value
  if (ms <= 0) return t('approval.pending.expired')
  const s = Math.floor(ms / 1000)
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  const rem = s % 60
  return `${m}m${rem}s`
})

const hasDetails = computed(() => !!(props.approval.reason || props.approval.workDir || props.approval.context))

const cardClass = computed(() => {
  if (props.approval.status === 'approved') return 'status-approved'
  if (props.approval.status === 'denied') return 'status-denied'
  if (props.approval.status === 'expired') return 'status-expired'
  const risk = riskWeight(props.approval.riskLevel)
  if (risk >= 3) return 'risk-high'
  if (risk === 2) return 'risk-medium'
  if (risk === 1) return 'risk-low'
  return 'risk-medium'
})

function riskWeight(level: string): number {
  const map: Record<string, number> = {
    critical: 4, high: 3, medium: 2, low: 1,
    '4': 4, '3': 3, '2': 2, '1': 1, '0': 1,
  }
  return map[String(level)] || 0
}

function riskLevelKey(level: string | number): string {
  const map: Record<string, string> = {
    low: 'low', medium: 'medium', high: 'high', critical: 'critical',
    '0': 'low', '1': 'low', '2': 'medium', '3': 'high', '4': 'critical',
  }
  return map[String(level)] || 'medium'
}

function sanitizeCommand(cmd: string): string {
  if (!cmd) return ''
  let result = cmd
  result = result.replace(/Bearer\s+[\w.\-]+/gi, 'Bearer ****')
  result = result.replace(/(api[_-]?key|token|secret|password|passwd)(\s*[=:]\s*)["']?[\w.\-]+["']?/gi, '$1$2****')
  result = result.replace(/AKIA[0-9A-Z]{16}/g, 'AKIA****')
  result = result.replace(/-----BEGIN[\s\S]*?-----END[A-Z\s]+KEY-----/g, '[REDACTED KEY]')
  return result
}

async function handleResolve(approved: boolean) {
  if (submitting.value) return
  submitting.value = true
  try {
    await chatStore.resolveChatApproval(props.sessionId, props.approval.id, approved)
  } catch {
    // 错误已在 store 中写入 error.value，此处静默
  } finally {
    submitting.value = false
  }
}
</script>

<style scoped>
.chat-approval-card {
  border: 1px solid #e0e0e0;
  border-left: 3px solid #2080f0;
  border-radius: 6px;
  padding: 8px 12px;
  margin: 0;
  background: #fafbfc;
  font-size: 13px;
  animation: slideIn 0.25s ease;
}

@keyframes slideIn {
  from { opacity: 0; transform: translateY(-4px); }
  to { opacity: 1; transform: translateY(0); }
}

.chat-approval-card.risk-high {
  border-left-color: #d03050;
  background: #fff5f5;
}
.chat-approval-card.risk-medium {
  border-left-color: #f0a020;
  background: #fffbeb;
}
.chat-approval-card.risk-low {
  border-left-color: #18a058;
}
.chat-approval-card.status-approved {
  border-left-color: #18a058;
  background: #f0fdf4;
  opacity: 0.85;
}
.chat-approval-card.status-denied {
  border-left-color: #d03050;
  background: #fef2f2;
  opacity: 0.85;
}
.chat-approval-card.status-expired {
  border-left-color: #f0a020;
  background: #fffbeb;
  opacity: 0.7;
}

/* 单行布局：风险点 + 命令 + 右侧操作 */
.card-main {
  display: flex;
  align-items: center;
  gap: 10px;
  min-height: 28px;
}

/* 风险等级用色点替代 Tag，节省大量横向空间 */
.risk-dot {
  width: 9px;
  height: 9px;
  border-radius: 50%;
  flex-shrink: 0;
  background: #2080f0;
}
.risk-dot.risk-low { background: #18a058; }
.risk-dot.risk-medium { background: #f0a020; }
.risk-dot.risk-high { background: #d03050; }
.risk-dot.risk-critical { background: #d03050; box-shadow: 0 0 0 2px rgba(208, 48, 80, 0.25); }

.command-text {
  margin: 0;
  font-family: 'SF Mono', 'Fira Code', 'Consolas', monospace;
  font-size: 13px;
  color: #333;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  min-width: 0;
  flex: 1;
  line-height: 1.5;
}

.card-right {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

.countdown {
  font-size: 12px;
  color: #f0a020;
  font-variant-numeric: tabular-nums;
  line-height: 1;
}
.countdown.urgent {
  color: #d03050;
  font-weight: 600;
}

.status-tag {
  font-size: 15px;
  line-height: 1;
}
.status-tag.status-approved { color: #18a058; }
.status-tag.status-denied { color: #d03050; }
.status-tag.status-expired { color: #f0a020; }

/* 通用图标按钮：极简，无边框 */
.icon-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 26px;
  height: 26px;
  padding: 0;
  border: none;
  border-radius: 4px;
  background: transparent;
  cursor: pointer;
  font-size: 15px;
  line-height: 1;
  color: #666;
  transition: background 0.15s, color 0.15s;
}
.icon-btn:hover:not(:disabled) {
  background: rgba(0, 0, 0, 0.06);
}
.icon-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.detail-btn.active {
  background: rgba(32, 128, 240, 0.12);
  color: #2080f0;
}

.deny-btn {
  color: #d03050;
}
.deny-btn:hover:not(:disabled) {
  background: rgba(208, 48, 80, 0.12);
}

.approve-btn {
  color: #18a058;
}
.approve-btn:hover:not(:disabled) {
  background: rgba(24, 160, 88, 0.12);
}

/* 详情面板：仅展开时存在 */
.card-details {
  margin-top: 8px;
  padding-top: 8px;
  border-top: 1px dashed #e0e0e0;
  font-size: 12px;
}

.detail-row {
  display: flex;
  gap: 8px;
  margin-bottom: 4px;
}

.detail-label {
  color: #999;
  flex-shrink: 0;
}

.detail-value {
  color: #555;
  word-break: break-all;
}

.context-text {
  margin: 6px 0 0;
  padding: 8px 10px;
  background: #1e1e1e;
  border-radius: 4px;
  font-family: 'SF Mono', 'Fira Code', 'Consolas', monospace;
  font-size: 12px;
  color: #d4d4d4;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 160px;
  overflow-y: auto;
  line-height: 1.5;
}

@media (prefers-color-scheme: dark) {
  .chat-approval-card {
    background: #1e1e1e;
    border-color: #333;
  }
  .chat-approval-card.risk-high { background: #2a1518; }
  .chat-approval-card.risk-medium { background: #2a2014; }
  .chat-approval-card.risk-low { background: #16241a; }
  .chat-approval-card.status-approved { background: #16241a; }
  .chat-approval-card.status-denied { background: #2a1518; }
  .chat-approval-card.status-expired { background: #2a2014; }
  .command-text { color: #ddd; }
  .icon-btn { color: #aaa; }
  .icon-btn:hover:not(:disabled) { background: rgba(255, 255, 255, 0.08); }
  .detail-btn.active { background: rgba(32, 128, 240, 0.2); }
  .card-details { border-top-color: #333; }
  .detail-label { color: #777; }
  .detail-value { color: #bbb; }
}
</style>
