<script setup lang="ts">
import { CornerDownLeft, Search } from '@lucide/vue'

export interface CommandOption {
  id: string
  label: string
  hint?: string
  group: string
  icon: typeof Search
  to: string
}

const props = defineProps<{ open: boolean, options: CommandOption[] }>()
const emit = defineEmits<{ close: [] }>()

const { t } = useI18n()
const query = ref('')
const highlighted = ref(0)
const list = ref<HTMLElement | null>(null)

/** Substring match on both the label and its hint, so "replay" finds the queue
 *  by name and "status=open" finds it by where it goes. */
const matches = computed(() => {
  const needle = query.value.trim().toLowerCase()
  if (!needle) return props.options
  return props.options.filter((option) =>
    `${option.label} ${option.hint || ''}`.toLowerCase().includes(needle),
  )
})

// A free-text query is worth an action of its own: the palette lists
// destinations, and anything it cannot name should still reach the feedback
// search rather than dead-ending on "no results".
const searchAction = computed<CommandOption | null>(() => {
  const term = query.value.trim()
  if (!term) return null
  return {
    id: 'search-feedback',
    label: t('navigation.palette.searchFor', { query: term }),
    group: t('navigation.palette.groups.search'),
    icon: Search,
    to: `/feedback?q=${encodeURIComponent(term)}`,
  }
})

const results = computed(() =>
  searchAction.value ? [searchAction.value, ...matches.value] : matches.value,
)

/** Grouped for rendering while `results` stays flat, so the arrow keys move
 *  through one sequence and never have to walk back out of a group. */
const groups = computed(() => {
  const ordered: Array<{ name: string, options: CommandOption[] }> = []
  for (const option of results.value) {
    const last = ordered.at(-1)
    if (last && last.name === option.group) last.options.push(option)
    else ordered.push({ name: option.group, options: [option] })
  }
  return ordered
})

function indexOf(option: CommandOption) {
  return results.value.findIndex((candidate) => candidate.id === option.id)
}

function move(step: number) {
  const total = results.value.length
  if (total === 0) return
  highlighted.value = (highlighted.value + step + total) % total
  scrollHighlightedIntoView()
}

async function scrollHighlightedIntoView() {
  await nextTick()
  list.value
    ?.querySelector<HTMLElement>('[data-highlighted="true"]')
    ?.scrollIntoView({ block: 'nearest' })
}

async function choose(option?: CommandOption) {
  const target = option || results.value[highlighted.value]
  if (!target) return
  emit('close')
  await navigateTo(target.to)
}

// Every reopen starts from a clean query: a palette that remembers the last
// search makes the first keystroke a correction rather than a beginning.
watch(
  () => props.open,
  (open) => {
    if (!open) return
    query.value = ''
    highlighted.value = 0
  },
)

watch(results, () => {
  if (highlighted.value >= results.value.length) highlighted.value = 0
})
</script>

<template>
  <AppDialog
    :open="open"
    initial-focus="input"
    :aria-label="t('navigation.palette.title')"
    @close="emit('close')"
  >
    <div
      class="app-palette"
      @keydown.down.prevent="move(1)"
      @keydown.up.prevent="move(-1)"
      @keydown.enter.prevent="choose()"
    >
      <div class="app-palette__search">
        <Search :size="17" aria-hidden="true" />
        <input
          v-model="query"
          type="text"
          autocomplete="off"
          spellcheck="false"
          :placeholder="t('navigation.palette.placeholder')"
          :aria-label="t('navigation.palette.title')"
        />
      </div>

      <div ref="list" class="app-palette__results">
        <p v-if="results.length === 0" class="app-palette__empty">
          {{ t('navigation.palette.empty') }}
        </p>
        <template v-for="group in groups" :key="group.name">
          <p class="app-palette__group">{{ group.name }}</p>
          <button
            v-for="option in group.options"
            :key="option.id"
            type="button"
            class="app-palette__option"
            :data-highlighted="indexOf(option) === highlighted"
            @click="choose(option)"
            @mousemove="highlighted = indexOf(option)"
          >
            <component :is="option.icon" :size="16" aria-hidden="true" />
            <b>{{ option.label }}</b>
            <small v-if="option.hint">{{ option.hint }}</small>
          </button>
        </template>
      </div>

      <footer class="app-palette__hints">
        <span><kbd class="app-kbd">↑</kbd><kbd class="app-kbd">↓</kbd>{{ t('navigation.palette.move') }}</span>
        <span><kbd class="app-kbd"><CornerDownLeft :size="11" /></kbd>{{ t('navigation.palette.select') }}</span>
        <span><kbd class="app-kbd">esc</kbd>{{ t('navigation.palette.cancel') }}</span>
      </footer>
    </div>
  </AppDialog>
</template>

<style scoped>
/* AppDialog centres its slot; the palette pulls itself to the upper third so
   the results grow downward into empty space instead of pushing the field
   around as the query narrows them. */
.app-palette {
  display: flex;
  width: min(620px, calc(100vw - 28px));
  max-height: min(540px, calc(100dvh - 130px));
  align-self: start;
  margin-top: clamp(20px, 9vh, 92px);
  flex-direction: column;
  overflow: hidden;
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: 10px;
  box-shadow: var(--shadow-overlay);
}

.app-palette__search {
  display: flex;
  min-height: 56px;
  flex: 0 0 auto;
  align-items: center;
  gap: 11px;
  padding: 0 16px;
  color: var(--muted);
  border-bottom: 1px solid var(--line);
}

.app-palette__search input {
  width: 100%;
  min-width: 0;
  border: 0;
  outline: 0;
  background: transparent;
  color: var(--ink);
  font-size: 15px;
}

.app-palette__search input::placeholder {
  color: var(--placeholder);
}

.app-palette__results {
  min-height: 0;
  flex: 1 1 auto;
  overflow-y: auto;
  overscroll-behavior: contain;
  padding: 6px 8px 8px;
}

.app-palette__group {
  padding: 11px 10px 5px;
  color: var(--muted);
  font-size: 10px;
  font-weight: 750;
  letter-spacing: 0.07em;
  text-transform: uppercase;
}

.app-palette__group:first-child {
  padding-top: 4px;
}

.app-palette__option {
  display: grid;
  width: 100%;
  min-height: 40px;
  grid-template-columns: 16px minmax(0, 1fr) auto;
  align-items: center;
  gap: 11px;
  padding: 0 10px;
  background: transparent;
  color: var(--ink-soft);
  border: 0;
  border-radius: 7px;
  font-size: 13px;
  text-align: left;
}

.app-palette__option[data-highlighted='true'] {
  background: var(--rail-active);
  color: var(--teal);
}

.app-palette__option b {
  overflow: hidden;
  font-weight: 650;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.app-palette__option small {
  overflow: hidden;
  color: var(--muted);
  font-size: 11px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.app-palette__empty {
  display: grid;
  min-height: 120px;
  place-items: center;
  color: var(--muted);
  font-size: 12px;
}

.app-palette__hints {
  display: flex;
  flex: 0 0 auto;
  align-items: center;
  gap: 16px;
  padding: 9px 14px;
  background: var(--footer-soft);
  color: var(--muted);
  border-top: 1px solid var(--line);
  font-size: 11px;
}

.app-palette__hints span {
  display: flex;
  align-items: center;
  gap: 6px;
}

.app-palette__hints span:last-child {
  margin-left: auto;
}

@media (max-width: 600px) {
  .app-palette {
    max-height: calc(100dvh - 90px);
    margin-top: 12px;
  }

  /* The hint bar describes keys a touch keyboard does not offer. */
  .app-palette__hints {
    display: none;
  }
}
</style>
