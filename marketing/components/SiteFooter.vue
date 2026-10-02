<script setup lang="ts">
const { copy } = useMarketingI18n()

import { footerGroups } from '~/data/site'
import { ArrowUpRight } from '@lucide/vue'
const links = useAppLinks()
</script>

<template>
  <footer class="home-footer home-width">
    <div class="home-footer-main">
      <div class="home-footer-brand">
        <NuxtLinkLocale to="/" :aria-label="copy('Fidibeki')"
          ><BrandMark /><span>{{ copy('fidibeki') }}</span></NuxtLinkLocale
        >
        <p>
          {{ copy('Listen. Make it better.') }}<br />{{
            copy('Let them know.')
          }}
        </p>
        <a
          :href="links.demo"
          class="home-footer-demo"
          target="_blank"
          rel="noopener"
          >{{ copy('Book a demo') }}
          <ArrowUpRight :size="14" aria-hidden="true"
        /></a>
      </div>
      <nav
        v-for="group in footerGroups"
        :key="group.heading"
        :aria-label="copy(group.heading)"
      >
        <h2>{{ copy(group.heading) }}</h2>
        <NuxtLinkLocale
          v-for="item in group.links"
          :key="item.to"
          :to="item.to"
          >{{ copy(item.label) }}</NuxtLinkLocale
        >
        <a v-if="group.heading === 'Get started'" :href="links.signIn">{{
          copy('Sign in')
        }}</a>
        <a
          v-if="group.heading === 'Get started'"
          :href="links.demo"
          target="_blank"
          rel="noopener"
          >{{ copy('Book a demo') }}</a
        >
      </nav>
    </div>
    <div class="home-footer-bottom">
      <span>© {{ new Date().getFullYear() }} {{ copy('Fidibeki') }}</span>
      <a href="mailto:support@fidibeki.com">{{ copy('Get in touch') }}</a>
      <a
        href="https://www.producthunt.com/products/fidibeki"
        target="_blank"
        rel="noopener"
        >{{ copy('On Product Hunt') }}</a
      >
      <a
        href="https://www.linkedin.com/company/fidibeki"
        target="_blank"
        rel="noopener"
        >{{ copy('LinkedIn') }}</a
      >
      <StatusPill />
      <LanguageSwitcher />
      <ThemeToggle />
    </div>
  </footer>
</template>

<style scoped>
.home-footer {
  padding-top: 64px;
  padding-bottom: 24px;
}
.home-footer-main {
  display: grid;
  grid-template-columns: 1.5fr repeat(4, 1fr);
  gap: 32px;
}
.home-footer-brand > a:first-child {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-size: 24px;
  font-weight: 650;
  letter-spacing: -0.04em;
}
.home-footer-brand :deep(svg) {
  width: 29px;
  height: 29px;
}
.home-footer-brand p {
  margin: 24px 0 0;
  font-size: 16px;
  line-height: 1.6;
}
.home-footer-demo {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  margin-top: 24px;
  font-size: 14px;
}
.home-footer-brand .home-footer-demo :deep(svg) {
  width: 14px;
  height: 14px;
}
.home-footer nav {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 14px;
}
.home-footer h2 {
  margin: 0 0 10px;
  font-size: 14px;
  color: var(--home-muted);
  font-weight: 400;
}
.home-footer nav a {
  font-size: 14px;
}
.home-footer a:hover {
  text-decoration: underline;
  text-underline-offset: 4px;
}
.home-footer-bottom {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 24px;
  margin-top: 100px;
  font-size: 12px;
  color: var(--home-muted);
}
.home-footer-bottom :deep(button) {
  margin-left: auto;
  border: 0;
  border-radius: 50%;
  color: var(--home-ink);
}
@media (max-width: 899px) {
  .home-footer-main {
    grid-template-columns: repeat(4, 1fr);
    gap: 28px;
  }
  .home-footer-brand {
    grid-column: 1 / -1;
    margin-bottom: 16px;
  }
  .home-footer-brand p {
    margin-top: 12px;
  }
  .home-footer-bottom {
    margin-top: 56px;
  }
}
@media (max-width: 560px) {
  .home-footer-main {
    grid-template-columns: 1fr 1fr;
    row-gap: 38px;
  }
  .home-footer {
    padding-top: 36px;
  }
  .home-footer-bottom {
    gap: 16px;
    font-size: 12px;
  }
}
</style>
