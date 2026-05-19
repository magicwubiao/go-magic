<template>
  <div>
    <h2 style="margin-bottom: 24px;">Tools</h2>
    <n-tabs type="line">
      <n-tab-pane name="tools" tab="All Tools">
        <n-spin v-if="toolsStore.loading" />
        <n-list v-else>
          <n-list-item v-for="tool in toolsStore.tools" :key="tool.id">
            <n-thing :title="tool.name">
              <template #description>
                <n-space>
                  <n-tag size="small">{{ tool.category }}</n-tag>
                  <n-text depth="3">{{ tool.description }}</n-text>
                </n-space>
              </template>
              <template #action>
                <n-tag :type="tool.enabled ? 'success' : 'default'">
                  {{ tool.enabled ? 'Enabled' : 'Disabled' }}
                </n-tag>
              </template>
            </n-thing>
          </n-list-item>
        </n-list>
      </n-tab-pane>

      <n-tab-pane name="toolsets" tab="Toolsets">
        <n-list>
          <n-list-item v-for="toolset in toolsStore.toolsets" :key="toolset.id">
            <n-thing :title="toolset.name">
              <template #description>
                {{ toolset.tools.length }} tools
              </template>
              <template #action>
                <n-switch
                  :value="toolset.enabled"
                  @update:value="toggleToolset(toolset.id, $event)"
                />
              </template>
            </n-thing>
          </n-list-item>
        </n-list>
      </n-tab-pane>
    </n-tabs>
  </div>
</template>

<script setup lang="ts">
import { onMounted } from 'vue'
import { useToolsStore } from '@/stores/tools'

const toolsStore = useToolsStore()

async function toggleToolset(id: string, enabled: boolean) {
  if (enabled) {
    await toolsStore.enableToolset(id)
  } else {
    await toolsStore.disableToolset(id)
  }
}

onMounted(() => {
  toolsStore.loadTools()
  toolsStore.loadToolsets()
})
</script>
