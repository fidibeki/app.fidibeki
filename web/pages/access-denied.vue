<script setup lang="ts">
import { ShieldAlert } from '@lucide/vue'

const { t } = useI18n()

useHead({ title: computed(() => t('auth.accessDenied.pageTitle')) })
</script>

<template>
  <div class="app-page">
    <header class="app-page-head">
      <div>
        <p class="app-eyebrow">{{ t('auth.accessDenied.eyebrow') }}</p>
        <h1>{{ t('auth.accessDenied.title') }}</h1>
        <span>{{ t('auth.accessDenied.description') }}</span>
      </div>
    </header>
    <section class="app-surface app-state" role="status">
      <div>
        <ShieldAlert :size="25" />
        <h2>{{ t('auth.accessDenied.stateTitle') }}</h2>
        <p>{{ t('auth.accessDenied.stateDescription') }}</p>
      </div>
    </section>
  </div>
</template>
