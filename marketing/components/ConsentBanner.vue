<script setup lang="ts">
import type { ConsentSelections } from '~/utils/consent-choices'

const { pending, accept, decline, save, initialize } = useConsent()

// Initialized on mount so the prerendered HTML never contains the banner
// and hydration stays consistent.
const ready = ref(false)

onMounted(() => {
  initialize()
  ready.value = true
})

const visible = computed(() => ready.value && pending.value)

// Per-purpose choices sit behind "More options" and start unchecked, so neither pre-selection nor
// visual emphasis nudges the visitor toward a particular decision — the same rules the widget's
// own consent prompt follows.
const expanded = ref(false)
const selections = reactive<ConsentSelections>({
  umami: false,
  widget: false,
  widgetAnalytics: false,
  sessionReplay: false,
})

// The widget purposes are grants forwarded to the widget, so without the
// feedback widget itself there is nothing to grant.
watch(() => selections.widget, (enabled) => {
  if (!enabled) {
    selections.widgetAnalytics = false
    selections.sessionReplay = false
  }
})
</script>

<template>
  <aside v-if="visible" class="consent-banner" aria-label="Privacy choices">
    <p class="consent-text">
      We use privacy-friendly analytics (Umami) and our own feedback widget to see what's working
      and hear from you. Allowing covers Umami plus the widget's usage analytics, session replay,
      and AI-assisted analysis of feedback you send — nothing loads or records before you agree.
      <NuxtLink to="/privacy">Learn more</NuxtLink>
    </p>
    <fieldset v-if="expanded" class="consent-options">
      <legend class="sr-only">Choose what to allow</legend>
      <label class="consent-option">
        <input v-model="selections.umami" type="checkbox">
        <span>
          <strong>Site analytics</strong>
          Cookieless Umami page statistics; no personal data.
        </span>
      </label>
      <label class="consent-option">
        <input v-model="selections.widget" type="checkbox">
        <span>
          <strong>Feedback widget</strong>
          The in-page form for sending us feedback. Submissions may be analysed with AI, as
          disclosed in the form.
        </span>
      </label>
      <label class="consent-option consent-suboption">
        <input
          v-model="selections.widgetAnalytics"
          type="checkbox"
          :disabled="!selections.widget"
        >
        <span>
          <strong>Widget usage analytics</strong>
          How the feedback widget is used — shown, opened, submitted.
        </span>
      </label>
      <label class="consent-option consent-suboption">
        <input v-model="selections.sessionReplay" type="checkbox" :disabled="!selections.widget">
        <span>
          <strong>Session replay</strong>
          Records your interactions on the page to help us reproduce issues.
        </span>
      </label>
    </fieldset>
    <div class="consent-actions">
      <button type="button" class="btn btn-ghost consent-btn" @click="decline">Decline</button>
      <button
        v-if="expanded"
        type="button"
        class="btn btn-ghost consent-btn"
        @click="save({ ...selections })"
      >
        Save choices
      </button>
      <button type="button" class="btn btn-primary consent-btn" @click="accept">Accept All</button>
    </div>
    <button
      v-if="!expanded"
      type="button"
      class="consent-more"
      aria-expanded="false"
      @click="expanded = true"
    >
      More options
    </button>
  </aside>
</template>

<style scoped>
.consent-banner {
  position: fixed;
  bottom: 1rem;
  left: 1rem;
  right: 1rem;
  z-index: 60;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: center;
  gap: 0.75rem 1.5rem;
  max-width: 46rem;
  margin-inline: auto;
  padding: 0.85rem 1.25rem;
  border: 1px solid var(--line);
  border-radius: var(--radius-lg);
  background: color-mix(in srgb, var(--canvas) 92%, transparent);
  backdrop-filter: blur(12px);
  -webkit-backdrop-filter: blur(12px);
  box-shadow: var(--shadow-overlay);
  animation: consent-in 420ms cubic-bezier(0.2, 0.65, 0.25, 1);
}

@keyframes consent-in {
  from {
    opacity: 0;
    transform: translateY(12px);
  }
}

.consent-text {
  margin: 0;
  font-size: 0.88rem;
  color: var(--ink-soft);
  text-wrap: pretty;
}

.consent-options {
  display: grid;
  gap: 0.65rem;
  width: 100%;
  margin: 0;
  padding: 0.75rem 0 0;
  border: 0;
  border-top: 1px solid var(--line);
}

.consent-option {
  display: flex;
  gap: 0.6rem;
  font-size: 0.84rem;
  color: var(--ink-soft);
  cursor: pointer;
}

.consent-option input {
  flex-shrink: 0;
  margin-top: 0.15rem;
  accent-color: var(--accent, currentColor);
}

.consent-option:has(input:disabled) {
  opacity: 0.55;
  cursor: not-allowed;
}

.consent-option strong {
  display: block;
  color: var(--ink);
  font-weight: 650;
}

.consent-suboption {
  margin-left: 1.4rem;
}

.consent-actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: center;
  gap: 0.5rem 0.75rem;
  flex-shrink: 0;
}

.consent-more {
  width: 100%;
  padding: 0;
  border: 0;
  background: none;
  color: var(--ink-soft);
  font-size: 0.84rem;
  font-weight: 600;
  text-align: center;
  text-decoration: underline;
  cursor: pointer;
}

.consent-more:hover {
  color: var(--ink);
}

.consent-btn {
  padding: 0.45rem 0.95rem;
  font-size: 0.86rem;
}

.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0 0 0 0);
  white-space: nowrap;
  border: 0;
}
</style>
