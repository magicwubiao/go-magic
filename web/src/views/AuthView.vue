<template>
  <div class="auth-container">
    <n-card class="auth-card" :title="authStore.configured ? t('auth.login') : t('auth.setPassword')">
      <n-alert v-if="authStore.error" type="error" style="margin-bottom: 16px;" closable @close="authStore.error = null">
        {{ authStore.error }}
      </n-alert>

      <n-form @submit.prevent="handleSubmit">
        <n-form-item :label="t('auth.password')">
          <n-input
            v-model:value="password"
            type="password"
            show-password-on="click"
            :placeholder="t('auth.enterPassword')"
            :minlength="4"
            autofocus
            @keydown.enter="handleSubmit"
          />
        </n-form-item>
        <n-button type="primary" block :loading="authStore.loading" @click="handleSubmit">
          {{ authStore.configured ? t('auth.loginButton') : t('auth.setPasswordButton') }}
        </n-button>
      </n-form>

      <n-text v-if="!authStore.configured" depth="3" style="display: block; margin-top: 12px; text-align: center;">
        {{ t('auth.firstTimeSetup') }}
      </n-text>
    </n-card>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'

const { t } = useI18n()
const router = useRouter()
const authStore = useAuthStore()
const password = ref('')

onMounted(async () => {
  await authStore.checkStatus()
  if (authStore.isAuthenticated) {
    router.replace('/')
  }
})

async function handleSubmit(): Promise<void> {
  if (!password.value || password.value.length < 4) {
    authStore.error = t('auth.passwordMinLength')
    return
  }

  let success = false
  if (authStore.configured) {
    success = await authStore.login(password.value)
  } else {
    success = await authStore.setup(password.value)
  }

  if (success) {
    password.value = ''
    router.push('/')
  }
}
</script>

<style scoped>
.auth-container {
  display: flex;
  justify-content: center;
  align-items: center;
  min-height: 100vh;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
}

.auth-card {
  width: 400px;
  max-width: 90vw;
}
</style>
