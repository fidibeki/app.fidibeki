type Admin = { id: string; email: string; permissions: string[] }
type Session = { accessToken: string | null; admin: Admin | null }
const empty = (): Session => ({ accessToken: null, admin: null })

export function usePlatformAuth() {
  const session = useState<Session>('platform-session', empty)
  const authenticated = computed(() => Boolean(session.value.accessToken && session.value.admin))
  function apply(response: { access_token: string; admin: Admin }) { session.value = { accessToken: response.access_token, admin: response.admin } }
  function clear() { session.value = empty() }
  return { session, authenticated, apply, clear }
}
