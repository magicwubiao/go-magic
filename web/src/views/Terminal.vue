<template>
  <div class="terminal-view">
    <!-- Terminal Tabs -->
    <n-tabs type="card" closable @close="closeTab" v-model:value="activeTabId">
      <n-tab-pane
        v-for="tab in tabs"
        :key="tab.id"
        :name="tab.id"
        :tab="tab.name"
      >
        <div ref="terminalRef" class="terminal-container"></div>
      </n-tab-pane>

      <!-- Add New Tab -->
      <template #suffix>
        <n-dropdown :options="newTabOptions" @select="handleNewTab">
          <n-button quaternary size="small">
            <template #icon>
              <n-icon :component="Add" />
            </template>
          </n-button>
        </n-dropdown>
      </template>
    </n-tabs>

    <!-- Create Session Modal -->
    <n-modal v-model:show="showNewSession" preset="card" title="New Terminal Session" style="width: 400px">
      <n-form :model="newSessionForm" label-placement="top">
        <n-form-item label="Backend">
          <n-select
            v-model:value="newSessionForm.backend"
            :options="backendOptions"
            placeholder="Select backend"
          />
        </n-form-item>

        <n-form-item label="Working Directory" v-if="newSessionForm.backend === 'local'">
          <n-input
            v-model:value="newSessionForm.workingDir"
            placeholder="/home/user"
          />
        </n-form-item>

        <n-form-item label="Docker Image" v-if="newSessionForm.backend === 'docker'">
          <n-input
            v-model:value="newSessionForm.dockerImage"
            placeholder="golang:1.25"
          />
        </n-form-item>

        <n-form-item label="SSH Host" v-if="newSessionForm.backend === 'ssh'">
          <n-input
            v-model:value="newSessionForm.sshHost"
            placeholder="user@host"
          />
        </n-form-item>

        <n-form-item label="Session Name">
          <n-input
            v-model:value="newSessionForm.name"
            placeholder="My Terminal"
          />
        </n-form-item>
      </n-form>

      <template #footer>
        <n-space justify="end">
          <n-button @click="showNewSession = false">Cancel</n-button>
          <n-button type="primary" @click="createSession">
            Create
          </n-button>
        </n-space>
      </template>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted, onUnmounted, nextTick, h } from 'vue'
import {
  NTabs,
  NTabPane,
  NButton,
  NIcon,
  NDropdown,
  NModal,
  NForm,
  NFormItem,
  NInput,
  NSelect,
  NSpace,
} from 'naive-ui'
import { Add, Terminal } from '@vicons/ionicons5'

interface TerminalTab {
  id: string
  name: string
  backend: string
  terminal?: any
  ws?: WebSocket
}

const tabs = ref<TerminalTab[]>([])
const activeTabId = ref<string>('')
const terminalRef = ref<HTMLElement[]>([])
const showNewSession = ref(false)

const newSessionForm = reactive({
  backend: 'local',
  name: '',
  workingDir: '/',
  dockerImage: 'golang:1.25-alpine',
  sshHost: '',
})

const backendOptions = [
  { label: 'Local', value: 'local' },
  { label: 'Docker', value: 'docker' },
  { label: 'SSH', value: 'ssh' },
]

const newTabOptions = [
  { label: 'New Local Terminal', key: 'local' },
  { label: 'New Docker Terminal', key: 'docker' },
  { label: 'New SSH Terminal', key: 'ssh' },
]

let tabCounter = 0
let terminalInstances: Map<string, any> = new Map()

function handleNewTab(key: string) {
  newSessionForm.backend = key
  showNewSession.value = true
}

async function createSession() {
  tabCounter++
  const tabId = `tab-${tabCounter}`
  const tabName = newSessionForm.name || `${newSessionForm.backend}-${tabCounter}`

  const tab: TerminalTab = {
    id: tabId,
    name: tabName,
    backend: newSessionForm.backend,
  }

  tabs.value.push(tab)
  activeTabId.value = tabId

  showNewSession.value = false

  // Reset form
  newSessionForm.name = ''
  newSessionForm.workingDir = '/'
  newSessionForm.dockerImage = 'golang:1.25-alpine'
  newSessionForm.sshHost = ''

  // Initialize terminal after DOM update
  await nextTick()
  initTerminal(tab)
}

async function initTerminal(tab: TerminalTab) {
  // Find the terminal container for this tab
  const tabIndex = tabs.value.findIndex((t) => t.id === tab.id)
  const container = terminalRef.value[tabIndex]

  if (!container) {
    console.error('Terminal container not found')
    return
  }

  try {
    // Dynamic import xterm
    const { Terminal: XTerm } = await import('@xterm/xterm')
    const { FitAddon } = await import('@xterm/addon-fit')

    const terminal = new XTerm({
      cursorBlink: true,
      fontSize: 14,
      fontFamily: 'Menlo, Monaco, "Courier New", monospace',
      theme: {
        background: '#1e1e1e',
        foreground: '#d4d4d4',
        cursor: '#ffffff',
        selection: '#264f78',
      },
      rows: 30,
      cols: 120,
    })

    const fitAddon = new FitAddon()
    terminal.loadAddon(fitAddon)
    terminal.open(container)
    fitAddon.fit()

    // Store references
    terminalInstances.set(tab.id, { terminal, fitAddon })
    tab.terminal = terminal

    // Connect via WebSocket
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const wsUrl = `${protocol}//${window.location.host}/api/terminal/ws?backend=${tab.backend}`

    const ws = new WebSocket(wsUrl)
    tab.ws = ws

    ws.onopen = () => {
      terminal.write('Connected to terminal\r\n\r\n$ ')
    }

    ws.onmessage = (event) => {
      terminal.write(event.data)
    }

    ws.onclose = () => {
      terminal.write('\r\n\r\nConnection closed')
    }

    ws.onerror = () => {
      terminal.write('\r\n\r\nConnection error')
    }

    // Handle terminal input
    terminal.onData((data: string) => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(data)
      }
    })

    // Handle resize
    terminal.onResize(({ cols, rows }) => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: 'resize', cols, rows }))
      }
    })

    // Handle window resize
    const resizeObserver = new ResizeObserver(() => {
      fitAddon.fit()
    })
    resizeObserver.observe(container)

  } catch (e) {
    console.error('Failed to initialize terminal:', e)
    if (container) {
      container.innerHTML = '<div style="color: red; padding: 20px;">Failed to load terminal. Please refresh.</div>'
    }
  }
}

function closeTab(id: string) {
  const index = tabs.value.findIndex((t) => t.id === id)
  if (index === -1) return

  const tab = tabs.value[index]

  // Close WebSocket
  if (tab.ws) {
    tab.ws.close()
  }

  // Dispose terminal
  const instance = terminalInstances.get(id)
  if (instance) {
    instance.terminal.dispose()
    terminalInstances.delete(id)
  }

  // Remove tab
  tabs.value.splice(index, 1)

  // Switch to another tab if active tab was closed
  if (activeTabId.value === id && tabs.value.length > 0) {
    activeTabId.value = tabs.value[Math.max(0, index - 1)].id
  }
}

onMounted(() => {
  // Create initial terminal
  createSession()
})

onUnmounted(() => {
  // Clean up all terminals
  tabs.value.forEach((tab) => {
    if (tab.ws) tab.ws.close()
    const instance = terminalInstances.get(tab.id)
    if (instance) instance.terminal.dispose()
  })
  terminalInstances.clear()
})
</script>

<style lang="scss">
@import '@xterm/xterm/css/xterm.css';

.terminal-view {
  height: calc(100vh - 84px);
  display: flex;
  flex-direction: column;
}

.terminal-container {
  height: calc(100vh - 180px);
  background: #1e1e1e;
  padding: 8px;
  border-radius: 4px;

  .xterm {
    height: 100%;
  }

  .xterm-viewport {
    overflow-y: auto !important;
  }
}

.n-tabs {
  height: 100%;
  display: flex;
  flex-direction: column;

  .n-tab-pane {
    height: calc(100% - 50px);
    padding: 0 !important;
  }

  .n-tabs-nav {
    flex-shrink: 0;
  }
}
</style>
