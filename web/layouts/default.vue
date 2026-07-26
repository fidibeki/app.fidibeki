<script setup lang="ts">
import {
  Archive,
  BarChart3,
  Bell,
  BellRing,
  ChevronDown,
  CircleAlert,
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
  UserRoundPen,
  X,
} from '@lucide/vue'
import { feedbackQueueFromQuery, type FeedbackQueueID } from '~/utils/feedback-queue'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const { can, member, tenant } = useAuth()
const { plural } = useRegionalFormat()
const { $apiFetch, $clearSession } = useNuxtApp()
const mobileNavOpen = ref(false)
const search = ref(typeof route.query.q === 'string' ? route.query.q : '')
const loggingOut = ref(false)
const accountMenu = ref<HTMLDetailsElement | null>(null)
const mobileMenuButton = ref<HTMLButtonElement | null>(null)
const mobileNavCloseButton = ref<HTMLButtonElement | null>(null)

const showQueueRail = computed(() => can('feedback:read') && (route.path.startsWith('/feedback') || route.path === '/'))
const workspaceHome = computed(() => firstAccessibleWorkspacePath(member.value?.permissions || []))
const displayName = computed(() => member.value?.name || member.value?.email.split('@')[0] || t('common.account'))
const trialDays = computed(() => {
  if (!tenant.value?.trial_ends_at) return null
  const days = Math.ceil((new Date(tenant.value.trial_ends_at).getTime() - Date.now()) / 86_400_000)
  return Math.max(0, days)
})
const trialLabel = computed(() => trialDays.value === null
  ? ''
  : plural('navigation.trial', trialDays.value, { plan: tenant.value?.plan || '' }))

const primaryNavigation = [
  { labelKey: 'navigation.overview', to: '/', icon: House, permissions: ['feedback:read', 'issue:read'] },
  { labelKey: 'navigation.feedback', to: '/feedback', icon: Inbox, permissions: ['feedback:read'] },
  { labelKey: 'navigation.issues', to: '/issues', icon: LayoutList, permissions: ['issue:read'] },
  { labelKey: 'navigation.notifications', to: '/notifications', icon: Bell, permissions: ['issue:read'] },
  { labelKey: 'navigation.analytics', to: '/analytics', icon: BarChart3, permissions: ['project:read'] },
  { labelKey: 'navigation.projects', to: '/projects', icon: FolderKanban, permissions: ['project:read'] },
  { labelKey: 'navigation.settings', to: '/settings', icon: Settings, permissions: ['tenant:read'] },
]
const availablePrimaryNavigation = computed(() => primaryNavigation.filter(item => item.permissions.every(can)))

const queueNavigation: Array<{ id: FeedbackQueueID, labelKey: string, to: string, icon: typeof Inbox }> = [
  { id: 'open', labelKey: 'navigation.queues.open', to: '/feedback?status=open', icon: Inbox },
  { id: 'moderation', labelKey: 'navigation.queues.moderation', to: '/feedback?moderation=flagged', icon: ShieldAlert },
  { id: 'ai_failed', labelKey: 'navigation.queues.aiFailed', to: '/feedback?processing=failed', icon: CircleAlert },
  { id: 'replay', labelKey: 'navigation.queues.replay', to: '/feedback?attachment=replay', icon: PanelRightOpen },
  { id: 'unlinked', labelKey: 'navigation.queues.unlinked', to: '/feedback?issue=unlinked', icon: Link2 },
  { id: 'archived', labelKey: 'navigation.queues.archived', to: '/feedback?status=archived', icon: Archive },
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
      <button ref="mobileMenuButton" class="app-icon-button app-mobile-menu" type="button" :title="t('navigation.openNavigation')" @click="openMobileNavigation">
        <Menu :size="19" />
        <span class="sr-only">{{ t('navigation.openNavigation') }}</span>
      </button>
      <NuxtLink :to="workspaceHome" class="app-brand" :aria-label="t('navigation.workspaceLabel')">
        <img class="app-brand-light" src="/brand/fidibeki-color.png" alt="Fidibeki" />
        <img class="app-brand-dark" src="/brand/fidibeki-white.png" alt="Fidibeki" />
      </NuxtLink>
      <NuxtLink class="app-tenant-switcher" to="/settings" :title="t('navigation.workspaceSettings')">
        <span><WorkspaceMark :tenant-id="tenant?.id || ''" :name="tenant?.name" :logo-version="tenant?.logo_version" /></span>
        <b>{{ tenant?.name || t('common.workspace') }}</b>
        <Settings :size="14" />
      </NuxtLink>
      <form v-if="can('feedback:read')" class="app-global-search" role="search" @submit.prevent="submitSearch">
        <Search :size="15" />
        <input v-model="search" type="search" autocomplete="off" :placeholder="t('navigation.searchFeedback')" :aria-label="t('navigation.searchFeedback')" />
      </form>
      <NuxtLink v-if="tenant?.status === 'trial'" to="/billing" class="app-trial-status">
        <i /> {{ trialLabel }}
      </NuxtLink>
      <ThemeToggle />
      <details ref="accountMenu" class="app-account-menu">
        <summary>
          <i class="app-account-avatar"><MemberAvatar :member-id="member?.id || ''" :name="member?.name" :email="member?.email" :avatar-version="member?.avatar_version" /></i>
          <span>{{ displayName }}</span><ChevronDown :size="13" />
        </summary>
        <div>
          <p><b>{{ displayName }}</b><span>{{ member?.email }}</span></p>
          <NuxtLink to="/profile"><UserRoundPen :size="15" /> {{ t('navigation.profile') }}</NuxtLink>
          <NuxtLink to="/settings/notifications"><BellRing :size="15" /> {{ t('navigation.notificationPreferences') }}</NuxtLink>
          <NuxtLink v-if="can('billing:read') || can('billing:manage')" to="/billing"><CreditCard :size="15" /> {{ t('navigation.billing') }}</NuxtLink>
          <button type="button" :disabled="loggingOut" @click="logout"><LogOut :size="15" /> {{ t('navigation.signOut') }}</button>
        </div>
      </details>
    </header>

    <aside
      class="app-global-rail"
      :data-open="mobileNavOpen"
      :role="mobileNavOpen ? 'dialog' : undefined"
      :aria-modal="mobileNavOpen ? 'true' : undefined"
      :aria-label="mobileNavOpen ? t('navigation.navigation') : undefined"
      @keydown.esc.stop.prevent="closeMobileNavigation()"
    >
      <div class="app-mobile-rail-head">
        <b>{{ t('navigation.navigation') }}</b>
        <button ref="mobileNavCloseButton" class="app-icon-button" type="button" :title="t('navigation.closeNavigation')" @click="closeMobileNavigation()"><X :size="18" /></button>
      </div>
      <nav :aria-label="t('navigation.primaryLabel')">
        <NuxtLink
          v-for="item in availablePrimaryNavigation"
          :key="item.to"
          :to="item.to"
          :data-active="isPrimaryActive(item.to)"
        >
          <component :is="item.icon" :size="20" />
          <span>{{ t(item.labelKey) }}</span>
        </NuxtLink>
      </nav>
    </aside>
    <div v-if="mobileNavOpen" class="app-nav-scrim" aria-hidden="true" @click="closeMobileNavigation()" />

    <aside v-if="showQueueRail" class="app-queue-rail" :inert="mobileNavOpen">
      <div class="app-queue-heading"><span>{{ t('navigation.feedbackQueues') }}</span></div>
      <nav :aria-label="t('navigation.feedbackQueues')">
        <NuxtLink
          v-for="item in queueNavigation"
          :key="item.id"
          :to="item.to"
          :data-active="activeQueue === item.id"
          :aria-current="activeQueue === item.id ? 'page' : undefined"
        >
          <component :is="item.icon" :size="15" />
          <span>{{ t(item.labelKey) }}</span>
        </NuxtLink>
      </nav>
    </aside>

    <div v-if="tenant?.deletion_requested_at" class="app-readonly-banner" role="status" :inert="mobileNavOpen">
      {{ t('navigation.deletionPending') }}
      <NuxtLink to="/billing">{{ t('navigation.reviewDeletion') }}</NuxtLink>
    </div>

    <main class="app-main" tabindex="-1" :inert="mobileNavOpen">
      <slot />
    </main>
    <AppToast />
  </div>
</template>
