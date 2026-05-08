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
// NEU: stash for the element's original `title` attribute. Set once on the
// first bindEffect call and restored on every transition into "allowed" so
// accessibility tooltips set elsewhere are not nuked by v-can.
const ORIGINAL_TITLE_KEY = '__vCanOriginalTitle__'
// NEU: MutationObserver handle so a parent component re-rendering and
// resetting `disabled` / `class` doesn't silently un-lock the element.
const OBSERVER_KEY = '__vCanObserver__'
// NEU: latest decision snapshot, consulted by the MutationObserver to decide
// whether to re-apply the locked state without re-running the resolver.
const LAST_DECISION_KEY = '__vCanLastDecision__'

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

// GEÄNDERT: restores the element's original title attribute (if any) instead
// of unconditionally removing it. The original value was captured once in
// bindEffect via captureOriginalTitle.
function applyAllowed(el: HTMLElement) {
  el.classList.remove(LOCKED_CLASS)
  el.removeAttribute('aria-disabled')
  const original = (el as any)[ORIGINAL_TITLE_KEY] as string | null | undefined
  if (original) {
    el.setAttribute('title', original)
  } else {
    el.removeAttribute('title')
  }
  ;(el as HTMLButtonElement).disabled = false
}

// captureOriginalTitle remembers whatever title attribute was on the element
// BEFORE v-can first touched it. Idempotent across re-binds because we only
// write the property once. NEU.
function captureOriginalTitle(el: HTMLElement) {
  if (Object.prototype.hasOwnProperty.call(el, ORIGINAL_TITLE_KEY)) return
  const current = el.getAttribute('title')
  ;(el as any)[ORIGINAL_TITLE_KEY] = current ?? null
}

function installClickGuard(el: HTMLElement) {
  if ((el as any)[CLICK_HANDLER_KEY]) return
  const handler = (event: Event) => {
    if (el.classList.contains(LOCKED_CLASS)) {
      // GEÄNDERT: keydown also covered (Space/Enter on focusable buttons).
      // For keydown we additionally check the key so we don't accidentally
      // swallow Tab / arrow keys used for focus management.
      if (event.type === 'keydown') {
        const key = (event as KeyboardEvent).key
        if (key !== 'Enter' && key !== ' ' && key !== 'Spacebar') return
      }
      event.preventDefault()
      event.stopImmediatePropagation()
    }
  }
  ;(el as any)[CLICK_HANDLER_KEY] = handler
  // Capture phase so we run before the component's own @click.
  el.addEventListener('click', handler, true)
  // NEU: keydown capture so a focused locked button can't be activated via
  // keyboard (Enter/Space). TDesign's t-button binds keydown internally to
  // dispatch a synthetic click; intercepting in capture phase stops it.
  el.addEventListener('keydown', handler, true)
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
    // GEÄNDERT: also unregister the keydown listener installed for keyboard
    // activation guard, mirror of installClickGuard's pair.
    el.removeEventListener('keydown', handler, true)
    delete (el as any)[CLICK_HANDLER_KEY]
  }
  // NEU: also disconnect the MutationObserver so it doesn't fire on the
  // restored DOM and re-lock the element after teardown.
  const observer = (el as any)[OBSERVER_KEY] as MutationObserver | undefined
  if (observer) {
    observer.disconnect()
    delete (el as any)[OBSERVER_KEY]
  }
  delete (el as any)[LAST_DECISION_KEY]
  applyAllowed(el)
}

// NEU: installEnforcementObserver watches the disabled/class/title attributes
// and re-applies the locked state if a parent component (e.g. TDesign t-button
// re-rendering on prop change) has cleared them. Without this, an external
// disabled toggle silently bypasses the permission gate visually — the user
// sees a clickable button while v-can's click guard quietly absorbs the click.
function installEnforcementObserver(el: HTMLElement) {
  if ((el as any)[OBSERVER_KEY]) return
  const observer = new MutationObserver(() => {
    const decision = (el as any)[LAST_DECISION_KEY] as PermissionDecision | undefined
    if (!decision || decision.allowed) return
    // Re-assert the locked state if any of our enforcement attributes were
    // cleared. We compare to avoid an infinite loop where our own setAttribute
    // re-triggers the observer (the observer checks the current state and only
    // mutates if it doesn't match the desired state).
    const isLocked = el.classList.contains(LOCKED_CLASS) &&
      el.getAttribute('aria-disabled') === 'true' &&
      (el as HTMLButtonElement).disabled === true
    if (!isLocked) {
      applyDenied(el, decision.reason)
    }
  })
  observer.observe(el, {
    attributes: true,
    attributeFilter: ['disabled', 'class', 'aria-disabled', 'title'],
  })
  ;(el as any)[OBSERVER_KEY] = observer
}

function bindEffect(el: HTMLElement, opts: VCanOptions) {
  // Tear down any prior watcher (the binding value may have changed).
  teardown(el)
  // NEU: capture the original title attribute before any allow/deny logic
  // mutates it, so applyAllowed can restore it later. Idempotent across
  // re-binds: only writes on first call.
  captureOriginalTitle(el)
  installClickGuard(el)

  const scope = effectScope(true)
  ;(el as any)[SCOPE_KEY] = scope

  scope.run(() => {
    const decision = Array.isArray(opts.flag) ? useAnyOf(...opts.flag) : useCan(opts.flag)
    watch(
      decision,
      (d: PermissionDecision) => {
        // NEU: stash decision so the MutationObserver can re-assert state
        // without re-running the resolver.
        const effective: PermissionDecision = {
          allowed: d.allowed,
          reason: opts.reason || d.reason,
        }
        ;(el as any)[LAST_DECISION_KEY] = effective
        if (effective.allowed) {
          applyAllowed(el)
        } else {
          applyDenied(el, effective.reason)
        }
      },
      { immediate: true },
    )
  })
  // NEU: install the enforcement observer AFTER the initial watch fires so
  // the LAST_DECISION_KEY is populated before any mutation event.
  installEnforcementObserver(el)
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
