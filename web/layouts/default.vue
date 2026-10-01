<script setup lang="ts">
import {
  Activity,
  Archive,
  BarChart3,
  Bell,
  BellRing,
  Blocks,
  Building2,
  ChevronDown,
  ChevronRight,
  CircleAlert,
  CreditCard,
  Fingerprint,
  FolderKanban,
  House,
  Inbox,
  KeyRound,
  LayoutList,
  Link2,
  LogOut,
  Menu,
  MessageSquareQuote,
  MonitorPlay,
  PanelRightOpen,
  Scale,
  ScrollText,
  Search,
  Settings,
  ShieldAlert,
  ShieldCheck,
  UserRoundPen,
  Users,
  X,
} from '@lucide/vue'
import { feedbackQueueFromQuery, type FeedbackQueueID } from '~/utils/feedback-queue'
import { settingsSectionFromQuery, settingsSectionIDs, settingsSectionRoute } from '~/utils/settings-sections'
import type { CommandOption } from '~/components/AppCommandPalette.client.vue'
import type { TenantConfiguration } from '~/types/api'

const route = useRoute()
const { t } = useI18n()
const { can, member, tenant } = useAuth()
const { plural } = useRegionalFormat()
const { $apiFetch, $clearSession } = useNuxtApp()
const mobileNavOpen = ref(false)
const paletteOpen = ref(false)
const queuesOpen = ref(true)
const loggingOut = ref(false)
const accountMenu = ref<HTMLDetailsElement | null>(null)
const mobileMenuButton = ref<HTMLButtonElement | null>(null)
const mobileNavCloseButton = ref<HTMLButtonElement | null>(null)

// Which features the plan carries, read from the server rather than mapped from
// the plan name here: a second copy of that mapping would drift from the one the
// API enforces, and the rail would offer a screen the routes refuse.
const { data: configuration } = await useAsyncData(
  'workspace-plan',
  () => $apiFetch<TenantConfiguration>('/v1/config'),
  { server: false },
)
const planFeatures = computed(() => configuration.value?.plan.features)

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

// No item carries an unread badge yet. The one count worth showing costs a
// whole notification list on every cold load, which is a lot of payload for a
// pill; once /v1/config carries it — that call is already made above — a badge
// here is free.
const primaryNavigation = [
  { labelKey: 'navigation.overview', to: '/', icon: House, permissions: ['feedback:read', 'issue:read'] },
  { labelKey: 'navigation.feedback', to: '/feedback', icon: Inbox, permissions: ['feedback:read'] },
  { labelKey: 'navigation.replays', to: '/replays', icon: MonitorPlay, permissions: ['session:read'], feature: 'session_replay' as const },
  { labelKey: 'navigation.issues', to: '/issues', icon: LayoutList, permissions: ['issue:read'] },
  { labelKey: 'navigation.notifications', to: '/notifications', icon: Bell, permissions: ['issue:read'] },
  { labelKey: 'navigation.testimonials', to: '/testimonials', icon: MessageSquareQuote, permissions: ['testimonial:read'], feature: 'testimonials' as const },
  { labelKey: 'navigation.analytics', to: '/analytics', icon: BarChart3, permissions: ['project:read'] },
  { labelKey: 'navigation.projects', to: '/projects', icon: FolderKanban, permissions: ['project:read'] },
  { labelKey: 'navigation.settings', to: '/settings', icon: Settings, permissions: ['tenant:read'] },
]
const availablePrimaryNavigation = computed(() => primaryNavigation.filter((item) => {
  if (!item.permissions.every(can)) return false
  const feature = 'feature' in item ? item.feature : undefined
  // Until the plan is known, a gated item stays hidden. Showing it and taking it
  // away a moment later is worse than showing it a moment late.
  return !feature || planFeatures.value?.[feature] === true
}))

const queueNavigation: Array<{ id: FeedbackQueueID, labelKey: string, to: string, icon: typeof Inbox }> = [
  { id: 'open', labelKey: 'navigation.queues.open', to: '/feedback?status=open', icon: Inbox },
  { id: 'moderation', labelKey: 'navigation.queues.moderation', to: '/feedback?moderation=flagged', icon: ShieldAlert },
  { id: 'ai_failed', labelKey: 'navigation.queues.aiFailed', to: '/feedback?processing=failed', icon: CircleAlert },
  { id: 'replay', labelKey: 'navigation.queues.replay', to: '/feedback?attachment=replay', icon: PanelRightOpen },
  { id: 'unlinked', labelKey: 'navigation.queues.unlinked', to: '/feedback?issue=unlinked', icon: Link2 },
  { id: 'archived', labelKey: 'navigation.queues.archived', to: '/feedback?status=archived', icon: Archive },
]
const activeQueue = computed(() => route.path.startsWith('/feedback') ? feedbackQueueFromQuery(route.query) : null)

// Settings sections live in the rail like the feedback queues do, but the
// group earns its space only while the operator is in Settings: nine standing
// rows for occasional administration would out-shout the daily destinations.
const settingsSectionIcons: Record<string, typeof Settings> = {
  workspace: Building2,
  members: Users,
  security: ShieldCheck,
  sso: Fingerprint,
  keys: KeyRound,
  integrations: Blocks,
  health: Activity,
  privacy: Scale,
  audit: ScrollText,
}
const settingsOpen = ref(route.path.startsWith('/settings'))
watch(() => route.path, (path) => {
  if (path.startsWith('/settings')) settingsOpen.value = true
})
// On narrow viewports the settings sections render as chips on the settings
// page instead (the rail is a drawer there). Rendering both would put two
// identically named links in the document.
const narrowViewport = ref(false)
onMounted(() => {
  const media = window.matchMedia('(max-width: 700px)')
  narrowViewport.value = media.matches
  media.addEventListener('change', (event) => { narrowViewport.value = event.matches })
})
const activeSettingsSection = computed(() =>
  route.path === '/settings' ? settingsSectionFromQuery(route.query) : null,
)

function isPrimaryActive(to: string) {
  if (to === '/') return route.path === '/'
  return route.path.startsWith(to)
}

// The palette offers the destinations the rail already knows plus the account
// pages that only live behind the avatar menu — reachable without hunting for
// the menu that hides them.
const commandOptions = computed<CommandOption[]>(() => {
  const options: CommandOption[] = availablePrimaryNavigation.value.map((item) => ({
    id: `page:${item.to}`,
    label: t(item.labelKey),
    hint: item.to,
    group: t('navigation.palette.groups.pages'),
    icon: item.icon,
    to: item.to,
  }))

  if (can('feedback:read')) {
    options.push(...queueNavigation.map((item) => ({
      id: `queue:${item.id}`,
      label: t(item.labelKey),
      hint: item.to,
      group: t('navigation.feedbackQueues'),
      icon: item.icon,
      to: item.to,
    })))
  }

  options.push({
    id: 'account:profile',
    label: t('navigation.profile'),
    hint: '/profile',
    group: t('navigation.palette.groups.account'),
    icon: UserRoundPen,
    to: '/profile',
  })
  if (can('billing:read') || can('billing:manage')) {
    options.push({
      id: 'account:billing',
      label: t('navigation.billing'),
      hint: '/billing',
      group: t('navigation.palette.groups.account'),
      icon: CreditCard,
      to: '/billing',
    })
  }
  return options
})

function openPalette() {
  void closeMobileNavigation(false)
  paletteOpen.value = true
}

function handleShortcut(event: KeyboardEvent) {
  if (event.key !== 'k' && event.key !== 'K') return
  if (!event.metaKey && !event.ctrlKey) return
  // The palette is not rendered without this permission, so the shortcut must
  // not swallow the browser's own use of the combination either.
  if (!can('feedback:read')) return
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
    paletteOpen.value = false
    if (accountMenu.value) accountMenu.value.open = false
  },
)
</script>

<template>
  <div class="app-shell">
    <header class="app-topbar" :inert="mobileNavOpen">
      <button ref="mobileMenuButton" class="app-icon-button app-mobile-menu" type="button" :title="t('navigation.openNavigation')" @click="openMobileNavigation">
        <Menu :size="19" />
        <span class="sr-only">{{ t('navigation.openNavigation') }}</span>
      </button>
      <NuxtLink :to="workspaceHome" class="app-brand" :aria-label="t('navigation.workspaceLabel')">
        <img class="app-brand-light" src="/brand/fidibeki-color.png" alt="Fidibeki" />
        <img class="app-brand-dark" src="/brand/fidibeki-white.png" alt="Fidibeki" />
      </NuxtLink>
      <button v-if="can('feedback:read')" class="app-search-trigger" type="button" :title="t('navigation.palette.title')" @click="openPalette">
        <Search :size="15" />
        <span>{{ t('navigation.searchFeedback') }}</span>
        <kbd class="app-kbd">{{ shortcutHint }}</kbd>
      </button>
      <div class="app-topbar-actions">
        <SupportPanel v-if="member && !tenant?.deletion_requested_at" />
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
      </div>
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
      <!-- The workspace heads the navigation it scopes, so the topbar carries
           one identity instead of two competing marks. -->
      <NuxtLink class="app-tenant-switcher" to="/settings" :title="t('navigation.workspaceSettings')">
        <span><WorkspaceMark :tenant-id="tenant?.id || ''" :name="tenant?.name" :logo-version="tenant?.logo_version" /></span>
        <b>{{ tenant?.name || t('common.workspace') }}</b>
        <Settings :size="14" />
      </NuxtLink>
      <nav :aria-label="t('navigation.primaryLabel')">
        <template v-for="item in availablePrimaryNavigation" :key="item.to">
          <NuxtLink :to="item.to" :data-active="isPrimaryActive(item.to)">
            <component :is="item.icon" :size="17" />
            <span>{{ t(item.labelKey) }}</span>
          </NuxtLink>

          <details
            v-if="item.to === '/feedback'"
            class="app-rail-section"
            :open="queuesOpen"
            @toggle="queuesOpen = ($event.target as HTMLDetailsElement).open"
          >
            <summary><ChevronRight :size="12" />{{ t('navigation.feedbackQueues') }}</summary>
            <nav class="app-rail-queues" :aria-label="t('navigation.feedbackQueues')">
              <NuxtLink
                v-for="queue in queueNavigation"
                :key="queue.id"
                :to="queue.to"
                :data-active="activeQueue === queue.id"
                :aria-current="activeQueue === queue.id ? 'page' : undefined"
              >
                <component :is="queue.icon" :size="14" />
                <span>{{ t(queue.labelKey) }}</span>
              </NuxtLink>
            </nav>
          </details>

          <details
            v-if="item.to === '/settings' && !narrowViewport"
            class="app-rail-section"
            :open="settingsOpen"
            @toggle="settingsOpen = ($event.target as HTMLDetailsElement).open"
          >
            <summary><ChevronRight :size="12" />{{ t('admin.settings.sections') }}</summary>
            <nav class="app-rail-queues" :aria-label="t('admin.settings.sections')">
              <NuxtLink
                v-for="section in settingsSectionIDs"
                :key="section"
                :to="settingsSectionRoute(section)"
                :data-active="activeSettingsSection === section"
                :aria-current="activeSettingsSection === section ? 'page' : undefined"
              >
                <component :is="settingsSectionIcons[section]" :size="14" />
                <span>{{ t(`admin.settings.tabs.${section}`) }}</span>
              </NuxtLink>
            </nav>
          </details>
        </template>
      </nav>
    </aside>
    <div v-if="mobileNavOpen" class="app-nav-scrim" aria-hidden="true" @click="closeMobileNavigation()" />

    <div v-if="tenant?.deletion_requested_at" class="app-readonly-banner" role="status" :inert="mobileNavOpen">
      {{ t('navigation.deletionPending') }}
      <NuxtLink to="/billing">{{ t('navigation.reviewDeletion') }}</NuxtLink>
    </div>

    <main class="app-main" tabindex="-1" :inert="mobileNavOpen">
      <slot />
    </main>
    <AppCommandPalette v-if="can('feedback:read')" :open="paletteOpen" :options="commandOptions" @close="paletteOpen = false" />
    <AppToast />
  </div>
</template>
