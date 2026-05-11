<template>
  <div class="tools-view">
    <n-card title="Toolsets">
      <n-tabs type="segment">
        <n-tab-pane name="all" tab="All Toolsets">
          <n-list hoverable clickable>
            <n-list-item v-for="toolset in toolsets" :key="toolset.name">
              <n-thing :title="toolset.name" :description="toolset.description">
                <template #header-extra>
                  <n-switch :default-value="toolset.enabled" />
                </template>
                <template #description>
                  <n-space>
                    <n-tag v-for="tool in toolset.tools" :key="tool" size="small">{{ tool }}</n-tag>
                  </n-space>
                </template>
              </n-thing>
            </n-list-item>
          </n-list>
        </n-tab-pane>
        <n-tab-pane name="web" tab="Web">
          <n-alert type="info">Web tools for searching and extracting content from the internet.</n-alert>
        </n-tab-pane>
        <n-tab-pane name="terminal" tab="Terminal">
          <n-alert type="info">Terminal tools for executing commands on your system.</n-alert>
        </n-tab-pane>
        <n-tab-pane name="file" tab="File">
          <n-alert type="info">File tools for reading, writing, and editing files.</n-alert>
        </n-tab-pane>
      </n-tabs>
    </n-card>
  </div>
</template>

<script setup>
const toolsets = [
  { name: 'web', description: 'Web search and content extraction', enabled: true, tools: ['web_search', 'web_extract'] },
  { name: 'browser', description: 'Browser automation', enabled: true, tools: ['browser_navigate', 'browser_click'] },
  { name: 'terminal', description: 'Terminal command execution', enabled: true, tools: ['execute_command', 'terminal'] },
  { name: 'file', description: 'File read/write/edit operations', enabled: true, tools: ['read_file', 'write_file', 'file_edit'] },
  { name: 'skills', description: 'Skill management', enabled: true, tools: ['skill_list', 'skill_view', 'skill_manage'] },
  { name: 'memory', description: 'Persistent memory', enabled: true, tools: ['memory_store', 'memory_recall'] },
  { name: 'delegation', description: 'Sub-agent delegation', enabled: true, tools: ['delegate_task', 'poll_task'] },
  { name: 'homeassistant', description: 'Smart home control', enabled: false, tools: ['ha_list_entities', 'ha_call_service'] },
  { name: 'code_execution', description: 'Python code execution', enabled: true, tools: ['execute_code'] },
  { name: 'utility', description: 'Utility functions', enabled: true, tools: ['json', 'yaml', 'hash', 'uuid'] }
]
</script>
