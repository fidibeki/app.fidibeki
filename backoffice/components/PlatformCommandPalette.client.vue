<script setup lang="ts">
import { CornerDownLeft, Search } from '@lucide/vue'

export interface PlatformCommand {
  id: string
  label: string
  hint?: string
  group: string
  icon: typeof Search
  to: string
}

const props = defineProps<{ open: boolean, commands: PlatformCommand[] }>()
const emit = defineEmits<{ close: [] }>()

const dialog = ref<HTMLDialogElement | null>(null)
const query = ref('')
const highlighted = ref(0)
let restoreFocusTo: HTMLElement | null = null

/** Substring match on label and hint, so "susp" finds a page by name and
 *  "status=trial" finds the same page by where it goes. */
const matches = computed(() => {
  const needle = query.value.trim().toLowerCase()
  if (!needle) return props.commands
  return props.commands.filter((command) =>
    `${command.label} ${command.hint || ''}`.toLowerCase().includes(needle),
  )
})

// A free-text query always resolves to the workspace directory: the operator's
// most common jump is "find this tenant", and the palette must not dead-end on
// a name it does not list.
const searchAction = computed<PlatformCommand | null>(() => {
  const term = query.value.trim()
  if (!term) return null
  return {
    id: 'search-workspaces',
    label: `Search workspaces for “${term}”`,
    group: 'Search',
    icon: Search,
    to: `/workspaces?q=${encodeURIComponent(term)}`,
  }
})

// Exact destinations outrank the free-text action: an operator who can see the
// page they typed should reach it on Enter, not a search-results detour.
const results = computed(() =>
  searchAction.value ? [...matches.value, searchAction.value] : matches.value,
)

const groups = computed(() => {
  const ordered: Array<{ name: string, commands: PlatformCommand[] }> = []
  for (const command of results.value) {
    const last = ordered.at(-1)
    if (last && last.name === command.group) last.commands.push(command)
    else ordered.push({ name: command.group, commands: [command] })
  }
  return ordered
})

const activeOptionID = computed(() => {
  const active = results.value[highlighted.value]
  return active ? `platform-option-${active.id}` : undefined
})

function indexOf(command: PlatformCommand) {
  return results.value.findIndex((candidate) => candidate.id === command.id)
}

function move(step: number) {
  const total = results.value.length
  if (total === 0) return
  highlighted.value = (highlighted.value + step + total) % total
  nextTick(() => {
    dialog.value
      ?.querySelector('[aria-selected="true"]')
      ?.scrollIntoView({ block: 'nearest' })
  })
}

async function choose(command?: PlatformCommand) {
  const target = command || results.value[highlighted.value]
  if (!target) return
  emit('close')
  await navigateTo(target.to)
}

watch(
  () => props.open,
  async (open) => {
    await nextTick()
    if (open && dialog.value && !dialog.value.open) {
      query.value = ''
      highlighted.value = 0
      restoreFocusTo = document.activeElement instanceof HTMLElement ? document.activeElement : null
      dialog.value.showModal()
      await nextTick()
      dialog.value.querySelector('input')?.focus()
    }
    if (!open && dialog.value?.open) {
      dialog.value.close()
      await nextTick()
      restoreFocusTo?.focus()
      restoreFocusTo = null
    }
  },
  { immediate: true, flush: 'post' },
)

watch(results, () => {
  if (highlighted.value >= results.value.length) highlighted.value = 0
})

function closeFromBackdrop(event: MouseEvent) {
  if (event.target === dialog.value) emit('close')
}
</script>

<template>
  <dialog
    ref="dialog"
    class="platform-palette-dialog"
    aria-label="Search and jump to"
    @cancel.prevent="emit('close')"
    @click="closeFromBackdrop"
  >
    <div
      class="platform-palette"
      @keydown.down.prevent="move(1)"
      @keydown.up.prevent="move(-1)"
      @keydown.enter.prevent="choose()"
    >
      <div class="platform-palette__search">
        <Search :size="17" aria-hidden="true" />
        <input
          type="text"
          role="combobox"
          aria-expanded="true"
          aria-controls="platform-palette-results"
          aria-haspopup="listbox"
          :aria-activedescendant="activeOptionID"
          autocomplete="off"
          spellcheck="false"
          placeholder="Search workspaces, or jump to a section…"
          aria-label="Search and jump to"
          :value="query"
          @input="query = ($event.target as HTMLInputElement).value"
        />
      </div>

      <div id="platform-palette-results" class="platform-palette__results" role="listbox" aria-label="Results">
        <p v-if="results.length === 0" class="platform-palette__empty">Nothing matches that.</p>
        <template v-for="group in groups" :key="group.name">
          <p class="platform-palette__group" role="presentation">{{ group.name }}</p>
          <button
            v-for="command in group.commands"
            :id="`platform-option-${command.id}`"
            :key="command.id"
            type="button"
            class="platform-palette__option"
            role="option"
            :aria-selected="indexOf(command) === highlighted"
            tabindex="-1"
            @click="choose(command)"
            @mousemove="highlighted = indexOf(command)"
          >
            <component :is="command.icon" :size="16" aria-hidden="true" />
            <b>{{ command.label }}</b>
            <small v-if="command.hint">{{ command.hint }}</small>
          </button>
        </template>
      </div>
      <p class="sr-only" role="status">
        {{ results.length === 1 ? '1 result' : `${results.length} results` }}
      </p>

      <footer class="platform-palette__hints">
        <span><kbd class="platform-kbd">↑</kbd><kbd class="platform-kbd">↓</kbd>Move</span>
        <span><kbd class="platform-kbd"><CornerDownLeft :size="11" /></kbd>Select</span>
        <span><kbd class="platform-kbd">esc</kbd>Close</span>
      </footer>
    </div>
  </dialog>
</template>
