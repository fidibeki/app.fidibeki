<script setup lang="ts">
import { highlightCode } from '~/utils/highlight'

const props = withDefaults(
  defineProps<{ code: string, language?: string, inline?: boolean }>(),
  { language: 'html', inline: false },
)

/**
 * v-html is safe here because highlightCode escapes on both of its branches:
 * highlight.js escapes the source it tokenizes, and a language it has no
 * grammar for is escaped before it is returned.
 */
const rendered = computed(() => highlightCode(props.code, props.language))
</script>

<template>
  <!-- Inline drops the <pre> so the caller keeps its own box. The project
       installer shows its snippet as a single ellipsised line inside a 40px
       bar, and a block element there would be a different component. -->
  <code v-if="inline" class="app-code" v-html="rendered" />
  <pre v-else class="app-code"><code v-html="rendered" /></pre>
</template>

<style scoped>
/* Mapped onto the workspace palette rather than an imported hljs theme, so a
   snippet belongs to the page it sits on and follows the theme toggle. Amber is
   already in the palette and is the one hue far enough from teal that a string
   and an element name cannot be confused. */
.app-code { color: var(--ink-soft); }
.app-code :deep(.hljs-tag),
.app-code :deep(.hljs-punctuation),
.app-code :deep(.hljs-comment) { color: var(--muted); }
.app-code :deep(.hljs-comment) { font-style: italic; }
.app-code :deep(.hljs-name),
.app-code :deep(.hljs-keyword),
.app-code :deep(.hljs-literal) { color: var(--teal); font-weight: 700; }
.app-code :deep(.hljs-attr) { color: var(--ink); }
.app-code :deep(.hljs-string),
.app-code :deep(.hljs-number) { color: var(--amber); }
.app-code :deep(.hljs-title) { color: var(--ink); font-weight: 700; }
</style>
