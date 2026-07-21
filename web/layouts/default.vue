<script setup lang="ts">
import {
  Archive,
  Bell,
  ChevronDown,
  CircleAlert,
  CircleUserRound,
  CreditCard,
  FolderKanban,
  House,
  Inbox,
  LayoutList,
  Link2,
  LogOut,
  Menu,
  PanelRightOpen,
  Search,
  Settings,
  ShieldAlert,
  X,
} from '@lucide/vue'

const route = useRoute()
const router = useRouter()
const { can, member, tenant } = useAuth()
const { $apiFetch, $clearSession } = useNuxtApp()
const mobileNavOpen = ref(false)
const search = ref(typeof route.query.q === 'string' ? route.query.q : '')
const loggingOut = ref(false)
const accountMenu = ref<HTMLDetailsElement | null>(null)

const showQueueRail = computed(() => can('feedback:read') && (route.path.startsWith('/feedback') || route.path === '/'))
const workspaceHome = computed(() => firstAccessibleWorkspacePath(member.value?.permissions || []))
const tenantInitials = computed(() =>
  (tenant.value?.name || 'Workspace')
    .split(/\s+/)
    .slice(0, 2)
    .map((part) => part[0]?.toUpperCase())
    .join(''),
)
const displayName = computed(() => member.value?.name || member.value?.email.split('@')[0] || 'Account')
const trialDays = computed(() => {
  if (!tenant.value?.trial_ends_at) return null
  const days = Math.ceil((new Date(tenant.value.trial_ends_at).getTime() - Date.now()) / 86_400_000)
  return Math.max(0, days)
})

const primaryNavigation = [
  { label: 'Overview', to: '/', icon: House, permissions: ['feedback:read', 'issue:read'] },
  { label: 'Feedback', to: '/feedback', icon: Inbox, permissions: ['feedback:read'] },
  { label: 'Issues', to: '/issues', icon: LayoutList, permissions: ['issue:read'] },
  { label: 'Projects', to: '/projects', icon: FolderKanban, permissions: ['project:read'] },
  { label: 'Settings', to: '/settings', icon: Settings, permissions: ['tenant:read'] },
]
const availablePrimaryNavigation = computed(() => primaryNavigation.filter(item => item.permissions.every(can)))

const queueNavigation = [
  { label: 'Open', to: '/feedback?status=open', icon: Inbox },
  { label: 'Moderation', to: '/feedback?moderation=flagged', icon: ShieldAlert },
  { label: 'AI failed', to: '/feedback?processing=failed', icon: CircleAlert },
  { label: 'Has replay', to: '/feedback?attachment=replay', icon: PanelRightOpen },
  { label: 'Unlinked', to: '/feedback?issue=unlinked', icon: Link2 },
  { label: 'Archived', to: '/feedback?status=archived', icon: Archive },
]

function isPrimaryActive(to: string) {
  if (to === '/') return route.path === '/'
  return route.path.startsWith(to)
}

function submitSearch() {
  router.push({ path: '/feedback', query: search.value.trim() ? { q: search.value.trim() } : {} })
}

async function logout() {
  if (loggingOut.value) return
  loggingOut.value = true
  try {
    await $apiFetch('/auth/logout', { method: 'POST', auth: false, retryAuth: false })
  } catch {
    // Local session must still be cleared when the server session already expired.
  } finally {
    $clearSession()
    await navigateTo('/login')
    loggingOut.value = false
  }
}

watch(
  () => route.fullPath,
  () => {
    mobileNavOpen.value = false
    if (accountMenu.value) accountMenu.value.open = false
  },
)
</script>

<template>
  <div class="app-shell" :class="{ 'app-shell--queues': showQueueRail }">
    <header class="app-topbar">
      <button class="app-icon-button app-mobile-menu" type="button" title="Open navigation" @click="mobileNavOpen = true">
        <Menu :size="19" />
        <span class="sr-only">Open navigation</span>
      </button>
      <NuxtLink :to="workspaceHome" class="app-brand" aria-label="Fidibeki workspace">
        <span>F</span><b>fidibeki</b>
      </NuxtLink>
      <button class="app-tenant-switcher" type="button" title="Current workspace">
        <span>{{ tenantInitials }}</span>
        <b>{{ tenant?.name || 'Workspace' }}</b>
        <ChevronDown :size="14" />
      </button>
      <form v-if="can('feedback:read')" class="app-global-search" role="search" @submit.prevent="submitSearch">
        <Search :size="15" />
        <input v-model="search" type="search" autocomplete="off" placeholder="Search feedback" aria-label="Search feedback" />
      </form>
      <NuxtLink v-if="tenant?.status === 'trial'" to="/billing" class="app-trial-status">
        <i /> {{ tenant.plan }} trial · {{ trialDays }} days
      </NuxtLink>
      <button class="app-icon-button" type="button" title="Notifications">
        <Bell :size="17" />
        <span class="sr-only">Notifications</span>
      </button>
      <details ref="accountMenu" class="app-account-menu">
        <summary><CircleUserRound :size="19" /><span>{{ displayName }}</span><ChevronDown :size="13" /></summary>
        <div>
          <p><b>{{ displayName }}</b><span>{{ member?.email }}</span></p>
          <NuxtLink v-if="can('billing:read')" to="/billing"><CreditCard :size="15" /> Billing</NuxtLink>
          <button type="button" :disabled="loggingOut" @click="logout"><LogOut :size="15" /> Sign out</button>
        </div>
      </details>
    </header>

    <aside class="app-global-rail" :data-open="mobileNavOpen">
      <div class="app-mobile-rail-head">
        <b>Navigation</b>
        <button class="app-icon-button" type="button" title="Close navigation" @click="mobileNavOpen = false"><X :size="18" /></button>
      </div>
      <nav aria-label="Primary navigation">
        <NuxtLink
          v-for="item in availablePrimaryNavigation"
          :key="item.to"
          :to="item.to"
          :data-active="isPrimaryActive(item.to)"
        >
          <component :is="item.icon" :size="20" />
          <span>{{ item.label }}</span>
        </NuxtLink>
      </nav>
    </aside>
    <button v-if="mobileNavOpen" class="app-nav-scrim" type="button" aria-label="Close navigation" @click="mobileNavOpen = false" />

    <aside v-if="showQueueRail" class="app-queue-rail">
      <div class="app-queue-heading"><span>Feedback queues</span></div>
      <nav aria-label="Feedback queues">
        <NuxtLink v-for="item in queueNavigation" :key="item.label" :to="item.to">
          <component :is="item.icon" :size="15" />
          <span>{{ item.label }}</span>
        </NuxtLink>
      </nav>
      <div class="app-queue-note">
        <CircleAlert :size="15" />
        <p><b>Attention queue</b><span>AI and moderation signals stay visible here.</span></p>
      </div>
    </aside>

    <div v-if="tenant?.deletion_requested_at" class="app-readonly-banner" role="status">
      This workspace is pending deletion and is read-only.
      <NuxtLink to="/billing">Review deletion</NuxtLink>
    </div>

    <main class="app-main" tabindex="-1">
      <slot />
    </main>
  </div>
</template>
