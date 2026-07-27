export default defineNuxtPlugin(() => {
  const { session, apply, clear } = usePlatformAuth()
  let refresh: Promise<void> | null = null
  async function refreshSession() {
    if (refresh) return refresh
    refresh = (async () => { try { apply(await $fetch('/admin/auth/refresh', { method: 'POST', credentials: 'include' })) } catch (error) { clear(); throw error } finally { refresh = null } })()
    return refresh
  }
  async function api<T>(path: string, options: Record<string, unknown> = {}, retried = false): Promise<T> {
    const headers = new Headers(options.headers as HeadersInit | undefined)
    if (session.value.accessToken) headers.set('authorization', `Bearer ${session.value.accessToken}`)
    try { return await $fetch(path, { ...options, headers, credentials: 'include' }) as T } catch (error: any) {
      if (!retried && error?.statusCode === 401 && !path.startsWith('/admin/auth/')) { await refreshSession(); return api<T>(path, options, true) }
      throw error
    }
  }
  return { provide: { platformApi: api, refreshPlatformSession: refreshSession } }
})
