<script setup lang="ts">
import { footerGroups, siteTagline, loopStages } from '~/data/site'

const linkedInUrl = 'https://www.linkedin.com/company/fidibeki'

const links = useAppLinks()
</script>

<template>
  <footer class="site-footer">
    <div class="container">
      <div class="footer-top">
        <div class="footer-brand">
          <BrandLogo :height="22" />
          <p class="footer-tagline">{{ siteTagline }}</p>
          <p class="footer-loop" aria-label="The Fidibeki loop">
            <template v-for="(stage, index) in loopStages" :key="stage.key">
              <span class="footer-loop-stage">{{ stage.label }}</span>
              <span v-if="index < loopStages.length - 1" aria-hidden="true" class="footer-loop-arrow">→</span>
            </template>
          </p>
          <StatusPill class="footer-status" />
        </div>

        <nav
          v-for="group in footerGroups"
          :key="group.heading"
          class="footer-group"
          :aria-label="group.heading"
        >
          <h2 class="footer-heading">{{ group.heading }}</h2>
          <ul class="footer-list">
            <li v-for="link in group.links" :key="link.to">
              <NuxtLink :to="link.to" class="footer-link">{{ link.label }}</NuxtLink>
            </li>
            <template v-if="group.heading === 'Get started'">
              <li>
                <a :href="links.signIn" class="footer-link">Sign in</a>
              </li>
              <li>
                <a :href="links.demo" class="footer-link" rel="noopener" target="_blank">Book a demo</a>
              </li>
            </template>
          </ul>
        </nav>
      </div>

      <div class="footer-bottom">
        <p class="footer-copyright">
          © {{ new Date().getFullYear() }} Fidibeki. All rights reserved.
          <span aria-hidden="true"> · </span>
          <a href="mailto:support@fidibeki.com" class="footer-link">support@fidibeki.com</a>
        </p>
        <div class="footer-social">
          <a
            v-if="linkedInUrl"
            :href="linkedInUrl"
            class="footer-social-link"
            aria-label="Fidibeki on LinkedIn"
            rel="noopener"
            target="_blank"
          >
            <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true" fill="currentColor">
              <path d="M4.98 3.5C4.98 4.88 3.87 6 2.5 6S0 4.88 0 3.5 1.12 1 2.5 1s2.48 1.12 2.48 2.5zM.2 8h4.6v14.8H.2V8zm7.68 0h4.41v2.02h.06c.61-1.16 2.11-2.38 4.35-2.38 4.65 0 5.51 3.06 5.51 7.04v8.12h-4.6v-7.2c0-1.72-.03-3.93-2.4-3.93-2.4 0-2.77 1.87-2.77 3.8v7.33H7.88V8z" />
            </svg>
          </a>
          <span v-else class="footer-social-placeholder" title="LinkedIn, coming soon">
            <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true" fill="currentColor">
              <path d="M4.98 3.5C4.98 4.88 3.87 6 2.5 6S0 4.88 0 3.5 1.12 1 2.5 1s2.48 1.12 2.48 2.5zM.2 8h4.6v14.8H.2V8zm7.68 0h4.41v2.02h.06c.61-1.16 2.11-2.38 4.35-2.38 4.65 0 5.51 3.06 5.51 7.04v8.12h-4.6v-7.2c0-1.72-.03-3.93-2.4-3.93-2.4 0-2.77 1.87-2.77 3.8v7.33H7.88V8z" />
            </svg>
            <span class="footer-social-note">soon</span>
          </span>
        </div>
      </div>
    </div>
  </footer>
</template>

<style scoped>
.site-footer {
  border-top: 1px solid var(--line);
  background: var(--surface-soft);
  padding-block: 3rem 2rem;
}

.footer-top {
  display: grid;
  grid-template-columns: 1fr;
  gap: 2rem;
}

.footer-brand {
  max-width: 20rem;
}

.footer-tagline {
  margin-top: 0.9rem;
  color: var(--muted);
  font-size: 0.95rem;
}

.footer-loop {
  display: flex;
  flex-wrap: wrap;
  gap: 0.35rem;
  font-size: 0.78rem;
  color: var(--accent-text);
  font-weight: 600;
}

.footer-loop-arrow {
  color: var(--muted);
}

.footer-heading {
  margin: 0 0 0.8rem;
  font-family: var(--font-body);
  font-size: 0.8rem;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--muted);
}

.footer-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 0.55rem;
}

.footer-link {
  color: var(--ink-soft);
  font-size: 0.94rem;
}

.footer-link:hover {
  color: var(--accent-text);
}

.footer-bottom {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  margin-top: 2.5rem;
  padding-top: 1.5rem;
  border-top: 1px solid var(--line);
}

.footer-status {
  align-self: flex-start;
}

.footer-copyright {
  margin: 0;
  color: var(--muted);
  font-size: 0.85rem;
}

.footer-social-link,
.footer-social-placeholder {
  display: inline-flex;
  align-items: center;
  gap: 0.4rem;
  color: var(--muted);
}

.footer-social-link:hover {
  color: var(--accent-text);
}

.footer-social-note {
  font-size: 0.72rem;
  border: 1px solid var(--line-strong);
  border-radius: 999px;
  padding: 0.05rem 0.45rem;
}

@media (min-width: 700px) {
  .footer-top {
    grid-template-columns: 1.4fr repeat(4, 1fr);
  }
}
</style>
