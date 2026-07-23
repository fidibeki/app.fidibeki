export function useAppLinks() {
  const config = useRuntimeConfig()
  const appUrl = config.public.appUrl.replace(/\/$/, '')
  return {
    appUrl,
    signIn: `${appUrl}/login`,
    signUp: `${appUrl}/signup`,
    /** External scheduling link — Book a demo buttons open it directly. */
    demo: config.public.demoSchedulerUrl,
  }
}
