<script setup lang="ts">
import { ArrowUpRight, Menu, X } from '@lucide/vue'
import { primaryNav } from '~/data/site'

const links = useAppLinks()
const menuOpen = ref(false)
const menuButton = ref<HTMLButtonElement | null>(null)
const route = useRoute()

function closeMenu(returnFocus = false) {
  menuOpen.value = false
  if (returnFocus) menuButton.value?.focus()
}

watch(
  () => route.fullPath,
  () => closeMenu(),
)
</script>

<template>
  <header class="home-header" @keydown.esc="closeMenu(true)">
    <div class="home-width home-header-inner">
      <NuxtLink to="/" class="home-brand" aria-label="Fidibeki home">
        <BrandMark /> <span>fidibeki</span>
      </NuxtLink>
      <nav class="home-nav" aria-label="Primary">
        <NuxtLink
          v-for="item in primaryNav"
          :key="item.to"
          :to="item.to"
          :class="{
            'section-active':
              route.path === item.to || route.path.startsWith(`${item.to}/`),
          }"
          :aria-current="
            route.path === item.to
              ? 'page'
              : route.path.startsWith(`${item.to}/`)
                ? 'location'
                : undefined
          "
          >{{ item.label }}</NuxtLink
        >
      </nav>
      <div class="home-header-actions">
        <a :href="links.signIn" class="home-signin">Log in</a>
        <a :href="links.signUp" class="home-button home-header-cta"
          >Start free trial <ArrowUpRight :size="15" aria-hidden="true"
        /></a>
        <button
          ref="menuButton"
          class="home-menu-toggle"
          type="button"
          :aria-label="menuOpen ? 'Close menu' : 'Open menu'"
          :aria-expanded="menuOpen"
          aria-controls="home-mobile-nav"
          @click="menuOpen = !menuOpen"
        >
          <X v-if="menuOpen" :size="22" aria-hidden="true" />
          <Menu v-else :size="22" aria-hidden="true" />
        </button>
      </div>
    </div>
    <nav
      v-if="menuOpen"
      id="home-mobile-nav"
      class="home-mobile-nav"
      aria-label="Mobile"
    >
      <div class="home-width home-mobile-links">
        <NuxtLink
          v-for="item in primaryNav"
          :key="item.to"
          :to="item.to"
          :class="{
            'section-active':
              route.path === item.to || route.path.startsWith(`${item.to}/`),
          }"
          :aria-current="
            route.path === item.to
              ? 'page'
              : route.path.startsWith(`${item.to}/`)
                ? 'location'
                : undefined
          "
          >{{ item.label }}</NuxtLink
        >
        <a :href="links.signIn">Log in</a>
        <a :href="links.signUp"
          >Start free trial <ArrowUpRight :size="17" aria-hidden="true"
        /></a>
        <a :href="links.demo" target="_blank" rel="noopener"
          >Book a demo <ArrowUpRight :size="17" aria-hidden="true"
        /></a>
      </div>
    </nav>
  </header>
</template>

<style scoped>
.home-header {
  position: sticky;
  top: 0;
  z-index: 60;
  background: var(--home-canvas);
  color: var(--home-ink);
}
.home-header-inner {
  display: flex;
  align-items: center;
  gap: 42px;
  min-height: 80px;
  padding-block: 16px;
}
.home-brand:hover {
  text-decoration: none;
}
.home-nav .section-active,
.home-mobile-links .section-active {
  text-decoration: underline;
  text-underline-offset: 6px;
}
.home-brand {
  display: inline-flex;
  flex-shrink: 0;
  align-items: center;
  gap: 8px;
  font-size: 24px;
  font-weight: 650;
  letter-spacing: -0.04em;
}
.home-brand :deep(svg) {
  width: 30px;
  height: 30px;
}
.home-nav {
  display: flex;
  align-items: center;
  gap: 26px;
  font-size: 14px;
}
.home-nav a,
.home-signin {
  text-decoration: none;
}
.home-nav a:hover,
.home-signin:hover {
  color: var(--home-muted);
}
.home-header-actions {
  display: flex;
  align-items: center;
  gap: 24px;
  margin-left: auto;
  font-size: 14px;
}
.home-menu-toggle {
  display: none;
  align-items: center;
  justify-content: center;
  width: 44px;
  height: 44px;
  padding: 0;
  border: 0;
  color: inherit;
  background: transparent;
  cursor: pointer;
}
.home-mobile-nav {
  position: absolute;
  inset: 100% 0 auto;
  padding-block: 12px 24px;
  background: var(--home-canvas);
  border-bottom: 1px solid var(--home-line);
  max-height: calc(100dvh - 80px);
  overflow-y: auto;
}
.home-mobile-links {
  display: grid;
}
.home-mobile-nav a {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px 0;
  font-size: 18px;
}
@media (max-width: 1100px) {
  .home-header-inner {
    gap: 28px;
  }
  .home-nav {
    gap: 18px;
  }
  .home-header-actions {
    gap: 18px;
  }
}
@media (max-width: 899px) {
  .home-nav,
  .home-signin {
    display: none;
  }
  .home-menu-toggle {
    display: flex;
  }
  .home-header-inner {
    min-height: 72px;
    padding-block: 12px;
  }
  .home-mobile-nav {
    max-height: calc(100dvh - 72px);
  }
  .home-header-actions {
    gap: 10px;
  }
  .home-header .home-header-cta {
    min-height: 40px;
    padding: 9px 14px;
    font-size: 12px;
  }
  .home-brand:hover {
    text-decoration: none;
  }
  .home-nav .section-active,
  .home-mobile-links .section-active {
    text-decoration: underline;
    text-underline-offset: 6px;
  }
  .home-brand {
    font-size: 24px;
  }
  .home-brand :deep(svg) {
    width: 27px;
    height: 27px;
  }
}
@media (min-width: 900px) {
  .home-mobile-nav {
    display: none;
  }
}
@media (max-width: 380px) {
  .home-header-inner {
    gap: 10px;
  }
  .home-header-actions {
    gap: 4px;
  }
  .home-header .home-header-cta {
    padding-inline: 12px;
  }
  .home-header-cta :deep(svg) {
    display: none;
  }
}
</style>
