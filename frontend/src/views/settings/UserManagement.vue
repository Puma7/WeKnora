<template>
  <div class="user-management">
    <div class="section-header">
      <h2>{{ t('userManagement.title') }}</h2>
      <p class="section-description">{{ t('userManagement.description') }}</p>
    </div>

    <div class="tabs">
      <button :class="{ active: tab === 'users' }" @click="tab = 'users'">
        {{ t('userManagement.tabs.users') }}
      </button>
      <button :class="{ active: tab === 'invitations' }" @click="tab = 'invitations'">
        {{ t('userManagement.tabs.invitations') }}
      </button>
      <button :class="{ active: tab === 'roles' }" @click="tab = 'roles'">
        {{ t('userManagement.tabs.roles') }}
      </button>
      <button :class="{ active: tab === 'groups' }" @click="tab = 'groups'">
        {{ t('userManagement.tabs.groups') }}
      </button>
    </div>

    <!-- Users tab -->
    <div v-if="tab === 'users'" class="panel">
      <div v-if="loadingUsers" class="loading-inline">
        <t-loading size="small" />
        <span>{{ t('userManagement.loading') }}</span>
      </div>
      <div v-else-if="users.length === 0" class="empty-state">
        {{ t('userManagement.noUsers') }}
      </div>
      <div v-else class="user-table">
        <div v-for="user in users" :key="user.id" class="user-row">
          <div class="user-meta">
            <div class="user-name">
              {{ user.username }}
              <span v-if="!user.is_active" class="status disabled">{{ t('userManagement.disabled') }}</span>
            </div>
            <div class="user-email">{{ user.email }}</div>
          </div>
          <div class="user-controls">
            <select
              :value="user.role"
              :disabled="!canEditUser(user)"
              @change="onRoleChange(user, ($event.target as HTMLSelectElement).value as UserRole)"
            >
              <option value="owner">{{ t('userManagement.role.owner') }}</option>
              <option value="admin">{{ t('userManagement.role.admin') }}</option>
              <option value="member">{{ t('userManagement.role.member') }}</option>
              <option value="viewer">{{ t('userManagement.role.viewer') }}</option>
            </select>

            <t-button
              size="small"
              variant="outline"
              :disabled="!canEditUser(user)"
              @click="openPermsDialog(user)"
            >
              {{ t('userManagement.permissions') }}
            </t-button>

            <t-button
              size="small"
              :theme="user.is_active ? 'warning' : 'success'"
              variant="outline"
              :disabled="!canEditUser(user) || user.id === currentUserId"
              @click="onToggleActive(user)"
            >
              {{ user.is_active ? t('userManagement.disable') : t('userManagement.enable') }}
            </t-button>
          </div>
        </div>
      </div>
    </div>

    <!-- Roles tab (lazy-mounted) -->
    <div v-else-if="tab === 'roles'" class="panel">
      <RolesTab />
    </div>

    <!-- Groups tab (lazy-mounted) -->
    <div v-else-if="tab === 'groups'" class="panel">
      <GroupsTab />
    </div>

    <!-- Invitations tab -->
    <div v-else class="panel">
      <div class="invite-form">
        <h3>{{ t('userManagement.invite.heading') }}</h3>
        <div class="form-row">
          <input
            v-model="newInvite.email"
            type="email"
            :placeholder="t('userManagement.invite.emailPlaceholder')"
            class="text-input"
          />
          <select v-model="newInvite.role" class="select-input">
            <option value="admin">{{ t('userManagement.role.admin') }}</option>
            <option value="member">{{ t('userManagement.role.member') }}</option>
            <option value="viewer">{{ t('userManagement.role.viewer') }}</option>
          </select>
          <input
            v-model.number="newInvite.validity_days"
            type="number"
            min="1"
            max="90"
            :placeholder="t('userManagement.invite.validityPlaceholder')"
            class="number-input"
          />
        </div>
        <div class="form-row">
          <input
            v-model="newInvite.note"
            type="text"
            :placeholder="t('userManagement.invite.notePlaceholder')"
            class="text-input wide"
          />
          <t-button v-can="'invite_users'" theme="primary" :disabled="!canSubmitInvite" @click="onCreateInvite">
            {{ t('userManagement.invite.submit') }}
          </t-button>
        </div>
        <div v-if="lastMagicLink" class="magic-link">
          <strong>{{ t('userManagement.invite.linkLabel') }}:</strong>
          <code>{{ lastMagicLink }}</code>
          <t-button size="small" variant="text" @click="copyLink(lastMagicLink)">
            {{ t('userManagement.invite.copy') }}
          </t-button>
        </div>
      </div>

      <h3>{{ t('userManagement.invite.listHeading') }}</h3>
      <div v-if="loadingInvites" class="loading-inline">
        <t-loading size="small" />
      </div>
      <div v-else-if="invitations.length === 0" class="empty-state">
        {{ t('userManagement.invite.noInvitations') }}
      </div>
      <div v-else class="invite-table">
        <div v-for="inv in invitations" :key="inv.id" class="invite-row">
          <div class="invite-meta">
            <div class="invite-email">{{ inv.email }}</div>
            <div class="invite-sub">
              {{ t('userManagement.role.' + inv.role) }} •
              {{ t('userManagement.invite.expiresAt') }} {{ formatDate(inv.expires_at) }}
            </div>
            <div v-if="inv.note" class="invite-note">{{ inv.note }}</div>
          </div>
          <div class="invite-controls">
            <span class="status" :class="inv.status">{{ t('userManagement.invite.status.' + inv.status) }}</span>
            <t-button
              v-if="inv.status === 'pending' && inv.magic_link_path"
              size="small"
              variant="text"
              @click="copyLink(absoluteLink(inv.magic_link_path))"
            >
              {{ t('userManagement.invite.copy') }}
            </t-button>
            <t-button
              v-if="inv.status === 'pending'"
              size="small"
              theme="danger"
              variant="outline"
              @click="onRevoke(inv)"
            >
              {{ t('userManagement.invite.revoke') }}
            </t-button>
          </div>
        </div>
      </div>
    </div>

    <!-- Permissions dialog -->
    <t-dialog
      :visible="permsDialogOpen"
      :header="t('userManagement.permissionsDialog.title')"
      :on-close="() => (permsDialogOpen = false)"
      :on-confirm="onSavePermissions"
    >
      <div v-if="permsTarget" class="perms-form">
        <div class="perms-hint">{{ t('userManagement.permissionsDialog.hint') }}</div>
        <label v-for="flag in permFlags" :key="flag" class="perms-row">
          <select v-model="permsDraft[flag]">
            <option :value="null">{{ t('userManagement.permissionsDialog.useDefault') }}</option>
            <option :value="true">{{ t('userManagement.permissionsDialog.allow') }}</option>
            <option :value="false">{{ t('userManagement.permissionsDialog.deny') }}</option>
          </select>
          <span>{{ t('userManagement.permissionsDialog.flags.' + flag) }}</span>
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
  type AdminUserInfo,
  type Invitation,
  type UserPermissions,
  type UserRole,
  createInvitation,
  listAdminUsers,
  listInvitations,
  revokeInvitation,
  setUserActive,
  updateUserPermissions,
  updateUserRole,
} from '@/api/admin'
import { useAuthStore } from '@/stores/auth'
import RolesTab from '@/views/settings/components/RolesTab.vue'
import GroupsTab from '@/views/settings/components/GroupsTab.vue'

const { t } = useI18n()
const auth = useAuthStore()
const currentUserId = computed(() => auth.user?.id || '')
// GEÄNDERT: typed role access (UserInfo.role is now a UserRole literal).
const currentUserRole = computed<UserRole>(() => (auth.user?.role as UserRole | undefined) || 'member')
const ROLE_LEVELS: Record<UserRole, number> = { owner: 4, admin: 3, member: 2, viewer: 1 }

const tab = ref<'users' | 'invitations' | 'roles' | 'groups'>('users')

// Users tab state
const users = ref<AdminUserInfo[]>([])
const loadingUsers = ref(false)

async function loadUsers() {
  loadingUsers.value = true
  try {
    const resp = await listAdminUsers(1, 200)
    users.value = resp?.users ?? []
  } catch (err: any) {
    MessagePlugin.error(err?.message || t('userManagement.loadFailed'))
  } finally {
    loadingUsers.value = false
  }
}

function canEditUser(user: AdminUserInfo): boolean {
  if (user.id === currentUserId.value) return false
  return ROLE_LEVELS[currentUserRole.value] > ROLE_LEVELS[user.role]
}

async function onRoleChange(user: AdminUserInfo, role: UserRole) {
  try {
    const updated = await updateUserRole(user.id, role)
    Object.assign(user, updated)
    MessagePlugin.success(t('userManagement.roleUpdated'))
  } catch (err: any) {
    MessagePlugin.error(err?.message || t('userManagement.updateFailed'))
    await loadUsers()
  }
}

async function onToggleActive(user: AdminUserInfo) {
  try {
    const updated = await setUserActive(user.id, !user.is_active)
    Object.assign(user, updated)
  } catch (err: any) {
    MessagePlugin.error(err?.message || t('userManagement.updateFailed'))
  }
}

// Permissions dialog state
const permFlags = [
  'can_chat', 'can_search', 'can_create_kb',
  'can_invite_users', 'can_manage_users', 'can_manage_kbs',
] as const
type PermFlag = typeof permFlags[number]
const permsDialogOpen = ref(false)
const permsTarget = ref<AdminUserInfo | null>(null)
const permsDraft = reactive<Record<PermFlag, boolean | null>>({
  can_chat: null,
  can_search: null,
  can_create_kb: null,
  can_invite_users: null,
  can_manage_users: null,
  can_manage_kbs: null,
})

function openPermsDialog(user: AdminUserInfo) {
  permsTarget.value = user
  for (const f of permFlags) {
    const value = (user.permissions as any)?.[f]
    permsDraft[f] = value === undefined ? null : value
  }
  permsDialogOpen.value = true
}

async function onSavePermissions() {
  if (!permsTarget.value) return
  // Send only explicit overrides; null means "use role default" -> drop the field.
  const payload: UserPermissions = {}
  for (const f of permFlags) {
    if (permsDraft[f] !== null) (payload as any)[f] = permsDraft[f]
  }
  try {
    const updated = await updateUserPermissions(permsTarget.value.id, payload)
    const idx = users.value.findIndex(u => u.id === updated.id)
    if (idx >= 0) users.value[idx] = updated
    permsDialogOpen.value = false
    MessagePlugin.success(t('userManagement.permissionsUpdated'))
  } catch (err: any) {
    MessagePlugin.error(err?.message || t('userManagement.updateFailed'))
  }
}

// Invitations tab state
const invitations = ref<Invitation[]>([])
const loadingInvites = ref(false)
const newInvite = reactive({
  email: '',
  role: 'member' as UserRole,
  validity_days: 7 as number | undefined,
  note: '',
})
const lastMagicLink = ref('')

const canSubmitInvite = computed(() => /.+@.+\..+/.test(newInvite.email))

async function loadInvitations() {
  loadingInvites.value = true
  try {
    invitations.value = await listInvitations()
  } catch (err: any) {
    MessagePlugin.error(err?.message || t('userManagement.loadFailed'))
  } finally {
    loadingInvites.value = false
  }
}

async function onCreateInvite() {
  try {
    const resp = await createInvitation({
      email: newInvite.email.trim(),
      role: newInvite.role,
      validity_days: newInvite.validity_days || 7,
      note: newInvite.note || undefined,
    })
    invitations.value.unshift(resp.data)
    lastMagicLink.value = absoluteLink(resp.data.magic_link_path || `/invite/${resp.token}`)
    newInvite.email = ''
    newInvite.note = ''
    MessagePlugin.success(t('userManagement.invite.created'))
  } catch (err: any) {
    MessagePlugin.error(err?.message || t('userManagement.invite.createFailed'))
  }
}

async function onRevoke(inv: Invitation) {
  try {
    await revokeInvitation(inv.id)
    inv.status = 'revoked'
  } catch (err: any) {
    MessagePlugin.error(err?.message || t('userManagement.updateFailed'))
  }
}

function absoluteLink(path: string): string {
  return `${window.location.origin}${path}`
}

async function copyLink(link: string) {
  try {
    await navigator.clipboard.writeText(link)
    MessagePlugin.success(t('userManagement.invite.copied'))
  } catch {
    MessagePlugin.warning(link)
  }
}

function formatDate(s: string | null | undefined) {
  if (!s) return '-'
  return new Date(s).toLocaleString()
}

onMounted(() => {
  loadUsers()
  loadInvitations()
})
</script>

<style lang="less" scoped>
.user-management { padding: 0 4px; }
.section-header h2 { font-size: 18px; font-weight: 600; margin: 0 0 4px; }
.section-description { color: var(--td-text-color-secondary); font-size: 13px; margin: 0 0 16px; }

.tabs { display: flex; gap: 8px; margin-bottom: 16px; border-bottom: 1px solid var(--td-component-stroke); }
.tabs button {
  background: transparent; border: none; padding: 8px 12px; cursor: pointer;
  color: var(--td-text-color-secondary); border-bottom: 2px solid transparent;
}
.tabs button.active { color: var(--td-brand-color); border-bottom-color: var(--td-brand-color); font-weight: 500; }

.panel { display: flex; flex-direction: column; gap: 16px; }

.user-table, .invite-table { display: flex; flex-direction: column; gap: 8px; }
.user-row, .invite-row {
  display: flex; align-items: center; justify-content: space-between;
  padding: 12px; border: 1px solid var(--td-component-stroke); border-radius: 8px;
  background: var(--td-bg-color-container);
}
.user-meta, .invite-meta { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
.user-name { font-weight: 500; display: flex; align-items: center; gap: 8px; }
.user-email, .invite-sub { color: var(--td-text-color-secondary); font-size: 12px; }
.invite-note { color: var(--td-text-color-secondary); font-size: 12px; font-style: italic; }
.user-controls, .invite-controls { display: flex; align-items: center; gap: 8px; flex-shrink: 0; }
.user-controls select, .form-row select, .form-row input { height: 32px; border: 1px solid var(--td-component-stroke); border-radius: 6px; padding: 0 8px; background: var(--td-bg-color-container); color: var(--td-text-color-primary); }

.status { font-size: 11px; padding: 2px 6px; border-radius: 4px; background: var(--td-bg-color-secondarycontainer); color: var(--td-text-color-secondary); }
.status.pending { background: rgba(7,192,95,0.1); color: var(--td-brand-color); }
.status.accepted { background: rgba(7,192,95,0.15); color: var(--td-brand-color); }
.status.revoked, .status.disabled { background: rgba(220,38,38,0.1); color: #b91c1c; }
.status.expired { background: rgba(245,158,11,0.1); color: #b45309; }

.invite-form { padding: 16px; border: 1px dashed var(--td-component-stroke); border-radius: 8px; }
.invite-form h3 { font-size: 14px; margin: 0 0 12px; }
.form-row { display: flex; gap: 8px; margin-bottom: 8px; align-items: center; }
.text-input { flex: 1; }
.text-input.wide { flex: 2; }
.number-input { width: 80px; }
.magic-link { background: var(--td-bg-color-secondarycontainer); padding: 8px 12px; border-radius: 6px; word-break: break-all; font-size: 12px; }
.magic-link code { color: var(--td-brand-color); }

.empty-state, .loading-inline { color: var(--td-text-color-secondary); padding: 24px 0; text-align: center; }

.perms-form { display: flex; flex-direction: column; gap: 12px; }
.perms-row { display: flex; gap: 12px; align-items: center; }
.perms-row select { width: 130px; }
.perms-hint { color: var(--td-text-color-secondary); font-size: 12px; }
</style>
