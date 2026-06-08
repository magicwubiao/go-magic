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
  web_search: '🔍', web_fetch: '🌐', web_select: '🖱️',
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
  gap: 4px;
  padding: 2px 8px;
  background: transparent;
  border-radius: 10px;
  font-size: 11px;
  color: #aaa;
  margin: 1px 2px 1px 0;
  transition: background 0.2s;
}

.tool-call-compact.status-running {
  color: #999;
}

.tool-call-compact.status-completed {
  color: #bbb;
}

.tool-call-compact.status-error {
  color: #d9a0a0;
}

.tool-emoji {
  font-size: 11px;
  opacity: 0.6;
}

.tool-name {
  font-family: 'SF Mono', 'Fira Code', monospace;
  font-size: 10px;
}

.tool-indicator {
  font-size: 9px;
}

.tool-indicator.running {
  width: 4px;
  height: 4px;
  border-radius: 50%;
  background: #999;
  animation: pulse 1.2s ease-in-out infinite;
}

@keyframes pulse {
  0%, 100% { opacity: 0.3; transform: scale(0.8); }
  50% { opacity: 1; transform: scale(1); }
}

.tool-indicator.completed {
  color: #bbb;
}

.tool-indicator.error {
  color: #d9a0a0;
  font-weight: bold;
}

/* Dark mode */
@media (prefers-color-scheme: dark) {
  .tool-call-compact {
    color: #666;
  }
  .tool-call-compact.status-running {
    color: #777;
  }
  .tool-call-compact.status-completed {
    color: #555;
  }
  .tool-call-compact.status-error {
    color: #855;
  }
}
</style>
