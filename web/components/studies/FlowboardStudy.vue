<script setup lang="ts">
import {
  AlertTriangle,
  ArrowRight,
  Bell,
  Check,
  ChevronDown,
  CircleCheck,
  CircleDot,
  ExternalLink,
  Flag,
  Gauge,
  Inbox,
  LayoutGrid,
  Link2,
  List,
  Menu,
  MessageSquareText,
  MonitorPlay,
  MoreHorizontal,
  PanelRightOpen,
  Play,
  Plus,
  Search,
  Settings,
  SlidersHorizontal,
  Sparkles,
  UserRound,
  X,
} from '@lucide/vue'
import { feedbackItems, overviewMetrics, projects } from '~/data/design-fixtures'
import type { FeedbackItem, FeedbackStatus, ViewSlug } from '~/types/design'
import { filterFeedback } from '~/utils/design'

const props = defineProps<{ view: ViewSlug }>()
const route = useRoute()
const router = useRouter()
const { filters, selected, selectedId, selectedIds, notice, select, toggleSelected, confirm } =
  usePrototypeState()

const feedbackMode = ref<'board' | 'table'>('board')
const filtersOpen = ref(false)
const mobileNavOpen = ref(false)
const sheetOpen = ref(false)
const statusOverrides = reactive<Record<string, FeedbackStatus>>({})

const lanes: Array<{ status: FeedbackStatus; label: string; detail: string }> = [
  { status: 'new', label: 'New', detail: 'Needs first review' },
  { status: 'reviewed', label: 'Reviewed', detail: 'Ready for a decision' },
  { status: 'linked', label: 'Linked', detail: 'Connected to product work' },
  { status: 'resolved', label: 'Resolved', detail: 'Closed with an outcome' },
]

const statusLabels: Record<FeedbackStatus, string> = {
  new: 'New',
  reviewed: 'Reviewed',
  linked: 'Linked',
  resolved: 'Resolved',
  spam: 'Spam',
}

const navItems = [
  { label: 'Overview', icon: Gauge, view: 'overview' },
  { label: 'Feedback', icon: MessageSquareText, view: 'feedback' },
  { label: 'Issues', icon: CircleDot, view: '' },
  { label: 'Settings', icon: Settings, view: '' },
]

const workingItems = computed(() => {
  const items = filterFeedback(feedbackItems.map((item) => ({
    ...item,
    status: statusOverrides[item.id] ?? item.status,
  })), filters)

  if (filters.date === '24h') {
    return items.filter((item) => item.receivedAt >= '2026-07-20T12:42:00Z')
  }
  return items
})

const selectedItem = computed<FeedbackItem>(() => ({
  ...selected.value,
  status: statusOverrides[selected.value.id] ?? selected.value.status,
}))

const selectedCount = computed(() => selectedIds.value.length)

function laneItems(status: FeedbackStatus) {
  return workingItems.value.filter((item) => item.status === status)
}

function openEvidence(item: FeedbackItem) {
  select(item.id)
  sheetOpen.value = true
}

function openDetail(item: FeedbackItem) {
  select(item.id)
  sheetOpen.value = false
  void router.push({ path: '/designs/flowboard/feedback-detail', query: { feedback: item.id } })
}

function moveItem(item: FeedbackItem, status: FeedbackStatus) {
  statusOverrides[item.id] = status
  confirm(`${item.id} moved to ${statusLabels[status]}. Prototype changes are temporary.`)
}

function moveSelected(status: FeedbackStatus) {
  for (const id of selectedIds.value) statusOverrides[id] = status
  confirm(`${selectedIds.value.length} items moved to ${statusLabels[status]}. Prototype changes are temporary.`)
  selectedIds.value = []
}

function clearFilters() {
  Object.assign(filters, {
    project: 'all',
    status: 'all',
    rating: 'all',
    attachment: 'all',
    moderation: 'all',
    issue: 'all',
    date: '30d',
    sentiment: 'all',
    sort: 'newest',
    query: '',
  })
}

const requestedFeedback = Array.isArray(route.query.feedback) ? route.query.feedback[0] : route.query.feedback
if (requestedFeedback && feedbackItems.some((item) => item.id === requestedFeedback)) select(requestedFeedback)
</script>

<template>
  <div class="flowboard-shell">
    <aside class="flow-rail" :data-open="mobileNavOpen">
      <div class="flow-brand" aria-label="Fidibeki">
        <span>F</span>
        <strong>Fidibeki</strong>
        <button class="rail-mobile-close" type="button" title="Close navigation" @click="mobileNavOpen = false">
          <X :size="19" aria-hidden="true" />
        </button>
      </div>
      <nav aria-label="Product navigation">
        <button
          v-for="item in navItems"
          :key="item.label"
          type="button"
          :data-active="item.view === view || (item.view === 'feedback' && view === 'feedback-detail')"
          @click="item.view ? router.push(`/designs/flowboard/${item.view}`) : confirm(`${item.label} is outside this study.`)"
        >
          <component :is="item.icon" :size="18" aria-hidden="true" />
          <span>{{ item.label }}</span>
        </button>
      </nav>
      <div class="rail-account">
        <button type="button" @click="confirm('Workspace switching is disabled in this prototype.')">
          <span class="avatar">AS</span>
          <span>Acme Studio</span>
          <ChevronDown :size="14" aria-hidden="true" />
        </button>
      </div>
    </aside>

    <section class="flow-workspace">
      <header class="flow-toolbar">
        <button
          class="icon-button mobile-menu"
          type="button"
          :aria-expanded="mobileNavOpen"
          title="Open navigation"
          @click="mobileNavOpen = !mobileNavOpen"
        >
          <Menu :size="20" aria-hidden="true" />
        </button>
        <button class="project-select" type="button" @click="confirm('Project scope is fixed for this study.')">
          <span class="project-mark">S</span>
          <span><small>Project</small><strong>All projects</strong></span>
          <ChevronDown :size="14" aria-hidden="true" />
        </button>
        <label class="global-search">
          <Search :size="16" aria-hidden="true" />
          <span class="sr-only">Search feedback</span>
          <input v-model="filters.query" type="search" placeholder="Search feedback" />
        </label>
        <span class="trial-badge">Pro trial · 9 days</span>
        <button class="icon-button" type="button" title="Notifications" @click="confirm('You have no new notifications.')">
          <Bell :size="18" aria-hidden="true" />
        </button>
        <button class="user-button" type="button" title="Account" @click="confirm('Account controls are outside this study.')">
          <UserRound :size="17" aria-hidden="true" />
        </button>
      </header>

      <main class="flow-main">
        <template v-if="props.view !== 'feedback-detail'">
          <div class="page-heading flow-heading">
            <div>
              <p class="flow-breadcrumb">Acme Studio <span>/</span> Feedback workflow</p>
              <h1>{{ props.view === 'overview' ? 'Product feedback' : 'Feedback queue' }}</h1>
              <p>{{ workingItems.length }} reports across 3 active projects</p>
            </div>
            <div class="heading-actions">
              <div v-if="props.view === 'feedback'" class="view-toggle" aria-label="Feedback layout">
                <button type="button" :data-active="feedbackMode === 'board'" title="Board view" @click="feedbackMode = 'board'">
                  <LayoutGrid :size="16" aria-hidden="true" />
                  <span>Board</span>
                </button>
                <button type="button" :data-active="feedbackMode === 'table'" title="Table view" @click="feedbackMode = 'table'">
                  <List :size="16" aria-hidden="true" />
                  <span>Table</span>
                </button>
              </div>
              <button class="secondary-button" type="button" :aria-expanded="filtersOpen" @click="filtersOpen = !filtersOpen">
                <SlidersHorizontal :size="16" aria-hidden="true" />
                Filters
              </button>
            </div>
          </div>

          <section v-if="props.view === 'overview'" class="metric-strip" aria-label="Feedback summary">
            <div v-for="metric in overviewMetrics" :key="metric.label">
              <span>{{ metric.label }}</span>
              <strong>{{ metric.value }}</strong>
              <small :class="metric.direction === 'up' ? 'positive' : 'neutral'">{{ metric.delta }}</small>
            </div>
            <div class="metric-attention">
              <span>Needs attention</span>
              <strong>3</strong>
              <small>1 moderated · 2 unlinked</small>
            </div>
          </section>

          <section v-if="filtersOpen" class="flow-filters" aria-label="Feedback filters">
            <label class="filter-query">
              Search
              <input v-model="filters.query" class="field" type="search" placeholder="ID, topic, project, or text" />
            </label>
            <label>
              Project
              <select v-model="filters.project" class="select-field">
                <option value="all">All projects</option>
                <option v-for="project in projects" :key="project.slug" :value="project.slug">{{ project.name }}</option>
              </select>
            </label>
            <label>
              Status
              <select v-model="filters.status" class="select-field">
                <option value="all">Any status</option>
                <option v-for="lane in lanes" :key="lane.status" :value="lane.status">{{ lane.label }}</option>
              </select>
            </label>
            <label>
              Rating
              <select v-model="filters.rating" class="select-field">
                <option value="all">Any rating</option>
                <option v-for="rating in 5" :key="rating" :value="String(rating)">{{ rating }} stars</option>
              </select>
            </label>
            <label>
              Evidence
              <select v-model="filters.attachment" class="select-field">
                <option value="all">Any evidence</option>
                <option value="screenshot">Screenshot</option>
                <option value="audio">Audio</option>
                <option value="video">Video</option>
                <option value="replay">Replay</option>
              </select>
            </label>
            <label>
              Moderation
              <select v-model="filters.moderation" class="select-field">
                <option value="all">Any state</option>
                <option value="clear">Clear</option>
                <option value="flagged">Flagged</option>
              </select>
            </label>
            <label>
              Issue
              <select v-model="filters.issue" class="select-field">
                <option value="all">Any relation</option>
                <option value="linked">Linked</option>
                <option value="unlinked">Unlinked</option>
              </select>
            </label>
            <label>
              Received
              <select v-model="filters.date" class="select-field">
                <option value="24h">Last 24 hours</option>
                <option value="7d">Last 7 days</option>
                <option value="30d">Last 30 days</option>
              </select>
            </label>
            <label>
              Sentiment
              <select v-model="filters.sentiment" class="select-field">
                <option value="all">Any sentiment</option>
                <option value="positive">Positive</option>
                <option value="neutral">Neutral</option>
                <option value="negative">Negative</option>
              </select>
            </label>
            <label>
              Sort
              <select v-model="filters.sort" class="select-field">
                <option value="newest">Newest first</option>
                <option value="oldest">Oldest first</option>
                <option value="rating-low">Lowest rating</option>
                <option value="rating-high">Highest rating</option>
              </select>
            </label>
            <button class="quiet-button reset-filters" type="button" @click="clearFilters">
              <X :size="15" aria-hidden="true" /> Clear all
            </button>
          </section>

          <section v-if="feedbackMode === 'board' || props.view === 'overview'" class="workflow-section">
            <div class="workflow-heading">
              <div>
                <span class="section-label">Lifecycle</span>
                <h2>From signal to shipped work</h2>
              </div>
              <span class="muted">{{ workingItems.length }} visible</span>
            </div>
            <div v-if="workingItems.length" class="flow-board">
              <section v-for="lane in lanes" :key="lane.status" class="flow-lane" :data-status="lane.status">
                <header>
                  <span class="lane-state"><i /> {{ lane.label }}</span>
                  <strong>{{ laneItems(lane.status).length }}</strong>
                  <p>{{ lane.detail }}</p>
                </header>
                <div class="lane-items">
                  <article
                    v-for="item in laneItems(lane.status)"
                    :key="item.id"
                    class="flow-card"
                    :data-selected="selectedId === item.id"
                    tabindex="0"
                    @click="openEvidence(item)"
                    @keydown.enter="openEvidence(item)"
                  >
                    <div class="card-topline">
                      <label class="select-box" @click.stop>
                        <input
                          type="checkbox"
                          :checked="selectedIds.includes(item.id)"
                          :aria-label="`Select ${item.id}`"
                          @change="toggleSelected(item.id)"
                        />
                      </label>
                      <RatingMark :rating="item.rating" compact />
                      <span class="card-id">{{ item.id }}</span>
                      <span class="card-age">{{ item.age }}</span>
                    </div>
                    <p>{{ item.excerpt }}</p>
                    <div class="card-meta">
                      <span><i :style="{ background: projects.find((project) => project.slug === item.projectSlug)?.accent }" />{{ item.project }}</span>
                      <AttachmentIcons :attachments="item.attachments" />
                    </div>
                    <div class="card-footer">
                      <span v-if="item.issue" class="issue-link"><Link2 :size="13" /> {{ item.issue }}</span>
                      <span v-else class="muted">No issue</span>
                      <span v-if="item.moderationFlags.length" class="moderation-mark" title="Moderation flag">
                        <Flag :size="13" /> Flagged
                      </span>
                      <span v-else-if="item.aiStatus === 'failed'" class="processing-failed"><AlertTriangle :size="13" /> AI failed</span>
                      <span v-else class="topic">{{ item.topic }}</span>
                    </div>
                  </article>
                  <div v-if="laneItems(lane.status).length === 0" class="lane-empty">
                    <Check :size="16" aria-hidden="true" />
                    <span>No matching feedback</span>
                  </div>
                </div>
              </section>
            </div>
            <div v-else class="empty-feedback">
              <Inbox :size="24" aria-hidden="true" />
              <strong>No feedback matches these filters</strong>
              <button class="quiet-button" type="button" @click="clearFilters">Clear filters</button>
            </div>
          </section>

          <section v-else class="table-section" aria-label="Feedback table">
            <div class="table-heading">
              <div><span class="section-label">All feedback</span><h2>Queue</h2></div>
              <span class="muted">{{ workingItems.length }} results</span>
            </div>
            <table class="flow-table">
              <thead>
                <tr><th><span class="sr-only">Select</span></th><th>Feedback</th><th>Project</th><th>Status</th><th>Evidence</th><th>Issue</th><th>Received</th></tr>
              </thead>
              <tbody>
                <tr v-for="item in workingItems" :key="item.id" :data-selected="selectedId === item.id" @click="openEvidence(item)">
                  <td @click.stop><input type="checkbox" :checked="selectedIds.includes(item.id)" :aria-label="`Select ${item.id}`" @change="toggleSelected(item.id)" /></td>
                  <td><div class="table-feedback"><RatingMark :rating="item.rating" compact /><span><strong>{{ item.excerpt }}</strong><small>{{ item.id }} · {{ item.topic }}</small></span></div></td>
                  <td>{{ item.project }}</td>
                  <td><span class="status-pill" :class="`status-${item.status}`">{{ statusLabels[item.status] }}</span></td>
                  <td><AttachmentIcons :attachments="item.attachments" /></td>
                  <td><span v-if="item.issue" class="issue-link"><Link2 :size="13" /> {{ item.issue }}</span><span v-else class="muted">Unlinked</span></td>
                  <td>{{ item.age }}</td>
                </tr>
              </tbody>
            </table>
            <div class="mobile-feedback-list">
              <article v-for="item in workingItems" :key="item.id" @click="openEvidence(item)">
                <RatingMark :rating="item.rating" compact />
                <div><strong>{{ item.excerpt }}</strong><span>{{ item.project }} · {{ item.age }}</span></div>
                <PanelRightOpen :size="17" aria-hidden="true" />
              </article>
            </div>
          </section>
        </template>

        <template v-else>
          <div class="detail-heading">
            <div>
              <button class="back-link" type="button" @click="router.push('/designs/flowboard/feedback')">
                <ArrowRight :size="15" aria-hidden="true" /> Feedback queue
              </button>
              <div class="detail-title-row">
                <RatingMark :rating="selectedItem.rating" />
                <div><h1>{{ selectedItem.id }}</h1><p>{{ selectedItem.project }} · {{ selectedItem.age }}</p></div>
              </div>
            </div>
            <button class="icon-button" type="button" title="More actions" @click="confirm('Additional actions are outside this study.')"><MoreHorizontal :size="20" /></button>
          </div>
          <div class="detail-layout">
            <div class="evidence-canvas">
              <section class="feedback-statement">
                <span class="section-label">Cleaned feedback</span>
                <h2>{{ selectedItem.cleanedText }}</h2>
                <div class="statement-meta">
                  <span><UserRound :size="14" /> {{ selectedItem.reporter }}</span>
                  <span>{{ selectedItem.browser }} · {{ selectedItem.device }}</span>
                  <span>{{ selectedItem.page }}</span>
                </div>
              </section>
              <section class="evidence-block">
                <div class="evidence-heading"><div><span class="section-label">Screenshot</span><h2>Checkout evidence</h2></div><button class="secondary-button" type="button" @click="confirm('Original evidence is fixture-only.')"><ExternalLink :size="15" /> Original</button></div>
                <ProductScene />
              </section>
              <section class="evidence-block">
                <div class="evidence-heading"><div><span class="section-label">Session replay</span><h2>Interaction before submission</h2></div><span :class="selectedItem.replayReady ? 'ready-label' : 'muted'"><MonitorPlay :size="15" /> {{ selectedItem.replayReady ? 'Replay ready' : 'No replay available' }}</span></div>
                <ProductScene v-if="selectedItem.replayReady" replay />
                <div v-else class="replay-empty"><MonitorPlay :size="24" /><strong>No replay was captured for this report</strong></div>
              </section>
              <section v-if="selectedItem.transcript" class="transcript-block">
                <span class="section-label">Transcript</span>
                <p>“{{ selectedItem.transcript }}”</p>
              </section>
            </div>
            <aside class="lifecycle-panel">
              <div>
                <span class="section-label">Lifecycle</span>
                <h2>{{ statusLabels[selectedItem.status] }}</h2>
              </div>
              <ol class="lifecycle-steps">
                <li v-for="(lane, index) in lanes" :key="lane.status" :data-current="selectedItem.status === lane.status" :data-complete="lanes.findIndex((entry) => entry.status === selectedItem.status) > index">
                  <span><Check v-if="lanes.findIndex((entry) => entry.status === selectedItem.status) > index" :size="13" /><i v-else /></span>
                  <div><strong>{{ lane.label }}</strong><small>{{ lane.detail }}</small></div>
                </li>
              </ol>
              <label class="move-field">Move to<select :value="selectedItem.status" class="select-field" @change="moveItem(selectedItem, ($event.target as HTMLSelectElement).value as FeedbackStatus)"><option v-for="lane in lanes" :key="lane.status" :value="lane.status">{{ lane.label }}</option></select></label>
              <div class="issue-section">
                <span class="section-label">Product work</span>
                <div v-if="selectedItem.issue" class="linked-issue"><span><Link2 :size="16" /> Linked issue</span><strong>{{ selectedItem.issue }}</strong><small>In progress · Storefront</small></div>
                <button v-else class="primary-button" type="button" @click="confirm('Issue creation is fixture-only.')"><Plus :size="16" /> Create issue</button>
                <button class="secondary-button" type="button" @click="confirm('Issue linking is fixture-only.')"><Link2 :size="15" /> {{ selectedItem.issue ? 'Change link' : 'Link existing' }}</button>
              </div>
              <dl class="detail-facts">
                <div><dt>Sentiment</dt><dd :class="`sentiment-${selectedItem.sentiment}`">{{ selectedItem.sentiment }}</dd></div>
                <div><dt>AI analysis</dt><dd><Sparkles :size="13" /> {{ selectedItem.aiStatus }}</dd></div>
                <div><dt>Viewport</dt><dd>{{ selectedItem.viewport }}</dd></div>
                <div><dt>Email</dt><dd>{{ selectedItem.email || 'Not provided' }}</dd></div>
              </dl>
              <div class="detail-actions">
                <button class="secondary-button" type="button" @click="confirm('Email drafting is outside this study.')">Reply</button>
                <button class="primary-button" type="button" @click="moveItem(selectedItem, 'resolved')"><CircleCheck :size="16" /> Resolve</button>
              </div>
            </aside>
          </div>
        </template>
      </main>
    </section>

    <div v-if="sheetOpen && props.view !== 'feedback-detail'" class="sheet-scrim" @click="sheetOpen = false" />
    <aside v-if="sheetOpen && props.view !== 'feedback-detail'" class="evidence-sheet" aria-label="Selected feedback evidence">
      <header>
        <div><span class="section-label">Selected feedback</span><h2>{{ selectedItem.id }}</h2></div>
        <button class="icon-button" type="button" title="Close evidence" @click="sheetOpen = false"><X :size="19" /></button>
      </header>
      <div class="sheet-body">
        <div class="sheet-rating"><RatingMark :rating="selectedItem.rating" /><span class="status-pill" :class="`status-${selectedItem.status}`">{{ statusLabels[selectedItem.status] }}</span><span class="muted">{{ selectedItem.age }}</span></div>
        <p class="sheet-excerpt">{{ selectedItem.cleanedText }}</p>
        <div class="sheet-source"><span><strong>{{ selectedItem.project }}</strong>{{ selectedItem.page }}</span><span>{{ selectedItem.reporter }}</span></div>
        <ProductScene v-if="selectedItem.attachments.includes('screenshot') || selectedItem.replayReady" :replay="selectedItem.replayReady" />
        <div v-else class="sheet-no-preview"><Inbox :size="22" /><span>No visual evidence attached</span></div>
        <div class="sheet-signals">
          <div><span>Sentiment</span><strong :class="`sentiment-${selectedItem.sentiment}`">{{ selectedItem.sentiment }}</strong></div>
          <div><span>Topic</span><strong>{{ selectedItem.topic }}</strong></div>
          <div><span>Issue</span><strong>{{ selectedItem.issue || 'Unlinked' }}</strong></div>
        </div>
      </div>
      <footer>
        <label>Move to<select :value="selectedItem.status" class="select-field" @change="moveItem(selectedItem, ($event.target as HTMLSelectElement).value as FeedbackStatus)"><option v-for="lane in lanes" :key="lane.status" :value="lane.status">{{ lane.label }}</option></select></label>
        <button class="primary-button" type="button" @click="openDetail(selectedItem)">Open detail <ArrowRight :size="15" /></button>
      </footer>
    </aside>

    <div v-if="selectedCount" class="bulk-bar">
      <strong>{{ selectedCount }} selected</strong>
      <span class="bulk-divider" />
      <button type="button" @click="moveSelected('reviewed')"><Check :size="15" /> Mark reviewed</button>
      <button type="button" @click="confirm('Bulk issue linking is fixture-only.')"><Link2 :size="15" /> Link issue</button>
      <button type="button" @click="moveSelected('resolved')"><CircleCheck :size="15" /> Resolve</button>
      <button class="bulk-close" type="button" title="Clear selection" @click="selectedIds = []"><X :size="17" /></button>
    </div>

    <div v-if="notice" class="prototype-toast" role="status">{{ notice }}</div>
  </div>
</template>

<style scoped>
.flowboard-shell { --flow-bg: #f8f7f4; display: grid; min-height: calc(100vh - 58px); grid-template-columns: 94px minmax(0, 1fr); background: var(--flow-bg); color: #18211f; }
.flow-rail { position: sticky; z-index: 20; top: 0; display: flex; height: 100vh; min-height: 640px; flex-direction: column; background: #202927; color: #f4f7f6; border-right: 1px solid #303b38; }
.flow-brand { display: grid; height: 76px; place-items: center; border-bottom: 1px solid #35403d; }
.flow-brand > span { display: grid; width: 32px; height: 32px; place-items: center; background: #44b8aa; color: #10201d; border-radius: 6px; font-weight: 850; }
.flow-brand strong { display: none; }
.rail-mobile-close { display: none; }
.flow-rail nav { display: flex; flex-direction: column; gap: 4px; padding: 16px 9px; }
.flow-rail nav button { display: flex; min-height: 56px; align-items: center; justify-content: center; flex-direction: column; gap: 5px; padding: 5px; background: transparent; color: #aebbb8; border: 1px solid transparent; border-radius: 6px; font-size: 10px; font-weight: 650; }
.flow-rail nav button:hover, .flow-rail nav button[data-active='true'] { background: #303c39; color: #fff; border-color: #43504d; }
.flow-rail nav button[data-active='true'] { box-shadow: inset 3px 0 #54c7b9; }
.rail-account { margin-top: auto; padding: 10px 9px 14px; border-top: 1px solid #35403d; }
.rail-account button { display: grid; width: 100%; min-height: 50px; place-items: center; background: transparent; color: #fff; border: 0; }
.rail-account button > span:not(.avatar), .rail-account button > svg { display: none; }
.avatar { display: grid; width: 30px; height: 30px; place-items: center; background: #e8d8b5; color: #3e3524; border-radius: 50%; font-size: 10px; font-weight: 800; }
.flow-workspace { min-width: 0; }
.flow-toolbar { position: sticky; z-index: 15; top: 0; display: flex; height: 64px; align-items: center; gap: 12px; padding: 0 24px; background: rgb(255 255 255 / 96%); border-bottom: 1px solid #dfe3e1; }
.mobile-menu { display: none; }
.project-select { display: flex; min-width: 184px; min-height: 42px; align-items: center; gap: 9px; padding: 4px 9px; background: #fff; border: 1px solid #ccd4d1; border-radius: 6px; text-align: left; }
.project-select > span:nth-child(2) { display: flex; min-width: 0; flex: 1; flex-direction: column; }
.project-select small { color: var(--muted); font-size: 9px; line-height: 1.2; text-transform: uppercase; }
.project-select strong { overflow: hidden; font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
.project-mark { display: grid; width: 27px; height: 27px; place-items: center; background: #dff3ef; color: #076f65; border-radius: 5px; font-size: 11px; font-weight: 800; }
.global-search { display: flex; width: min(430px, 38vw); min-height: 40px; align-items: center; gap: 8px; padding: 0 11px; background: #f7f8f7; border: 1px solid #dce2e0; border-radius: 6px; }
.global-search input { width: 100%; min-width: 0; background: transparent; border: 0; outline: none; }
.trial-badge { margin-left: auto; padding: 5px 9px; background: #fff0d7; color: #754105; border: 1px solid #efd098; border-radius: 999px; font-size: 11px; font-weight: 700; white-space: nowrap; }
.user-button { display: grid; width: 34px; height: 34px; place-items: center; background: #edf2f0; border: 1px solid #d4dedb; border-radius: 50%; }
.flow-main { max-width: 1600px; min-height: calc(100vh - 64px); margin: 0 auto; padding: 30px 30px 80px; }
.flow-heading { align-items: center; }
.flow-breadcrumb { margin: 0 0 7px !important; color: #77827f; font-size: 11px; font-weight: 650; }
.flow-breadcrumb span { padding: 0 5px; color: #b1bab7; }
.heading-actions { display: flex; align-items: center; gap: 8px; }
.view-toggle { display: flex; padding: 3px; background: #ecefed; border: 1px solid #d8dddb; border-radius: 6px; }
.view-toggle button { display: flex; min-height: 34px; align-items: center; gap: 6px; padding: 0 10px; background: transparent; color: #62706c; border: 0; border-radius: 4px; font-size: 12px; font-weight: 650; }
.view-toggle button[data-active='true'] { background: #fff; color: #1b2825; box-shadow: 0 1px 2px rgb(22 34 31 / 10%); }
.metric-strip { display: grid; margin-top: 22px; grid-template-columns: repeat(4, 1fr); background: #fff; border: 1px solid #d9dfdd; border-radius: 6px; }
.metric-strip > div { display: grid; min-height: 92px; grid-template-columns: 1fr auto; align-content: center; gap: 3px 14px; padding: 15px 20px; border-right: 1px solid #e2e6e5; }
.metric-strip > div:last-child { border-right: 0; }
.metric-strip span { grid-column: 1 / -1; color: #697572; font-size: 11px; font-weight: 650; }
.metric-strip strong { font-size: 25px; line-height: 1; }
.metric-strip small { align-self: end; color: #697572; font-size: 11px; }
.metric-strip .positive { color: #17734d; }
.metric-strip .metric-attention { background: #fffaf1; }
.flow-filters { display: grid; margin-top: 18px; grid-template-columns: repeat(5, minmax(130px, 1fr)); gap: 12px; padding: 16px; background: #fff; border: 1px solid #d8dfdc; border-radius: 6px; box-shadow: 0 8px 24px rgb(26 39 35 / 6%); }
.flow-filters label, .move-field { display: flex; min-width: 0; flex-direction: column; gap: 5px; color: #65716e; font-size: 10px; font-weight: 700; text-transform: uppercase; }
.flow-filters .filter-query { grid-column: span 2; }
.flow-filters .field { min-width: 0; font-size: 12px; text-transform: none; }
.flow-filters .select-field { width: 100%; min-width: 0; font-size: 12px; text-transform: none; }
.reset-filters { align-self: end; }
.workflow-section, .table-section { margin-top: 24px; }
.workflow-heading, .table-heading { display: flex; min-height: 52px; align-items: center; justify-content: space-between; }
.workflow-heading h2, .table-heading h2 { margin-top: 3px; font-size: 17px; }
.flow-board { display: grid; min-width: 0; grid-template-columns: repeat(4, minmax(220px, 1fr)); gap: 10px; }
.flow-lane { min-width: 0; padding: 0 8px 16px; background: #eef1ef; border-top: 3px solid #7290bd; border-radius: 4px; }
.flow-lane[data-status='reviewed'] { background: #f3f1e9; border-top-color: #b98235; }
.flow-lane[data-status='linked'] { background: #edf1f8; border-top-color: #4273be; }
.flow-lane[data-status='resolved'] { background: #edf4ef; border-top-color: #4b906c; }
.flow-lane > header { display: grid; min-height: 72px; grid-template-columns: 1fr auto; align-content: center; padding: 10px 4px 8px; }
.lane-state { display: flex; align-items: center; gap: 7px; font-size: 12px; font-weight: 750; }
.lane-state i { width: 8px; height: 8px; background: #527caf; border-radius: 50%; }
[data-status='reviewed'] .lane-state i { background: #ae762b; }
[data-status='linked'] .lane-state i { background: #3568b4; }
[data-status='resolved'] .lane-state i { background: #347a55; }
.flow-lane > header > strong { font-size: 12px; }
.flow-lane > header p { grid-column: 1 / -1; margin-top: 3px; color: #75807d; font-size: 10px; }
.lane-items { display: flex; flex-direction: column; gap: 8px; }
.flow-card { min-width: 0; padding: 12px; background: #fff; border: 1px solid #d7dddb; border-radius: 5px; box-shadow: 0 1px 2px rgb(25 36 33 / 5%); cursor: pointer; }
.flow-card:hover, .flow-card:focus, .flow-card[data-selected='true'] { border-color: #55a99f; box-shadow: 0 0 0 2px rgb(8 127 115 / 11%); outline: none; }
.card-topline { display: flex; min-height: 28px; align-items: center; gap: 7px; }
.select-box { display: grid; width: 23px; height: 28px; place-items: center; }
.select-box input { margin: 0; accent-color: var(--teal); }
.card-id, .card-age { color: #77827f; font-size: 10px; font-weight: 650; }
.card-age { margin-left: auto; }
.flow-card > p { display: -webkit-box; min-height: 54px; margin: 10px 0; overflow: hidden; color: #26322f; font-size: 12px; font-weight: 650; line-height: 1.5; -webkit-box-orient: vertical; -webkit-line-clamp: 3; }
.card-meta, .card-footer { display: flex; min-width: 0; align-items: center; justify-content: space-between; gap: 7px; padding-top: 9px; border-top: 1px solid #edf0ef; }
.card-meta > span:first-child { display: flex; min-width: 0; align-items: center; gap: 5px; overflow: hidden; color: #64706d; font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.card-meta i { width: 7px; height: 7px; border-radius: 2px; }
.card-footer { min-height: 32px; margin-top: 8px; color: #66726f; font-size: 10px; }
.issue-link, .moderation-mark, .processing-failed, .ready-label { display: inline-flex; align-items: center; gap: 4px; font-weight: 700; white-space: nowrap; }
.issue-link { color: #285cae; }
.moderation-mark, .processing-failed { color: #a42d3a; }
.topic { max-width: 52%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.lane-empty { display: flex; min-height: 78px; align-items: center; justify-content: center; flex-direction: column; gap: 5px; color: #85908d; border: 1px dashed #cbd3d0; border-radius: 5px; font-size: 10px; }
.empty-feedback, .replay-empty, .sheet-no-preview { display: flex; min-height: 180px; align-items: center; justify-content: center; flex-direction: column; gap: 9px; color: #6d7975; background: #fff; border: 1px dashed #c7d0cd; border-radius: 6px; }
.flow-table { width: 100%; background: #fff; border: 1px solid #d9dfdd; border-collapse: separate; border-spacing: 0; border-radius: 6px; table-layout: fixed; }
.flow-table th { height: 42px; padding: 0 10px; color: #6a7673; border-bottom: 1px solid #dde3e1; font-size: 10px; text-align: left; text-transform: uppercase; }
.flow-table th:first-child { width: 42px; }.flow-table th:nth-child(2) { width: 38%; }.flow-table th:nth-child(3) { width: 13%; }.flow-table th:nth-child(4) { width: 12%; }.flow-table th:nth-child(5) { width: 10%; }.flow-table th:nth-child(6) { width: 12%; }
.flow-table td { height: 66px; padding: 8px 10px; border-bottom: 1px solid #e8ecea; overflow: hidden; font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }
.flow-table tr { cursor: pointer; }.flow-table tbody tr:hover, .flow-table tr[data-selected='true'] { background: #edf5f3; }
.table-feedback { display: flex; min-width: 0; align-items: center; gap: 9px; }.table-feedback > span { display: flex; min-width: 0; flex-direction: column; }.table-feedback strong { overflow: hidden; font-size: 11px; text-overflow: ellipsis; }.table-feedback small { color: #77827f; }
.mobile-feedback-list { display: none; }
.detail-heading { display: flex; align-items: flex-start; justify-content: space-between; padding-bottom: 22px; border-bottom: 1px solid #dce2e0; }
.back-link { display: inline-flex; min-height: 32px; align-items: center; gap: 6px; margin-bottom: 10px; padding: 0; background: transparent; color: #51605c; border: 0; font-size: 11px; font-weight: 650; }.back-link svg { transform: rotate(180deg); }
.detail-title-row { display: flex; align-items: center; gap: 12px; }.detail-title-row h1 { font-size: 22px; }.detail-title-row p { color: #6e7a76; font-size: 11px; }
.detail-layout { display: grid; align-items: start; grid-template-columns: minmax(0, 1fr) 330px; gap: 28px; padding-top: 24px; }
.evidence-canvas { min-width: 0; }.feedback-statement { padding: 4px 0 24px; border-bottom: 1px solid #dce2e0; }.feedback-statement h2 { max-width: 820px; margin-top: 8px; font-size: 19px; line-height: 1.45; }.statement-meta { display: flex; flex-wrap: wrap; gap: 8px 20px; margin-top: 14px; color: #64716d; font-size: 11px; }.statement-meta span { display: flex; align-items: center; gap: 5px; }
.evidence-block { padding: 24px 0; border-bottom: 1px solid #dce2e0; }.evidence-heading { display: flex; align-items: center; justify-content: space-between; gap: 16px; margin-bottom: 13px; }.evidence-heading h2 { margin-top: 3px; font-size: 16px; }.ready-label { color: #247c55; font-size: 11px; }
.transcript-block { padding: 24px 0; }.transcript-block p { max-width: 760px; margin-top: 8px; color: #33413d; font-size: 14px; line-height: 1.65; }
.lifecycle-panel { position: sticky; top: 88px; padding: 20px; background: #fff; border: 1px solid #d6ddda; border-radius: 6px; }.lifecycle-panel h2 { margin-top: 4px; font-size: 18px; }
.lifecycle-steps { display: flex; margin: 20px 0; padding: 0; flex-direction: column; list-style: none; }.lifecycle-steps li { position: relative; display: grid; min-height: 52px; grid-template-columns: 26px 1fr; gap: 8px; color: #7a8582; }.lifecycle-steps li::after { position: absolute; top: 24px; bottom: 0; left: 10px; width: 1px; background: #d8dfdc; content: ''; }.lifecycle-steps li:last-child::after { display: none; }.lifecycle-steps li > span { z-index: 1; display: grid; width: 21px; height: 21px; place-items: center; background: #fff; border: 1px solid #bdc7c3; border-radius: 50%; }.lifecycle-steps li i { width: 6px; height: 6px; background: #b9c3c0; border-radius: 50%; }.lifecycle-steps li div { display: flex; flex-direction: column; }.lifecycle-steps li strong { color: inherit; font-size: 11px; }.lifecycle-steps li small { font-size: 9px; }.lifecycle-steps li[data-complete='true'], .lifecycle-steps li[data-current='true'] { color: #167463; }.lifecycle-steps li[data-current='true'] > span { background: #dff3ef; border-color: #5aab9d; }
.move-field .select-field { width: 100%; margin-top: 3px; text-transform: none; }.issue-section { display: flex; margin-top: 20px; flex-direction: column; gap: 8px; padding-top: 18px; border-top: 1px solid #e0e5e3; }.linked-issue { display: flex; padding: 11px; flex-direction: column; gap: 3px; background: #edf2fa; border-left: 3px solid #3970c1; border-radius: 3px; }.linked-issue span { display: flex; align-items: center; gap: 5px; color: #41649a; font-size: 10px; }.linked-issue small { color: #6a7890; font-size: 10px; }
.detail-facts { margin: 20px 0 0; padding-top: 12px; border-top: 1px solid #e0e5e3; }.detail-facts div { display: flex; min-height: 34px; align-items: center; justify-content: space-between; gap: 10px; border-bottom: 1px solid #edf0ef; font-size: 10px; }.detail-facts dt { color: #6e7976; }.detail-facts dd { display: flex; align-items: center; gap: 4px; margin: 0; overflow: hidden; font-weight: 650; text-overflow: ellipsis; white-space: nowrap; }.detail-actions { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; margin-top: 18px; }
.sentiment-positive { color: #17734d; }.sentiment-negative { color: #aa3542; }.sentiment-neutral { color: #84601a; }
.sheet-scrim { position: fixed; z-index: 40; inset: 0; background: rgb(24 33 31 / 18%); }.evidence-sheet { position: fixed; z-index: 45; top: 0; right: 0; display: flex; width: min(440px, 100vw); height: 100vh; flex-direction: column; background: #fff; border-left: 1px solid #cad3d0; box-shadow: -18px 0 48px rgb(20 32 29 / 12%); }.evidence-sheet > header { display: flex; min-height: 72px; align-items: center; justify-content: space-between; padding: 0 20px; border-bottom: 1px solid #dce2e0; }.evidence-sheet > header h2 { margin-top: 3px; font-size: 18px; }.sheet-body { min-height: 0; padding: 20px; overflow-y: auto; }.sheet-rating { display: flex; align-items: center; gap: 8px; }.sheet-rating .muted { margin-left: auto; font-size: 11px; }.sheet-excerpt { margin: 17px 0; font-size: 16px; font-weight: 650; line-height: 1.5; }.sheet-source { display: flex; align-items: flex-start; justify-content: space-between; gap: 15px; margin-bottom: 18px; color: #66736f; font-size: 10px; }.sheet-source > span:first-child { display: flex; min-width: 0; flex-direction: column; }.sheet-source > span:first-child :last-child { overflow: hidden; text-overflow: ellipsis; }.sheet-body :deep(.product-scene) { min-height: 240px; }.sheet-signals { margin-top: 17px; border-top: 1px solid #e0e5e3; }.sheet-signals > div { display: flex; min-height: 41px; align-items: center; justify-content: space-between; gap: 12px; border-bottom: 1px solid #e8ecea; font-size: 11px; }.sheet-signals span { color: #6b7773; }.evidence-sheet > footer { display: grid; margin-top: auto; grid-template-columns: 1fr auto; gap: 10px; padding: 14px 20px; background: #f8faf9; border-top: 1px solid #dce2e0; }.evidence-sheet > footer label { display: flex; min-width: 0; align-items: center; gap: 8px; color: #65716e; font-size: 10px; font-weight: 700; text-transform: uppercase; }.evidence-sheet > footer .select-field { min-width: 0; max-width: 120px; font-size: 11px; text-transform: none; }
.bulk-bar { position: fixed; z-index: 50; bottom: 22px; left: 50%; display: flex; min-height: 52px; align-items: center; gap: 5px; padding: 6px 8px 6px 16px; background: #202927; color: #fff; border: 1px solid #3b4744; border-radius: 6px; box-shadow: 0 12px 36px rgb(20 31 28 / 24%); transform: translateX(-50%); }.bulk-bar strong { margin-right: 5px; font-size: 11px; white-space: nowrap; }.bulk-bar button { display: inline-flex; min-height: 38px; align-items: center; gap: 6px; padding: 0 9px; background: transparent; color: #eff4f2; border: 0; border-radius: 4px; font-size: 11px; }.bulk-bar button:hover { background: #34413e; }.bulk-divider { width: 1px; height: 25px; background: #46534f; }.bulk-bar .bulk-close { width: 38px; justify-content: center; padding: 0; }

@media (max-width: 1080px) {
  .flow-board { grid-template-columns: repeat(2, minmax(230px, 1fr)); }
  .flow-filters { grid-template-columns: repeat(3, 1fr); }
  .metric-strip { grid-template-columns: repeat(2, 1fr); }.metric-strip > div:nth-child(2) { border-right: 0; }.metric-strip > div:nth-child(-n+2) { border-bottom: 1px solid #e2e6e5; }
  .flow-table th:nth-child(3), .flow-table td:nth-child(3), .flow-table th:nth-child(5), .flow-table td:nth-child(5) { display: none; }
}

@media (max-width: 720px) {
  .flowboard-shell { display: block; min-height: calc(100vh - 58px); }
  .flow-rail { position: fixed; z-index: 70; top: 0; left: 0; width: min(300px, 86vw); height: 100vh; min-height: 0; transform: translateX(-105%); transition: transform 150ms ease; }.flow-rail[data-open='true'] { transform: translateX(0); }
  .flow-brand { display: flex; height: 66px; justify-content: flex-start; gap: 10px; padding: 0 10px 0 16px; }.flow-brand strong { display: block; }.rail-mobile-close { display: inline-grid; width: 40px; height: 40px; margin-left: auto; place-items: center; background: transparent; color: #fff; border: 1px solid transparent; border-radius: 5px; }
  .flow-rail nav button { min-height: 48px; justify-content: flex-start; flex-direction: row; gap: 10px; padding: 0 12px; font-size: 12px; }
  .rail-account button { display: flex; justify-content: flex-start; gap: 9px; padding: 0 10px; }.rail-account button > span:not(.avatar), .rail-account button > svg { display: block; }.rail-account button > svg { margin-left: auto; }
  .flow-toolbar { height: 58px; gap: 7px; padding: 0 12px; }.mobile-menu { display: inline-grid; }.project-select { min-width: 0; flex: 1; }.project-select > span:nth-child(2) small { display: none; }.project-select strong { font-size: 11px; }.global-search { width: 40px; padding: 0; justify-content: center; }.global-search input { position: absolute; width: 1px; height: 1px; opacity: 0; }.trial-badge { display: none; }.flow-toolbar > .icon-button:not(.mobile-menu), .user-button { display: none; }
  .flow-main { min-height: calc(100vh - 58px); padding: 20px 14px 88px; }.flow-heading { align-items: stretch; }.flow-breadcrumb { display: none; }.flow-heading h1 { font-size: 20px; }.heading-actions { justify-content: space-between; }.heading-actions > .secondary-button { margin-left: auto; }.view-toggle button { min-width: 42px; justify-content: center; }.view-toggle button span { display: none; }
  .metric-strip { margin-top: 18px; grid-template-columns: repeat(2, 1fr); }.metric-strip > div { min-height: 76px; padding: 12px; }.metric-strip strong { font-size: 21px; }.metric-strip .metric-attention { grid-column: 1 / -1; }.metric-strip > div:nth-child(3) { border-right: 0; }.metric-strip > div:last-child { border-top: 1px solid #e2e6e5; }
  .flow-filters { grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 9px; padding: 12px; }.flow-filters .reset-filters { min-height: 40px; }
  .workflow-section, .table-section { margin-top: 18px; }.workflow-heading, .table-heading { min-height: 48px; }.workflow-heading h2, .table-heading h2 { font-size: 15px; }
  .flow-board { display: flex; flex-direction: column; gap: 12px; }.flow-lane { padding: 0 8px 8px; }.flow-lane > header { min-height: 62px; }.lane-items { gap: 7px; }.flow-card { padding: 11px; }.flow-card > p { min-height: 0; -webkit-line-clamp: 2; }
  .flow-table { display: none; }.mobile-feedback-list { display: flex; flex-direction: column; background: #fff; border: 1px solid #d9dfdd; border-radius: 6px; }.mobile-feedback-list article { display: grid; min-height: 80px; grid-template-columns: auto minmax(0, 1fr) auto; align-items: center; gap: 10px; padding: 10px; border-bottom: 1px solid #e2e7e5; }.mobile-feedback-list article:last-child { border-bottom: 0; }.mobile-feedback-list article > div { display: flex; min-width: 0; flex-direction: column; }.mobile-feedback-list strong { display: -webkit-box; overflow: hidden; font-size: 11px; -webkit-box-orient: vertical; -webkit-line-clamp: 2; }.mobile-feedback-list span { margin-top: 4px; color: #71807b; font-size: 10px; }
  .detail-heading { padding-bottom: 16px; }.detail-layout { display: block; padding-top: 16px; }.feedback-statement h2 { font-size: 16px; }.statement-meta { flex-direction: column; gap: 5px; }.evidence-heading { align-items: flex-start; }.evidence-heading .secondary-button { width: 40px; padding: 0; font-size: 0; }.lifecycle-panel { position: static; margin-top: 20px; padding: 16px; }.detail-actions { position: sticky; z-index: 10; bottom: 0; margin: 18px -16px -16px; padding: 12px 16px; background: #fff; border-top: 1px solid #dce2e0; }
  .evidence-sheet { width: 100%; }.evidence-sheet > header { min-height: 62px; }.sheet-body { padding: 16px; }.sheet-excerpt { font-size: 14px; }.evidence-sheet > footer { grid-template-columns: 1fr; padding: 12px 16px; }.evidence-sheet > footer label { justify-content: space-between; }.evidence-sheet > footer .select-field { max-width: 160px; }.evidence-sheet > footer .primary-button { width: 100%; }
  .bulk-bar { right: 10px; bottom: 10px; left: 10px; justify-content: space-between; padding-left: 12px; transform: none; }.bulk-bar button { width: 40px; justify-content: center; padding: 0; }.bulk-bar button svg + * { display: none; }.bulk-bar button:not(.bulk-close) { font-size: 0; }.bulk-divider { display: none; }
}

@media (prefers-reduced-motion: reduce) { .flow-rail { transition: none; } }
</style>
