<script setup lang="ts">
import type { DetailSection } from '~/data/types'

defineProps<{ sections: DetailSection[] }>()
</script>

<template>
  <div class="prose">
    <section v-for="section in sections" :key="section.heading">
      <h2>{{ section.heading }}</h2>
      <p>{{ section.body }}</p>
      <ul v-if="section.bullets">
        <li v-for="bullet in section.bullets" :key="bullet">{{ bullet }}</li>
      </ul>
      <CodeBlock v-if="section.code" :code="section.code.snippet" :language="section.code.language" />
    </section>
  </div>
</template>
