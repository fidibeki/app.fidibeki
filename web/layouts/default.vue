<script setup lang="ts">
import {
  Archive,
  BarChart3,
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
import { feedbackQueueFromQuery, type FeedbackQueueID } from '~/utils/feedback-queue'

const route = useRoute()
const router = useRouter()
const { can, member, tenant } = useAuth()
const { $apiFetch, $clearSession } = useNuxtApp()
const mobileNavOpen = ref(false)
const search = ref(typeof route.query.q === 'string' ? route.query.q : '')
const loggingOut = ref(false)
const accountMenu = ref<HTMLDetailsElement | null>(null)
const mobileMenuButton = ref<HTMLButtonElement | null>(null)
const mobileNavCloseButton = ref<HTMLButtonElement | null>(null)

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
  { label: 'Notifications', to: '/notifications', icon: Bell, permissions: ['issue:read'] },
  { label: 'Analytics', to: '/analytics', icon: BarChart3, permissions: ['project:read'] },
  { label: 'Projects', to: '/projects', icon: FolderKanban, permissions: ['project:read'] },
  { label: 'Settings', to: '/settings', icon: Settings, permissions: ['tenant:read'] },
]
const availablePrimaryNavigation = computed(() => primaryNavigation.filter(item => item.permissions.every(can)))

const queueNavigation: Array<{ id: FeedbackQueueID, label: string, to: string, icon: typeof Inbox }> = [
  { id: 'open', label: 'Open', to: '/feedback?status=open', icon: Inbox },
  { id: 'moderation', label: 'Moderation', to: '/feedback?moderation=flagged', icon: ShieldAlert },
  { id: 'ai_failed', label: 'AI failed', to: '/feedback?processing=failed', icon: CircleAlert },
  { id: 'replay', label: 'Has replay', to: '/feedback?attachment=replay', icon: PanelRightOpen },
  { id: 'unlinked', label: 'Unlinked', to: '/feedback?issue=unlinked', icon: Link2 },
  { id: 'archived', label: 'Archived', to: '/feedback?status=archived', icon: Archive },
]
const activeQueue = computed(() => route.path.startsWith('/feedback') ? feedbackQueueFromQuery(route.query) : null)

function isPrimaryActive(to: string) {
  if (to === '/') return route.path === '/'
  return route.path.startsWith(to)
}

function submitSearch() {
  router.push({ path: '/feedback', query: search.value.trim() ? { q: search.value.trim() } : {} })
}

async function openMobileNavigation() {
  mobileNavOpen.value = true
  await nextTick()
  mobileNavCloseButton.value?.focus()
}

async function closeMobileNavigation(restoreFocus = true) {
  if (!mobileNavOpen.value) return
  mobileNavOpen.value = false
  if (restoreFocus) {
    await nextTick()
    mobileMenuButton.value?.focus()
  }
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
    void closeMobileNavigation(false)
    if (accountMenu.value) accountMenu.value.open = false
  },
)
</script>

<template>
  <div class="app-shell" :class="{ 'app-shell--queues': showQueueRail }">
    <header class="app-topbar" :inert="mobileNavOpen">
      <button ref="mobileMenuButton" class="app-icon-button app-mobile-menu" type="button" title="Open navigation" @click="openMobileNavigation">
        <Menu :size="19" />
        <span class="sr-only">Open navigation</span>
      </button>
      <NuxtLink :to="workspaceHome" class="app-brand" aria-label="Fidibeki workspace">
        <img src="/brand/fidibeki-color.png" alt="Fidibeki" />
      </NuxtLink>
      <NuxtLink class="app-tenant-switcher" to="/settings" title="Workspace settings">
        <span>{{ tenantInitials }}</span>
        <b>{{ tenant?.name || 'Workspace' }}</b>
        <Settings :size="14" />
      </NuxtLink>
      <form v-if="can('feedback:read')" class="app-global-search" role="search" @submit.prevent="submitSearch">
        <Search :size="15" />
        <input v-model="search" type="search" autocomplete="off" placeholder="Search feedback" aria-label="Search feedback" />
      </form>
      <NuxtLink v-if="tenant?.status === 'trial'" to="/billing" class="app-trial-status">
        <i /> {{ tenant.plan }} trial · {{ trialDays }} days
      </NuxtLink>
      <details ref="accountMenu" class="app-account-menu">
        <summary><CircleUserRound :size="19" /><span>{{ displayName }}</span><ChevronDown :size="13" /></summary>
        <div>
          <p><b>{{ displayName }}</b><span>{{ member?.email }}</span></p>
          <NuxtLink v-if="can('billing:read') || can('billing:manage')" to="/billing"><CreditCard :size="15" /> Billing</NuxtLink>
          <button type="button" :disabled="loggingOut" @click="logout"><LogOut :size="15" /> Sign out</button>
        </div>
      </details>
    </header>

    <aside
      class="app-global-rail"
      :data-open="mobileNavOpen"
      :role="mobileNavOpen ? 'dialog' : undefined"
      :aria-modal="mobileNavOpen ? 'true' : undefined"
      :aria-label="mobileNavOpen ? 'Navigation' : undefined"
      @keydown.esc.stop.prevent="closeMobileNavigation()"
    >
      <div class="app-mobile-rail-head">
        <b>Navigation</b>
        <button ref="mobileNavCloseButton" class="app-icon-button" type="button" title="Close navigation" @click="closeMobileNavigation()"><X :size="18" /></button>
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
    <div v-if="mobileNavOpen" class="app-nav-scrim" aria-hidden="true" @click="closeMobileNavigation()" />

    <aside v-if="showQueueRail" class="app-queue-rail" :inert="mobileNavOpen">
      <div class="app-queue-heading"><span>Feedback queues</span></div>
      <nav aria-label="Feedback queues">
        <NuxtLink
          v-for="item in queueNavigation"
          :key="item.id"
          :to="item.to"
          :data-active="activeQueue === item.id"
          :aria-current="activeQueue === item.id ? 'page' : undefined"
        >
          <component :is="item.icon" :size="15" />
          <span>{{ item.label }}</span>
        </NuxtLink>
      </nav>
      <div class="app-queue-note">
        <CircleAlert :size="15" />
        <p><b>Attention queue</b><span>AI and moderation signals stay visible here.</span></p>
      </div>
    </aside>

    <div v-if="tenant?.deletion_requested_at" class="app-readonly-banner" role="status" :inert="mobileNavOpen">
      This workspace is pending deletion and is read-only.
      <NuxtLink to="/billing">Review deletion</NuxtLink>
    </div>

    <main class="app-main" tabindex="-1" :inert="mobileNavOpen">
      <slot />
    </main>
  </div>
</template>
