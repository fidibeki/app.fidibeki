<script setup lang="ts">
import { Building2, ChevronDown, House, ListTodo, LogOut, MessageCircle, Search, ShieldAlert } from '@lucide/vue'
import type { PlatformCommand } from '~/components/PlatformCommandPalette.client.vue'

const route = useRoute()
const { session, clear } = usePlatformAuth()
const paletteOpen = ref(false)
const accountMenu = ref<HTMLDetailsElement | null>(null)
const main = ref<HTMLElement | null>(null)

const navigation = computed(() => [
  { label: 'Overview', to: '/', icon: House },
  { label: 'Workspaces', to: '/workspaces', icon: Building2 },
  ...(session.value.admin?.permissions.includes('platform:support:read') ? [{ label: 'Conversations', to: '/support', icon: MessageCircle }] : []),
  { label: 'Queues', to: '/queues', icon: ListTodo },
  { label: 'Privacy', to: '/privacy', icon: ShieldAlert },
])
const displayName = computed(() => session.value.admin?.email.split('@')[0] || 'Platform admin')

const commands = computed<PlatformCommand[]>(() => [
  ...navigation.value.map((item) => ({
    id: `page:${item.to}`,
    label: item.label,
    hint: item.to,
    group: 'Go to',
    icon: item.icon,
    to: item.to,
  })),
  // The directory's saved filters, so plan/status cohorts are one keystroke
  // away without the operator re-building the filter form each time.
  { id: 'filter:trials', label: 'Workspaces on trial', hint: '/workspaces?status=trial', group: 'Directory', icon: Building2, to: '/workspaces?status=trial' },
  { id: 'filter:suspended', label: 'Suspended workspaces', hint: '/workspaces?status=suspended', group: 'Directory', icon: Building2, to: '/workspaces?status=suspended' },
])

function isActive(to: string) {
  return to === '/' ? route.path === '/' : route.path.startsWith(to)
}

function handleShortcut(event: KeyboardEvent) {
  if (event.key !== 'k' && event.key !== 'K') return
  if (!event.metaKey && !event.ctrlKey) return
  event.preventDefault()
  paletteOpen.value = !paletteOpen.value
}

// The account menu is a plain <details>, which never closes itself: outside
// clicks and Escape must dismiss it or keyboard users have to re-toggle it.
function dismissAccountMenu(event: Event) {
  const menu = accountMenu.value
  if (!menu?.open) return
  if (event instanceof MouseEvent && menu.contains(event.target as Node)) return
  menu.open = false
}

function handleEscape(event: KeyboardEvent) {
  if (event.key !== 'Escape') return
  if (accountMenu.value?.open) {
    accountMenu.value.open = false
    accountMenu.value.querySelector('summary')?.focus()
  }
}

// Starts at the portable label and is corrected once the client knows the
// platform: rendering the same string on both passes keeps hydration quiet.
const shortcutHint = ref('Ctrl K')

onMounted(() => {
  window.addEventListener('keydown', handleShortcut)
  window.addEventListener('keydown', handleEscape)
  document.addEventListener('click', dismissAccountMenu)
  if (/Mac|iPhone|iPad/i.test(navigator.userAgent)) shortcutHint.value = '⌘ K'
})
onBeforeUnmount(() => {
  window.removeEventListener('keydown', handleShortcut)
  window.removeEventListener('keydown', handleEscape)
  document.removeEventListener('click', dismissAccountMenu)
})

async function logout() {
  try {
    await $fetch('/admin/auth/logout', { method: 'POST', credentials: 'include' })
  } finally {
    clear()
    await navigateTo('/login')
  }
}

watch(
  () => route.fullPath,
  async () => {
    paletteOpen.value = false
    if (accountMenu.value) accountMenu.value.open = false
    // Keyboard users land on the new page's content, not on the rail link
    // they just left; the skip link makes the same jump available on load.
    await nextTick()
    main.value?.focus()
  },
)
</script>

<template>
  <div class="backoffice-shell">
    <a class="backoffice-skip-link" href="#backoffice-main">Skip to content</a>
    <header class="backoffice-topbar">
      <NuxtLink to="/" class="backoffice-brand" aria-label="Fidibeki platform administration">
        <span>F</span><b>fidibeki</b><em>Platform</em>
      </NuxtLink>
      <button class="backoffice-search-trigger" type="button" title="Search and jump to" @click="paletteOpen = true">
        <Search :size="15" />
        <span>Search workspaces</span>
        <kbd class="platform-kbd">{{ shortcutHint }}</kbd>
      </button>
      <details ref="accountMenu" class="backoffice-account-menu">
        <summary><i>{{ displayName.slice(0, 1).toUpperCase() }}</i><span>{{ displayName }}</span><ChevronDown :size="13" /></summary>
        <div>
          <p><b>{{ displayName }}</b><span>{{ session.admin?.email }}</span></p>
          <button type="button" @click="logout"><LogOut :size="15" /> Sign out</button>
        </div>
      </details>
    </header>

    <aside class="backoffice-rail">
      <nav aria-label="Platform navigation">
        <NuxtLink
          v-for="item in navigation"
          :key="item.to"
          :to="item.to"
          :data-active="isActive(item.to)"
          :aria-current="isActive(item.to) ? 'page' : undefined"
        >
          <component :is="item.icon" :size="17" /><span>{{ item.label }}</span>
        </NuxtLink>
      </nav>
      <p class="backoffice-rail-note">Observe first.<br>Change deliberately.</p>
    </aside>

    <main id="backoffice-main" ref="main" class="backoffice-main" tabindex="-1"><slot /></main>
    <PlatformCommandPalette :open="paletteOpen" :commands="commands" @close="paletteOpen = false" />
  </div>
</template>
