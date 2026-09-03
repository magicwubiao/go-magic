<script setup lang="ts">
defineProps<{
  collapsed?: boolean
}>()
</script>

<template>
  <div class="sidebar-logo" :class="{ collapsed }">
    <!-- TODO: "GO MAGIC" is hardcoded; consider making it configurable via app config / i18n -->
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
  border-bottom: 1px solid var(--border-color, #e0e0e0);
}

.logo-text {
  font-size: 18px;
  font-weight: 700;
  color: var(--text-color, #333);
  font-family: 'Segoe UI', -apple-system, BlinkMacSystemFont, sans-serif;
}

.collapsed {
  justify-content: center;
}

.logo-text-collapsed {
  font-size: 16px;
  font-weight: 700;
  color: var(--text-color, #333);
  font-family: 'Segoe UI', -apple-system, BlinkMacSystemFont, sans-serif;
}
</style>
