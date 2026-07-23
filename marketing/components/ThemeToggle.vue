<script setup lang="ts">
import { Moon, Sun } from '@lucide/vue'

const { theme, toggle, syncFromDocument } = useTheme()

onMounted(syncFromDocument)

const label = computed(() =>
  theme.value === 'dark' ? 'Switch to light theme' : 'Switch to dark theme',
)
</script>

<template>
  <button type="button" class="theme-toggle" :aria-label="label" :title="label" @click="toggle">
    <Sun v-if="theme === 'dark'" :size="18" aria-hidden="true" />
    <Moon v-else :size="18" aria-hidden="true" />
  </button>
</template>

<style scoped>
.theme-toggle {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 38px;
  height: 38px;
  border: 1px solid var(--line);
  border-radius: var(--radius);
  background: transparent;
  color: var(--ink-soft);
  cursor: pointer;
  transition: border-color 140ms ease, color 140ms ease;
}

.theme-toggle:hover {
  border-color: var(--accent);
  color: var(--accent-text);
}

.theme-toggle :deep(svg) {
  transition: transform 220ms ease;
}

.theme-toggle:hover :deep(svg) {
  transform: rotate(14deg);
}
</style>
