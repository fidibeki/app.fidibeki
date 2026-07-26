<script setup lang="ts">
import { highlightCode } from '~/utils/highlight'

const props = defineProps<{ code: string, language?: string }>()

/**
 * v-html is safe here because highlightCode escapes on both of its branches:
 * highlight.js escapes the source it tokenizes, and a language it has no
 * grammar for is escaped before it is returned.
 */
const rendered = computed(() => highlightCode(props.code, props.language || ''))
</script>

<template>
  <pre class="code-block"><code v-html="rendered" /></pre>
</template>

<style scoped>
/* The palette is the site's own tokens rather than an imported hljs theme, so a
   snippet reads as part of the page in both colour schemes instead of being a
   rectangle borrowed from someone else's editor. */
.code-block { color: var(--ink-soft); }
.code-block :deep(.hljs-tag),
.code-block :deep(.hljs-punctuation),
.code-block :deep(.hljs-comment) { color: var(--muted); }
.code-block :deep(.hljs-comment) { font-style: italic; }
.code-block :deep(.hljs-name),
.code-block :deep(.hljs-keyword),
.code-block :deep(.hljs-literal) { color: var(--accent-text); font-weight: 600; }
.code-block :deep(.hljs-attr) { color: var(--ink); }
.code-block :deep(.hljs-string),
.code-block :deep(.hljs-number) { color: var(--code-string); }
.code-block :deep(.hljs-title) { color: var(--ink); font-weight: 600; }
</style>
