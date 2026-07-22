import type { AuthResponse, AuthSession } from '~/types/api'
import type { ApiFetch, ApiFetchOptions } from '~/utils/api-fetch'
import { authForwardedHeaderNames } from '~/utils/auth-request-headers'

export default defineNuxtPlugin(() => {
  const session = useState<AuthSession>('auth:session')
  const forwardedHeaders = import.meta.server ? useRequestHeaders([...authForwardedHeaderNames]) : undefined
  const requestEvent = import.meta.server ? useRequestEvent() : undefined
  const rawFetch = $fetch as unknown as <T>(request: string, options?: Record<string, unknown>) => Promise<T>
  const rawFetchResponse = $fetch.raw as unknown as <T>(request: string, options?: Record<string, unknown>) => Promise<{
    _data?: T
    headers: Headers
  }>
  let refreshPromise: Promise<void> | null = null

  function applySession(response: AuthResponse) {
    session.value = {
      accessToken: response.access_token,
      expiresAt: Date.now() + Math.max(0, response.expires_in) * 1000,
      member: response.member,
      tenant: response.tenant,
    }
  }

  function clearSession() {
    session.value = {
      accessToken: null,
      expiresAt: null,
      member: null,
      tenant: null,
    }
  }

  async function refreshSession() {
    if (refreshPromise) return refreshPromise
    refreshPromise = (async () => {
      try {
        const upstream = await rawFetchResponse<AuthResponse>('/auth/refresh', {
          method: 'POST',
          credentials: 'include',
          headers: forwardedHeaders,
        })
        if (import.meta.server && requestEvent) {
          forwardResponseCookies(upstream.headers, cookie => requestEvent.node.res.appendHeader('set-cookie', cookie))
        }
        if (!upstream._data) throw new Error('Authentication refresh returned no data')
        applySession(upstream._data)
      } catch (error) {
        clearSession()
        throw error
      } finally {
        refreshPromise = null
      }
    })()
    return refreshPromise
  }

  const apiFetch: ApiFetch = async <T>(request: string, options: ApiFetchOptions = {}) => {
    const { auth = true, retryAuth = true, ...fetchOptions } = options
    const headers = new Headers(fetchOptions.headers as HeadersInit | undefined)
    if (import.meta.server && forwardedHeaders?.host && !headers.has('host')) {
      headers.set('host', forwardedHeaders.host)
    }
    if (import.meta.server && forwardedHeaders?.cookie && !headers.has('cookie')) {
      headers.set('cookie', forwardedHeaders.cookie)
    }
    if (auth && session.value?.accessToken) {
      headers.set('Authorization', `Bearer ${session.value.accessToken}`)
    }
    try {
      return await rawFetch<T>(request, {
        ...fetchOptions,
        headers,
        credentials: 'include',
      })
    } catch (error) {
      const status = (error as { response?: { status?: number } })?.response?.status
      const isAuthEndpoint = request.startsWith('/auth/')
      if (auth && retryAuth && status === 401 && !isAuthEndpoint) {
        await refreshSession()
        return apiFetch<T>(request, { ...options, retryAuth: false })
      }
      throw error
    }
  }

  return {
    provide: {
      apiFetch,
      refreshSession,
      clearSession,
    },
  }
})
