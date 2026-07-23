<script setup lang="ts">
import { Menu, X } from '@lucide/vue'
import { primaryNav } from '~/data/site'

const links = useAppLinks()
const route = useRoute()
const menuOpen = ref(false)
const scrolled = ref(false)

// Hysteresis keeps the pill from flip-flopping (and restarting its
// transition) when the scroll position hovers around a single threshold.
const ENTER_AT = 48
const EXIT_AT = 8
let ticking = false

function updateScrolled() {
  ticking = false
  const y = window.scrollY
  if (!scrolled.value && y > ENTER_AT) {
    scrolled.value = true
  } else if (scrolled.value && y < EXIT_AT) {
    scrolled.value = false
  }
}

function onScroll() {
  if (!ticking) {
    ticking = true
    requestAnimationFrame(updateScrolled)
  }
}

onMounted(() => {
  updateScrolled()
  window.addEventListener('scroll', onScroll, { passive: true })
})

onBeforeUnmount(() => {
  window.removeEventListener('scroll', onScroll)
})

watch(
  () => route.path,
  () => {
    menuOpen.value = false
  },
)

watch(menuOpen, (open) => {
  if (import.meta.client) {
    document.documentElement.style.overflow = open ? 'hidden' : ''
  }
})

function isActive(to: string) {
  return route.path === to || route.path.startsWith(`${to}/`)
}
</script>

<template>
  <header class="site-header" :class="{ scrolled: scrolled || menuOpen }">
    <div class="header-shell">
      <div class="header-inner">
        <NuxtLink to="/" class="header-brand" aria-label="Fidibeki home">
          <BrandLogo :height="24" />
        </NuxtLink>

        <nav class="header-nav" aria-label="Primary">
          <NuxtLink
            v-for="item in primaryNav"
            :key="item.to"
            :to="item.to"
            class="header-link"
            :class="{ active: isActive(item.to) }"
            :aria-current="isActive(item.to) ? 'page' : undefined"
          >
            {{ item.label }}
          </NuxtLink>
        </nav>

        <div class="header-actions">
          <ThemeToggle />
          <a :href="links.signIn" class="header-link header-signin">Sign in</a>
          <a :href="links.signUp" class="btn btn-primary header-cta">Start free trial</a>
          <button
            type="button"
            class="menu-button"
            :aria-expanded="menuOpen"
            aria-controls="mobile-menu"
            :aria-label="menuOpen ? 'Close menu' : 'Open menu'"
            @click="menuOpen = !menuOpen"
          >
            <X v-if="menuOpen" :size="22" aria-hidden="true" />
            <Menu v-else :size="22" aria-hidden="true" />
          </button>
        </div>
      </div>
    </div>

    <div v-if="menuOpen" id="mobile-menu" class="mobile-menu">
      <nav class="mobile-nav" aria-label="Mobile">
        <NuxtLink
          v-for="(item, index) in primaryNav"
          :key="item.to"
          :to="item.to"
          class="mobile-link"
          :style="{ animationDelay: `${80 + index * 60}ms` }"
          :aria-current="isActive(item.to) ? 'page' : undefined"
        >
          {{ item.label }}
        </NuxtLink>
        <div class="mobile-cta-group" :style="{ animationDelay: `${80 + primaryNav.length * 60}ms` }">
          <a :href="links.signUp" class="btn btn-primary btn-large">Start free trial</a>
          <a :href="links.signIn" class="btn btn-ghost btn-large">Sign in</a>
          <a :href="links.demo" class="btn btn-ghost btn-large" rel="noopener" target="_blank">Book a demo</a>
        </div>
      </nav>
    </div>
  </header>
</template>

<style scoped>
.site-header {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  z-index: 50;
}

/* Full-bleed and transparent at the top of the page; a floating, blurred
   pill once the visitor scrolls. Only the shell width animates in layout —
   the vertical float uses transform and everything else is paint-only, so
   the transition stays smooth. */
.header-shell {
  --header-ease: cubic-bezier(0.33, 1, 0.68, 1);
  margin-inline: auto;
  max-width: 100%;
  border: 1px solid transparent;
  border-radius: 16px;
  background: transparent;
  transform: translateY(0);
  backdrop-filter: blur(0px);
  -webkit-backdrop-filter: blur(0px);
  will-change: transform;
  transition:
    max-width 380ms var(--header-ease),
    transform 380ms var(--header-ease),
    border-color 380ms var(--header-ease),
    background-color 380ms var(--header-ease),
    backdrop-filter 380ms var(--header-ease),
    -webkit-backdrop-filter 380ms var(--header-ease),
    box-shadow 380ms var(--header-ease);
}

.site-header.scrolled .header-shell {
  max-width: min(75rem, calc(100% - 2rem));
  transform: translateY(0.75rem);
  border-color: var(--line);
  background: color-mix(in srgb, var(--canvas) 88%, transparent);
  backdrop-filter: blur(16px);
  -webkit-backdrop-filter: blur(16px);
  box-shadow: var(--shadow-card);
}

.header-inner {
  position: relative;
  display: flex;
  align-items: center;
  gap: 1.5rem;
  height: var(--header-height);
  padding-inline: var(--gutter);
}

.header-brand {
  display: inline-flex;
  align-items: center;
  flex-shrink: 0;
}

.header-nav {
  display: none;
  align-items: center;
  gap: 1.35rem;
  margin-inline: auto;
}

.header-link {
  color: var(--ink-soft);
  font-size: 0.94rem;
  font-weight: 500;
}

.header-link:hover {
  color: var(--accent-text);
  text-decoration: none;
}

.header-link.active {
  color: var(--accent-text);
  font-weight: 640;
}

.header-actions {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  margin-left: auto;
}

.header-signin,
.header-cta {
  display: none;
}

.menu-button {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 38px;
  height: 38px;
  border: 1px solid var(--line);
  border-radius: var(--radius);
  background: transparent;
  color: var(--ink);
  cursor: pointer;
}

.mobile-menu {
  position: fixed;
  inset: 0;
  z-index: -1;
  overflow-y: auto;
  background: var(--canvas);
  padding-top: calc(var(--header-height) + 2rem);
  padding-bottom: 2rem;
}

.mobile-nav {
  display: flex;
  flex-direction: column;
  padding-inline: var(--gutter);
}

.mobile-link {
  padding: 0.9rem 0;
  border-bottom: 1px solid var(--line);
  color: var(--ink);
  font-size: 1.6rem;
  font-weight: 640;
  font-family: var(--font-display);
  opacity: 0;
  transform: translateY(14px);
  animation: menu-reveal 480ms cubic-bezier(0.2, 0.65, 0.25, 1) forwards;
}

.mobile-link:hover {
  text-decoration: none;
  color: var(--accent-text);
}

.mobile-cta-group {
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
  margin-top: 1.75rem;
  opacity: 0;
  transform: translateY(14px);
  animation: menu-reveal 480ms cubic-bezier(0.2, 0.65, 0.25, 1) forwards;
}

@keyframes menu-reveal {
  to {
    opacity: 1;
    transform: none;
  }
}

@media (prefers-reduced-motion: reduce) {
  .mobile-link,
  .mobile-cta-group {
    opacity: 1;
    transform: none;
    animation: none;
  }
}

@media (min-width: 900px) {
  .header-nav {
    display: flex;
  }

  .header-signin,
  .header-cta {
    display: inline-flex;
  }

  .header-cta {
    padding: 0.5rem 1rem;
    font-size: 0.9rem;
  }

  .menu-button,
  .mobile-menu {
    display: none;
  }
}

/* Optical centering: the nav's own auto margins split the free space so the
   gap to the logo equals the gap to the actions. The actions must not carry
   margin-left auto here or the two auto margins would fight and skew the nav. */
@media (min-width: 900px) {
  .header-actions {
    margin-left: 0;
  }
}
</style>
