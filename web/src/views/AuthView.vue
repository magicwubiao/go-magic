<template>
  <div class="auth-container">
    <div class="auth-header">
      <locale-switch light />
    </div>
    <n-card class="auth-card" :title="authStore.configured ? t('auth.login') : t('auth.setPassword')">
      <n-alert v-if="authStore.error" type="error" style="margin-bottom: 16px;" closable @close="authStore.error = null">
        {{ authStore.error }}
      </n-alert>

      <n-form @submit.prevent="handleSubmit">
        <n-form-item :label="t('auth.password')">
          <n-input
            v-model:value="password"
            :type="showPassword ? 'text' : 'password'"
            :placeholder="t('auth.enterPassword')"
            :minlength="8"
            autofocus
          >
            <template #suffix>
              <n-button
                quaternary
                circle
                size="small"
                :title="showPassword ? t('auth.hidePassword') : t('auth.showPassword')"
                @click="showPassword = !showPassword"
              >
                <template #icon>
                  <n-icon>
                    <svg v-if="showPassword" viewBox="0 0 512 512" width="16" height="16">
                      <path fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="32" d="M400 256c-28.9 33.1-64.6 64-144 64s-115.1-30.9-144-64c28.9-33.1 64.6-64 144-64s115.1 30.9 144 64z"/>
                      <line x1="80" y1="112" x2="432" y2="400" stroke="currentColor" stroke-linecap="round" stroke-miterlimit="10" stroke-width="32"/>
                    </svg>
                    <svg v-else viewBox="0 0 512 512" width="16" height="16">
                      <path d="M255.66 112c-77.94 0-157.89 45.11-220.83 135.33a16 16 0 0 0-.27 17.77C82.92 340.8 161.8 400 255.66 400c92.84 0 173.34-59.38 221.79-135.25a16.14 16.14 0 0 0 0-17.47C428.89 172.28 347.8 112 255.66 112z"/>
                      <circle cx="256" cy="256" r="80" fill="none" stroke="currentColor" stroke-miterlimit="10" stroke-width="32"/>
                    </svg>
                  </n-icon>
                </template>
              </n-button>
            </template>
          </n-input>
        </n-form-item>

        <n-form-item v-if="authStore.configured" :show-label="false" class="remember-item">
          <n-checkbox v-model:checked="remember">
            {{ t('auth.rememberMe') }}
          </n-checkbox>
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
import { useMessage } from 'naive-ui'
import LocaleSwitch from '@/components/LocaleSwitch.vue'

const { t } = useI18n()
const router = useRouter()
const message = useMessage()
const authStore = useAuthStore()
const password = ref('')
const showPassword = ref(false)
const remember = ref(true)

onMounted(async () => {
  await authStore.checkStatus()
  if (authStore.isAuthenticated) {
    router.replace('/')
  }
})

async function handleSubmit(): Promise<void> {
  if (!password.value || password.value.length < 8) {
    authStore.error = t('auth.passwordMinLength')
    return
  }

  let success = false
  if (authStore.configured) {
    success = await authStore.login(password.value, remember.value)
  } else {
    success = await authStore.setup(password.value)
  }

  if (success) {
    password.value = ''
    message.success(t('auth.loginSuccess'))
    router.push('/')
  } else {
    // Surface a friendlier message for rate-limit responses.
    const msg = authStore.error || ''
    if (msg.includes('429') || /too many/i.test(msg)) {
      authStore.error = t('auth.tooManyAttempts')
    } else if (authStore.configured) {
      authStore.error = t('auth.loginFailed')
    }
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
  position: relative;
}

.auth-header {
  position: absolute;
  top: 16px;
  right: 16px;
  z-index: 10;
}

.auth-card {
  width: 400px;
  max-width: 90vw;
}

.remember-item {
  margin-bottom: 12px;
}
</style>