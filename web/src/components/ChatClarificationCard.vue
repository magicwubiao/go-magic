<template>
  <!-- 澄清卡片：紧凑单列布局。
       结构精简原则：无独立顶栏行——问题文字占主体，倒计时/状态/关闭悬浮其右；
       补充说明为单行输入（回车即提交），无操作提示行。 -->
  <div class="chat-clarify-card" :class="cardClass">
    <!-- 行1：问题 + 右侧状态（倒计时 / ✕） -->
    <div class="clarify-qrow">
      <span class="clarify-question">{{ clarification.question }}</span>
      <span v-if="clarification.status === 'pending'" class="countdown-pill" :class="{ urgent: remainingMs < 60000 }">
        ⏳ {{ remainingDisplay }}
      </span>
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

    <!-- 背景说明（可选，紧凑引用行） -->
    <div v-if="clarification.context" class="clarify-context">{{ clarification.context }}</div>

    <!-- 选项（纵向紧凑行，单选/多选通过 mark 形状区分） -->
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

    <!-- 单行补充说明 + 提交（仅挂起 / 提交中可见） -->
    <div v-if="clarification.status === 'pending' || clarification.status === 'answering'" class="clarify-compose">
      <n-input
        v-model:value="note"
        size="small"
        round
        :disabled="clarification.status !== 'pending'"
        :placeholder="t('chat.clarifyNotePlaceholder')"
        class="clarify-note"
        @keydown.enter.exact.prevent="submit"
      />
      <n-spin v-if="clarification.status === 'answering'" size="small" />
      <n-button
        v-else
        size="small"
        circle
        type="primary"
        :title="t('chat.clarifySubmit')"
        :disabled="!canSubmit"
        @click="submit"
      >
        <template #icon>
          <span class="clarify-send-ico" aria-hidden="true">➤</span>
        </template>
      </n-button>
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
  max-width: 560px;
  margin: 0 auto;
  border: 1px solid #e2deee;
  border-radius: 10px;
  padding: 10px 12px 11px;
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

/* ===== 行1：问题 + 状态 ===== */
.clarify-qrow {
  display: flex;
  align-items: flex-start;
  gap: 8px;
}

.clarify-question {
  flex: 1;
  min-width: 0;
  color: #1f1f2b;
  font-size: 13.5px;
  font-weight: 600;
  line-height: 1.5;
  word-break: break-word;
}

.countdown-pill {
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  align-self: center;
  border-radius: 999px;
  padding: 1px 8px;
  font-size: 11.5px;
  font-weight: 500;
  font-variant-numeric: tabular-nums;
  line-height: 1.6;
  color: #b26a00;
  background: #fff4dc;
  border: 1px solid #ffe3a6;
}
.countdown-pill.urgent {
  color: #d03050;
  background: #fff1f0;
  border-color: #ffccc7;
}

/* ✕ 关闭按钮：取消本轮澄清 */
.clarify-close {
  flex-shrink: 0;
  width: 20px;
  height: 20px;
  border: none;
  background: transparent;
  color: #9a9aac;
  border-radius: 6px;
  font-size: 12px;
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

/* ===== 背景说明 ===== */
.clarify-context {
  margin-top: 5px;
  padding: 3px 9px;
  border-left: 2px solid #d8cdf7;
  border-radius: 0 6px 6px 0;
  background: rgba(124, 77, 255, 0.06);
  color: #6a6a80;
  font-size: 12px;
  line-height: 1.5;
  word-break: break-word;
}

/* ===== 选项：纵向紧凑行 ===== */
.clarify-options {
  margin-top: 7px;
  display: grid;
  gap: 4px;
}

.option-row {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  text-align: left;
  padding: 4px 10px;
  border: 1px solid #e2dcf3;
  border-radius: 7px;
  background: #fff;
  color: #2f2f3a;
  font-size: 12.5px;
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
  width: 15px;
  height: 15px;
  border-radius: 50%;
  border: 1.5px solid #b4a9dd;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: #fff;
  transition: all 0.15s;
}
.option-mark.multi {
  border-radius: 4px;
}
.option-mark .option-check {
  font-size: 10px;
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

/* ===== 单行补充说明 + 提交 ===== */
.clarify-compose {
  margin-top: 7px;
  display: flex;
  align-items: center;
  gap: 6px;
}

.clarify-note {
  flex: 1;
  min-width: 0;
}

.clarify-send-ico {
  font-size: 12px;
  line-height: 1;
  display: inline-block;
  transform: translateY(-1px);
}

/* ===== 终态 ===== */
.clarify-final {
  margin-top: 7px;
  padding: 5px 10px;
  border-radius: 7px;
  font-size: 12.5px;
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
}
</style>
