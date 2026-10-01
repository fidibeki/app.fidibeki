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
  `${trialDays} day free trial, no credit card required`,
  'Your own workspace at your-team.fidibeki.com',
  'Feedback widget, issue triage, and reporter update tools from day one',
  'Passwordless sign-in for you and your team',
]
</script>

<template>
  <div>
    <section class="section auth-section">
      <div class="container auth-inner">
        <div class="auth-card">
          <BrandMark class="auth-mark" />
          <h1>Create your workspace</h1>
          <p class="lede">
            Pick a workspace name, verify your email, and start capturing
            feedback.
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
          <p class="auth-legal">
            By creating a workspace you agree to our
            <NuxtLink to="/tos">Terms of Service</NuxtLink> and
            <NuxtLink to="/privacy">Privacy notice</NuxtLink>.
          </p>
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
  padding-block: 80px 100px;
}
.auth-inner {
  display: flex;
  justify-content: center;
}
.auth-card {
  width: min(100%, 520px);
  text-align: left;
}
.auth-mark {
  width: 38px;
  height: 38px;
  margin-bottom: 32px;
}
.auth-card h1 {
  font-size: clamp(34px, 4vw, 44px);
  margin-bottom: 24px;
}
.auth-card .lede {
  font-size: 17px;
}
.auth-list {
  list-style: none;
  padding: 28px 0;
  margin: 36px 0;
  border-block: 1px solid var(--line);
  display: grid;
  gap: 18px;
  font-size: 15px;
}
.auth-list li {
  display: flex;
  gap: 12px;
  align-items: flex-start;
}
.auth-check {
  flex-shrink: 0;
  margin-top: 4px;
}
.auth-legal {
  font-size: 12px;
  color: var(--muted);
  line-height: 1.7;
  margin: 24px 0 0;
}
.auth-legal a,
.auth-alt a {
  text-decoration: underline;
  text-underline-offset: 3px;
}
.auth-alt {
  font-size: 14px;
  color: var(--muted);
  margin: 32px 0 0;
}
@media (max-width: 600px) {
  .auth-section {
    padding-block: 40px 64px;
  }
}
</style>
