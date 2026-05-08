<template>
  <div class="groups-tab">
    <div class="tab-header">
      <div>
        <h3>{{ t('userManagement.groups.heading') }}</h3>
        <p class="tab-description">{{ t('userManagement.groups.description') }}</p>
      </div>
      <t-button theme="primary" @click="openCreateDialog">
        {{ t('userManagement.groups.create') }}
      </t-button>
    </div>

    <div v-if="loading" class="loading-inline">
      <t-loading size="small" />
      <span>{{ t('userManagement.loading') }}</span>
    </div>
    <div v-else-if="groups.length === 0" class="empty-state">
      {{ t('userManagement.groups.empty') }}
    </div>

    <div v-else class="groups-grid">
      <!-- Left: group list -->
      <div class="groups-list">
        <button
          v-for="g in groups"
          :key="g.id"
          :class="['group-card', { active: selectedGroupId === g.id }]"
          @click="onSelectGroup(g.id)"
        >
          <div class="group-name">{{ g.name }}</div>
          <div v-if="g.description" class="group-description">{{ g.description }}</div>
          <div class="group-meta">
            {{ t('userManagement.groups.memberCount', { count: g.member_count }) }}
            <span v-if="g.role_id" class="role-pill">{{ roleLabelFor(g.role_id) }}</span>
          </div>
        </button>
      </div>

      <!-- Right: details + members -->
      <div v-if="selectedGroup" class="group-detail">
        <div class="detail-head">
          <div>
            <h4>{{ selectedGroup.name }}</h4>
            <p v-if="selectedGroup.description" class="hint">{{ selectedGroup.description }}</p>
          </div>
          <div class="detail-actions">
            <t-button size="small" variant="outline" @click="openEditDialog(selectedGroup)">
              {{ t('common.edit') }}
            </t-button>
            <t-button size="small" theme="danger" variant="outline" @click="onDelete(selectedGroup)">
              {{ t('common.delete') }}
            </t-button>
          </div>
        </div>

        <div class="detail-section">
          <div class="section-label">{{ t('userManagement.groups.roleLabel') }}</div>
          <select :value="selectedGroup.role_id ?? ''" @change="onRoleChange(($event.target as HTMLSelectElement).value)">
            <option value="">{{ t('userManagement.groups.noRole') }}</option>
            <option v-for="r in roleOptions" :key="r.id" :value="r.id">{{ r.label }}</option>
          </select>
        </div>

        <div class="detail-section">
          <div class="section-head">
            <div class="section-label">{{ t('userManagement.groups.members') }} ({{ members.length }})</div>
            <t-button size="small" variant="outline" @click="memberPickerOpen = true">
              {{ t('userManagement.groups.addMember') }}
            </t-button>
          </div>
          <div v-if="loadingMembers" class="loading-inline">
            <t-loading size="small" />
          </div>
          <div v-else-if="members.length === 0" class="empty-state small">
            —
          </div>
          <div v-else class="member-list">
            <div v-for="m in members" :key="m.id" class="member-row">
              <div>
                <div class="member-name">{{ m.username }}</div>
                <div class="member-email">{{ m.email }}</div>
              </div>
              <t-button size="small" variant="text" theme="danger" @click="onRemoveMember(m)">
                {{ t('userManagement.groups.removeMember') }}
              </t-button>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- Create / edit dialog -->
    <t-dialog
      :visible="dialogOpen"
      :header="dialogMode === 'create' ? t('userManagement.groups.createTitle') : t('userManagement.groups.editTitle')"
      :on-close="() => (dialogOpen = false)"
      :on-confirm="onSubmitDialog"
    >
      <div class="group-form">
        <label>
          <span class="form-label">{{ t('userManagement.groups.nameLabel') }}</span>
          <input v-model="form.name" :placeholder="t('userManagement.groups.namePlaceholder')" />
        </label>
        <label>
          <span class="form-label">{{ t('common.description') || 'Description' }}</span>
          <input v-model="form.description" :placeholder="t('userManagement.groups.descriptionPlaceholder')" />
        </label>
        <label>
          <span class="form-label">{{ t('userManagement.groups.roleLabel') }}</span>
          <select v-model="form.role_id">
            <option value="">{{ t('userManagement.groups.noRole') }}</option>
            <option v-for="r in roleOptions" :key="r.id" :value="r.id">{{ r.label }}</option>
          </select>
        </label>
      </div>
    </t-dialog>

    <!-- Member picker -->
    <t-dialog
      :visible="memberPickerOpen"
      :header="t('userManagement.groups.addMember')"
      :on-close="() => (memberPickerOpen = false)"
      :footer="false"
    >
      <div class="picker">
        <input
          v-model="memberQuery"
          :placeholder="t('userManagement.groups.addMemberSearchPlaceholder')"
          @input="onSearchMembers"
        />
        <div class="picker-results">
          <div v-if="memberSearchLoading" class="loading-inline"><t-loading size="small" /></div>
          <div
            v-else
            v-for="user in memberSearchResults"
            :key="user.id"
            class="picker-row"
            @click="onAddMember(user)"
          >
            <div>
              <div class="member-name">{{ user.username }}</div>
              <div class="member-email">{{ user.email }}</div>
            </div>
            <span class="add-icon">+</span>
          </div>
          <div v-if="!memberSearchLoading && memberSearchResults.length === 0 && memberQuery.length > 0" class="empty-state small">
            —
          </div>
        </div>
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
  type GroupInfo,
  type RoleInfo,
  addGroupMember,
  createGroup,
  deleteGroup,
  getGroup,
  listGroupMembers,
  listGroups,
  listRoles,
  removeGroupMember,
  searchAdminUsers,
  updateGroup,
} from '@/api/admin'

const { t } = useI18n()

const loading = ref(false)
const groups = ref<GroupInfo[]>([])
const roleOptions = ref<RoleInfo[]>([])
const selectedGroupId = ref<string>('')
const selectedGroup = ref<GroupInfo | null>(null)
const members = ref<AdminUserInfo[]>([])
const loadingMembers = ref(false)

function roleLabelFor(roleId: string | null | undefined): string {
  if (!roleId) return ''
  return roleOptions.value.find(r => r.id === roleId)?.label ?? roleId
}

async function refreshGroups() {
  loading.value = true
  try {
    const [g, rs] = await Promise.all([listGroups(), listRoles()])
    groups.value = g
    roleOptions.value = rs.roles
    if (!selectedGroupId.value && g.length > 0) {
      await onSelectGroup(g[0].id)
    }
  } catch (err: any) {
    MessagePlugin.error(err?.message || t('userManagement.loadFailed'))
  } finally {
    loading.value = false
  }
}

async function onSelectGroup(id: string) {
  selectedGroupId.value = id
  loadingMembers.value = true
  try {
    const [g, m] = await Promise.all([getGroup(id), listGroupMembers(id)])
    selectedGroup.value = g
    members.value = m
    // Sync the listing snapshot's member_count with the freshly-loaded members
    // count so the left rail reflects post-edit state without a refetch.
    const idx = groups.value.findIndex(x => x.id === id)
    if (idx >= 0) groups.value[idx].member_count = m.length
  } catch (err: any) {
    MessagePlugin.error(err?.message || t('userManagement.loadFailed'))
  } finally {
    loadingMembers.value = false
  }
}

async function onRoleChange(newRoleId: string) {
  if (!selectedGroup.value) return
  const role_id = newRoleId === '' ? null : newRoleId
  try {
    const updated = await updateGroup(selectedGroup.value.id, {
      name: selectedGroup.value.name,
      description: selectedGroup.value.description,
      role_id,
    })
    selectedGroup.value = updated
    const idx = groups.value.findIndex(g => g.id === updated.id)
    if (idx >= 0) groups.value[idx] = { ...groups.value[idx], ...updated }
    MessagePlugin.success(t('userManagement.groups.updated'))
  } catch (err: any) {
    MessagePlugin.error(err?.message || t('userManagement.updateFailed'))
  }
}

// Create / edit dialog
const dialogOpen = ref(false)
const dialogMode = ref<'create' | 'edit'>('create')
const form = reactive<{ id?: string; name: string; description: string; role_id: string }>({
  name: '',
  description: '',
  role_id: '',
})

function openCreateDialog() {
  dialogMode.value = 'create'
  form.id = undefined
  form.name = ''
  form.description = ''
  form.role_id = ''
  dialogOpen.value = true
}

function openEditDialog(group: GroupInfo) {
  dialogMode.value = 'edit'
  form.id = group.id
  form.name = group.name
  form.description = group.description ?? ''
  form.role_id = group.role_id ?? ''
  dialogOpen.value = true
}

async function onSubmitDialog() {
  try {
    if (dialogMode.value === 'create') {
      const created = await createGroup({
        name: form.name.trim(),
        description: form.description.trim() || undefined,
        role_id: form.role_id || null,
      })
      groups.value.push(created)
      await onSelectGroup(created.id)
      MessagePlugin.success(t('userManagement.groups.created'))
    } else if (form.id) {
      const updated = await updateGroup(form.id, {
        name: form.name.trim(),
        description: form.description.trim() || undefined,
        role_id: form.role_id || null,
      })
      const idx = groups.value.findIndex(g => g.id === updated.id)
      if (idx >= 0) groups.value[idx] = { ...groups.value[idx], ...updated }
      if (selectedGroup.value?.id === updated.id) selectedGroup.value = updated
      MessagePlugin.success(t('userManagement.groups.updated'))
    }
    dialogOpen.value = false
  } catch (err: any) {
    MessagePlugin.error(err?.message || t('userManagement.updateFailed'))
  }
}

async function onDelete(group: GroupInfo) {
  if (!window.confirm(t('userManagement.groups.deleteConfirm', { name: group.name }))) return
  try {
    await deleteGroup(group.id)
    groups.value = groups.value.filter(g => g.id !== group.id)
    if (selectedGroupId.value === group.id) {
      selectedGroupId.value = groups.value[0]?.id ?? ''
      if (selectedGroupId.value) {
        await onSelectGroup(selectedGroupId.value)
      } else {
        selectedGroup.value = null
        members.value = []
      }
    }
    MessagePlugin.success(t('userManagement.groups.deleted'))
  } catch (err: any) {
    MessagePlugin.error(err?.message || t('userManagement.updateFailed'))
  }
}

// Member picker
const memberPickerOpen = ref(false)
const memberQuery = ref('')
const memberSearchLoading = ref(false)
const memberSearchResults = ref<AdminUserInfo[]>([])

let searchToken = 0
async function onSearchMembers() {
  const q = memberQuery.value.trim()
  if (!q) {
    memberSearchResults.value = []
    return
  }
  // Token-based race protection: a stale slow response can't replace a newer
  // result if the user has typed more characters in the meantime.
  const myToken = ++searchToken
  memberSearchLoading.value = true
  try {
    const results = await searchAdminUsers(q, 20)
    if (myToken !== searchToken) return
    // Filter out users that are already members of this group.
    const memberIds = new Set(members.value.map(m => m.id))
    memberSearchResults.value = results.filter(u => !memberIds.has(u.id))
  } catch {
    if (myToken === searchToken) memberSearchResults.value = []
  } finally {
    if (myToken === searchToken) memberSearchLoading.value = false
  }
}

async function onAddMember(user: AdminUserInfo) {
  if (!selectedGroup.value) return
  try {
    await addGroupMember(selectedGroup.value.id, user.id)
    members.value.push(user)
    memberSearchResults.value = memberSearchResults.value.filter(u => u.id !== user.id)
    MessagePlugin.success(t('userManagement.groups.updated'))
  } catch (err: any) {
    MessagePlugin.error(err?.message || t('userManagement.updateFailed'))
  }
}

async function onRemoveMember(user: AdminUserInfo) {
  if (!selectedGroup.value) return
  try {
    await removeGroupMember(selectedGroup.value.id, user.id)
    members.value = members.value.filter(m => m.id !== user.id)
  } catch (err: any) {
    MessagePlugin.error(err?.message || t('userManagement.updateFailed'))
  }
}

onMounted(refreshGroups)
</script>

<style lang="less" scoped>
.groups-tab { display: flex; flex-direction: column; gap: 16px; }
.tab-header { display: flex; justify-content: space-between; align-items: flex-start; gap: 16px; }
.tab-header h3 { margin: 0 0 4px; font-size: 16px; }
.tab-description { color: var(--td-text-color-secondary); margin: 0; font-size: 13px; max-width: 600px; }

.loading-inline, .empty-state { color: var(--td-text-color-secondary); padding: 24px 0; text-align: center; }
.empty-state.small { padding: 8px 0; }

.groups-grid { display: grid; grid-template-columns: 280px 1fr; gap: 16px; }
.groups-list { display: flex; flex-direction: column; gap: 6px; max-height: 600px; overflow-y: auto; }
.group-card {
  text-align: left; cursor: pointer; background: var(--td-bg-color-container);
  border: 1px solid var(--td-component-stroke); border-radius: 8px; padding: 12px; color: var(--td-text-color-primary);
}
.group-card:hover { background: var(--td-bg-color-container-hover); }
.group-card.active { border-color: var(--td-brand-color); background: var(--td-brand-color-1); }
.group-name { font-weight: 500; margin-bottom: 2px; }
.group-description { color: var(--td-text-color-secondary); font-size: 12px; margin-bottom: 4px; }
.group-meta { color: var(--td-text-color-secondary); font-size: 12px; display: flex; gap: 8px; align-items: center; }
.role-pill { background: var(--td-bg-color-secondarycontainer); padding: 1px 6px; border-radius: 4px; font-size: 11px; }

.group-detail { padding: 16px; background: var(--td-bg-color-container); border: 1px solid var(--td-component-stroke); border-radius: 8px; display: flex; flex-direction: column; gap: 16px; }
.detail-head { display: flex; justify-content: space-between; align-items: flex-start; gap: 12px; }
.detail-head h4 { margin: 0 0 4px; font-size: 14px; }
.detail-actions { display: flex; gap: 8px; }

.section-head { display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px; }
.section-label { font-size: 12px; font-weight: 500; color: var(--td-text-color-secondary); }
.detail-section select { width: 240px; height: 32px; border: 1px solid var(--td-component-stroke); border-radius: 6px; padding: 0 8px; background: var(--td-bg-color-container); color: var(--td-text-color-primary); }

.member-list { display: flex; flex-direction: column; gap: 4px; }
.member-row { display: flex; justify-content: space-between; align-items: center; padding: 8px 12px; background: var(--td-bg-color-secondarycontainer); border-radius: 6px; }
.member-name { font-weight: 500; }
.member-email { color: var(--td-text-color-secondary); font-size: 12px; }

.group-form { display: flex; flex-direction: column; gap: 12px; }
.group-form label { display: flex; flex-direction: column; gap: 4px; }
.group-form input, .group-form select { height: 32px; border: 1px solid var(--td-component-stroke); border-radius: 6px; padding: 0 8px; background: var(--td-bg-color-container); color: var(--td-text-color-primary); }
.form-label { font-size: 12px; color: var(--td-text-color-secondary); }

.picker input { width: 100%; height: 32px; border: 1px solid var(--td-component-stroke); border-radius: 6px; padding: 0 8px; background: var(--td-bg-color-container); color: var(--td-text-color-primary); margin-bottom: 8px; }
.picker-results { max-height: 320px; overflow-y: auto; display: flex; flex-direction: column; gap: 4px; }
.picker-row { display: flex; justify-content: space-between; align-items: center; padding: 8px 12px; background: var(--td-bg-color-secondarycontainer); border-radius: 6px; cursor: pointer; }
.picker-row:hover { background: var(--td-bg-color-secondarycontainer-hover); }
.add-icon { font-weight: 600; color: var(--td-brand-color); font-size: 18px; }
</style>
