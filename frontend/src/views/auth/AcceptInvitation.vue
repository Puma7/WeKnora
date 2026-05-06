<template>
  <div class="invite-page">
    <div class="invite-card">
      <h1 class="invite-title">{{ $t('invitation.title') }}</h1>

      <div v-if="loading" class="loading">
        <t-loading />
        <p>{{ $t('invitation.loading') }}</p>
      </div>

      <div v-else-if="loadError" class="error-state">
        <h2>{{ $t('invitation.invalidTitle') }}</h2>
        <p>{{ loadError }}</p>
        <t-button theme="primary" @click="goLogin">{{ $t('invitation.backToLogin') }}</t-button>
      </div>

      <template v-else-if="preview">
        <div class="preview">
          <p class="preview-line">
            <strong>{{ preview.invited_by_username || $t('invitation.fromAdmin') }}</strong>
            {{ $t('invitation.invitedYou') }}
          </p>
          <p v-if="preview.tenant_name" class="preview-line">
            {{ $t('invitation.tenantLabel') }}: <strong>{{ preview.tenant_name }}</strong>
          </p>
          <p class="preview-line">
            {{ $t('invitation.roleLabel') }}: <strong>{{ $t('userManagement.role.' + preview.role) }}</strong>
          </p>
          <p class="preview-line muted">
            {{ $t('invitation.expiresAt') }} {{ formatDate(preview.expires_at) }}
          </p>
          <p v-if="preview.note" class="preview-note">{{ preview.note }}</p>
        </div>

        <t-form
          ref="formRef"
          :data="form"
          :rules="rules"
          @submit="onSubmit"
          layout="vertical"
        >
          <t-form-item :label="$t('auth.email')" name="email">
            <t-input v-model="form.email" disabled />
          </t-form-item>
          <t-form-item :label="$t('auth.username')" name="username">
            <t-input v-model="form.username" :placeholder="$t('auth.usernamePlaceholder')" />
          </t-form-item>
          <t-form-item :label="$t('auth.password')" name="password">
            <t-input v-model="form.password" type="password" :placeholder="$t('auth.passwordPlaceholder')" />
          </t-form-item>
          <t-form-item :label="$t('auth.confirmPassword')" name="confirmPassword">
            <t-input v-model="form.confirmPassword" type="password" :placeholder="$t('auth.confirmPasswordPlaceholder')" />
          </t-form-item>

          <t-button type="submit" theme="primary" block :loading="submitting">
            {{ submitting ? $t('invitation.creatingAccount') : $t('invitation.createAccount') }}
          </t-button>
        </t-form>
      </template>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'

import { previewInvitation, type InvitationPublicView } from '@/api/admin'
import { login, register } from '@/api/auth'
import { useAuthStore } from '@/stores/auth'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const { t } = useI18n()

const token = computed(() => String(route.params.token || ''))
const loading = ref(true)
const loadError = ref('')
const preview = ref<InvitationPublicView | null>(null)
const submitting = ref(false)
const formRef = ref()

const form = reactive({
  email: '',
  username: '',
  password: '',
  confirmPassword: '',
})

const rules = computed(() => ({
  username: [
    { required: true, message: t('auth.usernameRequired'), type: 'error' },
    { min: 2, message: t('auth.usernameMinLength'), type: 'error' },
  ],
  password: [
    { required: true, message: t('auth.passwordRequired'), type: 'error' },
    { min: 8, message: t('auth.passwordMinLength'), type: 'error' },
  ],
  confirmPassword: [
    { required: true, message: t('auth.confirmPasswordRequired'), type: 'error' },
    {
      validator: (val: string) => val === form.password,
      message: t('auth.passwordMismatch'),
      type: 'error',
    },
  ],
}))

function formatDate(s: string) {
  return new Date(s).toLocaleString()
}

function goLogin() {
  router.replace('/login')
}

async function onSubmit() {
  const valid = await formRef.value?.validate()
  if (valid !== true) return

  submitting.value = true
  try {
    const resp = await register({
      email: form.email,
      username: form.username,
      password: form.password,
      invitation_token: token.value,
    })
    if (!resp.success) {
      MessagePlugin.error(resp.message || t('auth.registerFailed'))
      return
    }
    // Auto-login so the new user lands directly in the platform.
    const loginResp = await login({ email: form.email, password: form.password })
    if (loginResp.success && loginResp.token && loginResp.user && loginResp.tenant) {
      auth.setUser({
        id: loginResp.user.id,
        username: loginResp.user.username,
        email: loginResp.user.email,
        avatar: loginResp.user.avatar,
        tenant_id: String(loginResp.tenant.id),
        can_access_all_tenants: !!loginResp.user.can_access_all_tenants,
        role: (loginResp.user as any).role,
        permissions: (loginResp.user as any).permissions,
        created_at: loginResp.user.created_at,
        updated_at: loginResp.user.updated_at,
      })
      auth.setToken(loginResp.token)
      if (loginResp.refresh_token) auth.setRefreshToken(loginResp.refresh_token)
      auth.setTenant({
        id: String(loginResp.tenant.id),
        name: loginResp.tenant.name,
        api_key: loginResp.tenant.api_key,
        owner_id: loginResp.user.id,
        created_at: loginResp.tenant.created_at,
        updated_at: loginResp.tenant.updated_at,
      })
      MessagePlugin.success(t('invitation.welcome'))
      router.replace('/platform/knowledge-bases')
    } else {
      // Account created but auto-login failed; bounce to login screen.
      MessagePlugin.success(t('auth.registerSuccess'))
      router.replace(`/login?email=${encodeURIComponent(form.email)}`)
    }
  } catch (err: any) {
    MessagePlugin.error(err?.message || t('auth.registerError'))
  } finally {
    submitting.value = false
  }
}

onMounted(async () => {
  if (!token.value) {
    loadError.value = t('invitation.missingToken')
    loading.value = false
    return
  }
  try {
    const view = await previewInvitation(token.value)
    if (!view) {
      loadError.value = t('invitation.invalidMessage')
    } else {
      preview.value = view
      form.email = view.email
    }
  } catch (err: any) {
    loadError.value = err?.message || t('invitation.invalidMessage')
  } finally {
    loading.value = false
  }
})
</script>

<style lang="less" scoped>
.invite-page {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--td-bg-color-page);
  padding: 24px;
}
.invite-card {
  width: 100%;
  max-width: 460px;
  background: var(--td-bg-color-container);
  border: 1px solid var(--td-component-stroke);
  border-radius: 12px;
  padding: 32px;
  box-shadow: 0 6px 28px rgba(15, 23, 42, 0.08);
}
.invite-title { font-size: 22px; font-weight: 600; margin: 0 0 16px; }
.preview { margin-bottom: 24px; padding: 16px; border-radius: 8px; background: var(--td-bg-color-secondarycontainer); }
.preview-line { margin: 0 0 6px; font-size: 14px; }
.preview-line.muted { color: var(--td-text-color-secondary); font-size: 12px; }
.preview-note { margin-top: 8px; padding: 8px; background: var(--td-bg-color-container); border-radius: 6px; font-size: 13px; font-style: italic; }
.loading { text-align: center; padding: 32px 0; }
.error-state { text-align: center; padding: 16px 0; }
.error-state h2 { font-size: 16px; margin: 0 0 8px; }
.error-state p { color: var(--td-text-color-secondary); margin: 0 0 16px; }
</style>
