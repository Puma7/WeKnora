/**
 * usePermission — single source of truth for "can the current user do X" in the UI.
 *
 * The auth store hydrates `auth.user.permissions` from the backend's /auth/me
 * payload, where every flag is a definite boolean (the resolver service guarantees
 * full population — see internal/application/service/permission_resolver.go).
 * The composable surfaces those flags as reactive refs so any component can ask
 * "is X allowed right now?" and get a value that updates when the user re-logs
 * or switches tenants.
 *
 * Two consumers:
 *   1. Components that need to react in script ("disable this submit button if
 *      the user can't manage_kbs"): import useCan and read .allowed in a
 *      computed.
 *   2. Components that need to gate a single element ("disable + tooltip when
 *      the user can't create_kb"): use the v-can directive (see
 *      src/directives/can.ts) which calls into this composable.
 *
 * Permission keys MUST match the catalog in
 * internal/types/role.go DefaultPermissionCatalog. Adding a new flag is a
 * 4-step change (backend constant, catalog entry, migration row, FE locale
 * string) — keep them in sync.
 */
import { computed, type ComputedRef } from 'vue'
import { useAuthStore } from '@/stores/auth'
import i18n from '@/i18n'

export type PermissionKey =
  | 'chat'
  | 'search'
  | 'create_kb'
  | 'invite_users'
  | 'manage_users'
  | 'manage_kbs'

/** Wire-format flag names (matching backend's UserPermissions JSON). */
type WirePermissionField =
  | 'can_chat'
  | 'can_search'
  | 'can_create_kb'
  | 'can_invite_users'
  | 'can_manage_users'
  | 'can_manage_kbs'

const FLAG_TO_FIELD: Record<PermissionKey, WirePermissionField> = {
  chat: 'can_chat',
  search: 'can_search',
  create_kb: 'can_create_kb',
  invite_users: 'can_invite_users',
  manage_users: 'can_manage_users',
  manage_kbs: 'can_manage_kbs',
}

export interface PermissionDecision {
  /** Whether the current user holds the permission. */
  allowed: boolean
  /** i18n-resolved reason text suitable for a tooltip. Always populated. */
  reason: string
}

/**
 * useCan(flag) returns a reactive decision for a single permission flag.
 *
 * Behavior when the user is not logged in or the permissions field is missing:
 * the decision is denied. This matches backend behavior: an unauthenticated
 * request fails the auth middleware before any flag check, so the FE staying
 * conservative here is correct.
 *
 * Behavior on flag-missing (typo or older backend): denied with a generic
 * reason. Strict mode would throw, but a console warning + soft-deny is more
 * forgiving for hot-deploys where backend and frontend versions briefly differ.
 */
export function useCan(flag: PermissionKey): ComputedRef<PermissionDecision> {
  const auth = useAuthStore()
  return computed<PermissionDecision>(() => {
    const field = FLAG_TO_FIELD[flag]
    if (!field) {
      // Defensive: the caller passed a flag the composable doesn't know.
      // Soft-deny rather than throw so a future feature flag rollout doesn't
      // crash the whole UI on stale clients.
      // eslint-disable-next-line no-console
      console.warn(`[useCan] unknown permission flag: ${String(flag)}`)
      return { allowed: false, reason: i18n.global.t('permissions.deniedDefault') }
    }
    const perms = auth.user?.permissions as Record<WirePermissionField, boolean> | undefined
    const allowed = !!(perms && perms[field] === true)
    return {
      allowed,
      reason: allowed ? '' : i18n.global.t('permissions.deniedDefault'),
    }
  })
}

/**
 * useAnyOf returns true if the user holds any of the listed flags.
 * Useful for menu-entry gating where multiple permissions could grant access.
 */
export function useAnyOf(...flags: PermissionKey[]): ComputedRef<PermissionDecision> {
  const auth = useAuthStore()
  return computed<PermissionDecision>(() => {
    const perms = auth.user?.permissions as Record<WirePermissionField, boolean> | undefined
    if (!perms) {
      return { allowed: false, reason: i18n.global.t('permissions.deniedDefault') }
    }
    const allowed = flags.some(f => perms[FLAG_TO_FIELD[f]] === true)
    return {
      allowed,
      reason: allowed ? '' : i18n.global.t('permissions.deniedDefault'),
    }
  })
}

/**
 * canSync is the imperative escape hatch for code paths that don't have a
 * reactive scope (e.g. router beforeEach guards, axios interceptors). Returns
 * the current decision but doesn't track changes — the caller is expected to
 * re-run on login/logout.
 */
export function canSync(flag: PermissionKey): boolean {
  const auth = useAuthStore()
  const field = FLAG_TO_FIELD[flag]
  if (!field) return false
  const perms = auth.user?.permissions as Record<WirePermissionField, boolean> | undefined
  return !!(perms && perms[field] === true)
}
