<script setup lang="ts">
import { Check, ExternalLink } from '@lucide/vue'
import { trialDays } from '~/data/pricing'

const links = useAppLinks()

useSiteMeta({
  title: 'Sign up',
  description: `Create a Fidibeki workspace in about a minute. ${trialDays}-day free trial, no credit card required.`,
  path: '/sign-up',
  breadcrumbs: [
    { name: 'Home', path: '/' },
    { name: 'Sign up', path: '/sign-up' },
  ],
})

const included = [
  `${trialDays}-day free trial — no credit card required`,
  'Your own workspace at your-team.fidibeki.com',
  'Feedback widget, issue triage, and reporter updates from day one',
  'Passwordless sign-in for you and your team',
]
</script>

<template>
  <div>
    <section class="section auth-section">
      <div class="container auth-inner">
        <div class="card auth-card">
          <BrandMark class="auth-mark" />
          <h1>Create your workspace</h1>
          <p class="lede">
            Pick a workspace name, verify your email, and start capturing feedback.
          </p>
          <ul class="auth-list">
            <li v-for="item in included" :key="item">
              <Check :size="16" aria-hidden="true" class="auth-check" />
              <span>{{ item }}</span>
            </li>
          </ul>
          <a :href="links.signUp" class="btn btn-primary btn-large auth-cta">
            Continue to sign up <ExternalLink :size="16" aria-hidden="true" />
          </a>
          <p class="auth-alt">
            Already have a workspace?
            <a :href="links.signIn">Sign in</a>
          </p>
        </div>
      </div>
    </section>
  </div>
</template>

<style scoped>
.auth-section {
  min-height: 60vh;
  display: flex;
  align-items: center;
  background: var(--hero-glow);
}

.auth-inner {
  display: flex;
  justify-content: center;
}

.auth-card {
  max-width: 30rem;
  text-align: center;
  padding: 2.5rem 2rem;
  box-shadow: var(--shadow-card);
}

.auth-mark {
  font-size: 2.2rem;
  color: var(--accent);
  margin-inline: auto;
  margin-bottom: 1rem;
}

.auth-card h1 {
  font-size: 1.7rem;
}

.auth-card .lede {
  font-size: 1rem;
  margin-inline: auto;
}

.auth-list {
  list-style: none;
  margin: 1.4rem 0 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 0.6rem;
  text-align: left;
  font-size: 0.95rem;
}

.auth-list li {
  display: flex;
  gap: 0.5rem;
  align-items: flex-start;
}

.auth-check {
  color: var(--accent);
  flex-shrink: 0;
  margin-top: 0.2rem;
}

.auth-cta {
  margin-top: 1.4rem;
}

.auth-alt {
  margin: 1.25rem 0 0;
  color: var(--muted);
  font-size: 0.92rem;
}
</style>
