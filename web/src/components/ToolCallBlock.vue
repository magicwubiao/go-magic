<template>
  <div class="tool-call-compact" :class="[`status-${toolCall.status}`]">
    <span class="tool-emoji">{{ emoji }}</span>
    <span class="tool-name">{{ toolCall.name }}</span>
    <span v-if="toolCall.status === 'running'" class="tool-indicator running"></span>
    <span v-else-if="toolCall.status === 'completed'" class="tool-indicator completed">{{ toolCall.duration }}</span>
    <span v-else class="tool-indicator error">!</span>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { ToolCallEvent } from '@/stores/chat'

const props = defineProps<{
  toolCall: ToolCallEvent
}>()

const emojiMap: Record<string, string> = {
  web_search: '🔍', web_extract: '📄', web_fetch: '🌐', web_select: '🖱️',
  read_file: '📄', write_file: '✏️', file_edit: '📝', list_files: '📁',
  directory_tree: '📂', search_in_files: '🔎', execute_command: '⚡',
  execute_code: '💻', browser_navigate: '🌍', delegate_task: '🎭',
  memory_store: '💾', memory_recall: '🧠', session_search: '🔎',
  cronjob: '⏰', skill: '📚', clarify: '❓', image_gen: '🎨',
  image_edit: '🖼️', tts: '🔊', asr: '🎤', send_message: '💬',
  todo: '✅', gitignore: '📋', batch_file_ops: '📦',
  project_analyze: '📊', diff_patch: '🔀', ha: '🏠',
  kanban_show: '📋', kanban_complete: '✅', kanban_block: '🚫',
  kanban_heartbeat: '💓', kanban_comment: '💬', kanban_create: '➕', kanban_link: '🔗',
}

const emoji = computed(() => emojiMap[props.toolCall.name] || '🔧')
</script>

<style scoped>
.tool-call-compact {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 4px 10px;
  background: #f5f5f5;
  border-radius: 12px;
  font-size: 12px;
  color: #666;
  margin: 2px 4px 2px 0;
  transition: background 0.2s;
}

.tool-call-compact.status-running {
  background: #e6f7ff;
  color: #1890ff;
}

.tool-call-compact.status-completed {
  background: #f6ffed;
  color: #52c41a;
}

.tool-call-compact.status-error {
  background: #fff2f0;
  color: #ff4d4f;
}

.tool-emoji {
  font-size: 13px;
  opacity: 0.8;
}

.tool-name {
  font-family: 'SF Mono', 'Fira Code', monospace;
  font-size: 11px;
}

.tool-indicator {
  font-size: 10px;
}

.tool-indicator.running {
  width: 5px;
  height: 5px;
  border-radius: 50%;
  background: #1890ff;
  animation: pulse 1.2s ease-in-out infinite;
}

@keyframes pulse {
  0%, 100% { opacity: 0.3; transform: scale(0.8); }
  50% { opacity: 1; transform: scale(1); }
}

.tool-indicator.completed {
  color: #52c41a;
}

.tool-indicator.error {
  color: #ff4d4f;
  font-weight: bold;
}

/* Dark mode */
@media (prefers-color-scheme: dark) {
  .tool-call-compact {
    background: #2a2a2a;
    color: #999;
  }
  .tool-call-compact.status-running {
    background: #1a3a4a;
    color: #40a9ff;
  }
  .tool-call-compact.status-completed {
    background: #1a3a1a;
    color: #73d13d;
  }
  .tool-call-compact.status-error {
    background: #3a1a1a;
    color: #ff7875;
  }
}
</style>
