<template>
  <div class="roles-tab">
    <div class="tab-header">
      <div>
        <h3>{{ t('userManagement.roles.heading') }}</h3>
        <p class="tab-description">{{ t('userManagement.roles.description') }}</p>
      </div>
      <t-button theme="primary" @click="openCreateDialog">
        {{ t('userManagement.roles.create') }}
      </t-button>
    </div>

    <div v-if="loading" class="loading-inline">
      <t-loading size="small" />
      <span>{{ t('userManagement.loading') }}</span>
    </div>
    <div v-else-if="roles.length === 0" class="empty-state">
      {{ t('userManagement.roles.empty') }}
    </div>

    <div v-else class="roles-grid">
      <!-- Left: role list -->
      <div class="roles-list">
        <button
          v-for="role in roles"
          :key="role.id"
          :class="['role-card', { active: selectedRoleId === role.id }]"
          @click="selectedRoleId = role.id"
        >
          <div class="role-card-head">
            <span class="role-label">{{ role.label }}</span>
            <span v-if="role.is_system" class="badge system">
              {{ t('userManagement.roles.systemBadge') }}
            </span>
          </div>
          <div class="role-key">{{ role.key }}</div>
          <div v-if="role.description" class="role-description">{{ role.description }}</div>
        </button>
      </div>

      <!-- Right: permission matrix for the selected role -->
      <div v-if="selectedRole" class="matrix-panel">
        <div class="matrix-head">
          <div>
            <h4>{{ t('userManagement.matrix.title') }} — {{ selectedRole.label }}</h4>
            <p v-if="selectedRole.is_system" class="locked-note">
              {{ t('userManagement.roles.lockedSystem') }}
            </p>
            <p v-else class="hint">{{ t('userManagement.matrix.hint') }}</p>
          </div>
          <div class="matrix-actions" v-if="!selectedRole.is_system">
            <t-button size="small" variant="outline" @click="openEditDialog(selectedRole)">
              {{ t('common.edit') }}
            </t-button>
            <t-button size="small" theme="danger" variant="outline" @click="onDelete(selectedRole)">
              {{ t('common.delete') }}
            </t-button>
          </div>
        </div>

        <div class="matrix-table">
          <div class="matrix-row matrix-header-row">
            <div>{{ t('userManagement.matrix.headers.permission') }}</div>
            <div>{{ t('userManagement.matrix.headers.value') }}</div>
          </div>
          <div
            v-for="entry in catalogByCategory"
            :key="entry.key"
            class="matrix-row"
          >
            <div class="matrix-permission">
              <div class="matrix-permission-label">
                {{ permissionLabel(entry) }}
              </div>
              <div class="matrix-permission-meta">
                {{ t(`permissions.categories.${entry.category}`) }}
              </div>
            </div>
            <div class="matrix-value">
              <select
                :value="cellValue(selectedRole, entry.key)"
                :disabled="selectedRole.is_system"
                @change="onCellChange(selectedRole, entry.key, ($event.target as HTMLSelectElement).value)"
              >
                <option value="default">{{ t('userManagement.matrix.useDefault') }}</option>
                <option value="allow">{{ t('userManagement.matrix.allow') }}</option>
                <option value="deny">{{ t('userManagement.matrix.deny') }}</option>
              </select>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- Create / Edit dialog -->
    <t-dialog
      :visible="dialogOpen"
      :header="dialogMode === 'create' ? t('userManagement.roles.createTitle') : t('userManagement.roles.editTitle')"
      :on-close="() => (dialogOpen = false)"
      :on-confirm="onSubmitDialog"
    >
      <div class="role-form">
        <label>
          <span class="form-label">{{ t('userManagement.roles.labelLabel') }}</span>
          <input v-model="form.label" :placeholder="t('userManagement.roles.labelPlaceholder')" />
        </label>
        <label>
          <span class="form-label">{{ t('userManagement.roles.keyLabel') }}</span>
          <input v-model="form.key" :placeholder="t('userManagement.roles.keyPlaceholder')" />
        </label>
        <label>
          <span class="form-label">{{ t('common.description') || 'Description' }}</span>
          <input v-model="form.description" :placeholder="t('userManagement.roles.descriptionPlaceholder')" />
        </label>
      </div>
    </t-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'

import {
  type PermissionCatalogEntry,
  type RoleInfo,
  createRole,
  deleteRole,
  listRoles,
  setRolePermission,
  updateRole,
} from '@/api/admin'

const { t } = useI18n()

const loading = ref(false)
const roles = ref<RoleInfo[]>([])
const catalog = ref<PermissionCatalogEntry[]>([])
const selectedRoleId = ref<string>('')

const selectedRole = computed(() => roles.value.find(r => r.id === selectedRoleId.value))

// Catalog rendering: keep the natural order returned by the backend (already
// grouped by category); the i18n side renders the per-row category label.
const catalogByCategory = computed(() => catalog.value)

function permissionLabel(entry: PermissionCatalogEntry): string {
  // Try the locale-defined label first; fall back to the backend-supplied one
  // if the translation is missing. vue-i18n's t() returns the key itself when
  // there is no translation, so we compare against the key to detect that.
  const i18nKey = `permissions.flags.${entry.key}.label`
  const translated = t(i18nKey)
  return translated === i18nKey ? entry.label : translated
}

function cellValue(role: RoleInfo, key: string): 'default' | 'allow' | 'deny' {
  if (!(key in role.permissions)) return 'default'
  return role.permissions[key] ? 'allow' : 'deny'
}

async function refreshRoles() {
  loading.value = true
  try {
    const resp = await listRoles()
    roles.value = resp.roles
    catalog.value = resp.catalog
    if (!selectedRoleId.value && roles.value.length > 0) {
      selectedRoleId.value = roles.value[0].id
    }
  } catch (err: any) {
    MessagePlugin.error(err?.message || t('userManagement.loadFailed'))
  } finally {
    loading.value = false
  }
}

async function onCellChange(role: RoleInfo, key: string, raw: string) {
  if (role.is_system) return
  let allowed: boolean | null
  if (raw === 'allow') allowed = true
  else if (raw === 'deny') allowed = false
  else allowed = null
  try {
    await setRolePermission(role.id, key, allowed)
    if (allowed === null) delete role.permissions[key]
    else role.permissions[key] = allowed
    MessagePlugin.success(t('userManagement.permissionsUpdated'))
  } catch (err: any) {
    MessagePlugin.error(err?.message || t('userManagement.updateFailed'))
  }
}

// Create / edit dialog
const dialogOpen = ref(false)
const dialogMode = ref<'create' | 'edit'>('create')
const form = reactive<{ id?: string; key: string; label: string; description: string }>({
  key: '',
  label: '',
  description: '',
})

function openCreateDialog() {
  dialogMode.value = 'create'
  form.id = undefined
  form.key = ''
  form.label = ''
  form.description = ''
  dialogOpen.value = true
}

function openEditDialog(role: RoleInfo) {
  dialogMode.value = 'edit'
  form.id = role.id
  form.key = role.key
  form.label = role.label
  form.description = role.description ?? ''
  dialogOpen.value = true
}

async function onSubmitDialog() {
  try {
    if (dialogMode.value === 'create') {
      const created = await createRole({
        key: form.key.trim(),
        label: form.label.trim(),
        description: form.description.trim() || undefined,
      })
      roles.value.push(created)
      selectedRoleId.value = created.id
      MessagePlugin.success(t('userManagement.roles.created'))
    } else if (form.id) {
      const updated = await updateRole(form.id, {
        key: form.key.trim(),
        label: form.label.trim(),
        description: form.description.trim() || undefined,
      })
      const idx = roles.value.findIndex(r => r.id === updated.id)
      if (idx >= 0) roles.value[idx] = { ...roles.value[idx], ...updated }
      MessagePlugin.success(t('userManagement.roles.updated'))
    }
    dialogOpen.value = false
  } catch (err: any) {
    MessagePlugin.error(err?.message || t('userManagement.updateFailed'))
  }
}

async function onDelete(role: RoleInfo) {
  // Use confirm() instead of t-dialog confirmation: the role-list view is
  // already pretty dense, and confirm() is good enough for this destructive
  // action that's protected by an in-use server-side check anyway.
  if (!window.confirm(t('userManagement.roles.deleteConfirm', { label: role.label }))) return
  try {
    await deleteRole(role.id)
    roles.value = roles.value.filter(r => r.id !== role.id)
    if (selectedRoleId.value === role.id) {
      selectedRoleId.value = roles.value[0]?.id ?? ''
    }
    MessagePlugin.success(t('userManagement.roles.deleted'))
  } catch (err: any) {
    // Backend signals "still referenced" with the "role_in_use:" prefix; we
    // surface that text directly — it includes the count of users + groups so
    // the operator can act without further drill-down.
    MessagePlugin.error(err?.message || t('userManagement.updateFailed'))
  }
}

onMounted(refreshRoles)
</script>

<style lang="less" scoped>
.roles-tab { display: flex; flex-direction: column; gap: 16px; }
.tab-header { display: flex; justify-content: space-between; align-items: flex-start; gap: 16px; }
.tab-header h3 { margin: 0 0 4px; font-size: 16px; }
.tab-description { color: var(--td-text-color-secondary); margin: 0; font-size: 13px; max-width: 600px; }

.loading-inline, .empty-state { color: var(--td-text-color-secondary); padding: 24px 0; text-align: center; }

.roles-grid { display: grid; grid-template-columns: 280px 1fr; gap: 16px; }

.roles-list { display: flex; flex-direction: column; gap: 6px; max-height: 600px; overflow-y: auto; }
.role-card {
  text-align: left; background: var(--td-bg-color-container); cursor: pointer;
  border: 1px solid var(--td-component-stroke); border-radius: 8px; padding: 12px; color: var(--td-text-color-primary);
  transition: border-color 0.15s, background 0.15s;
}
.role-card:hover { background: var(--td-bg-color-container-hover); }
.role-card.active { border-color: var(--td-brand-color); background: var(--td-brand-color-1); }
.role-card-head { display: flex; justify-content: space-between; align-items: center; margin-bottom: 2px; }
.role-label { font-weight: 500; }
.role-key { color: var(--td-text-color-secondary); font-size: 12px; font-family: var(--td-font-family-mono, monospace); }
.role-description { color: var(--td-text-color-secondary); font-size: 12px; margin-top: 4px; }

.badge.system { background: var(--td-bg-color-secondarycontainer); color: var(--td-text-color-secondary); padding: 2px 6px; border-radius: 4px; font-size: 11px; }

.matrix-panel { padding: 16px; background: var(--td-bg-color-container); border: 1px solid var(--td-component-stroke); border-radius: 8px; }
.matrix-head { display: flex; justify-content: space-between; align-items: flex-start; gap: 16px; margin-bottom: 12px; }
.matrix-head h4 { margin: 0 0 4px; font-size: 14px; }
.hint { color: var(--td-text-color-secondary); font-size: 12px; margin: 0; }
.locked-note { color: var(--td-text-color-secondary); font-size: 12px; margin: 0; font-style: italic; }
.matrix-actions { display: flex; gap: 8px; }

.matrix-table { display: flex; flex-direction: column; }
.matrix-row { display: grid; grid-template-columns: 1fr 200px; gap: 16px; padding: 10px 0; border-bottom: 1px solid var(--td-component-stroke); align-items: center; }
.matrix-header-row { font-weight: 500; color: var(--td-text-color-secondary); font-size: 12px; text-transform: uppercase; }
.matrix-permission-label { font-size: 14px; }
.matrix-permission-meta { font-size: 11px; color: var(--td-text-color-secondary); margin-top: 2px; }
.matrix-value select { width: 100%; height: 32px; border: 1px solid var(--td-component-stroke); border-radius: 6px; padding: 0 8px; background: var(--td-bg-color-container); color: var(--td-text-color-primary); }

.role-form { display: flex; flex-direction: column; gap: 12px; }
.role-form label { display: flex; flex-direction: column; gap: 4px; }
.role-form input { height: 32px; border: 1px solid var(--td-component-stroke); border-radius: 6px; padding: 0 8px; background: var(--td-bg-color-container); color: var(--td-text-color-primary); }
.form-label { font-size: 12px; color: var(--td-text-color-secondary); }
</style>
