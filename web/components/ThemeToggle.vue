<script setup lang="ts">
import { Moon, Sun } from '@lucide/vue'

const { t } = useI18n()
const { theme, toggle, syncFromDocument } = useTheme()

onMounted(syncFromDocument)

const label = computed(() =>
  theme.value === 'dark'
    ? t('navigation.theme.switchToLight')
    : t('navigation.theme.switchToDark'),
)
</script>

<template>
  <button
    type="button"
    class="app-icon-button app-theme-toggle"
    :aria-label="label"
    :title="label"
    @click="toggle"
  >
    <Sun v-if="theme === 'dark'" :size="18" aria-hidden="true" />
    <Moon v-else :size="18" aria-hidden="true" />
  </button>
</template>

<style scoped>
.app-theme-toggle :deep(svg) {
  transition: transform 220ms ease;
}

.app-theme-toggle:hover :deep(svg) {
  transform: rotate(14deg);
}

@media (prefers-reduced-motion: reduce) {
  .app-theme-toggle :deep(svg) {
    transition: none;
  }

  .app-theme-toggle:hover :deep(svg) {
    transform: none;
  }
}
</style>
