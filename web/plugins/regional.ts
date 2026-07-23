export default defineNuxtPlugin((nuxtApp) => {
  const { member, tenant } = useAuth()
  const regional = useRegionalContext()

  watch(
    () => [
      member.value?.locale,
      member.value?.timezone,
      tenant.value?.default_locale,
      tenant.value?.default_timezone,
    ],
    () => {
      void regional.apply()
    },
  )

  if (import.meta.client) {
    nuxtApp.hook('app:mounted', () => {
      void regional.captureBrowserPreferences()
    })
  }
})
