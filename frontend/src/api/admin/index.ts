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
