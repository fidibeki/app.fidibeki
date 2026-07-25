import { extractAPIError } from './safe-api-error'

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

/**
 * The machine-readable code an API error carries, used to branch on outcomes
 * the message alone cannot distinguish.
 */
export function apiErrorCode(error: unknown): string {
  const code = extractAPIError(error)?.code
  return typeof code === 'string' ? code : ''
}

export function apiFieldErrors(
  error: unknown,
  localizedFallbacks: Record<string, string> = {},
): Record<string, string> {
  const details = extractAPIError(error)?.details
  if (Array.isArray(details)) {
    const messages = Object.fromEntries(details.flatMap((item) => {
      if (!item || typeof item !== 'object' || Array.isArray(item)) return []
      const field = 'field' in item && typeof item.field === 'string' ? item.field.trim() : ''
      const message = 'message' in item && typeof item.message === 'string' ? item.message.trim() : ''
      return field && message ? [[field, message]] : []
    }))
    return { ...localizedFallbacks, ...messages }
  }
  if (!details || typeof details !== 'object') return localizedFallbacks
  const root = details as Record<string, unknown>
  const nested = root.fields
  const fields = nested && typeof nested === 'object' && !Array.isArray(nested)
    ? nested as Record<string, unknown>
    : root
  return {
    ...localizedFallbacks,
    ...Object.fromEntries(
      Object.entries(fields)
        .filter((entry): entry is [string, string] => typeof entry[1] === 'string'),
    ),
  }
}
