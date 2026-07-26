import type { AuthResponse, AuthSession, TwoFactorRequirement } from '~/types/api'
import { twoFactorRequirement } from '~/types/api'

const emptySession = (): AuthSession => ({
  accessToken: null,
  expiresAt: null,
  member: null,
  tenant: null,
})

export function useAuth() {
  const session = useState<AuthSession>('auth:session', emptySession)
  // What the member still owes before the session may open workspace data. It
  // is tracked separately because a challenge is owed before any token exists.
  const pendingTwoFactor = useState<TwoFactorRequirement | null>('auth:two-factor', () => null)
  const authenticated = computed(
    () => Boolean(session.value.accessToken && session.value.member && session.value.tenant),
  )

  const workspaceWritable = computed(
    () => Boolean(session.value.tenant && !session.value.tenant.deletion_requested_at),
  )

  const can = (permission: string) =>
    Boolean(session.value.member?.permissions.includes(permission))

  const canWrite = (permission: string) => workspaceWritable.value && can(permission)

  function clear() {
    pendingTwoFactor.value = null
    session.value = emptySession()
  }

  function apply(response: AuthResponse) {
    pendingTwoFactor.value = twoFactorRequirement(response)
    session.value = {
      accessToken: response.access_token,
      expiresAt: Date.now() + Math.max(0, response.expires_in) * 1000,
      member: response.member,
      tenant: response.tenant,
    }
  }

  return {
    session,
    pendingTwoFactor,
    authenticated,
    member: computed(() => session.value.member),
    tenant: computed(() => session.value.tenant),
    workspaceWritable,
    can,
    canWrite,
    apply,
    clear,
  }
}
