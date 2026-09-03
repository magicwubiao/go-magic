<template>
  <!-- 工具调用组：把同一个 assistant 回合内的所有工具调用包成一个可折叠区域。 -->
  <!-- 头部：⏱ + "运行命令" + 计数 + 折叠箭头；纯用户 toggle，不受状态变化自动改回。 -->
  <!-- 展开体：每条 ToolCallCard（各自独立可折叠）。 -->
  <div v-if="tools.length > 0" class="tool-call-group">
    <button
      class="tool-call-group-header"
      type="button"
      @click="toggle"
      :aria-expanded="expanded"
    >
      <span class="tool-call-group-icon">
        <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
          <circle cx="8" cy="8" r="6.5" fill="none" stroke="currentColor" stroke-width="1.3" />
          <path d="M8 4.5V8l2.2 1.3" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" />
        </svg>
      </span>
      <span class="tool-call-group-title">{{ t('chat.runCommand') }}</span>
      <span class="tool-call-group-count">{{ tools.length }}</span>
      <span class="tool-call-group-chevron" :class="{ open: expanded }">
        <svg viewBox="0 0 16 16" width="12" height="12" aria-hidden="true">
          <path d="M4 6l4 4 4-4" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" />
        </svg>
      </span>
    </button>
    <!-- n-collapse-transition：展开/收起平滑，不跳版 -->
    <n-collapse-transition :show="expanded">
      <div class="tool-call-group-body">
        <ToolCallCard
          v-for="tc in tools"
          :key="tc.id"
          :tool="tc"
        />
      </div>
    </n-collapse-transition>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import ToolCallCard from './ToolCallCard.vue'
import type { ToolCallEvent } from '@/stores/chat'

const props = defineProps<{
  tools: ToolCallEvent[]
}>()

const { t } = useI18n()

// 默认折叠；用户手动 toggle 后保持该状态——不再因"有工具运行中"被自动展开，
// 避免"刚折叠又被自动展开"造成的交互失灵感。
const expanded = ref(false)

function toggle() {
  expanded.value = !expanded.value
}
</script>

<style scoped>
.tool-call-group {
  margin: 8px 0 10px;
}

.tool-call-group-header {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 0;
  background: transparent;
  border: none;
  cursor: pointer;
  font-size: 13px;
  color: #6b7280;
  font-weight: 500;
  user-select: none;
}
.tool-call-group-header:hover {
  color: #374151;
}

.tool-call-group-icon {
  display: inline-flex;
  align-items: center;
  color: #9ca3af;
}
.tool-call-group-header:hover .tool-call-group-icon {
  color: #6b7280;
}

.tool-call-group-title {
  color: inherit;
}

.tool-call-group-count {
  font-size: 11px;
  color: #9ca3af;
  font-family: 'SF Mono', 'Consolas', monospace;
  font-variant-numeric: tabular-nums;
}

.tool-call-group-chevron {
  display: inline-flex;
  align-items: center;
  color: #9ca3af;
  margin-left: 2px;
  transition: transform 0.18s;
}
.tool-call-group-chevron.open {
  transform: rotate(180deg);
}

.tool-call-group-body {
  margin-top: 6px;
  padding-left: 6px;
}

/* ============ 暗色模式 ============ */
@media (prefers-color-scheme: dark) {
  .tool-call-group-header { color: #a0a3ab; }
  .tool-call-group-header:hover { color: #d6d9df; }
  .tool-call-group-icon { color: #6b6e76; }
  .tool-call-group-header:hover .tool-call-group-icon { color: #a0a3ab; }
  .tool-call-group-count { color: #6b6e76; }
  .tool-call-group-chevron { color: #6b6e76; }
}
</style>
