<script setup lang="ts">
import { onMounted } from 'vue'

withDefaults(defineProps<{ variant?: 'inline' | 'block' }>(), { variant: 'inline' })

const { status, load, configured, statusPageUrl } = useServiceStatus()

// Client-only, after paint. The prerendered markup carries the `unknown` state,
// so the page never ships a health claim it has not verified in this session.
onMounted(load)

const href = statusPageUrl || 'https://status.fidibeki.com'
</script>

<template>
  <a
    :href="href"
    class="status-pill"
    :class="[`is-${status.level}`, `variant-${variant}`]"
    rel="noopener"
    target="_blank"
  >
    <span class="status-dot" aria-hidden="true" />
    <span class="status-label">{{ configured ? status.label : 'Service status' }}</span>
  </a>
</template>

<style scoped>
/* Semantic status colours are local to this component: the design system has no
   danger/warning scale, and inventing a global one for a single badge would be
   a bigger change than the badge deserves. */
.status-pill {
  --status-ok: #0f7a5a;
  --status-warn: #8a5a00;
  --status-down: #b3261e;
  --status-unknown: var(--muted);

  display: inline-flex;
  align-items: center;
  gap: 0.45rem;
  padding: 0.3rem 0.7rem;
  border: 1px solid var(--line);
  border-radius: 999px;
  background: var(--surface);
  color: var(--ink-soft);
  font-size: 0.82rem;
  line-height: 1.3;
  text-decoration: none;
  transition: border-color 160ms ease, color 160ms ease;
}

:global(html[data-theme='dark']) .status-pill {
  --status-ok: #3ec5b2;
  --status-warn: #e0a06e;
  --status-down: #f0806e;
}

.status-pill:hover {
  text-decoration: none;
  border-color: var(--accent);
  color: var(--ink);
}

.status-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
  background: var(--status-unknown);
}

.is-operational .status-dot {
  background: var(--status-ok);
  /* A quiet halo, not a pulse: a status badge should not draw the eye when
     everything is fine. */
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--status-ok) 22%, transparent);
}

.is-degraded .status-dot {
  background: var(--status-warn);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--status-warn) 22%, transparent);
}

.is-down .status-dot {
  background: var(--status-down);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--status-down) 26%, transparent);
}

.is-down {
  border-color: color-mix(in srgb, var(--status-down) 45%, var(--line));
  color: var(--ink);
}

.variant-block {
  padding: 0.55rem 0.95rem;
  font-size: 0.9rem;
}

@media (prefers-reduced-motion: reduce) {
  .status-pill {
    transition: none;
  }
}
</style>
