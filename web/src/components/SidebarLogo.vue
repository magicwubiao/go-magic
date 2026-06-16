<script setup lang="ts">
defineProps<{
  collapsed?: boolean
}>()
</script>

<template>
  <div class="sidebar-logo" :class="{ collapsed }">
    <span v-if="!collapsed" class="logo-text">GO MAGIC</span>
    <span v-else class="logo-text-collapsed">M</span>
  </div>
</template>

<style scoped>
.sidebar-logo {
  display: flex;
  align-items: center;
  justify-content: flex-start;
  padding: 10px 16px;
  border-bottom: 1px solid #e0e0e0;
}

.logo-text {
  font-size: 18px;
  font-weight: 700;
  color: #333;
  font-family: 'Segoe UI', -apple-system, BlinkMacSystemFont, sans-serif;
}

.logo-text-collapsed {
  font-size: 16px;
  font-weight: 700;
  color: #333;
  font-family: 'Segoe UI', -apple-system, BlinkMacSystemFont, sans-serif;
}

:deep(.n-layout--collapsed) .sidebar-logo {
  justify-content: center;
  padding: 10px 0;
}
</style>
