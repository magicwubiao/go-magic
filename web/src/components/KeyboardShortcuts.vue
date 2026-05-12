<template>
  <div class="shortcuts-panel">
    <div class="shortcuts-header">
      <h3>⌨️ Keyboard Shortcuts</h3>
      <button @click="$emit('close')" class="close-btn">×</button>
    </div>
    <div class="shortcuts-list">
      <div class="shortcut-group">
        <h4>General</h4>
        <div class="shortcut-item">
          <span class="shortcut-keys">
            <kbd>Ctrl</kbd> + <kbd>K</kbd>
          </span>
          <span class="shortcut-desc">Open command palette</span>
        </div>
        <div class="shortcut-item">
          <span class="shortcut-keys">
            <kbd>Ctrl</kbd> + <kbd>N</kbd>
          </span>
          <span class="shortcut-desc">New chat</span>
        </div>
        <div class="shortcut-item">
          <span class="shortcut-keys">
            <kbd>Ctrl</kbd> + <kbd>L</kbd>
          </span>
          <span class="shortcut-desc">Clear chat</span>
        </div>
        <div class="shortcut-item">
          <span class="shortcut-keys">
            <kbd>Ctrl</kbd> + <kbd>S</kbd>
          </span>
          <span class="shortcut-desc">Export chat</span>
        </div>
      </div>

      <div class="shortcut-group">
        <h4>Messaging</h4>
        <div class="shortcut-item">
          <span class="shortcut-keys">
            <kbd>Enter</kbd>
          </span>
          <span class="shortcut-desc">Send message</span>
        </div>
        <div class="shortcut-item">
          <span class="shortcut-keys">
            <kbd>Shift</kbd> + <kbd>Enter</kbd>
          </span>
          <span class="shortcut-desc">New line</span>
        </div>
        <div class="shortcut-item">
          <span class="shortcut-keys">
            <kbd>/</kbd>
          </span>
          <span class="shortcut-desc">Focus input</span>
        </div>
        <div class="shortcut-item">
          <span class="shortcut-keys">
            <kbd>↑</kbd> <kbd>↓</kbd>
          </span>
          <span class="shortcut-desc">Navigate history</span>
        </div>
      </div>

      <div class="shortcut-group">
        <h4>Navigation</h4>
        <div class="shortcut-item">
          <span class="shortcut-keys">
            <kbd>Ctrl</kbd> + <kbd>1-5</kbd>
          </span>
          <span class="shortcut-desc">Switch tabs</span>
        </div>
        <div class="shortcut-item">
          <span class="shortcut-keys">
            <kbd>Ctrl</kbd> + <kbd>B</kbd>
          </span>
          <span class="shortcut-desc">Toggle sidebar</span>
        </div>
        <div class="shortcut-item">
          <span class="shortcut-keys">
            <kbd>Escape</kbd>
          </span>
          <span class="shortcut-desc">Close modal/panel</span>
        </div>
      </div>

      <div class="shortcut-group">
        <h4>Text Editing</h4>
        <div class="shortcut-item">
          <span class="shortcut-keys">
            <kbd>Ctrl</kbd> + <kbd>A</kbd>
          </span>
          <span class="shortcut-desc">Select all</span>
        </div>
        <div class="shortcut-item">
          <span class="shortcut-keys">
            <kbd>Ctrl</kbd> + <kbd>Z</kbd>
          </span>
          <span class="shortcut-desc">Undo</span>
        </div>
        <div class="shortcut-item">
          <span class="shortcut-keys">
            <kbd>Tab</kbd>
          </span>
          <span class="shortcut-desc">Auto-complete</span>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
defineEmits(['close'])
</script>

<style scoped>
.shortcuts-panel {
  background: var(--bg-primary);
  border-radius: 16px;
  width: 500px;
  max-height: 80vh;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}
.shortcuts-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 20px 24px;
  border-bottom: 1px solid var(--border-color);
}
.shortcuts-header h3 {
  margin: 0;
  font-size: 18px;
}
.close-btn {
  background: none;
  border: none;
  font-size: 24px;
  cursor: pointer;
  color: var(--text-secondary);
}
.shortcuts-list {
  flex: 1;
  overflow-y: auto;
  padding: 24px;
}
.shortcut-group {
  margin-bottom: 24px;
}
.shortcut-group:last-child {
  margin-bottom: 0;
}
.shortcut-group h4 {
  font-size: 14px;
  color: var(--text-secondary);
  margin-bottom: 12px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}
.shortcut-item {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 8px 0;
}
.shortcut-keys {
  display: flex;
  gap: 4px;
}
kbd {
  padding: 4px 8px;
  background: var(--bg-secondary);
  border: 1px solid var(--border-color);
  border-radius: 4px;
  font-size: 12px;
  font-family: monospace;
}
.shortcut-desc {
  font-size: 14px;
  color: var(--text-primary);
}
</style>
