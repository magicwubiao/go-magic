<script setup lang="ts">
defineProps<{
  collapsed?: boolean
}>()
</script>

<template>
  <div class="sidebar-logo" :class="{ collapsed }">
    <div class="logo-wrapper">
      <img src="/logo.svg" alt="Logo" class="logo-img" />
      <span v-if="!collapsed" class="logo-text">MAGIC</span>
    </div>
  </div>
</template>

<style scoped>
.sidebar-logo {
  display: flex;
  align-items: center;
  padding: 14px 16px;
  border-bottom: 1px solid #e0e0e0;
}

.sidebar-logo.collapsed {
  justify-content: center;
  padding: 14px 0;
}

.logo-wrapper {
  display: flex;
  align-items: center;
  gap: 0;
}

.logo-img {
  height: 36px;
  width: auto;
  flex-shrink: 0;
}

.logo-text {
  font-size: 20px;
  font-weight: 700;
  color: #333;
  letter-spacing: -0.5px;
  font-family: 'Segoe UI', -apple-system, BlinkMacSystemFont, sans-serif;
}
</style>
