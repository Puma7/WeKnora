<template>
  <t-dialog
    :visible="visible"
    :header="$t('kbPermissions.title')"
    :width="640"
    :footer="false"
    :on-close="onClose"
  >
    <div v-if="loading" class="loading">
      <t-loading size="small" />
    </div>
    <template v-else>
      <p class="hint">{{ $t('kbPermissions.hint') }}</p>

      <div class="add-row">
        <input
          v-model="searchQuery"
          type="text"
          :placeholder="$t('kbPermissions.searchPlaceholder')"
          class="text-input"
          @keydown.enter="onSearch"
        />
        <select v-model="newPermission" class="select-input">
          <option value="viewer">{{ $t('kbPermissions.permissions.viewer') }}</option>
          <option value="editor">{{ $t('kbPermissions.permissions.editor') }}</option>
          <option value="admin">{{ $t('kbPermissions.permissions.admin') }}</option>
        </select>
        <t-button theme="primary" @click="onSearch" :disabled="!searchQuery">
          {{ $t('kbPermissions.searchAction') }}
        </t-button>
      </div>

      <div v-if="searchResults.length" class="search-results">
        <p class="results-label">{{ $t('kbPermissions.searchResults') }}</p>
        <div v-for="user in searchResults" :key="user.id" class="result-row">
          <div class="user-info">
            <span class="user-name">{{ user.username }}</span>
            <span class="user-email">{{ user.email }}</span>
          </div>
          <t-button size="small" theme="primary" :disabled="hasGrant(user.id)" @click="onGrant(user.id)">
            {{ hasGrant(user.id) ? $t('kbPermissions.alreadyGranted') : $t('kbPermissions.grant') }}
          </t-button>
        </div>
      </div>

      <h4 class="section-title">{{ $t('kbPermissions.currentGrants') }}</h4>
      <div v-if="!grants.length" class="empty">{{ $t('kbPermissions.noGrants') }}</div>
      <div v-else class="grant-list">
        <div v-for="grant in grants" :key="grant.id" class="grant-row">
          <div class="user-info">
            <span class="user-name">{{ grant.username }}</span>
            <span class="user-email">{{ grant.email }}</span>
          </div>
          <select
            :value="grant.permission"
            class="select-input"
            @change="onChange(grant, ($event.target as HTMLSelectElement).value as Permission)"
          >
            <option value="viewer">{{ $t('kbPermissions.permissions.viewer') }}</option>
            <option value="editor">{{ $t('kbPermissions.permissions.editor') }}</option>
            <option value="admin">{{ $t('kbPermissions.permissions.admin') }}</option>
          </select>
          <t-button size="small" variant="outline" theme="danger" @click="onRevoke(grant)">
            {{ $t('common.delete') }}
          </t-button>
        </div>
      </div>
    </template>
  </t-dialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import { useI18n } from 'vue-i18n'

import {
  type KBUserPermissionResponse,
  grantKBUserPermission,
  listAdminUsers,
  listKBUserPermissions,
  revokeKBUserPermission,
  updateKBUserPermission,
} from '@/api/admin'

type Permission = 'viewer' | 'editor' | 'admin'

const props = defineProps<{
  visible: boolean
  kbId: string
}>()

const emit = defineEmits<{ (e: 'close'): void }>()
const { t } = useI18n()

const loading = ref(false)
const grants = ref<KBUserPermissionResponse[]>([])
const searchQuery = ref('')
const searchResults = ref<Array<{ id: string; username: string; email: string }>>([])
const newPermission = ref<Permission>('viewer')

const grantedIds = computed(() => new Set(grants.value.map(g => g.user_id)))
function hasGrant(userId: string) { return grantedIds.value.has(userId) }

async function loadGrants() {
  if (!props.kbId) return
  loading.value = true
  try {
    grants.value = await listKBUserPermissions(props.kbId)
  } catch (err: any) {
    MessagePlugin.error(err?.message || t('kbPermissions.loadFailed'))
  } finally {
    loading.value = false
  }
}

async function onSearch() {
  const q = searchQuery.value.trim().toLowerCase()
  if (!q) return
  try {
    // Tenant size is small; load + filter client-side rather than building a server-side
    // search endpoint just for this picker.
    const resp = await listAdminUsers(1, 200)
    const matches = (resp?.users || []).filter(u =>
      u.username.toLowerCase().includes(q) || u.email.toLowerCase().includes(q),
    )
    searchResults.value = matches.map(u => ({ id: u.id, username: u.username, email: u.email }))
  } catch (err: any) {
    MessagePlugin.error(err?.message || t('kbPermissions.searchFailed'))
  }
}

async function onGrant(userId: string) {
  try {
    const grant = await grantKBUserPermission(props.kbId, userId, newPermission.value)
    // Backend returns the bare row; reload to pick up display fields.
    await loadGrants()
    if (!grant) MessagePlugin.warning(t('kbPermissions.grantSucceededReload'))
  } catch (err: any) {
    MessagePlugin.error(err?.message || t('kbPermissions.grantFailed'))
  }
}

async function onChange(grant: KBUserPermissionResponse, perm: Permission) {
  try {
    await updateKBUserPermission(props.kbId, grant.id, perm)
    grant.permission = perm
  } catch (err: any) {
    MessagePlugin.error(err?.message || t('kbPermissions.updateFailed'))
    await loadGrants()
  }
}

async function onRevoke(grant: KBUserPermissionResponse) {
  try {
    await revokeKBUserPermission(props.kbId, grant.id)
    grants.value = grants.value.filter(g => g.id !== grant.id)
  } catch (err: any) {
    MessagePlugin.error(err?.message || t('kbPermissions.revokeFailed'))
  }
}

function onClose() { emit('close') }

watch(() => props.visible, v => { if (v) loadGrants() })
</script>

<style lang="less" scoped>
.hint { color: var(--td-text-color-secondary); font-size: 13px; margin: 0 0 12px; }
.add-row { display: flex; gap: 8px; margin-bottom: 12px; }
.text-input, .select-input { height: 32px; padding: 0 8px; border: 1px solid var(--td-component-stroke); border-radius: 6px; background: var(--td-bg-color-container); color: var(--td-text-color-primary); }
.text-input { flex: 1; }
.select-input { min-width: 110px; }
.search-results { margin-bottom: 16px; padding: 8px; border-radius: 6px; background: var(--td-bg-color-secondarycontainer); }
.results-label { margin: 0 0 6px; font-size: 12px; color: var(--td-text-color-secondary); }
.result-row, .grant-row { display: flex; align-items: center; gap: 8px; padding: 6px 0; }
.result-row + .result-row, .grant-row + .grant-row { border-top: 1px solid var(--td-component-stroke); }
.user-info { display: flex; flex-direction: column; flex: 1; min-width: 0; }
.user-name { font-weight: 500; }
.user-email { font-size: 12px; color: var(--td-text-color-secondary); }
.section-title { font-size: 14px; margin: 16px 0 8px; }
.empty { color: var(--td-text-color-secondary); padding: 12px 0; text-align: center; }
.grant-list { border: 1px solid var(--td-component-stroke); border-radius: 6px; padding: 0 12px; }
.loading { padding: 24px; text-align: center; }
</style>
