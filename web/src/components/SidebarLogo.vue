<script setup lang="ts">
defineProps<{
  collapsed?: boolean
}>()
</script>

<template>
  <div class="sidebar-logo" :class="{ collapsed }">
    <span v-if="!collapsed" class="logo-text">MAGIC</span>
    <span v-else class="logo-text-collapsed">M</span>
  </div>
</template>

<style scoped>
.sidebar-logo {
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 14px 16px;
  border-bottom: 1px solid #e0e0e0;
}

.logo-text {
  font-size: 20px;
  font-weight: 700;
  color: #333;
  letter-spacing: -0.5px;
  font-family: 'Segoe UI', -apple-system, BlinkMacSystemFont, sans-serif;
}

.logo-text-collapsed {
  font-size: 20px;
  font-weight: 700;
  color: #333;
  font-family: 'Segoe UI', -apple-system, BlinkMacSystemFont, sans-serif;
}
</style>
