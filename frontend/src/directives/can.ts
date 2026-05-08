/**
 * v-can — automatic permission-gating directive.
 *
 * GOAL: a developer adding a new admin-only button writes one attribute and
 * gets the full UX (disabled visual + tooltip on hover with the reason
 * "Keine Berechtigung — beim Administrator erfragen") without importing the
 * auth store, writing v-if logic, or remembering to handle the no-permission
 * case. New buttons inherit gating by adding `v-can="'manage_kbs'"`.
 *
 * USAGE:
 *   <t-button v-can="'create_kb'">{{ t('kb.create') }}</t-button>
 *   <button v-can="'manage_users'">Edit user</button>
 *   <a v-can="'invite_users'" @click="...">Invite</a>
 *
 *   <!-- Multi-flag (any-of) form -->
 *   <button v-can="['manage_kbs', 'manage_users']">…</button>
 *
 *   <!-- Custom tooltip text -->
 *   <button v-can="{ flag: 'create_kb', reason: 'Trial limit reached' }">…</button>
 *
 * BEHAVIOR:
 *   - When allowed: removes the disabled attribute and the .permission-locked class.
 *   - When denied:
 *       1. sets `disabled` and `aria-disabled` on the element
 *       2. adds the `permission-locked` CSS class (defined in App.vue / global
 *          stylesheet) which dims the element and changes the cursor
 *       3. attaches a native `title` attribute as the always-available tooltip
 *          (works for any element; no TDesign Tooltip wrapper required)
 *       4. captures click in capture phase and stops propagation, so disabled
 *          looks like disabled (some libraries' "disabled" only changes style,
 *          not click handling)
 *
 * The directive evaluates inside an effectScope tied to the element so changes
 * to auth.user.permissions reactively update the DOM without manual rerenders.
 */
import type { Directive, DirectiveBinding } from 'vue'
import { effectScope, watch, type EffectScope } from 'vue'

import { useCan, useAnyOf, type PermissionKey, type PermissionDecision } from '@/composables/usePermission'

const LOCKED_CLASS = 'permission-locked'
const SCOPE_KEY = '__vCanScope__'
const CLICK_HANDLER_KEY = '__vCanClickHandler__'

interface VCanOptions {
  flag: PermissionKey | PermissionKey[]
  reason?: string
}

type VCanValue = PermissionKey | PermissionKey[] | VCanOptions

function normalize(raw: VCanValue): VCanOptions {
  if (typeof raw === 'string') return { flag: raw }
  if (Array.isArray(raw)) return { flag: raw }
  return raw
}

function applyDenied(el: HTMLElement, reason: string) {
  el.classList.add(LOCKED_CLASS)
  el.setAttribute('aria-disabled', 'true')
  el.setAttribute('title', reason)
  // Some buttons (e.g. <button>, <input type="button">) honour the disabled
  // attribute natively; setting it here disables the click listener too.
  // For elements that don't have a disabled attribute, we still install a
  // capture-phase click stopper below.
  ;(el as HTMLButtonElement).disabled = true
}

function applyAllowed(el: HTMLElement) {
  el.classList.remove(LOCKED_CLASS)
  el.removeAttribute('aria-disabled')
  el.removeAttribute('title')
  ;(el as HTMLButtonElement).disabled = false
}

function installClickGuard(el: HTMLElement) {
  if ((el as any)[CLICK_HANDLER_KEY]) return
  const handler = (event: Event) => {
    if (el.classList.contains(LOCKED_CLASS)) {
      event.preventDefault()
      event.stopImmediatePropagation()
    }
  }
  ;(el as any)[CLICK_HANDLER_KEY] = handler
  // Capture phase so we run before the component's own @click.
  el.addEventListener('click', handler, true)
}

function teardown(el: HTMLElement) {
  const scope = (el as any)[SCOPE_KEY] as EffectScope | undefined
  if (scope) {
    scope.stop()
    delete (el as any)[SCOPE_KEY]
  }
  const handler = (el as any)[CLICK_HANDLER_KEY] as EventListener | undefined
  if (handler) {
    el.removeEventListener('click', handler, true)
    delete (el as any)[CLICK_HANDLER_KEY]
  }
  applyAllowed(el)
}

function bindEffect(el: HTMLElement, opts: VCanOptions) {
  // Tear down any prior watcher (the binding value may have changed).
  teardown(el)
  installClickGuard(el)

  const scope = effectScope(true)
  ;(el as any)[SCOPE_KEY] = scope

  scope.run(() => {
    const decision = Array.isArray(opts.flag) ? useAnyOf(...opts.flag) : useCan(opts.flag)
    watch(
      decision,
      (d: PermissionDecision) => {
        if (d.allowed) {
          applyAllowed(el)
        } else {
          applyDenied(el, opts.reason || d.reason)
        }
      },
      { immediate: true },
    )
  })
}

export const vCan: Directive<HTMLElement, VCanValue> = {
  mounted(el, binding: DirectiveBinding<VCanValue>) {
    bindEffect(el, normalize(binding.value))
  },
  updated(el, binding: DirectiveBinding<VCanValue>) {
    // Only rebind when the binding value actually changed (deep-compare via
    // JSON serialization; the values are tiny strings/arrays so this is cheap
    // and correct for our shape).
    const prev = JSON.stringify(binding.oldValue ?? null)
    const next = JSON.stringify(binding.value ?? null)
    if (prev !== next) {
      bindEffect(el, normalize(binding.value))
    }
  },
  unmounted(el) {
    teardown(el)
  },
}
