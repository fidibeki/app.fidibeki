export type ThemeName = 'light' | 'dark'

const STORAGE_KEY = 'fidibeki-theme'

export function useTheme() {
  const theme = useState<ThemeName>('theme', () => 'dark')

  function apply(next: ThemeName) {
    theme.value = next
    if (import.meta.client) {
      document.documentElement.dataset.theme = next
      try {
        localStorage.setItem(STORAGE_KEY, next)
      } catch {
        // Storage may be unavailable (private mode); the choice simply won't persist.
      }
    }
  }

  function toggle() {
    apply(theme.value === 'dark' ? 'light' : 'dark')
  }

  function syncFromDocument() {
    if (import.meta.client) {
      const current = document.documentElement.dataset.theme
      if (current === 'light' || current === 'dark') {
        theme.value = current
      }
    }
  }

  return { theme, apply, toggle, syncFromDocument }
}
