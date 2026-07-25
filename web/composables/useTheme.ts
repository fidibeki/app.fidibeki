export type ThemeName = 'light' | 'dark'

// Same key the marketing site uses, so both surfaces behave identically. Note
// localStorage is origin-scoped: a choice made on the marketing domain does not
// carry to a tenant subdomain, which resolves its own value and otherwise falls
// back to the system preference. Keep both in sync if this ever changes.
const STORAGE_KEY = 'fidibeki-theme'

export function useTheme() {
  const theme = useState<ThemeName>('theme', () => 'light')

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

  // The inline head script resolves the real theme before paint, so on mount we
  // adopt whatever it already wrote rather than overriding it.
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
