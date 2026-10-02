<script setup lang="ts">
import { Globe2 } from '@lucide/vue'
import { marketingLocales } from '~/utils/locale'

const { locale, setLocale } = useI18n()
const { copy } = useMarketingI18n()
const switchLocalePath = useSwitchLocalePath()
const ready = ref(false)
const open = ref(false)
const trigger = ref<HTMLButtonElement | null>(null)
const root = ref<HTMLElement | null>(null)
const route = useRoute()
const id = useId()
function close(returnFocus = false) {
  open.value = false
  if (returnFocus) trigger.value?.focus()
}
function outside(event: PointerEvent) {
  if (!root.value?.contains(event.target as Node)) close()
}
watch(
  () => route.fullPath,
  () => close(),
)
onMounted(() => {
  ready.value = true
  document.addEventListener('pointerdown', outside)
})
onBeforeUnmount(() => document.removeEventListener('pointerdown', outside))
</script>
<template>
  <div
    ref="root"
    class="language-switcher"
    @keydown.esc.stop="close(true)"
    @focusout="
      (event) => {
        if (!root?.contains(event.relatedTarget as Node)) close()
      }
    "
  >
    <button
      ref="trigger"
      :disabled="!ready"
      type="button"
      :aria-label="copy('Choose language')"
      :aria-expanded="open"
      :aria-controls="id"
      @click="open = !open"
    >
      <Globe2 :size="16" aria-hidden="true" /> {{ locale.toUpperCase() }}
    </button>
    <nav
      v-if="open"
      :id="id"
      class="language-options"
      :aria-label="copy('Language')"
    >
      <a
        v-for="language in marketingLocales"
        :key="language.code"
        :href="switchLocalePath(language.code)"
        :lang="language.language"
        :hreflang="language.language"
        :aria-current="locale === language.code ? 'true' : undefined"
        @click.prevent="setLocale(language.code)"
      >
        <span>{{ language.name }}</span
        ><span v-if="locale === language.code" aria-hidden="true">✓</span>
      </a>
    </nav>
  </div>
</template>
<style scoped>
.language-switcher {
  position: relative;
  flex-shrink: 0;
}
.language-switcher > button {
  display: flex;
  align-items: center;
  gap: 8px;
  min-height: 44px;
  border: 0;
  padding: 8px 4px;
  background: transparent;
  color: var(--ink);
  font-size: 13px;
  cursor: pointer;
}
.language-options {
  position: absolute;
  z-index: 80;
  right: 0;
  top: calc(100% + 4px);
  display: grid;
  width: 172px;
  padding: 8px;
  background: var(--canvas);
  border: 1px solid var(--line);
  border-radius: 10px;
}
.language-options a {
  display: flex;
  justify-content: space-between;
  align-items: center;
  min-height: 44px;
  padding: 10px 12px;
  border-radius: 6px;
  color: var(--ink);
  font-size: 14px;
  text-decoration: none;
}
.language-options a:hover,
.language-options a[aria-current] {
  background: var(--surface-soft);
}
:global(.home-mobile-links) .language-options {
  left: 0;
  right: auto;
}
:global(.home-footer) .language-options {
  top: auto;
  bottom: calc(100% + 4px);
}
</style>
