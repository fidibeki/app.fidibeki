<script setup lang="ts">
const { copy } = useMarketingI18n()

import { ArrowUpRight, Menu, X } from '@lucide/vue'
import { primaryNav } from '~/data/site'
import { unlocalizedPath } from '~/utils/locale'

const links = useAppLinks()
const menuOpen = ref(false)
const menuButton = ref<HTMLButtonElement | null>(null)
const router = useRouter()
// Global navigation follows the committed URL, without waiting for NuxtPage to finish rendering.
const sectionPath = computed(() =>
  unlocalizedPath(router.currentRoute.value.path),
)

function closeMenu(returnFocus = false) {
  menuOpen.value = false
  if (returnFocus) menuButton.value?.focus()
}

watch(
  () => router.currentRoute.value.fullPath,
  () => closeMenu(),
)
</script>

<template>
  <header class="home-header" @keydown.esc="closeMenu(true)">
    <div class="home-width home-header-inner">
      <NuxtLinkLocale
        to="/"
        class="home-brand"
        :aria-label="copy('Fidibeki home')"
      >
        <BrandMark /> <span>{{ copy('fidibeki') }}</span>
      </NuxtLinkLocale>
      <nav class="home-nav" :aria-label="copy('Primary')">
        <NuxtLinkLocale
          v-for="item in primaryNav"
          :key="item.to"
          :to="item.to"
          :class="{
            'section-active':
              sectionPath === item.to || sectionPath.startsWith(`${item.to}/`),
          }"
          :aria-current="
            sectionPath === item.to
              ? 'page'
              : sectionPath.startsWith(`${item.to}/`)
                ? 'location'
                : undefined
          "
          >{{ copy(item.label) }}</NuxtLinkLocale
        >
      </nav>
      <div class="home-header-actions">
        <LanguageSwitcher class="header-language" />
        <a :href="links.signIn" class="home-signin">{{ copy('Log in') }}</a>
        <a :href="links.signUp" class="home-button home-header-cta"
          >{{ copy('Start free trial') }}
          <ArrowUpRight :size="15" aria-hidden="true"
        /></a>
        <button
          ref="menuButton"
          class="home-menu-toggle"
          type="button"
          :aria-label="copy(menuOpen ? 'Close menu' : 'Open menu')"
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
      :aria-label="copy('Mobile')"
    >
      <div class="home-width home-mobile-links">
        <LanguageSwitcher />
        <NuxtLinkLocale
          v-for="item in primaryNav"
          :key="item.to"
          :to="item.to"
          :class="{
            'section-active':
              sectionPath === item.to || sectionPath.startsWith(`${item.to}/`),
          }"
          :aria-current="
            sectionPath === item.to
              ? 'page'
              : sectionPath.startsWith(`${item.to}/`)
                ? 'location'
                : undefined
          "
          >{{ copy(item.label) }}</NuxtLinkLocale
        >
        <a :href="links.signIn">{{ copy('Log in') }}</a>
        <a :href="links.signUp"
          >{{ copy('Start free trial') }}
          <ArrowUpRight :size="17" aria-hidden="true"
        /></a>
        <a :href="links.demo" target="_blank" rel="noopener"
          >{{ copy('Book a demo') }}
          <ArrowUpRight :size="17" aria-hidden="true"
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
  gap: 28px;
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
  gap: 20px;
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
  gap: 16px;
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
@media (max-width: 1100px) {
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
@media (min-width: 1101px) {
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
    padding-inline: 10px;
  }
  .home-header-cta :deep(svg) {
    display: none;
  }
}
</style>

<style scoped>
@media (max-width: 1100px) {
  .header-language {
    display: none;
  }
}
</style>
