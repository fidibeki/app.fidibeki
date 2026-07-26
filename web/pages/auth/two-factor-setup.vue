<script setup lang="ts">
import { ShieldAlert } from '@lucide/vue'

// The mandatory enrolment route. A member whose workspace switched to a
// required policy is sent here and cannot reach workspace data until the
// enrolment completes.
definePageMeta({ layout: false })

const { t } = useI18n()
const { member } = useAuth()
const pendingTwoFactor = useState<string | null>('auth:two-factor', () => null)

async function continueToWorkspace() {
  pendingTwoFactor.value = null
  await navigateTo(firstAccessibleWorkspacePath(member.value?.permissions || []))
}

useHead({ title: computed(() => t('auth.twoFactorSetup.pageTitle')) })
</script>

<template>
  <main class="auth-page">
    <section class="auth-context">
      <NuxtLink to="/" class="app-brand auth-brand"><img src="/brand/fidibeki-white.png" alt="Fidibeki" /></NuxtLink>
      <div>
        <h1>{{ t('auth.twoFactorSetup.contextTitle') }}</h1>
        <p>{{ t('auth.twoFactorSetup.contextDescription') }}</p>
      </div>
    </section>
    <section class="auth-panel two-factor-setup-panel">
      <div class="two-factor-setup-stack">
        <p class="two-factor-setup-notice">
          <ShieldAlert :size="15" />{{ t('auth.twoFactorSetup.requiredNotice') }}
        </p>
        <TwoFactorEnrollment mandatory @enrolled="continueToWorkspace" />
      </div>
    </section>
  </main>
</template>

<style scoped>
.auth-brand img { display: block; width: 142px; height: auto; }
.two-factor-setup-panel { align-items: flex-start; padding: 42px 32px; background: var(--canvas); }
.two-factor-setup-stack { display: grid; gap: 14px; width: 100%; max-width: 560px; }
.two-factor-setup-notice { display: flex; align-items: center; gap: 8px; margin: 0; padding: 10px 12px; background: var(--amber-soft); color: var(--amber-ink); border: 1px solid var(--amber-line); border-radius: 5px; font-size: 12px; font-weight: 700; }
</style>
