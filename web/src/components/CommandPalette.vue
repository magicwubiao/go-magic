<template>
  <Teleport to="body">
    <div v-if="show" class="command-palette-overlay" @click="close">
      <div class="command-palette" @click.stop>
        <input
          ref="inputRef"
          v-model="query"
          placeholder="Type a command..."
          class="command-input"
          @keydown.escape="close"
          @keydown.enter="executeCommand"
          @keydown.up.prevent="selectPrev"
          @keydown.down.prevent="selectNext"
        />
        <div class="command-list">
          <div
            v-for="(cmd, index) in filteredCommands"
            :key="cmd.id"
            class="command-item"
            :class="{ selected: selectedIndex === index }"
            @click="executeCommand(cmd)"
            @mouseenter="selectedIndex = index"
          >
            <span class="command-icon">{{ cmd.icon }}</span>
            <span class="command-name">{{ cmd.name }}</span>
            <span class="command-shortcut">{{ cmd.shortcut }}</span>
          </div>
        </div>
        <div v-if="filteredCommands.length === 0" class="no-results">
          No commands found
        </div>
      </div>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { ref, computed, watch, nextTick, onMounted, onUnmounted } from 'vue'

const props = defineProps<{ show: boolean }>()
const emit = defineEmits(['close', 'command'])

const query = ref('')
const selectedIndex = ref(0)
const inputRef = ref<HTMLInputElement | null>(null)

const commands = [
  { id: 'new-chat', name: 'New Chat', icon: '💬', shortcut: 'Ctrl+N', action: () => emit('command', 'new-chat') },
  { id: 'settings', name: 'Open Settings', icon: '⚙️', shortcut: 'Ctrl+,', action: () => emit('command', 'settings') },
  { id: 'clear-chat', name: 'Clear Chat', icon: '🗑️', shortcut: 'Ctrl+L', action: () => emit('command', 'clear-chat') },
  { id: 'export-chat', name: 'Export Chat', icon: '📤', shortcut: '', action: () => emit('command', 'export-chat') },
  { id: 'toggle-theme', name: 'Toggle Theme', icon: '🌓', shortcut: 'Ctrl+T', action: () => emit('command', 'toggle-theme') },
  { id: 'focus-input', name: 'Focus Input', icon: '⌨️', shortcut: '/', action: () => emit('command', 'focus-input') },
  { id: 'show-help', name: 'Show Help', icon: '❓', shortcut: 'F1', action: () => emit('command', 'show-help') },
]

const filteredCommands = computed(() => {
  if (!query.value) return commands
  const q = query.value.toLowerCase()
  return commands.filter(cmd => cmd.name.toLowerCase().includes(q))
})

watch(() => props.show, async (val) => {
  if (val) {
    query.value = ''
    selectedIndex.value = 0
    await nextTick()
    inputRef.value?.focus()
  }
})

function close() {
  emit('close')
}

function selectPrev() {
  if (selectedIndex.value > 0) selectedIndex.value--
}

function selectNext() {
  if (selectedIndex.value < filteredCommands.value.length - 1) selectedIndex.value++
}

function executeCommand(cmd?: any) {
  const target = cmd || filteredCommands.value[selectedIndex.value]
  if (target) {
    target.action()
    close()
  }
}

function handleKeydown(e: KeyboardEvent) {
  if (e.key === 'k' && (e.ctrlKey || e.metaKey)) {
    e.preventDefault()
    emit('close')
  }
}

onMounted(() => {
  document.addEventListener('keydown', handleKeydown)
})

onUnmounted(() => {
  document.removeEventListener('keydown', handleKeydown)
})
</script>

<style scoped>
.command-palette-overlay {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  justify-content: center;
  padding-top: 100px;
  z-index: 9999;
}
.command-palette {
  width: 500px;
  max-height: 400px;
  background: var(--bg-primary);
  border-radius: 12px;
  box-shadow: 0 20px 60px rgba(0, 0, 0, 0.3);
  overflow: hidden;
}
.command-input {
  width: 100%;
  padding: 16px 20px;
  font-size: 16px;
  border: none;
  border-bottom: 1px solid var(--border-color);
  background: transparent;
  color: var(--text-primary);
  outline: none;
}
.command-list {
  max-height: 300px;
  overflow-y: auto;
}
.command-item {
  display: flex;
  align-items: center;
  padding: 12px 20px;
  cursor: pointer;
  transition: background 0.15s;
}
.command-item:hover,
.command-item.selected {
  background: var(--hover-bg);
}
.command-icon {
  margin-right: 12px;
  font-size: 18px;
}
.command-name {
  flex: 1;
  font-size: 14px;
}
.command-shortcut {
  font-size: 12px;
  color: var(--text-secondary);
  background: var(--bg-secondary);
  padding: 2px 8px;
  border-radius: 4px;
}
.no-results {
  padding: 20px;
  text-align: center;
  color: var(--text-secondary);
}
</style>
