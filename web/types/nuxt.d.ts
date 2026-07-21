import type { ApiFetch } from '~/utils/api-fetch'

declare module '#app' {
  interface NuxtApp {
    $apiFetch: ApiFetch
    $refreshSession: () => Promise<void>
    $clearSession: () => void
  }
}

declare module 'vue' {
  interface ComponentCustomProperties {
    $apiFetch: ApiFetch
  }
}

export {}
