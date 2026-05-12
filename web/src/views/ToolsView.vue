<template>
  <div class="tools-view">
    <div class="view-header">
      <h2>Tools & Toolsets</h2>
    </div>

    <!-- Toolsets -->
    <div class="section">
      <h3>Toolsets</h3>
      <div class="toolsets-grid">
        <div
          v-for="toolset in toolsets"
          :key="toolset.name"
          class="toolset-card"
          :class="{ disabled: !toolset.enabled }"
        >
          <div class="toolset-header">
            <div class="toolset-icon">{{ getToolsetIcon(toolset.name) }}</div>
            <div class="toolset-info">
              <div class="toolset-name">{{ toolset.name }}</div>
              <div class="toolset-desc">{{ toolset.description }}</div>
            </div>
            <n-switch
              :value="toolset.enabled"
              @update:value="toggleToolset(toolset, $event)"
            />
          </div>
          <div class="toolset-tools">
            <n-tag
              v-for="tool in toolset.tools"
              :key="tool.name"
              size="small"
              :bordered="false"
            >
              {{ tool.name }}
            </n-tag>
          </div>
        </div>
      </div>
    </div>

    <!-- All Tools -->
    <div class="section">
      <h3>All Tools</h3>
      <div class="tools-grid">
        <div
          v-for="tool in tools"
          :key="tool.name"
          class="tool-item"
        >
          <div class="tool-name">{{ tool.name }}</div>
          <div class="tool-desc">{{ tool.description }}</div>
          <n-tag size="tiny" type="info">{{ tool.toolset }}</n-tag>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { NSwitch, NTag, useMessage } from 'naive-ui'
import { apiService, Toolset, Tool } from '../api'

const message = useMessage()
const toolsets = ref<Toolset[]>([])
const tools = ref<Tool[]>([])

onMounted(async () => {
  await Promise.all([loadToolsets(), loadTools()])
})

async function loadToolsets() {
  try {
    const response = await apiService.toolsets.list()
    toolsets.value = response.data
  } catch (err) {
    message.error('Failed to load toolsets')
  }
}

async function loadTools() {
  try {
    const response = await apiService.tools.list()
    tools.value = response.data
  } catch (err) {
    message.error('Failed to load tools')
  }
}

async function toggleToolset(toolset: Toolset, enabled: boolean) {
  try {
    await apiService.toolsets.toggle(toolset.name, enabled)
    toolset.enabled = enabled
    message.success(`Toolset ${enabled ? 'enabled' : 'disabled'}`)
  } catch (err) {
    message.error('Failed to toggle toolset')
  }
}

function getToolsetIcon(name: string): string {
  const icons: Record<string, string> = {
    web: '🌐',
    file: '📁',
    terminal: '💻',
    browser: '🌍',
    memory: '🧠',
    utility: '🔧',
  }
  return icons[name] || '🛠️'
}
</script>

<style scoped>
.tools-view {
  padding: 20px;
  height: 100%;
  overflow-y: auto;
}

.view-header {
  margin-bottom: 24px;
}

.view-header h2 {
  margin: 0;
}

.section {
  margin-bottom: 32px;
}

.section h3 {
  font-size: 16px;
  color: var(--text-secondary);
  margin-bottom: 16px;
}

.toolsets-grid {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.toolset-card {
  background: var(--bg-secondary);
  border-radius: 12px;
  padding: 16px;
  transition: opacity 0.2s;
}

.toolset-card.disabled {
  opacity: 0.5;
}

.toolset-header {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 12px;
}

.toolset-icon {
  font-size: 32px;
}

.toolset-info {
  flex: 1;
}

.toolset-name {
  font-weight: 600;
  text-transform: capitalize;
}

.toolset-desc {
  font-size: 13px;
  color: var(--text-secondary);
}

.toolset-tools {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.tools-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(250px, 1fr));
  gap: 12px;
}

.tool-item {
  background: var(--bg-secondary);
  border-radius: 8px;
  padding: 12px;
}

.tool-name {
  font-family: monospace;
  font-weight: 500;
  margin-bottom: 4px;
}

.tool-desc {
  font-size: 13px;
  color: var(--text-secondary);
  margin-bottom: 8px;
}
</style>
