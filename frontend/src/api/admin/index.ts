import { get, post, put, del } from '@/utils/request'

// ----- Types -----

export type UserRole = 'owner' | 'admin' | 'member' | 'viewer'

export type RegistrationMode = 'open' | 'invite_only' | 'whitelist' | 'disabled'

/**
 * Permission overrides. `null`/missing means "use the role's default".
 */
export interface UserPermissions {
  can_chat?: boolean | null
  can_search?: boolean | null
  can_create_kb?: boolean | null
  can_invite_users?: boolean | null
  can_manage_users?: boolean | null
  can_manage_kbs?: boolean | null
}

export interface AdminUserInfo {
  id: string
  username: string
  email: string
  avatar?: string
  tenant_id: number
  is_active: boolean
  can_access_all_tenants?: boolean
  role: UserRole
  permissions: UserPermissions
  invited_by_user_id?: string
  created_at: string
  updated_at: string
}

export interface ListUsersResponse {
  users: AdminUserInfo[]
  total: number
}

export interface RegistrationModePublic {
  mode: RegistrationMode
  has_email_whitelist: boolean
  allow_self_register: boolean
  allow_invited_register: boolean
}

export interface InvitationKBGrant {
  knowledge_base_id: string
  permission: 'viewer' | 'editor' | 'admin'
}

export interface CreateInvitationRequest {
  email: string
  role?: UserRole
  permissions?: UserPermissions
  knowledge_base_grants?: InvitationKBGrant[]
  validity_days?: number
  note?: string
}

export interface Invitation {
  id: string
  email: string
  tenant_id: number
  invited_by_user_id: string
  invited_by_username?: string
  role: UserRole
  permissions?: UserPermissions
  knowledge_base_grants?: InvitationKBGrant[]
  status: 'pending' | 'accepted' | 'revoked' | 'expired'
  expires_at: string
  accepted_at?: string | null
  accepted_user_id?: string
  revoked_at?: string | null
  note?: string
  created_at: string
  /** Relative path the admin can share, e.g. "/invite/<token>". */
  magic_link_path?: string
}

export interface CreateInvitationResponse {
  data: Invitation
  /** Raw token; exposed only on creation so the admin can copy the magic link. */
  token: string
}

export interface InvitationPublicView {
  email: string
  role: UserRole
  invited_by_username?: string
  tenant_name?: string
  expires_at: string
  note?: string
}

// ----- Registration mode (public, no auth required) -----

/**
 * Returns the active registration mode. Used to show or hide the public signup form.
 * Endpoint is in the auth-middleware whitelist, so the call works even when the user is signed out.
 */
export async function getRegistrationMode(): Promise<RegistrationModePublic> {
  const resp = await get('/api/v1/auth/registration-mode') as any
  return resp?.data as RegistrationModePublic
}

/**
 * Returns the public preview of an invitation by its raw token.
 * Used by the /invite/:token landing page so the invitee sees who invited them
 * and which role they'll get before creating an account.
 */
export async function previewInvitation(token: string): Promise<InvitationPublicView> {
  const resp = await get(`/api/v1/auth/invitations/${encodeURIComponent(token)}`) as any
  return resp?.data as InvitationPublicView
}

// ----- Admin: users -----

export async function listAdminUsers(page = 1, pageSize = 50): Promise<ListUsersResponse> {
  const resp = await get(`/api/v1/admin/users?page=${page}&page_size=${pageSize}`) as any
  return resp?.data as ListUsersResponse
}

/**
 * Server-side user search scoped to the current tenant. Use this for pickers
 * (KB grants, invitation autofill) instead of pulling the full user list and
 * filtering client-side.
 */
export async function searchAdminUsers(query: string, limit = 20): Promise<AdminUserInfo[]> {
  const q = query.trim()
  if (!q) return []
  const resp = await get(`/api/v1/admin/users/search?q=${encodeURIComponent(q)}&limit=${limit}`) as any
  return (resp?.data ?? []) as AdminUserInfo[]
}

export async function updateUserRole(userId: string, role: UserRole): Promise<AdminUserInfo> {
  const resp = await put(`/api/v1/admin/users/${userId}/role`, { role }) as any
  return resp?.data as AdminUserInfo
}

export async function updateUserPermissions(userId: string, permissions: UserPermissions): Promise<AdminUserInfo> {
  const resp = await put(`/api/v1/admin/users/${userId}/permissions`, { permissions }) as any
  return resp?.data as AdminUserInfo
}

export async function setUserActive(userId: string, isActive: boolean): Promise<AdminUserInfo> {
  const resp = await put(`/api/v1/admin/users/${userId}/active`, { is_active: isActive }) as any
  return resp?.data as AdminUserInfo
}

// ----- Admin: invitations -----

export async function listInvitations(): Promise<Invitation[]> {
  const resp = await get('/api/v1/admin/invitations') as any
  return (resp?.data ?? []) as Invitation[]
}

export async function createInvitation(req: CreateInvitationRequest): Promise<CreateInvitationResponse> {
  const resp = await post('/api/v1/admin/invitations', req) as any
  return { data: resp?.data as Invitation, token: resp?.token as string }
}

export async function revokeInvitation(id: string): Promise<void> {
  await del(`/api/v1/admin/invitations/${id}`)
}

// ----- KB user permissions -----

export interface KBUserPermissionResponse {
  id: string
  knowledge_base_id: string
  user_id: string
  username: string
  email: string
  avatar?: string
  permission: 'viewer' | 'editor' | 'admin'
  granted_by_user_id: string
  granted_by_name?: string
  created_at: string
  updated_at: string
}

export async function listKBUserPermissions(kbId: string): Promise<KBUserPermissionResponse[]> {
  const resp = await get(`/api/v1/knowledge-bases/${kbId}/user-permissions`) as any
  return (resp?.data ?? []) as KBUserPermissionResponse[]
}

export async function grantKBUserPermission(kbId: string, userId: string, permission: 'viewer' | 'editor' | 'admin'): Promise<KBUserPermissionResponse> {
  const resp = await post(`/api/v1/knowledge-bases/${kbId}/user-permissions`, { user_id: userId, permission }) as any
  return resp?.data as KBUserPermissionResponse
}

export async function updateKBUserPermission(kbId: string, grantId: string, permission: 'viewer' | 'editor' | 'admin'): Promise<KBUserPermissionResponse> {
  const resp = await put(`/api/v1/knowledge-bases/${kbId}/user-permissions/${grantId}`, { permission }) as any
  return resp?.data as KBUserPermissionResponse
}

export async function revokeKBUserPermission(kbId: string, grantId: string): Promise<void> {
  await del(`/api/v1/knowledge-bases/${kbId}/user-permissions/${grantId}`)
}

// ----- Admin: roles + permission matrix -----

/**
 * Permission catalog entry. The backend exposes the catalog under
 * /admin/roles/permission-catalog and also embeds it in the list-roles response
 * so the matrix editor can render with a single round-trip.
 */
export interface PermissionCatalogEntry {
  key: string
  label: string
  category: string
  description?: string
}

export interface RoleInfo {
  id: string
  tenant_id: number
  key: string
  label: string
  description?: string
  is_system: boolean
  /** Permission flag map: { "create_kb": true, "manage_users": false, ... }. */
  permissions: Record<string, boolean>
}

export async function listRoles(): Promise<{ roles: RoleInfo[]; catalog: PermissionCatalogEntry[] }> {
  const resp = await get('/api/v1/admin/roles') as any
  return {
    roles: (resp?.data ?? []) as RoleInfo[],
    catalog: (resp?.catalog ?? []) as PermissionCatalogEntry[],
  }
}

export async function getPermissionCatalog(): Promise<PermissionCatalogEntry[]> {
  const resp = await get('/api/v1/admin/roles/permission-catalog') as any
  return (resp?.data ?? []) as PermissionCatalogEntry[]
}

export async function createRole(req: { key: string; label: string; description?: string }): Promise<RoleInfo> {
  const resp = await post('/api/v1/admin/roles', req) as any
  return resp?.data as RoleInfo
}

export async function updateRole(roleId: string, req: { key?: string; label?: string; description?: string }): Promise<RoleInfo> {
  // Use generic patch via fetch since utils/request only exports get/post/put/del.
  // Sending a PUT-shaped payload to PATCH would still pass through, but to keep
  // semantics aligned we use the same helpers for write — the backend accepts
  // PATCH through Gin's PATCH binding, falling back to PUT here so we don't
  // need to extend the request util just for this one route.
  const resp = await put(`/api/v1/admin/roles/${roleId}`, req) as any
  return resp?.data as RoleInfo
}

export async function deleteRole(roleId: string): Promise<void> {
  await del(`/api/v1/admin/roles/${roleId}`)
}

/**
 * Set or clear a single matrix cell.
 *   allowed === true   -> grant the flag
 *   allowed === false  -> explicit deny
 *   allowed === null   -> clear (revert to system default)
 */
export async function setRolePermission(roleId: string, key: string, allowed: boolean | null): Promise<void> {
  // Backend expects { allowed: bool | omitted }. We always send the field;
  // when null, omit it via JSON shape so the resolver clears the row.
  const body: any = {}
  if (allowed !== null) body.allowed = allowed
  await put(`/api/v1/admin/roles/${roleId}/permissions/${encodeURIComponent(key)}`, body)
}

// ----- Admin: groups -----

export interface GroupInfo {
  id: string
  tenant_id: number
  name: string
  description?: string
  role_id?: string | null
  permissions: UserPermissions
  member_ids?: string[]
  member_count: number
}

export async function listGroups(): Promise<GroupInfo[]> {
  const resp = await get('/api/v1/admin/groups') as any
  return (resp?.data ?? []) as GroupInfo[]
}

export async function getGroup(groupId: string): Promise<GroupInfo> {
  const resp = await get(`/api/v1/admin/groups/${groupId}`) as any
  return resp?.data as GroupInfo
}

export async function createGroup(req: { name: string; description?: string; role_id?: string | null }): Promise<GroupInfo> {
  const resp = await post('/api/v1/admin/groups', req) as any
  return resp?.data as GroupInfo
}

export async function updateGroup(groupId: string, req: { name?: string; description?: string; role_id?: string | null }): Promise<GroupInfo> {
  const resp = await put(`/api/v1/admin/groups/${groupId}`, req) as any
  return resp?.data as GroupInfo
}

export async function deleteGroup(groupId: string): Promise<void> {
  await del(`/api/v1/admin/groups/${groupId}`)
}

export async function listGroupMembers(groupId: string): Promise<AdminUserInfo[]> {
  const resp = await get(`/api/v1/admin/groups/${groupId}/members`) as any
  return (resp?.data ?? []) as AdminUserInfo[]
}

export async function addGroupMember(groupId: string, userId: string): Promise<void> {
  await post(`/api/v1/admin/groups/${groupId}/members`, { user_id: userId })
}

export async function removeGroupMember(groupId: string, userId: string): Promise<void> {
  await del(`/api/v1/admin/groups/${groupId}/members/${userId}`)
}

export async function setGroupPermission(groupId: string, key: string, allowed: boolean | null): Promise<void> {
  const body: any = {}
  if (allowed !== null) body.allowed = allowed
  await put(`/api/v1/admin/groups/${groupId}/permissions/${encodeURIComponent(key)}`, body)
}
