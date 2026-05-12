<template>
  <div class="settings-view">
    <div class="settings-container">
      <div class="settings-header">
        <h1>⚙️ Settings</h1>
        <button @click="$emit('close')" class="close-btn">×</button>
      </div>

      <div class="settings-content">
        <!-- Model Settings -->
        <section class="settings-section">
          <h2>🤖 Model</h2>
          <div class="setting-item">
            <label>Provider</label>
            <select v-model="settings.provider">
              <option value="openai">OpenAI</option>
              <option value="deepseek">DeepSeek</option>
              <option value="anthropic">Anthropic</option>
              <option value="google">Google</option>
            </select>
          </div>
          <div class="setting-item">
            <label>Model</label>
            <select v-model="settings.model">
              <option value="gpt-4o">GPT-4o</option>
              <option value="gpt-4o-mini">GPT-4o Mini</option>
              <option value="claude-3-5-sonnet">Claude 3.5 Sonnet</option>
              <option value="deepseek-chat">DeepSeek Chat</option>
            </select>
          </div>
          <div class="setting-item">
            <label>Temperature</label>
            <input type="range" v-model="settings.temperature" min="0" max="2" step="0.1" />
            <span>{{ settings.temperature }}</span>
          </div>
          <div class="setting-item">
            <label>Max Tokens</label>
            <input type="number" v-model="settings.maxTokens" min="100" max="32000" />
          </div>
        </section>

        <!-- Tools Settings -->
        <section class="settings-section">
          <h2>🛠️ Tools</h2>
          <div class="setting-item checkbox">
            <input type="checkbox" id="web-search" v-model="settings.tools.webSearch" />
            <label for="web-search">Web Search</label>
          </div>
          <div class="setting-item checkbox">
            <input type="checkbox" id="file-operations" v-model="settings.tools.fileOperations" />
            <label for="file-operations">File Operations</label>
          </div>
          <div class="setting-item checkbox">
            <input type="checkbox" id="code-execution" v-model="settings.tools.codeExecution" />
            <label for="code-execution">Code Execution</label>
          </div>
          <div class="setting-item checkbox">
            <input type="checkbox" id="terminal" v-model="settings.tools.terminal" />
            <label for="terminal">Terminal</label>
          </div>
        </section>

        <!-- Display Settings -->
        <section class="settings-section">
          <h2>🎨 Display</h2>
          <div class="setting-item checkbox">
            <input type="checkbox" id="dark-mode" v-model="settings.darkMode" />
            <label for="dark-mode">Dark Mode</label>
          </div>
          <div class="setting-item">
            <label>Theme</label>
            <select v-model="settings.theme">
              <option value="default">Default</option>
              <option value="cyber">Cyber</option>
              <option value="slate">Slate</option>
              <option value="mono">Mono</option>
            </select>
          </div>
          <div class="setting-item">
            <label>Font Size</label>
            <input type="range" v-model="settings.fontSize" min="12" max="20" />
            <span>{{ settings.fontSize }}px</span>
          </div>
        </section>

        <!-- API Key -->
        <section class="settings-section">
          <h2>🔑 API Configuration</h2>
          <div class="setting-item">
            <label>API Key</label>
            <input type="password" v-model="settings.apiKey" placeholder="Enter your API key" />
          </div>
          <div class="setting-item">
            <label>API Base URL</label>
            <input type="text" v-model="settings.apiBase" placeholder="https://api.openai.com/v1" />
          </div>
        </section>
      </div>

      <div class="settings-footer">
        <button @click="resetSettings" class="btn-secondary">Reset</button>
        <button @click="saveSettings" class="btn-primary">Save Changes</button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive } from 'vue'

defineEmits(['close'])

const settings = reactive({
  provider: 'openai',
  model: 'gpt-4o-mini',
  temperature: 0.7,
  maxTokens: 4096,
  apiKey: '',
  apiBase: 'https://api.openai.com/v1',
  darkMode: true,
  theme: 'default',
  fontSize: 14,
  tools: {
    webSearch: true,
    fileOperations: false,
    codeExecution: true,
    terminal: false,
  },
})

function saveSettings() {
  localStorage.setItem('go-magic-settings', JSON.stringify(settings))
  alert('Settings saved!')
}

function resetSettings() {
  Object.assign(settings, {
    provider: 'openai',
    model: 'gpt-4o-mini',
    temperature: 0.7,
    maxTokens: 4096,
    apiKey: '',
    apiBase: 'https://api.openai.com/v1',
    darkMode: true,
    theme: 'default',
    fontSize: 14,
    tools: {
      webSearch: true,
      fileOperations: false,
      codeExecution: true,
      terminal: false,
    },
  })
}
</script>

<style scoped>
.settings-view {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  justify-content: center;
  align-items: center;
  z-index: 1000;
}
.settings-container {
  width: 600px;
  max-height: 80vh;
  background: var(--bg-primary);
  border-radius: 16px;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}
.settings-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 20px 24px;
  border-bottom: 1px solid var(--border-color);
}
.settings-header h1 {
  margin: 0;
  font-size: 20px;
}
.close-btn {
  background: none;
  border: none;
  font-size: 24px;
  cursor: pointer;
  color: var(--text-secondary);
}
.settings-content {
  flex: 1;
  overflow-y: auto;
  padding: 24px;
}
.settings-section {
  margin-bottom: 32px;
}
.settings-section:last-child {
  margin-bottom: 0;
}
.settings-section h2 {
  font-size: 16px;
  margin-bottom: 16px;
  color: var(--text-secondary);
}
.setting-item {
  display: flex;
  align-items: center;
  margin-bottom: 12px;
}
.setting-item label {
  width: 120px;
  font-size: 14px;
}
.setting-item select,
.setting-item input[type="text"],
.setting-item input[type="password"],
.setting-item input[type="number"] {
  flex: 1;
  padding: 8px 12px;
  border: 1px solid var(--border-color);
  border-radius: 6px;
  background: var(--bg-secondary);
  color: var(--text-primary);
  font-size: 14px;
}
.setting-item input[type="range"] {
  flex: 1;
  margin-right: 12px;
}
.setting-item.checkbox {
  gap: 12px;
}
.setting-item.checkbox input {
  width: 18px;
  height: 18px;
}
.setting-item.checkbox label {
  width: auto;
}
.settings-footer {
  display: flex;
  justify-content: flex-end;
  gap: 12px;
  padding: 16px 24px;
  border-top: 1px solid var(--border-color);
}
.btn-primary,
.btn-secondary {
  padding: 10px 20px;
  border-radius: 8px;
  font-size: 14px;
  cursor: pointer;
}
.btn-primary {
  background: var(--primary-color);
  color: white;
  border: none;
}
.btn-secondary {
  background: var(--bg-secondary);
  color: var(--text-primary);
  border: 1px solid var(--border-color);
}
</style>
