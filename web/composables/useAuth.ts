import type { AuthResponse, AuthSession } from '~/types/api'

const emptySession = (): AuthSession => ({
  accessToken: null,
  expiresAt: null,
  member: null,
  tenant: null,
})

export function useAuth() {
  const session = useState<AuthSession>('auth:session', emptySession)
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
    session.value = emptySession()
  }

  function apply(response: AuthResponse) {
    session.value = {
      accessToken: response.access_token,
      expiresAt: Date.now() + Math.max(0, response.expires_in) * 1000,
      member: response.member,
      tenant: response.tenant,
    }
  }

  return {
    session,
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
