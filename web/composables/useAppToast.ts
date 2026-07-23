export type AppToastTone = 'success' | 'error'

export interface AppToastState {
  id: number
  message: string
  tone: AppToastTone
}

let dismissTimer: ReturnType<typeof setTimeout> | null = null

export function useAppToast() {
  const toast = useState<AppToastState | null>('app:toast', () => null)

  function dismiss(id?: number) {
    if (id !== undefined && toast.value?.id !== id) return
    toast.value = null
    if (dismissTimer) clearTimeout(dismissTimer)
    dismissTimer = null
  }

  function show(message: string, tone: AppToastTone = 'success', duration = 3_000) {
    const id = (toast.value?.id ?? 0) + 1
    toast.value = { id, message, tone }
    if (!import.meta.client || duration <= 0) return
    if (dismissTimer) clearTimeout(dismissTimer)
    dismissTimer = setTimeout(() => dismiss(id), duration)
  }

  return {
    toast: readonly(toast),
    dismiss,
    success: (message: string, duration?: number) => show(message, 'success', duration),
    error: (message: string, duration?: number) => show(message, 'error', duration),
  }
}
