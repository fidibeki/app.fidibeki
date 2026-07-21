export type ApiMethod = 'GET' | 'POST' | 'PATCH' | 'PUT' | 'DELETE'

export interface ApiFetchOptions {
  method?: ApiMethod
  body?: unknown
  query?: Record<string, string | number | boolean | null | undefined>
  headers?: HeadersInit
  signal?: AbortSignal
  auth?: boolean
  retryAuth?: boolean
	responseType?: 'json' | 'text' | 'blob' | 'arrayBuffer'
}

export type ApiFetch = <T>(request: string, options?: ApiFetchOptions) => Promise<T>

export function apiErrorMessage(error: unknown, fallback = 'Something went wrong.'): string {
  if (error && typeof error === 'object') {
    const value = error as {
      data?: { error?: { message?: string } }
      message?: string
    }
    return value.data?.error?.message || value.message || fallback
  }
  return fallback
}
