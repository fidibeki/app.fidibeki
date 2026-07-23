// v-reveal directive: fades an element in the first time it scrolls into view.
// The hidden state is only applied on the client after mount, so prerendered
// HTML stays fully visible for crawlers and no-JS visitors, and reduced-motion
// users never see the effect at all. Pass a number for a stagger delay in ms.
export default defineNuxtPlugin((nuxtApp) => {
  if (import.meta.server) {
    nuxtApp.vueApp.directive('reveal', { getSSRProps: () => ({}) })
    return
  }

  const reduceMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches
  const observer = reduceMotion
    ? null
    : new IntersectionObserver(
        (entries, self) => {
          for (const entry of entries) {
            if (entry.isIntersecting) {
              entry.target.classList.add('is-revealed')
              self.unobserve(entry.target)
            }
          }
        },
        { threshold: 0.2, rootMargin: '0px 0px -80px 0px' },
      )

  nuxtApp.vueApp.directive('reveal', {
    mounted(el: HTMLElement, binding: { value?: number; modifiers?: Partial<Record<string, boolean>> }) {
      if (!observer) return
      // Elements already on screen when the page hydrates must not animate:
      // hiding them after first paint reads as a flicker, and the reveal
      // would play before anyone scrolls to it anyway.
      const rect = el.getBoundingClientRect()
      const initiallyVisible = rect.top < window.innerHeight - 40 && rect.bottom > 0
      if (binding.modifiers?.line) {
        // Draws the section's top rule from left to right instead of fading
        // the content itself.
        if (initiallyVisible) return
        el.classList.add('rule-draw')
      } else {
        if (initiallyVisible) return
        el.classList.add('reveal-on-scroll')
        if (typeof binding.value === 'number' && binding.value > 0) {
          el.style.transitionDelay = `${binding.value}ms`
        }
      }
      observer.observe(el)
    },
    getSSRProps: () => ({}),
  })
})
