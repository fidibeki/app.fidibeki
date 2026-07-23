<script setup lang="ts">
withDefaults(defineProps<{ height?: number }>(), { height: 26 })
</script>

<template>
  <span class="brand-logo" :style="{ '--logo-height': `${height}px` }">
    <img
      class="brand-logo-light"
      src="/brand/wordmark-color.png"
      srcset="/brand/wordmark-color.png 1x, /brand/wordmark-color@2x.png 2x"
      alt="Fidibeki"
      :height="height"
      :width="Math.round(height * 3.944)"
    />
    <img
      class="brand-logo-dark"
      src="/brand/wordmark-white.png"
      srcset="/brand/wordmark-white.png 1x, /brand/wordmark-white@2x.png 2x"
      alt="Fidibeki"
      :height="height"
      :width="Math.round(height * 3.944)"
    />
  </span>
</template>

<style scoped>
.brand-logo {
  display: inline-flex;
  align-items: center;
}

.brand-logo img {
  display: block;
  /* The global img reset sets height: auto; pin the rendered size here. */
  height: var(--logo-height);
  width: auto;
}

:root[data-theme='light'] .brand-logo-dark {
  display: none;
}

:root[data-theme='dark'] .brand-logo-light {
  display: none;
}
</style>
