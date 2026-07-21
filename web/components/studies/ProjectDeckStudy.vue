<script setup lang="ts">
import {
  Activity,
  AlertTriangle,
  ArrowLeft,
  ArrowRight,
  Bell,
  CheckCircle2,
  ChevronDown,
  CircleDot,
  ExternalLink,
  Filter,
  Inbox,
  LayoutDashboard,
  Link2,
  Menu,
  MoreHorizontal,
  Play,
  Plus,
  RefreshCw,
  Search,
  Settings2,
  SlidersHorizontal,
  Sparkles,
  UserRound,
  Users,
  X,
} from '@lucide/vue'
import { feedbackItems, overviewMetrics, projects } from '~/data/design-fixtures'
import type { FeedbackItem, ViewSlug } from '~/types/design'
import { defaultFilters } from '~/utils/design'

const props = defineProps<{ view: ViewSlug }>()

const {
  filters,
  filteredItems: baseFilteredItems,
  selected,
  notice,
  select,
  confirm,
} = usePrototypeState()

const activeProjectSlug = ref(projects[0]!.slug)
const drawerOpen = ref(false)
const filterPanelOpen = ref(false)
const mobileMenuOpen = ref(false)

filters.project = activeProjectSlug.value

const activeProject = computed(
  () => projects.find((project) => project.slug === activeProjectSlug.value) ?? projects[0]!,
)
const projectFeedback = computed(() =>
  feedbackItems.filter((item) => item.projectSlug === activeProjectSlug.value),
)
const filteredItems = computed(() => {
  if (filters.date === 'all') return baseFilteredItems.value
  const referenceTime = Math.max(...feedbackItems.map((item) => Date.parse(item.receivedAt)))
  const rangeHours = filters.date === '24h' ? 24 : filters.date === '7d' ? 24 * 7 : 24 * 30
  const cutoff = referenceTime - rangeHours * 60 * 60 * 1000
  return baseFilteredItems.value.filter((item) => Date.parse(item.receivedAt) >= cutoff)
})
const negativeCount = computed(
  () => projectFeedback.value.filter((item) => item.sentiment === 'negative').length,
)
const unlinkedCount = computed(
  () => projectFeedback.value.filter((item) => item.issue === null).length,
)
const activeFilterCount = computed(() => [
  filters.status,
  filters.rating,
  filters.attachment,
  filters.moderation,
  filters.issue,
  filters.sentiment,
].filter((value) => value !== 'all').length)

const navItems = [
  { label: 'Overview', view: 'overview', icon: LayoutDashboard },
  { label: 'Feedback', view: 'feedback', icon: Inbox },
  { label: 'Issues', view: '', icon: CircleDot },
  { label: 'Widget', view: '', icon: SlidersHorizontal },
  { label: 'Activity', view: '', icon: Activity },
  { label: 'Access', view: '', icon: Users },
] as const

function label(value: string) {
  return value.replace('-', ' ').replace(/^./, (character) => character.toUpperCase())
}

function changeProject() {
  filters.project = activeProjectSlug.value
  filters.query = ''
  const first = projectFeedback.value[0]
  if (first) select(first.id)
}

function resetFilters() {
  Object.assign(filters, defaultFilters(), { project: activeProjectSlug.value })
}

function openDrawer(item: FeedbackItem) {
  select(item.id)
  drawerOpen.value = true
}

function selectAdjacent(direction: -1 | 1) {
  const index = filteredItems.value.findIndex((item) => item.id === selected.value.id)
  if (index < 0) return
  const next = filteredItems.value[index + direction]
  if (next) select(next.id)
}

function handleNav(item: (typeof navItems)[number]) {
  mobileMenuOpen.value = false
  if (!item.view) confirm(`${item.label} is represented as a fixture-only destination.`)
}
</script>

<template>
  <div class="deck-shell">
    <header class="global-bar">
      <div class="brand" aria-label="Fidibeki">
        <span class="brand-mark">F</span>
        <strong>fidibeki</strong>
      </div>

      <div class="context-controls">
        <button class="tenant-button desktop-only" type="button" @click="confirm('Tenant switching is fixture-only.')">
          Acme Studio
          <ChevronDown :size="15" aria-hidden="true" />
        </button>
        <label class="project-switcher">
          <span class="sr-only">Active project</span>
          <span class="project-dot" :style="{ background: activeProject.accent }" />
          <select v-model="activeProjectSlug" @change="changeProject">
            <option v-for="project in projects" :key="project.slug" :value="project.slug">
              {{ project.name }}
            </option>
          </select>
          <ChevronDown :size="15" aria-hidden="true" />
        </label>
      </div>

      <label class="global-search desktop-only">
        <Search :size="16" aria-hidden="true" />
        <span class="sr-only">Search feedback in active project</span>
        <input v-model="filters.query" type="search" placeholder="Search this project" />
      </label>

      <div class="global-actions">
        <span class="trial-badge desktop-only">Pro trial · 9 days</span>
        <button class="icon-button desktop-only" type="button" title="Notifications" @click="confirm('No new notifications in this prototype.')">
          <Bell :size="18" aria-hidden="true" />
          <span class="sr-only">Notifications</span>
        </button>
        <button class="account-button desktop-only" type="button" title="Account" @click="confirm('Account actions are fixture-only.')">
          <UserRound :size="17" aria-hidden="true" />
          <span>AD</span>
        </button>
        <button class="icon-button mobile-menu-button" type="button" title="Project navigation" @click="mobileMenuOpen = !mobileMenuOpen">
          <X v-if="mobileMenuOpen" :size="20" aria-hidden="true" />
          <Menu v-else :size="20" aria-hidden="true" />
          <span class="sr-only">Toggle project navigation</span>
        </button>
      </div>
    </header>

    <nav class="project-nav" :data-open="mobileMenuOpen" aria-label="Project navigation">
      <div class="project-identity">
        <strong>{{ activeProject.name }}</strong>
        <span>{{ activeProject.domain }}</span>
      </div>
      <div class="project-links">
        <template v-for="item in navItems" :key="item.label">
          <NuxtLink
            v-if="item.view"
            :to="`/designs/project-deck/${item.view}`"
            :data-active="props.view === item.view || (props.view === 'feedback-detail' && item.view === 'feedback')"
            @click="handleNav(item)"
          >
            <component :is="item.icon" :size="16" aria-hidden="true" />
            {{ item.label }}
          </NuxtLink>
          <button v-else type="button" @click="handleNav(item)">
            <component :is="item.icon" :size="16" aria-hidden="true" />
            {{ item.label }}
          </button>
        </template>
      </div>
      <button class="project-settings" type="button" title="Project settings" @click="confirm('Project settings are fixture-only.')">
        <Settings2 :size="17" aria-hidden="true" />
        <span class="sr-only">Project settings</span>
      </button>
    </nav>

    <main class="deck-main">
      <template v-if="props.view === 'overview'">
        <section class="status-band">
          <div>
            <span class="status-line"><CheckCircle2 :size="17" /> Collecting feedback</span>
            <h1>{{ activeProject.name }}</h1>
            <p>{{ activeProject.domain }}</p>
          </div>
          <div class="status-facts">
            <div><span>Widget</span><strong>Healthy</strong></div>
            <div><span>Last event</span><strong>8 min ago</strong></div>
            <div><span>Environment</span><strong>Production</strong></div>
          </div>
          <button class="secondary-button" type="button" @click="confirm('The live widget preview is fixture-only.')">
            <ExternalLink :size="16" aria-hidden="true" />
            Open site
          </button>
        </section>

        <section class="full-band health-band">
          <div class="band-heading">
            <div>
              <span class="section-label">Feedback health</span>
              <h2>Signals from the last 30 days</h2>
            </div>
            <NuxtLink class="quiet-link" to="/designs/project-deck/feedback">
              View feedback <ArrowRight :size="15" aria-hidden="true" />
            </NuxtLink>
          </div>
          <div class="metric-strip">
            <div v-for="metric in overviewMetrics" :key="metric.label" class="metric-cell">
              <span>{{ metric.label }}</span>
              <strong class="metric-value">{{ metric.value }}</strong>
              <small :data-direction="metric.direction">{{ metric.delta }} from last period</small>
            </div>
            <div class="metric-cell attention-metric">
              <span>Needs review</span>
              <strong class="metric-value">{{ unlinkedCount }}</strong>
              <small>{{ negativeCount }} negative signals</small>
            </div>
          </div>
        </section>

        <section class="full-band activity-band">
          <div class="band-heading">
            <div>
              <span class="section-label">Recent feedback</span>
              <h2>Latest project activity</h2>
            </div>
            <span class="muted">Live collection</span>
          </div>
          <div class="activity-list">
            <button
              v-for="item in projectFeedback.slice(0, 4)"
              :key="item.id"
              type="button"
              @click="openDrawer(item)"
            >
              <RatingMark :rating="item.rating" compact />
              <span class="activity-copy">
                <strong>{{ item.excerpt }}</strong>
                <small>{{ item.id }} · {{ item.topic }}</small>
              </span>
              <AttachmentIcons :attachments="item.attachments" />
              <span class="status-pill" :class="`status-${item.status}`">{{ label(item.status) }}</span>
              <time>{{ item.age }}</time>
              <ArrowRight :size="15" aria-hidden="true" />
            </button>
          </div>
        </section>

        <section class="split-band">
          <div class="operation-section">
            <div class="band-heading">
              <div>
                <span class="section-label">Issue movement</span>
                <h2>Work linked to feedback</h2>
              </div>
              <CircleDot :size="18" aria-hidden="true" />
            </div>
            <div class="issue-row"><span>SHOP-128</span><strong>Promo total not updating</strong><em>In progress</em></div>
            <div class="issue-row"><span>SHOP-125</span><strong>Restore search scroll position</strong><em>Ready</em></div>
            <button class="quiet-link inline-action" type="button" @click="confirm('Issue management is fixture-only.')">
              Review all 12 open issues <ArrowRight :size="15" />
            </button>
          </div>
          <div class="operation-section widget-section">
            <div class="band-heading">
              <div>
                <span class="section-label">Widget collection</span>
                <h2>Production is receiving events</h2>
              </div>
              <CheckCircle2 :size="18" aria-hidden="true" />
            </div>
            <dl>
              <div><dt>Launcher</dt><dd>Bottom right</dd></div>
              <div><dt>Replay capture</dt><dd>Enabled</dd></div>
              <div><dt>Sampling</dt><dd>100%</dd></div>
            </dl>
            <button class="quiet-link inline-action" type="button" @click="confirm('Widget configuration is fixture-only.')">
              Configure widget <ArrowRight :size="15" />
            </button>
          </div>
        </section>
      </template>

      <template v-else-if="props.view === 'feedback'">
        <section class="feedback-heading">
          <div>
            <span class="section-label">{{ activeProject.name }}</span>
            <h1>Feedback</h1>
            <p>Review product signals and connect them to project work.</p>
          </div>
          <button class="primary-button" type="button" @click="confirm('Manual feedback creation is fixture-only.')">
            <Plus :size="16" aria-hidden="true" /> Add feedback
          </button>
        </section>

        <section class="filter-band" aria-label="Feedback filters">
          <label class="feedback-search">
            <Search :size="16" aria-hidden="true" />
            <span class="sr-only">Search feedback</span>
            <input v-model="filters.query" type="search" placeholder="Search feedback" />
          </label>
          <button class="secondary-button filter-toggle" type="button" :aria-expanded="filterPanelOpen" @click="filterPanelOpen = !filterPanelOpen">
            <Filter :size="16" aria-hidden="true" />
            Filters
            <span v-if="activeFilterCount" class="filter-count">{{ activeFilterCount }}</span>
          </button>
          <label class="compact-select sort-control">
            <span>Sort</span>
            <select v-model="filters.sort">
              <option value="newest">Newest</option>
              <option value="oldest">Oldest</option>
              <option value="rating-low">Rating: low</option>
              <option value="rating-high">Rating: high</option>
            </select>
            <ChevronDown :size="14" aria-hidden="true" />
          </label>

          <div v-if="filterPanelOpen" class="filter-grid">
            <label>
              Status
              <select v-model="filters.status">
                <option value="all">All statuses</option><option value="new">New</option><option value="reviewed">Reviewed</option><option value="linked">Linked</option><option value="resolved">Resolved</option>
              </select>
            </label>
            <label>
              Rating
              <select v-model="filters.rating">
                <option value="all">All ratings</option><option v-for="rating in 5" :key="rating" :value="String(rating)">{{ rating }} of 5</option>
              </select>
            </label>
            <label>
              Attachment
              <select v-model="filters.attachment">
                <option value="all">Any evidence</option><option value="screenshot">Screenshot</option><option value="audio">Audio</option><option value="video">Video</option><option value="replay">Replay</option>
              </select>
            </label>
            <label>
              Moderation
              <select v-model="filters.moderation">
                <option value="all">Any state</option><option value="flagged">Flagged</option><option value="clear">Clear</option>
              </select>
            </label>
            <label>
              Issue
              <select v-model="filters.issue">
                <option value="all">Any linkage</option><option value="linked">Linked</option><option value="unlinked">Unlinked</option>
              </select>
            </label>
            <label>
              Date
              <select v-model="filters.date">
                <option value="24h">Last 24 hours</option><option value="7d">Last 7 days</option><option value="30d">Last 30 days</option><option value="all">All time</option>
              </select>
            </label>
            <label>
              Sentiment
              <select v-model="filters.sentiment">
                <option value="all">Any sentiment</option><option value="positive">Positive</option><option value="neutral">Neutral</option><option value="negative">Negative</option>
              </select>
            </label>
            <label>
              Project
              <select v-model="activeProjectSlug" @change="changeProject">
                <option v-for="project in projects" :key="project.slug" :value="project.slug">{{ project.name }}</option>
              </select>
            </label>
            <button class="quiet-button reset-button" type="button" @click="resetFilters">Reset filters</button>
          </div>
        </section>

        <section class="feedback-surface">
          <div class="result-summary">
            <strong>{{ filteredItems.length }} feedback items</strong>
            <span class="muted">{{ activeProject.name }} · Last 30 days</span>
          </div>

          <div v-if="filteredItems.length" class="table-wrap">
            <table class="data-table deck-table">
              <thead>
                <tr>
                  <th class="rating-column">Rating</th>
                  <th>Feedback</th>
                  <th class="topic-column">Topic</th>
                  <th class="evidence-column">Evidence</th>
                  <th class="status-column">Status</th>
                  <th class="issue-column">Issue</th>
                  <th class="age-column">Received</th>
                  <th class="action-column"><span class="sr-only">Open</span></th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="item in filteredItems"
                  :key="item.id"
                  :data-selected="drawerOpen && selected.id === item.id"
                  tabindex="0"
                  @click="openDrawer(item)"
                  @keydown.enter="openDrawer(item)"
                  @keydown.space.prevent="openDrawer(item)"
                >
                  <td><RatingMark :rating="item.rating" compact /></td>
                  <td class="feedback-cell"><strong>{{ item.excerpt }}</strong><small>{{ item.id }} · {{ item.page }}</small></td>
                  <td><span class="topic-tag">{{ item.topic }}</span></td>
                  <td><AttachmentIcons :attachments="item.attachments" /></td>
                  <td><span class="status-pill" :class="`status-${item.status}`">{{ label(item.status) }}</span></td>
                  <td><span :class="item.issue ? 'issue-link' : 'muted'">{{ item.issue ?? 'Unlinked' }}</span></td>
                  <td>{{ item.age }}</td>
                  <td><ArrowRight :size="15" aria-hidden="true" /></td>
                </tr>
              </tbody>
            </table>

            <div class="mobile-feedback-list">
              <button v-for="item in filteredItems" :key="item.id" type="button" @click="openDrawer(item)">
                <span class="mobile-row-top"><RatingMark :rating="item.rating" compact /><strong>{{ item.id }}</strong><time>{{ item.age }}</time></span>
                <span class="mobile-excerpt">{{ item.excerpt }}</span>
                <span class="mobile-row-meta">
                  <span class="status-pill" :class="`status-${item.status}`">{{ label(item.status) }}</span>
                  <span>{{ item.topic }}</span>
                  <AttachmentIcons :attachments="item.attachments" />
                </span>
              </button>
            </div>
          </div>
          <div v-else class="empty-state">
            <Search :size="24" aria-hidden="true" />
            <strong>No feedback matches these filters</strong>
            <p>Adjust the filters or clear the search to see more project feedback.</p>
            <button class="secondary-button" type="button" @click="resetFilters">Reset filters</button>
          </div>
        </section>
      </template>

      <template v-else>
        <section class="detail-heading">
          <NuxtLink class="back-link" to="/designs/project-deck/feedback">
            <ArrowLeft :size="16" aria-hidden="true" /> Feedback
          </NuxtLink>
          <div class="detail-title-row">
            <div>
              <span class="section-label">{{ selected.id }}</span>
              <h1>{{ selected.topic }}</h1>
              <p>Received {{ selected.age }} from {{ selected.page }}</p>
            </div>
            <div class="detail-actions">
              <button class="secondary-button" type="button" @click="confirm('AI reprocessing queued locally.')">
                <RefreshCw :size="16" aria-hidden="true" /> Reprocess
              </button>
              <button class="primary-button" type="button" @click="confirm('Issue creation is fixture-only.')">
                <Plus :size="16" aria-hidden="true" /> Create issue
              </button>
              <button class="icon-button" type="button" title="More actions" @click="confirm('More triage actions are fixture-only.')">
                <MoreHorizontal :size="19" aria-hidden="true" /><span class="sr-only">More actions</span>
              </button>
            </div>
          </div>
        </section>

        <section class="detail-summary-band">
          <RatingMark :rating="selected.rating" />
          <div class="detail-quote">
            <blockquote>“{{ selected.cleanedText }}”</blockquote>
            <span>{{ selected.reporter }} · {{ selected.email || 'No email provided' }}</span>
          </div>
          <div class="summary-tags">
            <span class="status-pill" :class="`status-${selected.status}`">{{ label(selected.status) }}</span>
            <span class="status-pill" :class="`status-${selected.sentiment}`">{{ label(selected.sentiment) }} sentiment</span>
            <span v-if="selected.moderationFlags.length" class="status-pill status-failed"><AlertTriangle :size="12" /> Flagged</span>
          </div>
        </section>

        <section class="detail-grid">
          <div class="evidence-column-detail">
            <div class="detail-section-heading">
              <div><span class="section-label">Evidence</span><h2>Captured checkout state</h2></div>
              <AttachmentIcons :attachments="selected.attachments" />
            </div>
            <ProductScene />

            <div class="transcript-section">
              <div class="detail-section-heading">
                <div><span class="section-label">Transcript</span><h2>Reporter context</h2></div>
                <Sparkles :size="17" aria-hidden="true" />
              </div>
              <p v-if="selected.transcript">{{ selected.transcript }}</p>
              <p v-else class="muted">No audio or video transcript is attached to this feedback.</p>
            </div>

            <div class="replay-section">
              <div class="detail-section-heading">
                <div><span class="section-label">Session replay</span><h2>User journey at submission</h2></div>
                <span class="status-pill" :class="selected.replayReady ? 'status-ready' : 'status-processing'">
                  {{ selected.replayReady ? 'Ready' : 'Unavailable' }}
                </span>
              </div>
              <ProductScene v-if="selected.replayReady" replay />
              <div v-else class="replay-unavailable"><Play :size="22" /><strong>No replay available</strong><span>This submission did not include a playable session.</span></div>
            </div>
          </div>

          <aside class="detail-sidebar">
            <section>
              <span class="section-label">Triage</span>
              <h2>Project work</h2>
              <dl>
                <div><dt>Status</dt><dd>{{ label(selected.status) }}</dd></div>
                <div><dt>Issue</dt><dd>{{ selected.issue ?? 'Not linked' }}</dd></div>
                <div><dt>AI processing</dt><dd>{{ label(selected.aiStatus) }}</dd></div>
                <div><dt>Sentiment score</dt><dd>{{ selected.sentimentScore.toFixed(2) }}</dd></div>
              </dl>
              <button class="secondary-button sidebar-action" type="button" @click="confirm('Feedback marked reviewed locally.')">
                <CheckCircle2 :size="16" /> Mark reviewed
              </button>
              <button class="secondary-button sidebar-action" type="button" @click="confirm('Issue linking is fixture-only.')">
                <Link2 :size="16" /> {{ selected.issue ? 'Change issue' : 'Link issue' }}
              </button>
            </section>
            <section>
              <span class="section-label">Environment</span>
              <h2>Submission metadata</h2>
              <dl>
                <div><dt>Page</dt><dd>{{ selected.page }}</dd></div>
                <div><dt>Browser</dt><dd>{{ selected.browser }}</dd></div>
                <div><dt>Device</dt><dd>{{ selected.device }}</dd></div>
                <div><dt>Viewport</dt><dd>{{ selected.viewport }}</dd></div>
              </dl>
            </section>
            <section>
              <span class="section-label">Reporter</span>
              <h2>{{ selected.reporter }}</h2>
              <p class="muted">{{ selected.email || 'Anonymous submission' }}</p>
              <button class="quiet-link inline-action" type="button" @click="confirm('Reporter history is fixture-only.')">
                View reporter history <ArrowRight :size="15" />
              </button>
            </section>
          </aside>
        </section>
      </template>
    </main>

    <template v-if="drawerOpen && props.view !== 'feedback-detail'">
      <button class="drawer-backdrop" type="button" aria-label="Close feedback preview" @click="drawerOpen = false" />
      <aside class="feedback-drawer" role="dialog" aria-modal="true" aria-labelledby="drawer-title">
        <header>
          <div>
            <span class="section-label">{{ selected.id }}</span>
            <h2 id="drawer-title">{{ selected.topic }}</h2>
          </div>
          <div class="drawer-controls">
            <button class="icon-button" type="button" title="Previous feedback" @click="selectAdjacent(-1)"><ArrowLeft :size="18" /><span class="sr-only">Previous feedback</span></button>
            <button class="icon-button" type="button" title="Next feedback" @click="selectAdjacent(1)"><ArrowRight :size="18" /><span class="sr-only">Next feedback</span></button>
            <button class="icon-button" type="button" title="Close preview" @click="drawerOpen = false"><X :size="19" /><span class="sr-only">Close preview</span></button>
          </div>
        </header>
        <div class="drawer-body">
          <div class="drawer-rating"><RatingMark :rating="selected.rating" /><span class="status-pill" :class="`status-${selected.status}`">{{ label(selected.status) }}</span></div>
          <p class="drawer-quote">{{ selected.cleanedText }}</p>
          <ProductScene v-if="selected.attachments.includes('screenshot') || selected.replayReady" :replay="selected.replayReady" />
          <section class="drawer-metadata">
            <div><span>Reporter</span><strong>{{ selected.reporter }}</strong></div>
            <div><span>Page</span><strong>{{ selected.page }}</strong></div>
            <div><span>Environment</span><strong>{{ selected.browser }} · {{ selected.device }}</strong></div>
            <div><span>Issue</span><strong>{{ selected.issue ?? 'Not linked' }}</strong></div>
          </section>
        </div>
        <footer>
          <button class="secondary-button" type="button" @click="confirm('Feedback marked reviewed locally.')"><CheckCircle2 :size="16" /> Mark reviewed</button>
          <NuxtLink class="primary-button" to="/designs/project-deck/feedback-detail" @click="drawerOpen = false">
            Open full detail <ArrowRight :size="16" />
          </NuxtLink>
        </footer>
      </aside>
    </template>

    <div v-if="notice" class="prototype-toast" role="status">{{ notice }}</div>
  </div>
</template>

<style scoped>
.deck-shell {
  min-height: 100vh;
  background: #fff;
  color: #1e2927;
  --deck-line: #dce3e1;
  --deck-soft: #f6f8f7;
  --deck-ink-soft: #53615e;
}

.global-bar {
  position: sticky;
  z-index: 40;
  top: 0;
  display: flex;
  height: 56px;
  align-items: center;
  gap: 14px;
  padding: 0 22px;
  background: #1f2927;
  color: #f8fbfa;
  border-bottom: 1px solid #151c1a;
}

.brand,
.context-controls,
.global-actions,
.tenant-button,
.project-switcher,
.global-search,
.account-button {
  display: flex;
  align-items: center;
}

.brand {
  gap: 9px;
  padding-right: 18px;
  border-right: 1px solid #3d4845;
  font-size: 15px;
}

.brand-mark {
  display: grid;
  width: 27px;
  height: 27px;
  place-items: center;
  background: #16a394;
  color: #fff;
  border-radius: 5px;
  font-size: 14px;
  font-weight: 800;
}

.context-controls {
  gap: 5px;
}

.tenant-button,
.project-switcher,
.account-button {
  height: 36px;
  gap: 7px;
  padding: 0 9px;
  background: transparent;
  color: #f8fbfa;
  border: 1px solid transparent;
  border-radius: 5px;
  font-weight: 650;
}

.tenant-button:hover,
.project-switcher:hover,
.account-button:hover {
  background: #303b38;
  border-color: #44514d;
}

.project-switcher {
  position: relative;
  padding-right: 7px;
  background: #2a3532;
  border-color: #44514d;
}

.project-switcher select {
  width: 126px;
  appearance: none;
  background: transparent;
  color: inherit;
  border: 0;
  outline: 0;
  font-weight: 700;
}

.project-switcher option {
  color: var(--ink);
}

.project-dot {
  width: 8px;
  height: 8px;
  border-radius: 2px;
}

.global-search {
  width: min(440px, 36vw);
  height: 36px;
  gap: 8px;
  margin: 0 auto;
  padding: 0 10px;
  background: #141c1a;
  color: #aeb9b6;
  border: 1px solid #3b4844;
  border-radius: 5px;
}

.global-search input {
  width: 100%;
  background: transparent;
  color: #fff;
  border: 0;
  outline: 0;
}

.global-search input::placeholder {
  color: #9eaaa6;
}

.global-actions {
  gap: 5px;
  margin-left: auto;
}

.global-actions .icon-button {
  color: #eef4f2;
}

.global-actions .icon-button:hover {
  background: #303b38;
  border-color: #44514d;
}

.trial-badge {
  padding: 5px 9px;
  background: #203f3a;
  color: #8fe0d4;
  border: 1px solid #326159;
  border-radius: 4px;
  font-size: 12px;
  font-weight: 700;
}

.account-button span {
  font-size: 12px;
}

.mobile-menu-button {
  display: none;
}

.project-nav {
  position: sticky;
  z-index: 30;
  top: 56px;
  display: flex;
  height: 58px;
  align-items: center;
  padding: 0 30px;
  background: #fff;
  border-bottom: 1px solid var(--deck-line);
}

.project-identity {
  display: flex;
  min-width: 205px;
  flex-direction: column;
  line-height: 1.25;
}

.project-identity strong {
  font-size: 14px;
}

.project-identity span {
  color: var(--deck-ink-soft);
  font-size: 11px;
}

.project-links {
  display: flex;
  height: 100%;
  align-items: stretch;
  gap: 3px;
}

.project-links a,
.project-links button {
  position: relative;
  display: flex;
  min-width: 92px;
  align-items: center;
  justify-content: center;
  gap: 7px;
  padding: 0 13px;
  background: transparent;
  color: var(--deck-ink-soft);
  border: 0;
  font-weight: 650;
}

.project-links a:hover,
.project-links button:hover {
  background: var(--deck-soft);
  color: var(--ink);
}

.project-links a[data-active='true'] {
  color: #076f65;
}

.project-links a[data-active='true']::after {
  position: absolute;
  right: 10px;
  bottom: -1px;
  left: 10px;
  height: 3px;
  background: var(--teal);
  content: '';
}

.project-settings {
  display: grid;
  width: 40px;
  height: 40px;
  margin-left: auto;
  place-items: center;
  background: transparent;
  border: 1px solid transparent;
  border-radius: 5px;
}

.project-settings:hover {
  background: var(--deck-soft);
  border-color: var(--deck-line);
}

.deck-main {
  width: 100%;
  min-height: calc(100vh - 114px);
}

.status-band,
.full-band,
.split-band,
.feedback-heading,
.filter-band,
.feedback-surface,
.detail-heading,
.detail-summary-band,
.detail-grid {
  padding-right: max(32px, calc((100vw - 1320px) / 2));
  padding-left: max(32px, calc((100vw - 1320px) / 2));
}

.status-band {
  display: grid;
  min-height: 126px;
  grid-template-columns: minmax(220px, 1fr) auto auto;
  align-items: center;
  gap: 48px;
  background: var(--deck-soft);
  border-bottom: 1px solid var(--deck-line);
}

.status-band h1 {
  margin-top: 6px;
  font-size: 23px;
  line-height: 1.2;
}

.status-band p {
  margin-top: 3px;
  color: var(--deck-ink-soft);
}

.status-line {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: #176746;
  font-size: 12px;
  font-weight: 700;
}

.status-facts {
  display: grid;
  grid-template-columns: repeat(3, auto);
  gap: 42px;
}

.status-facts div {
  display: flex;
  flex-direction: column;
  gap: 3px;
}

.status-facts span {
  color: var(--deck-ink-soft);
  font-size: 11px;
}

.status-facts strong {
  font-size: 13px;
}

.full-band {
  padding-top: 30px;
  padding-bottom: 30px;
  border-bottom: 1px solid var(--deck-line);
}

.band-heading,
.detail-section-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 20px;
}

.band-heading h2,
.detail-section-heading h2 {
  margin-top: 4px;
  font-size: 17px;
  line-height: 1.25;
}

.quiet-link {
  display: inline-flex;
  min-height: 40px;
  align-items: center;
  gap: 6px;
  padding: 0 4px;
  background: transparent;
  color: #205db6;
  border: 0;
  font-weight: 700;
}

.metric-strip {
  display: grid;
  margin-top: 22px;
  border-top: 1px solid var(--deck-line);
  border-bottom: 1px solid var(--deck-line);
  grid-template-columns: repeat(4, 1fr);
}

.metric-cell {
  display: flex;
  min-width: 0;
  min-height: 118px;
  flex-direction: column;
  justify-content: center;
  gap: 5px;
  padding: 18px 24px;
  border-left: 1px solid var(--deck-line);
}

.metric-cell:first-child {
  border-left: 0;
}

.metric-cell > span,
.metric-cell small {
  color: var(--deck-ink-soft);
  font-size: 12px;
}

.metric-cell small[data-direction='up'] {
  color: #176746;
}

.attention-metric {
  background: #fffaf2;
}

.activity-list {
  margin-top: 18px;
  border-top: 1px solid var(--deck-line);
}

.activity-list > button {
  display: grid;
  width: 100%;
  min-height: 62px;
  grid-template-columns: 46px minmax(220px, 1fr) 100px 106px 62px 20px;
  align-items: center;
  gap: 12px;
  padding: 8px 12px;
  background: #fff;
  border: 0;
  border-bottom: 1px solid var(--deck-line);
  text-align: left;
}

.activity-list > button:hover {
  background: #f4f8f7;
}

.activity-copy {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 2px;
}

.activity-copy strong,
.activity-copy small {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.activity-copy small,
.activity-list time {
  color: var(--deck-ink-soft);
  font-size: 11px;
}

.split-band {
  display: grid;
  grid-template-columns: 1fr 1fr;
  padding-top: 0;
  padding-bottom: 0;
}

.operation-section {
  min-width: 0;
  padding: 30px 30px 32px 0;
  border-right: 1px solid var(--deck-line);
}

.operation-section + .operation-section {
  padding-right: 0;
  padding-left: 30px;
  border-right: 0;
}

.issue-row {
  display: grid;
  min-height: 54px;
  grid-template-columns: 90px 1fr auto;
  align-items: center;
  gap: 12px;
  border-bottom: 1px solid var(--deck-line);
}

.issue-row span {
  color: #205db6;
  font-size: 12px;
  font-weight: 750;
}

.issue-row em {
  color: var(--deck-ink-soft);
  font-size: 12px;
  font-style: normal;
}

.inline-action {
  margin-top: 10px;
}

.widget-section dl,
.detail-sidebar dl {
  margin: 12px 0 0;
}

.widget-section dl div,
.detail-sidebar dl div {
  display: flex;
  min-height: 39px;
  align-items: center;
  justify-content: space-between;
  gap: 18px;
  border-bottom: 1px solid var(--deck-line);
}

.widget-section dt,
.detail-sidebar dt {
  color: var(--deck-ink-soft);
  font-size: 12px;
}

.widget-section dd,
.detail-sidebar dd {
  margin: 0;
  font-size: 12px;
  font-weight: 700;
  text-align: right;
}

.feedback-heading,
.detail-heading {
  padding-top: 28px;
  padding-bottom: 22px;
  border-bottom: 1px solid var(--deck-line);
}

.feedback-heading {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 20px;
}

.feedback-heading h1,
.detail-heading h1 {
  margin-top: 4px;
  font-size: 24px;
  line-height: 1.2;
}

.feedback-heading p,
.detail-heading p {
  margin-top: 4px;
  color: var(--deck-ink-soft);
}

.filter-band {
  position: relative;
  display: flex;
  min-height: 66px;
  align-items: center;
  gap: 10px;
  background: var(--deck-soft);
  border-bottom: 1px solid var(--deck-line);
}

.feedback-search {
  display: flex;
  width: min(460px, 45vw);
  height: 40px;
  align-items: center;
  gap: 8px;
  padding: 0 11px;
  background: #fff;
  color: var(--deck-ink-soft);
  border: 1px solid #b9c5c1;
  border-radius: 5px;
}

.feedback-search input {
  width: 100%;
  background: transparent;
  border: 0;
  outline: 0;
}

.filter-toggle {
  margin-left: auto;
}

.filter-count {
  display: grid;
  min-width: 20px;
  height: 20px;
  padding: 0 5px;
  place-items: center;
  background: var(--teal);
  color: #fff;
  border-radius: 10px;
  font-size: 11px;
}

.compact-select {
  position: relative;
  display: flex;
  height: 40px;
  align-items: center;
  gap: 6px;
  padding: 0 9px;
  background: #fff;
  border: 1px solid #b9c5c1;
  border-radius: 5px;
}

.compact-select > span {
  color: var(--deck-ink-soft);
  font-size: 11px;
}

.compact-select select {
  appearance: none;
  padding-right: 18px;
  background: transparent;
  border: 0;
  outline: 0;
  font-weight: 650;
}

.compact-select svg {
  position: absolute;
  right: 7px;
  pointer-events: none;
}

.filter-grid {
  position: absolute;
  z-index: 20;
  top: 58px;
  right: max(32px, calc((100vw - 1320px) / 2));
  display: grid;
  width: min(780px, calc(100vw - 64px));
  grid-template-columns: repeat(4, 1fr);
  gap: 14px;
  padding: 18px;
  background: #fff;
  border: 1px solid var(--deck-line);
  border-radius: 6px;
  box-shadow: var(--shadow-overlay);
}

.filter-grid label {
  display: flex;
  flex-direction: column;
  gap: 5px;
  color: var(--deck-ink-soft);
  font-size: 11px;
  font-weight: 700;
}

.filter-grid select {
  width: 100%;
  height: 38px;
  padding: 0 9px;
  background: #fff;
  border: 1px solid #b9c5c1;
  border-radius: 4px;
}

.reset-button {
  align-self: end;
}

.feedback-surface {
  padding-top: 20px;
  padding-bottom: 48px;
}

.result-summary {
  display: flex;
  min-height: 38px;
  align-items: flex-start;
  justify-content: space-between;
  gap: 20px;
  font-size: 12px;
}

.table-wrap {
  border-top: 1px solid #b9c5c1;
  border-bottom: 1px solid #b9c5c1;
}

.deck-table th:nth-child(1), .deck-table td:nth-child(1) { width: 74px; }
.deck-table th:nth-child(2), .deck-table td:nth-child(2) { width: auto; }
.deck-table th:nth-child(3), .deck-table td:nth-child(3) { width: 150px; }
.deck-table th:nth-child(4), .deck-table td:nth-child(4) { width: 106px; }
.deck-table th:nth-child(5), .deck-table td:nth-child(5) { width: 108px; }
.deck-table th:nth-child(6), .deck-table td:nth-child(6) { width: 90px; }
.deck-table th:nth-child(7), .deck-table td:nth-child(7) { width: 78px; }
.deck-table th:nth-child(8), .deck-table td:nth-child(8) { width: 38px; }

.deck-table td {
  height: 64px;
}

.feedback-cell strong,
.feedback-cell small {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.feedback-cell small {
  margin-top: 3px;
  color: var(--deck-ink-soft);
  font-size: 11px;
}

.topic-tag,
.issue-link {
  color: #205db6;
  font-size: 12px;
  font-weight: 650;
}

.mobile-feedback-list {
  display: none;
}

.empty-state {
  display: flex;
  min-height: 330px;
  align-items: center;
  justify-content: center;
  flex-direction: column;
  gap: 8px;
  color: var(--deck-ink-soft);
  text-align: center;
}

.empty-state p {
  max-width: 360px;
  margin-bottom: 8px;
}

.back-link {
  display: inline-flex;
  min-height: 36px;
  align-items: center;
  gap: 6px;
  color: #205db6;
  font-weight: 700;
}

.detail-title-row {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 22px;
  margin-top: 10px;
}

.detail-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.detail-summary-band {
  display: flex;
  min-height: 112px;
  align-items: center;
  gap: 18px;
  background: var(--deck-soft);
  border-bottom: 1px solid var(--deck-line);
}

.detail-quote {
  min-width: 0;
  flex: 1;
}

.detail-quote blockquote {
  max-width: 810px;
  margin: 0;
  font-size: 16px;
  font-weight: 620;
  line-height: 1.45;
}

.detail-quote span {
  display: block;
  margin-top: 5px;
  color: var(--deck-ink-soft);
  font-size: 12px;
}

.summary-tags {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 6px;
}

.detail-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 320px;
  align-items: start;
  padding-top: 30px;
  padding-bottom: 60px;
}

.evidence-column-detail {
  min-width: 0;
  padding-right: 32px;
}

.detail-section-heading {
  margin-bottom: 14px;
}

.transcript-section,
.replay-section {
  margin-top: 32px;
  padding-top: 28px;
  border-top: 1px solid var(--deck-line);
}

.transcript-section > p {
  max-width: 780px;
  padding-left: 14px;
  border-left: 3px solid #9fddd4;
  line-height: 1.65;
}

.replay-unavailable {
  display: flex;
  min-height: 220px;
  align-items: center;
  justify-content: center;
  flex-direction: column;
  gap: 7px;
  background: var(--deck-soft);
  color: var(--deck-ink-soft);
  border: 1px solid var(--deck-line);
}

.detail-sidebar {
  border-left: 1px solid var(--deck-line);
}

.detail-sidebar section {
  padding: 0 0 26px 28px;
  margin-bottom: 26px;
  border-bottom: 1px solid var(--deck-line);
}

.detail-sidebar h2 {
  margin-top: 4px;
  font-size: 16px;
}

.sidebar-action {
  width: 100%;
  margin-top: 10px;
}

.drawer-backdrop {
  position: fixed;
  z-index: 60;
  inset: 0;
  width: 100%;
  height: 100%;
  padding: 0;
  background: rgb(16 25 23 / 26%);
  border: 0;
}

.feedback-drawer {
  position: fixed;
  z-index: 70;
  top: 0;
  right: 0;
  display: grid;
  width: min(520px, 100vw);
  height: 100vh;
  grid-template-rows: auto minmax(0, 1fr) auto;
  background: #fff;
  box-shadow: -18px 0 40px rgb(20 38 33 / 16%);
}

.feedback-drawer > header,
.feedback-drawer > footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 17px 20px;
  border-bottom: 1px solid var(--deck-line);
}

.feedback-drawer > header h2 {
  margin-top: 3px;
  font-size: 18px;
}

.feedback-drawer > footer {
  justify-content: flex-end;
  border-top: 1px solid var(--deck-line);
  border-bottom: 0;
}

.drawer-controls,
.drawer-rating {
  display: flex;
  align-items: center;
  gap: 5px;
}

.drawer-body {
  overflow-y: auto;
  padding: 22px 20px;
}

.drawer-quote {
  margin: 16px 0 20px;
  font-size: 16px;
  font-weight: 620;
  line-height: 1.55;
}

.drawer-metadata {
  margin-top: 22px;
  border-top: 1px solid var(--deck-line);
}

.drawer-metadata div {
  display: flex;
  min-height: 46px;
  align-items: center;
  justify-content: space-between;
  gap: 18px;
  border-bottom: 1px solid var(--deck-line);
}

.drawer-metadata span {
  color: var(--deck-ink-soft);
  font-size: 12px;
}

.drawer-metadata strong {
  max-width: 66%;
  font-size: 12px;
  text-align: right;
}

@media (max-width: 980px) {
  .project-nav {
    padding-right: 18px;
    padding-left: 18px;
  }

  .project-identity {
    min-width: 168px;
  }

  .project-links a,
  .project-links button {
    min-width: 70px;
    padding: 0 8px;
    font-size: 12px;
  }

  .status-facts {
    display: none;
  }

  .status-band {
    grid-template-columns: 1fr auto;
  }

  .activity-list > button {
    grid-template-columns: 46px minmax(180px, 1fr) 95px 62px 20px;
  }

  .activity-list .status-pill {
    display: none;
  }

  .topic-column,
  .deck-table th:nth-child(3),
  .deck-table td:nth-child(3),
  .issue-column,
  .deck-table th:nth-child(6),
  .deck-table td:nth-child(6) {
    display: none;
  }
}

@media (max-width: 720px) {
  .global-bar {
    padding: 0 12px;
  }

  .brand {
    padding-right: 10px;
  }

  .brand strong {
    display: none;
  }

  .project-switcher select {
    width: 112px;
  }

  .mobile-menu-button {
    display: inline-grid;
  }

  .project-nav {
    position: fixed;
    z-index: 35;
    top: 56px;
    right: 0;
    left: 0;
    display: none;
    height: auto;
    align-items: stretch;
    padding: 14px;
    background: #fff;
    box-shadow: 0 16px 30px rgb(20 38 33 / 14%);
  }

  .project-nav[data-open='true'] {
    display: block;
  }

  .project-identity {
    min-height: 50px;
    padding: 0 8px 12px;
    border-bottom: 1px solid var(--deck-line);
  }

  .project-links {
    display: grid;
    height: auto;
    grid-template-columns: repeat(2, 1fr);
    gap: 4px;
    padding-top: 8px;
  }

  .project-links a,
  .project-links button {
    min-height: 44px;
    justify-content: flex-start;
    padding: 0 11px;
  }

  .project-links a[data-active='true'] {
    background: var(--teal-soft);
    border-radius: 4px;
  }

  .project-links a[data-active='true']::after,
  .project-settings {
    display: none;
  }

  .deck-main {
    min-height: calc(100vh - 56px);
  }

  .status-band,
  .full-band,
  .split-band,
  .feedback-heading,
  .filter-band,
  .feedback-surface,
  .detail-heading,
  .detail-summary-band,
  .detail-grid {
    padding-right: 16px;
    padding-left: 16px;
  }

  .status-band {
    min-height: 140px;
    grid-template-columns: 1fr auto;
    gap: 12px;
  }

  .status-band .secondary-button {
    width: 40px;
    padding: 0;
    font-size: 0;
  }

  .metric-strip {
    grid-template-columns: 1fr 1fr;
  }

  .metric-cell {
    min-height: 104px;
    padding: 15px 12px;
    border-bottom: 1px solid var(--deck-line);
  }

  .metric-cell:nth-child(odd) {
    border-left: 0;
  }

  .activity-list > button {
    grid-template-columns: 42px minmax(0, 1fr) 42px;
    gap: 8px;
  }

  .activity-list :deep(.attachment-icons),
  .activity-list .status-pill,
  .activity-list time {
    display: none;
  }

  .split-band {
    grid-template-columns: 1fr;
  }

  .operation-section,
  .operation-section + .operation-section {
    padding: 26px 0;
    border-right: 0;
    border-bottom: 1px solid var(--deck-line);
  }

  .issue-row {
    grid-template-columns: 78px 1fr;
  }

  .issue-row em {
    display: none;
  }

  .feedback-heading {
    align-items: flex-start;
  }

  .feedback-heading .primary-button {
    width: 40px;
    padding: 0;
    font-size: 0;
  }

  .filter-band {
    flex-wrap: wrap;
    padding-top: 12px;
    padding-bottom: 12px;
  }

  .feedback-search {
    order: 0;
    width: 100%;
  }

  .filter-toggle {
    margin-left: 0;
  }

  .sort-control {
    margin-left: auto;
  }

  .filter-grid {
    position: static;
    width: 100%;
    grid-template-columns: 1fr 1fr;
    padding: 14px 0 2px;
    background: transparent;
    border: 0;
    border-top: 1px solid var(--deck-line);
    border-radius: 0;
    box-shadow: none;
  }

  .feedback-surface {
    padding-top: 16px;
  }

  .result-summary {
    min-height: 50px;
    flex-direction: column;
    gap: 2px;
  }

  .deck-table {
    display: none;
  }

  .mobile-feedback-list {
    display: block;
  }

  .mobile-feedback-list > button {
    display: flex;
    width: 100%;
    min-height: 154px;
    flex-direction: column;
    gap: 10px;
    padding: 14px 4px;
    background: #fff;
    border: 0;
    border-bottom: 1px solid var(--deck-line);
    text-align: left;
  }

  .mobile-row-top,
  .mobile-row-meta {
    display: flex;
    width: 100%;
    align-items: center;
    gap: 8px;
  }

  .mobile-row-top time {
    margin-left: auto;
    color: var(--deck-ink-soft);
    font-size: 11px;
  }

  .mobile-excerpt {
    font-weight: 650;
    line-height: 1.45;
  }

  .mobile-row-meta {
    flex-wrap: wrap;
    color: var(--deck-ink-soft);
    font-size: 11px;
  }

  .mobile-row-meta :deep(.attachment-icons) {
    margin-left: auto;
  }

  .detail-title-row,
  .detail-summary-band {
    align-items: flex-start;
    flex-direction: column;
  }

  .detail-actions {
    width: 100%;
  }

  .detail-actions .secondary-button,
  .detail-actions .primary-button {
    flex: 1;
  }

  .detail-summary-band {
    gap: 12px;
    padding-top: 18px;
    padding-bottom: 18px;
  }

  .summary-tags {
    justify-content: flex-start;
  }

  .detail-grid {
    grid-template-columns: 1fr;
    padding-top: 24px;
    padding-bottom: 40px;
  }

  .evidence-column-detail {
    padding-right: 0;
  }

  .detail-sidebar {
    margin-top: 30px;
    border-top: 1px solid var(--deck-line);
    border-left: 0;
  }

  .detail-sidebar section {
    padding: 25px 0;
    margin: 0;
  }

  .feedback-drawer {
    width: 100vw;
  }

  .feedback-drawer > header,
  .feedback-drawer > footer {
    padding: 13px 14px;
  }

  .feedback-drawer > footer {
    align-items: stretch;
    flex-direction: column-reverse;
  }

  .feedback-drawer > footer > * {
    width: 100%;
  }

  .drawer-body {
    padding: 18px 14px;
  }
}

@media (prefers-reduced-motion: no-preference) {
  .feedback-drawer {
    animation: deck-drawer-in 160ms ease-out;
  }
}

@keyframes deck-drawer-in {
  from { transform: translateX(18px); opacity: 0; }
}
</style>
