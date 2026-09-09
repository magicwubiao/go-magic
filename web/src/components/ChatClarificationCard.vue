<template>
  <div class="chat-clarify-card" :class="cardClass">
    <!-- 顶栏：标题（左）+ 状态（右：倒计时 / 已答复 / 已超时） -->
    <div class="clarify-header">
      <span class="clarify-badge">
        <span class="clarify-badge-ico" aria-hidden="true">💬</span>
        {{ t('chat.clarifyTitle') }}
      </span>
      <span v-if="clarification.status === 'pending'" class="countdown-pill" :class="{ urgent: remainingMs < 60000 }">
        ⏳ {{ remainingDisplay }}
      </span>
      <span v-else-if="clarification.status === 'answered'" class="status-pill ok">✓ {{ t('chat.clarifyAnswered') }}</span>
      <span v-else-if="clarification.status === 'expired'" class="status-pill warn">⏱ {{ t('chat.clarifyExpired') }}</span>
      <button
        v-if="clarification.status === 'pending'"
        type="button"
        class="clarify-close"
        :title="t('chat.clarifyDismiss')"
        @click="dismiss"
      >
        ✕
      </button>
    </div>

    <!-- 问题与背景说明 -->
    <div class="clarify-question">{{ clarification.question }}</div>
    <div v-if="clarification.context" class="clarify-context">{{ clarification.context }}</div>

    <!-- 选项（纵向行，单选/多选通过 mark 形状区分） -->
    <div v-if="clarification.options.length" class="clarify-options">
      <button
        v-for="opt in clarification.options"
        :key="opt"
        type="button"
        class="option-row"
        :class="{ selected: isSelected(opt) }"
        :disabled="clarification.status !== 'pending'"
        @click="toggleOption(opt)"
      >
        <span class="option-mark" :class="{ multi: clarification.multiSelect }" aria-hidden="true">
          <span class="option-check">✓</span>
        </span>
        <span class="option-text">{{ opt }}</span>
      </button>
    </div>

    <!-- 编辑区（仅挂起 / 提交中可见） -->
    <div v-if="clarification.status === 'pending' || clarification.status === 'answering'" class="clarify-editor">
      <n-input
        v-model:value="note"
        type="textarea"
        :autosize="{ minRows: 2, maxRows: 4 }"
        size="small"
        :disabled="clarification.status !== 'pending'"
        :placeholder="t('chat.clarifyNotePlaceholder')"
        class="clarify-note"
        @keydown.enter.exact.prevent="submit"
      />
      <div class="clarify-actions">
        <span class="clarify-actions-hint">{{ selectionHint }}</span>
        <div class="clarify-actions-right">
          <n-spin v-if="clarification.status === 'answering'" size="small" />
          <n-button
            v-else
            size="small"
            type="primary"
            :disabled="!canSubmit"
            @click="submit"
          >
            {{ t('chat.clarifySubmit') }}
          </n-button>
        </div>
      </div>
    </div>

    <!-- 终态一行（已答复显示你的答复摘要；已超时提示未收到答复） -->
    <div
      v-else-if="(clarification.status === 'answered' && clarification.answerSummary) || clarification.status === 'expired'"
      class="clarify-final"
      :class="clarification.status === 'answered' ? 'ok' : 'warn'"
    >
      <span v-if="clarification.status === 'answered'" class="clarify-final-check" aria-hidden="true">✓</span>
      <span v-else aria-hidden="true">⏱</span>
      <span>{{ clarification.status === 'answered' ? clarification.answerSummary : t('chat.clarifyNoAnswer') }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useChatStore, type PendingClarificationCard } from '@/stores/chat'

const props = defineProps<{
  clarification: PendingClarificationCard
  sessionId: string
}>()

const { t } = useI18n()
const chatStore = useChatStore()
const selected = ref<string[]>([])
const note = ref('')
const submitting = ref(false)

const now = ref(Date.now())
let timer: ReturnType<typeof setInterval> | null = null

function startTimer() {
  stopTimer()
  timer = setInterval(() => {
    now.value = Date.now()
    if (props.clarification.status === 'pending' && remainingMs.value <= 0) {
      chatStore.markClarifyExpired(props.sessionId, props.clarification.id)
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
  () => props.clarification.status,
  (status) => {
    if (status === 'pending') {
      startTimer()
    } else {
      stopTimer()
    }
  },
  { immediate: true },
)

onMounted(() => {
  if (props.clarification.status === 'pending') {
    startTimer()
  }
})

onUnmounted(() => {
  stopTimer()
})

const remainingMs = computed(() => {
  void now.value
  const exp = props.clarification.expiresAt
  if (!exp) return Infinity
  return exp * 1000 - now.value
})

const remainingDisplay = computed(() => {
  const ms = remainingMs.value
  if (ms <= 0) return t('chat.clarifyExpired')
  const s = Math.floor(ms / 1000)
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  const rem = s % 60
  return `${m}m${rem}s`
})

const canSubmit = computed(() => {
  if (props.clarification.status !== 'pending') return false
  const hasChoice = selected.value.length > 0
  const hasNote = note.value.trim().length > 0
  // 无选项时必须至少填说明
  if (!props.clarification.options.length) return hasNote
  return hasChoice || hasNote
})

// 操作区左侧的轻提示：说明当前可提交方式
const selectionHint = computed(() => {
  if (!props.clarification.options.length) return t('chat.clarifyNoteOnlyHint')
  if (selected.value.length > 0) {
    return t('chat.clarifySelectedCount', { n: selected.value.length })
  }
  return props.clarification.multiSelect
    ? t('chat.clarifyMultiHint')
    : t('chat.clarifySingleHint')
})

const cardClass = computed(() => {
  if (props.clarification.status === 'answered') return 'status-answered'
  if (props.clarification.status === 'expired') return 'status-expired'
  return ''
})

function isSelected(opt: string): boolean {
  return selected.value.includes(opt)
}

function toggleOption(opt: string) {
  if (props.clarification.status !== 'pending') return
  if (props.clarification.multiSelect) {
    // 多选：可同时选中多个选项
    const idx = selected.value.indexOf(opt)
    if (idx >= 0) {
      selected.value = selected.value.filter(o => o !== opt)
    } else {
      selected.value = [...selected.value, opt]
    }
  } else {
    // 单选：点选即替换，再点同一项取消
    selected.value = selected.value[0] === opt ? [] : [opt]
  }
}

async function submit() {
  if (!canSubmit.value || submitting.value) return
  submitting.value = true
  try {
    const noteText = note.value.trim()
    await chatStore.answerChatClarify(props.sessionId, props.clarification.id, selected.value, noteText)
  } catch {
    // 错误已在 store 写入 error.value
  } finally {
    submitting.value = false
  }
}

// ✕ 关闭：取消本轮澄清等待，模型收到"用户已关闭"后自行收尾
async function dismiss() {
  if (props.clarification.status !== 'pending') return
  try {
    await chatStore.dismissChatClarify(props.sessionId, props.clarification.id)
  } catch {
    // 错误已在 store 写入 error.value
  }
}
</script>

<style scoped>
.chat-clarify-card {
  box-sizing: border-box;
  width: 100%;
  max-width: 640px;
  margin: 0 auto;
  border: 1px solid #e2deee;
  border-radius: 10px;
  padding: 12px 16px 14px;
  background: #fbfaff;
  box-shadow: 0 1px 4px rgba(124, 77, 255, 0.06);
  font-size: 13px;
  color: #333;
  animation: slideIn 0.25s ease;
}

@keyframes slideIn {
  from { opacity: 0; transform: translateY(-4px); }
  to { opacity: 1; transform: translateY(0); }
}

.chat-clarify-card.status-answered {
  border-color: #cfe3d6;
  background: #f6fcf8;
}
.chat-clarify-card.status-expired {
  border-color: #e9d9b4;
  background: #fffcf5;
}

/* ===== 顶栏 ===== */
.clarify-header {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 6px;
}

.clarify-badge {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-size: 12px;
  font-weight: 600;
  color: #6a45e0;
  letter-spacing: 0.2px;
}
.clarify-badge-ico {
  font-size: 13px;
  line-height: 1;
}

.countdown-pill,
.status-pill {
  margin-left: auto;
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  border-radius: 999px;
  padding: 2px 10px;
  font-size: 12px;
  font-weight: 500;
  font-variant-numeric: tabular-nums;
  line-height: 1.6;
}

.countdown-pill {
  color: #b26a00;
  background: #fff4dc;
  border: 1px solid #ffe3a6;
}
.countdown-pill.urgent {
  color: #d03050;
  background: #fff1f0;
  border-color: #ffccc7;
}

.status-pill.ok {
  color: #18a058;
  background: #ecf9f0;
  border: 1px solid #b7ebc5;
}
.status-pill.warn {
  color: #b26a00;
  background: #fff4dc;
  border: 1px solid #ffe3a6;
}

/* ✕ 关闭按钮：取消本轮澄清 */
.clarify-close {
  flex-shrink: 0;
  width: 22px;
  height: 22px;
  border: none;
  background: transparent;
  color: #9a9aac;
  border-radius: 6px;
  font-size: 13px;
  line-height: 1;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: background 0.15s, color 0.15s;
}
.clarify-close:hover {
  background: #efe9fa;
  color: #6a45e0;
}

/* ===== 问题 / 背景 ===== */
.clarify-question {
  color: #1f1f2b;
  font-size: 14px;
  font-weight: 600;
  line-height: 1.55;
  word-break: break-word;
}

.clarify-context {
  margin-top: 6px;
  padding: 5px 10px;
  border-left: 2px solid #d8cdf7;
  border-radius: 0 6px 6px 0;
  background: rgba(124, 77, 255, 0.06);
  color: #6a6a80;
  font-size: 12px;
  line-height: 1.5;
  word-break: break-word;
}

/* ===== 选项：纵向行 ===== */
.clarify-options {
  margin-top: 10px;
  display: grid;
  gap: 6px;
}

.option-row {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  text-align: left;
  padding: 6px 11px;
  border: 1px solid #e2dcf3;
  border-radius: 8px;
  background: #fff;
  color: #2f2f3a;
  font-size: 13px;
  line-height: 1.5;
  cursor: pointer;
  transition: border-color 0.15s, background 0.15s;
  word-break: break-word;
}
.option-row:hover:not(:disabled) {
  border-color: #a48ef0;
  background: #f7f4ff;
}
.option-row.selected {
  border-color: #7c4dff;
  background: #f0ebff;
}
.option-row:disabled {
  cursor: not-allowed;
  opacity: 0.55;
}

.option-mark {
  flex: 0 0 auto;
  width: 16px;
  height: 16px;
  border-radius: 50%;
  border: 1.5px solid #b4a9dd;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: #fff;
  transition: all 0.15s;
}
.option-mark.multi {
  border-radius: 5px;
}
.option-mark .option-check {
  font-size: 11px;
  line-height: 1;
  color: #fff;
  opacity: 0;
  transition: opacity 0.15s;
}
.option-row.selected .option-mark {
  background: #7c4dff;
  border-color: #7c4dff;
}
.option-row.selected .option-mark .option-check {
  opacity: 1;
}

.option-text {
  flex: 1;
  min-width: 0;
}

/* ===== 编辑区 ===== */
.clarify-editor {
  margin-top: 10px;
}

.clarify-note {
  width: 100%;
}

.clarify-actions {
  margin-top: 8px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.clarify-actions-hint {
  min-width: 0;
  font-size: 12px;
  color: #9a9aac;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.clarify-actions-right {
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

/* ===== 终态 ===== */
.clarify-final {
  margin-top: 10px;
  padding: 7px 12px;
  border-radius: 8px;
  font-size: 13px;
  line-height: 1.5;
  display: flex;
  align-items: flex-start;
  gap: 6px;
  word-break: break-word;
}
.clarify-final.ok {
  background: #ecf9f0;
  color: #1f7a46;
}
.clarify-final.warn {
  background: #fff4dc;
  color: #ad6800;
}

/* ===== Dark ===== */
@media (prefers-color-scheme: dark) {
  .chat-clarify-card {
    background: #1e1b29;
    border-color: #38334a;
    color: #cfcbd9;
  }
  .chat-clarify-card.status-answered {
    background: #15231a;
    border-color: #27432f;
  }
  .chat-clarify-card.status-expired {
    background: #241d12;
    border-color: #4a3a1e;
  }
  .clarify-badge { color: #b9a3f5; }
  .clarify-question { color: #e8e6ef; }
  .clarify-context {
    border-left-color: #4a3f6b;
    background: rgba(124, 77, 255, 0.12);
    color: #a09bb0;
  }
  .option-row {
    background: #272333;
    border-color: #3d3750;
    color: #e0dcec;
  }
  .option-row:hover:not(:disabled) {
    background: #2f2942;
    border-color: #7c4dff;
  }
  .option-row.selected {
    background: #332a50;
    border-color: #8b62ff;
  }
  .option-mark {
    background: #272333;
    border-color: #5b5280;
  }
  .option-row.selected .option-mark {
    background: #7c4dff;
    border-color: #7c4dff;
  }
  .clarify-actions-hint { color: #7d7890; }
  .clarify-close { color: #7d7890; }
  .clarify-close:hover { background: #332a50; color: #b9a3f5; }
  .clarify-final.ok {
    background: #17301f;
    color: #6fdc9b;
  }
  .clarify-final.warn {
    background: #33291a;
    color: #e5a94f;
  }
  .countdown-pill { color: #e5a94f; background: #33291a; border-color: #5a4522; }
  .countdown-pill.urgent { color: #f56b86; background: #3a1f24; border-color: #6b2f3c; }
  .status-pill.ok { color: #6fdc9b; background: #17301f; border-color: #245236; }
  .status-pill.warn { color: #e5a94f; background: #33291a; border-color: #5a4522; }
}
</style>
