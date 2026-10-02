<script setup lang="ts">
const props = defineProps<{ code?: string; compact?: boolean }>()
const { t, locale } = useI18n()
const label = computed(() => {
  if (!props.code) return ''
  try { return new Intl.DisplayNames([locale.value], { type: 'language' }).of(props.code) || props.code }
  catch { return props.code }
})
</script>

<template>
  <span v-if="code || !compact" class="visitor-locale" :title="t('chat.browserLocaleHint')">
    <template v-if="code"><span v-if="!compact">{{ t('chat.browserLocale') }}: </span>{{ label }} <span class="visitor-locale-code">{{ code }}</span></template>
    <template v-else>{{ t('chat.browserLocaleUnknown') }}</template>
  </span>
</template>

<style scoped>
.visitor-locale { display: block; color: var(--ink-soft); font-size: 12px; line-height: 1.5; white-space: normal; overflow-wrap: anywhere; margin-top: 4px; }
.visitor-locale-code { display: inline-block; font-size: 11px; margin-left: 4px; white-space: nowrap; }
</style>
