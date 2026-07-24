import type { PrivacyChoice } from './types'

export interface PrivacyEntryCopy {
  privacyPolicy: string
  consentRetention: string
  withdrawn: string
  withdrawError: string
}

export interface PrivacyEntryHooks {
  onStatus(message: string): void
  // The controller marks the grant revoked — and notifies its listeners — before the API call
  // settles, so without this the surface would see an empty list and tear itself down while the
  // withdrawal it is reporting is still in flight.
  onBusyChanged(busy: boolean): void
  onSettled(): void
}

// Shared by the feedback panel's privacy column and the standalone privacy control: both surfaces
// list the same withdrawable grants, so the entry — and the withdrawal semantics below — must be
// written once.
export function createPrivacyEntry(
  choice: PrivacyChoice,
  copy: PrivacyEntryCopy,
  hooks: PrivacyEntryHooks,
): HTMLElement {
  const entry = document.createElement('article')
  entry.className = 'privacy-choice'
  entry.dataset.purpose = choice.purpose
  const title = document.createElement('h4')
  title.textContent = choice.title
  const description = document.createElement('p')
  description.textContent = choice.description
  const meta = document.createElement('p')
  meta.className = 'privacy-meta'
  meta.textContent = `${choice.controllerName} · ${copy.consentRetention.replace(
    '{days}',
    String(choice.retentionDays),
  )}`
  const policy = document.createElement('p')
  const policyLink = document.createElement('a')
  policyLink.href = choice.privacyPolicyURL
  policyLink.target = '_blank'
  policyLink.rel = 'noopener noreferrer'
  policyLink.textContent = copy.privacyPolicy
  policy.append(policyLink)
  const withdraw = document.createElement('button')
  withdraw.type = 'button'
  withdraw.className = 'secondary'
  withdraw.textContent = choice.withdrawLabel
  withdraw.addEventListener('click', () => {
    void (async () => {
      hooks.onStatus('')
      withdraw.disabled = true
      hooks.onBusyChanged(true)
      try {
        await choice.withdraw()
        hooks.onStatus(copy.withdrawn)
      } catch {
        // The controller marks the grant revoked locally before it calls the API, so collection has
        // already stopped even when the receipt could not be recorded; the entry disappears either
        // way and the status line reports the failure rather than pretending it succeeded.
        hooks.onStatus(copy.withdrawError)
      } finally {
        hooks.onBusyChanged(false)
      }
      hooks.onSettled()
    })()
  })
  entry.append(title, description, meta, policy, withdraw)
  return entry
}
