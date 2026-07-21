<script setup lang="ts">
import { ShieldAlert } from '@lucide/vue'

useHead({ title: 'Access required · Fidibeki' })
</script>

<template>
  <div class="app-page">
    <header class="app-page-head">
      <div>
        <p class="app-eyebrow">Workspace access</p>
        <h1>No sections assigned</h1>
        <span>Ask a workspace administrator to grant access to a product area.</span>
      </div>
    </header>
    <section class="app-surface app-state" role="status">
      <div>
        <ShieldAlert :size="25" />
        <h2>Permission required</h2>
        <p>Your account is active, but it does not currently include access to a workspace section.</p>
      </div>
    </section>
  </div>
</template>
