<script setup lang="ts">
import {
  Activity,
  Bell,
  ChevronDown,
  CircleHelp,
  CreditCard,
  FolderKanban,
  Inbox,
  KeyRound,
  LayoutDashboard,
  Link2,
  Menu,
  MoreHorizontal,
  Plus,
  RotateCcw,
  Search,
  Settings,
  SlidersHorizontal,
  Users,
} from '@lucide/vue'
import { feedbackItems, overviewMetrics, projects, satisfactionSeries } from '~/data/design-fixtures'
import type { FeedbackItem, ViewSlug } from '~/types/design'
import { conceptPath } from '~/utils/design'

const props = defineProps<{ view: ViewSlug }>()
const route = useRoute()
const router = useRouter()
const state = reactive(usePrototypeState())
const mobileNavOpen = ref(false)
const filtersOpen = ref(false)

if (typeof route.query.id === 'string') state.select(route.query.id)

const pageTitle = computed(() => {
  if (props.view === 'overview') return 'Overview'
  if (props.view === 'feedback') return 'Feedback'
  return state.selected.id
})

function openDetail(item: FeedbackItem) {
  state.select(item.id)
  router.push({ path: conceptPath('clearspace', 'feedback-detail'), query: { id: item.id } })
}

function openFeedback() {
  router.push(conceptPath('clearspace', 'feedback'))
}

const satisfactionBars = satisfactionSeries.map((value) => Math.round(((value - 3.2) / 1.4) * 84) + 18)
</script>

<template>
  <div class="clearspace">
    <aside class="cs-sidebar" :data-open="mobileNavOpen">
      <div class="cs-brand"><span>F</span><strong>fidibeki</strong></div>
      <button class="tenant-switch">
        <span class="tenant-avatar">AS</span>
        <span><b>Acme Studio</b><small>Pro trial</small></span>
        <ChevronDown :size="15" />
      </button>

      <nav aria-label="Primary navigation">
        <button :data-active="view === 'overview'" @click="router.push(conceptPath('clearspace', 'overview'))">
          <LayoutDashboard :size="17" /> Overview
        </button>
        <button><FolderKanban :size="17" /> Projects</button>
        <button :data-active="view !== 'overview'" @click="openFeedback"><Inbox :size="17" /> Feedback <span>18</span></button>
        <button><Activity :size="17" /> Issues <span>24</span></button>
      </nav>

      <p class="nav-section">Workspace</p>
      <nav aria-label="Workspace navigation">
        <button><Link2 :size="17" /> Integrations</button>
        <button><Users :size="17" /> Members</button>
        <button><KeyRound :size="17" /> API keys</button>
        <button><Settings :size="17" /> Settings</button>
        <button><CreditCard :size="17" /> Billing</button>
      </nav>

      <div class="trial-note">
        <div><strong>9 days</strong><span>left in trial</span></div>
        <span class="trial-meter"><i /></span>
        <button>Review plan</button>
      </div>
      <button class="cs-profile"><span>MC</span><b>Marta Costa</b><MoreHorizontal :size="16" /></button>
    </aside>

    <div v-if="mobileNavOpen" class="nav-scrim" @click="mobileNavOpen = false" />

    <section class="cs-workspace">
      <header class="cs-topbar">
        <button class="icon-button mobile-menu" title="Open navigation" @click="mobileNavOpen = true">
          <Menu :size="19" /><span class="sr-only">Open navigation</span>
        </button>
        <span class="breadcrumb">Acme Studio <b>/</b> {{ pageTitle }}</span>
        <div class="top-actions">
          <button class="icon-button" title="Help"><CircleHelp :size="18" /><span class="sr-only">Help</span></button>
          <button class="icon-button" title="Notifications"><Bell :size="18" /><span class="sr-only">Notifications</span></button>
        </div>
      </header>

      <main class="cs-content">
        <template v-if="view === 'overview'">
          <div class="page-heading">
            <div><h1>Overview</h1><p>Product feedback health across Acme Studio.</p></div>
            <div class="heading-actions">
              <select class="select-field" aria-label="Date range"><option>Last 30 days</option><option>Last 7 days</option></select>
              <button class="primary-button"><Plus :size="16" /> Add project</button>
            </div>
          </div>

          <section class="cs-metrics" aria-label="Workspace metrics">
            <article v-for="metric in overviewMetrics" :key="metric.label">
              <span class="muted">{{ metric.label }}</span>
              <div><strong class="metric-value">{{ metric.value }}</strong><small :class="metric.direction">{{ metric.delta }}</small></div>
              <span class="metric-caption">vs previous 30 days</span>
            </article>
          </section>

          <section class="cs-overview-grid">
            <article class="cs-panel satisfaction-panel">
              <header><div><h2>Satisfaction trend</h2><p>Average rating across all projects</p></div><span class="tag">4.1 average</span></header>
              <div class="satisfaction-chart" aria-label="Satisfaction increased from 3.6 to 4.1 over 30 days">
                <span v-for="(height, index) in satisfactionBars" :key="index" :style="{ height: `${height}%` }" />
              </div>
              <div class="chart-axis"><span>Jun 22</span><span>Jul 01</span><span>Jul 10</span><span>Jul 21</span></div>
            </article>

            <article class="cs-panel attention-panel">
              <header><div><h2>Needs attention</h2><p>Current triage workload</p></div></header>
              <button><span class="attention-icon blue">18</span><span><b>Unreviewed feedback</b><small>6 added today</small></span><span>View</span></button>
              <button><span class="attention-icon red">3</span><span><b>Moderation flags</b><small>1 requires a decision</small></span><span>View</span></button>
              <button><span class="attention-icon amber">4</span><span><b>AI processing</b><small>2 failed jobs</small></span><span>View</span></button>
            </article>
          </section>

          <section class="cs-panel recent-panel">
            <header><div><h2>Recent feedback</h2><p>Latest reports across the workspace</p></div><button class="quiet-button" @click="openFeedback">View all</button></header>
            <div class="table-scroll">
              <table class="data-table">
                <thead><tr><th style="width: 72px">Rating</th><th>Feedback</th><th style="width: 140px">Project</th><th style="width: 110px">Status</th><th style="width: 90px">Received</th></tr></thead>
                <tbody>
                  <tr v-for="item in feedbackItems.slice(0, 5)" :key="item.id" @click="openDetail(item)">
                    <td><RatingMark :rating="item.rating" compact /></td>
                    <td><strong>{{ item.excerpt }}</strong><small>{{ item.id }} · {{ item.topic }}</small></td>
                    <td>{{ item.project }}</td>
                    <td><span class="status-pill" :class="`status-${item.status}`">{{ item.status }}</span></td>
                    <td class="muted">{{ item.age }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </section>

          <section class="cs-panel project-health">
            <header><div><h2>Project health</h2><p>Collection and issue status</p></div></header>
            <div class="project-health-grid">
              <article v-for="project in projects" :key="project.slug">
                <span class="project-mark" :style="{ background: project.accent }">{{ project.name.slice(0, 1) }}</span>
                <div><strong>{{ project.name }}</strong><small>{{ project.domain }}</small></div>
                <span class="status-pill" :class="project.status === 'collecting' ? 'status-ready' : 'status-failed'">{{ project.status }}</span>
                <span><b>{{ project.feedbackCount }}</b><small>feedback</small></span>
                <span><b>{{ project.satisfaction }}</b><small>satisfaction</small></span>
              </article>
            </div>
          </section>
        </template>

        <template v-else-if="view === 'feedback'">
          <div class="page-heading">
            <div><h1>Feedback</h1><p>Review, filter, and connect customer evidence to product work.</p></div>
            <button class="secondary-button"><SlidersHorizontal :size="16" /> Saved views</button>
          </div>

          <section class="feedback-toolbar">
            <label class="search-field"><Search :size="16" /><input v-model="state.filters.query" placeholder="Search feedback, topic, or ID" /></label>
            <button class="secondary-button" :aria-expanded="filtersOpen" @click="filtersOpen = !filtersOpen"><SlidersHorizontal :size="16" /> Filters</button>
            <select v-model="state.filters.sort" class="select-field" aria-label="Sort feedback"><option value="newest">Newest</option><option value="oldest">Oldest</option><option value="rating-low">Rating: low</option><option value="rating-high">Rating: high</option></select>
          </section>

          <section v-if="filtersOpen" class="filter-drawer" aria-label="Feedback filters">
            <label>Project<select v-model="state.filters.project"><option value="all">All projects</option><option v-for="project in projects" :key="project.slug" :value="project.slug">{{ project.name }}</option></select></label>
            <label>Status<select v-model="state.filters.status"><option value="all">All statuses</option><option value="new">New</option><option value="reviewed">Reviewed</option><option value="linked">Linked</option><option value="resolved">Resolved</option></select></label>
            <label>Rating<select v-model="state.filters.rating"><option value="all">Any rating</option><option v-for="rating in 5" :key="rating" :value="String(rating)">{{ rating }} stars</option></select></label>
            <label>Evidence<select v-model="state.filters.attachment"><option value="all">Any evidence</option><option value="screenshot">Screenshot</option><option value="audio">Audio</option><option value="video">Video</option><option value="replay">Replay</option></select></label>
            <label>Moderation<select v-model="state.filters.moderation"><option value="all">Any state</option><option value="clear">Clear</option><option value="flagged">Flagged</option></select></label>
            <label>Issue<select v-model="state.filters.issue"><option value="all">Any link state</option><option value="linked">Linked</option><option value="unlinked">Unlinked</option></select></label>
            <label>Sentiment<select v-model="state.filters.sentiment"><option value="all">Any sentiment</option><option value="positive">Positive</option><option value="neutral">Neutral</option><option value="negative">Negative</option></select></label>
            <label>Date<select v-model="state.filters.date"><option value="30d">Last 30 days</option><option value="7d">Last 7 days</option><option value="today">Today</option></select></label>
          </section>

          <section class="cs-panel feedback-list-panel">
            <div class="list-summary"><span><b>{{ state.filteredItems.length }}</b> matching feedback</span><span class="muted">Cursor 01J3F7</span></div>
            <div v-if="state.filteredItems.length" class="table-scroll">
              <table class="data-table feedback-table">
                <thead><tr><th style="width: 72px">Rating</th><th>Feedback</th><th style="width: 132px">Project</th><th style="width: 110px">Evidence</th><th style="width: 112px">Issue</th><th style="width: 104px">Status</th><th style="width: 82px">Age</th></tr></thead>
                <tbody>
                  <tr v-for="item in state.filteredItems" :key="item.id" @click="openDetail(item)">
                    <td><RatingMark :rating="item.rating" compact /></td>
                    <td><strong>{{ item.excerpt }}</strong><small>{{ item.id }} · {{ item.topic }}</small></td>
                    <td>{{ item.project }}</td>
                    <td><AttachmentIcons :attachments="item.attachments" /></td>
                    <td><span v-if="item.issue" class="issue-link">{{ item.issue }}</span><span v-else class="muted">Unlinked</span></td>
                    <td><span class="status-pill" :class="`status-${item.status}`">{{ item.status }}</span></td>
                    <td class="muted">{{ item.age }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
            <div v-else class="empty-list"><Search :size="24" /><h2>No feedback matches</h2><p>Change or clear a filter to see more records.</p></div>
            <footer class="list-pagination"><button class="secondary-button" disabled>Previous</button><span>Showing {{ state.filteredItems.length }} records</span><button class="secondary-button">Next</button></footer>
          </section>
        </template>

        <template v-else>
          <button class="back-link" @click="openFeedback">← Back to feedback</button>
          <header class="detail-heading">
            <div><div class="detail-title-line"><RatingMark :rating="state.selected.rating" /><h1>{{ state.selected.id }}</h1><span class="status-pill" :class="`status-${state.selected.status}`">{{ state.selected.status }}</span></div><p>{{ state.selected.project }} · {{ state.selected.page }} · {{ state.selected.age }} ago</p></div>
            <div><button class="secondary-button" @click="state.confirm('Feedback marked reviewed in this prototype')">Mark reviewed</button><button class="primary-button" @click="state.confirm('Issue draft opened in this prototype')"><Plus :size="16" /> Create issue</button></div>
          </header>

          <section class="detail-layout">
            <div class="detail-main">
              <article class="cs-panel feedback-message">
                <span class="section-label">Customer feedback</span>
                <h2>{{ state.selected.cleanedText }}</h2>
                <p>Original: “{{ state.selected.excerpt }}”</p>
              </article>
              <article class="cs-panel evidence-panel">
                <header><div><h2>Session evidence</h2><p>Screenshot and replay share the captured checkout context.</p></div><span class="status-pill" :class="state.selected.replayReady ? 'status-ready' : 'status-processing'">{{ state.selected.replayReady ? 'Replay ready' : 'Processing' }}</span></header>
                <ProductScene :replay="state.selected.replayReady" />
              </article>
              <article v-if="state.selected.transcript" class="cs-panel transcript-panel">
                <header><div><h2>Transcript</h2><p>AI-cleaned from attached media</p></div></header>
                <blockquote>{{ state.selected.transcript }}</blockquote>
              </article>
            </div>

            <aside class="detail-context">
              <section>
                <h2>Triage</h2>
                <dl><div><dt>Sentiment</dt><dd><span class="status-pill" :class="`status-${state.selected.sentiment}`">{{ state.selected.sentiment }}</span></dd></div><div><dt>Topic</dt><dd>{{ state.selected.topic }}</dd></div><div><dt>Issue</dt><dd>{{ state.selected.issue ?? 'Not linked' }}</dd></div><div><dt>AI status</dt><dd><span class="status-pill" :class="`status-${state.selected.aiStatus}`">{{ state.selected.aiStatus }}</span></dd></div></dl>
                <button v-if="state.selected.aiStatus === 'failed'" class="secondary-button full" @click="state.confirm('AI reprocessing queued locally')"><RotateCcw :size="15" /> Reprocess all</button>
              </section>
              <section>
                <h2>Reporter</h2>
                <dl><div><dt>Name</dt><dd>{{ state.selected.reporter }}</dd></div><div><dt>Email</dt><dd>{{ state.selected.email || 'Not provided' }}</dd></div></dl>
              </section>
              <section>
                <h2>Environment</h2>
                <dl><div><dt>Browser</dt><dd>{{ state.selected.browser }}</dd></div><div><dt>Device</dt><dd>{{ state.selected.device }}</dd></div><div><dt>Viewport</dt><dd>{{ state.selected.viewport }}</dd></div></dl>
              </section>
            </aside>
          </section>
        </template>
      </main>
    </section>
    <div v-if="state.notice" class="prototype-toast" role="status">{{ state.notice }}</div>
  </div>
</template>

<style scoped>
.clearspace {
  --cs-sidebar: 232px;
  display: grid;
  min-height: calc(100vh - 48px);
  grid-template-columns: var(--cs-sidebar) minmax(0, 1fr);
  background: #f6f8f7;
}

.cs-sidebar {
  position: sticky;
  top: 48px;
  z-index: 40;
  display: flex;
  height: calc(100vh - 48px);
  flex-direction: column;
  padding: 16px 12px 12px;
  background: #f2f5f4;
  border-right: 1px solid var(--line);
}

.cs-brand {
  display: flex;
  height: 40px;
  align-items: center;
  gap: 9px;
  padding: 0 8px;
  font-size: 15px;
}

.cs-brand span {
  display: grid;
  width: 27px;
  height: 27px;
  place-items: center;
  background: var(--ink);
  color: #fff;
  border-radius: 5px;
  font-weight: 800;
}

.tenant-switch {
  display: grid;
  width: 100%;
  min-height: 54px;
  grid-template-columns: 32px minmax(0, 1fr) 16px;
  align-items: center;
  gap: 9px;
  margin: 10px 0 16px;
  padding: 7px 8px;
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: var(--radius);
  text-align: left;
}

.tenant-switch > span:nth-child(2) {
  display: flex;
  min-width: 0;
  flex-direction: column;
}

.tenant-switch small,
.cs-profile small {
  color: var(--muted);
}

.tenant-avatar,
.cs-profile > span {
  display: grid;
  width: 32px;
  height: 32px;
  place-items: center;
  background: var(--teal-soft);
  color: var(--teal);
  border-radius: 5px;
  font-size: 11px;
  font-weight: 800;
}

.cs-sidebar nav {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.cs-sidebar nav button {
  display: flex;
  width: 100%;
  min-height: 38px;
  align-items: center;
  gap: 10px;
  padding: 0 9px;
  background: transparent;
  color: var(--ink-soft);
  border: 0;
  border-radius: 5px;
  text-align: left;
}

.cs-sidebar nav button > span {
  min-width: 24px;
  margin-left: auto;
  padding: 1px 6px;
  background: #e1e7e5;
  border-radius: 999px;
  font-size: 11px;
  text-align: center;
}

.cs-sidebar nav button:hover,
.cs-sidebar nav button[data-active='true'] {
  background: #e2ece9;
  color: var(--ink);
  font-weight: 700;
}

.nav-section {
  margin: 24px 9px 8px;
  color: var(--muted);
  font-size: 10px;
  font-weight: 750;
  text-transform: uppercase;
}

.trial-note {
  margin-top: auto;
  padding: 12px;
  background: #e7f3f0;
  border: 1px solid #c8e2dc;
  border-radius: var(--radius);
}

.trial-note > div {
  display: flex;
  justify-content: space-between;
  font-size: 11px;
}

.trial-note > div strong {
  font-size: 13px;
}

.trial-meter {
  display: block;
  height: 4px;
  margin: 9px 0;
  overflow: hidden;
  background: #c2d9d4;
  border-radius: 3px;
}

.trial-meter i {
  display: block;
  width: 64%;
  height: 100%;
  background: var(--teal);
}

.trial-note button {
  padding: 0;
  background: transparent;
  color: var(--teal);
  border: 0;
  font-size: 11px;
  font-weight: 750;
}

.cs-profile {
  display: grid;
  min-height: 48px;
  grid-template-columns: 32px minmax(0, 1fr) 16px;
  align-items: center;
  gap: 9px;
  margin-top: 8px;
  padding: 5px 8px;
  background: transparent;
  border: 0;
  text-align: left;
}

.cs-workspace {
  min-width: 0;
}

.cs-topbar {
  position: sticky;
  top: 48px;
  z-index: 30;
  display: flex;
  height: 56px;
  align-items: center;
  padding: 0 24px;
  background: rgb(255 255 255 / 94%);
  border-bottom: 1px solid var(--line);
  backdrop-filter: blur(8px);
}

.breadcrumb {
  color: var(--muted);
  font-size: 13px;
}

.breadcrumb b {
  margin: 0 7px;
  color: var(--line-strong);
}

.top-actions {
  display: flex;
  gap: 3px;
  margin-left: auto;
}

.mobile-menu {
  display: none;
  margin-right: 4px;
}

.cs-content {
  width: min(1280px, 100%);
  margin: 0 auto;
  padding: 28px 32px 64px;
}

.heading-actions {
  display: flex;
  gap: 9px;
}

.cs-metrics {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  margin-top: 10px;
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: var(--radius);
}

.cs-metrics article {
  min-height: 122px;
  padding: 20px;
  border-right: 1px solid var(--line);
}

.cs-metrics article:last-child {
  border-right: 0;
}

.cs-metrics article > div {
  display: flex;
  align-items: baseline;
  gap: 10px;
  margin-top: 11px;
}

.cs-metrics small.up {
  color: var(--green);
}

.cs-metrics small.down {
  color: var(--blue);
}

.metric-caption {
  display: block;
  margin-top: 5px;
  color: var(--muted);
  font-size: 11px;
}

.cs-overview-grid {
  display: grid;
  grid-template-columns: minmax(0, 1.7fr) minmax(300px, 0.8fr);
  gap: 16px;
  margin-top: 16px;
}

.cs-panel {
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: var(--radius);
}

.cs-panel > header {
  display: flex;
  min-height: 68px;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 14px 17px;
  border-bottom: 1px solid var(--line);
}

.cs-panel > header h2,
.detail-context h2 {
  font-size: 14px;
  font-weight: 750;
}

.cs-panel > header p {
  margin-top: 3px;
  color: var(--muted);
  font-size: 12px;
}

.satisfaction-chart {
  display: flex;
  height: 180px;
  align-items: flex-end;
  gap: 8px;
  padding: 24px 22px 0;
  background-image: repeating-linear-gradient(to bottom, transparent 0 44px, #e8eeec 45px);
}

.satisfaction-chart span {
  flex: 1;
  min-width: 4px;
  background: var(--teal);
  border-radius: 3px 3px 0 0;
}

.chart-axis {
  display: flex;
  justify-content: space-between;
  padding: 8px 18px 13px;
  color: var(--muted);
  font-size: 10px;
}

.attention-panel > button {
  display: grid;
  width: 100%;
  min-height: 68px;
  grid-template-columns: 38px minmax(0, 1fr) auto;
  align-items: center;
  gap: 11px;
  padding: 10px 15px;
  background: transparent;
  border: 0;
  border-bottom: 1px solid var(--line);
  text-align: left;
}

.attention-panel > button:last-child {
  border-bottom: 0;
}

.attention-panel button > span:nth-child(2) {
  display: flex;
  min-width: 0;
  flex-direction: column;
}

.attention-panel button > span:last-child {
  color: var(--blue);
  font-size: 11px;
  font-weight: 700;
}

.attention-panel small {
  margin-top: 2px;
  color: var(--muted);
}

.attention-icon {
  display: grid;
  width: 36px;
  height: 36px;
  place-items: center;
  border-radius: 5px;
  font-weight: 750;
}

.attention-icon.blue { background: var(--blue-soft); color: var(--blue); }
.attention-icon.red { background: var(--red-soft); color: var(--red); }
.attention-icon.amber { background: var(--amber-soft); color: var(--amber); }

.recent-panel,
.project-health {
  margin-top: 16px;
}

.data-table td strong,
.data-table td small {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
}

.data-table td small {
  margin-top: 2px;
  color: var(--muted);
  font-size: 11px;
}

.project-health-grid article {
  display: grid;
  min-height: 66px;
  grid-template-columns: 34px minmax(150px, 1fr) 110px 90px 90px;
  align-items: center;
  gap: 12px;
  padding: 10px 16px;
  border-bottom: 1px solid var(--line);
}

.project-health-grid article:last-child {
  border-bottom: 0;
}

.project-mark {
  display: grid;
  width: 32px;
  height: 32px;
  place-items: center;
  color: #fff;
  border-radius: 5px;
  font-weight: 800;
}

.project-health-grid article > div,
.project-health-grid article > span:not(.project-mark, .status-pill) {
  display: flex;
  flex-direction: column;
}

.project-health-grid small {
  color: var(--muted);
  font-size: 11px;
}

.feedback-toolbar {
  display: grid;
  grid-template-columns: minmax(260px, 1fr) auto auto;
  gap: 8px;
  margin: 10px 0 12px;
}

.search-field {
  display: flex;
  height: 40px;
  align-items: center;
  gap: 9px;
  padding: 0 11px;
  background: var(--surface);
  border: 1px solid var(--line-strong);
  border-radius: var(--radius);
}

.search-field input {
  width: 100%;
  min-width: 0;
  border: 0;
  outline: 0;
}

.filter-drawer {
  display: grid;
  grid-template-columns: repeat(4, minmax(120px, 1fr));
  gap: 12px;
  margin-bottom: 12px;
  padding: 14px;
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: var(--radius);
}

.filter-drawer label {
  display: flex;
  flex-direction: column;
  gap: 5px;
  color: var(--muted);
  font-size: 11px;
  font-weight: 700;
}

.filter-drawer select {
  height: 36px;
  padding: 0 9px;
  background: #fff;
  border: 1px solid var(--line);
  border-radius: 5px;
}

.feedback-list-panel {
  overflow: hidden;
}

.list-summary,
.list-pagination {
  display: flex;
  min-height: 48px;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 0 14px;
}

.list-summary {
  border-bottom: 1px solid var(--line);
}

.list-pagination {
  border-top: 1px solid var(--line);
  color: var(--muted);
  font-size: 12px;
}

.list-pagination button:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

.issue-link {
  color: var(--blue);
  font-size: 12px;
  font-weight: 700;
}

.empty-list {
  display: grid;
  min-height: 280px;
  place-items: center;
  align-content: center;
  gap: 8px;
  color: var(--muted);
  text-align: center;
}

.empty-list h2 {
  color: var(--ink);
  font-size: 16px;
}

.back-link {
  margin-bottom: 15px;
  padding: 0;
  background: transparent;
  color: var(--blue);
  border: 0;
  font-weight: 700;
}

.detail-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 20px;
  margin-bottom: 20px;
}

.detail-heading > div:last-child {
  display: flex;
  gap: 8px;
}

.detail-heading p {
  margin-top: 8px;
  color: var(--muted);
}

.detail-title-line {
  display: flex;
  align-items: center;
  gap: 10px;
}

.detail-title-line h1 {
  font-size: 22px;
}

.detail-layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 300px;
  gap: 16px;
  align-items: start;
}

.detail-main {
  display: grid;
  gap: 16px;
}

.feedback-message {
  padding: 20px;
}

.feedback-message h2 {
  max-width: 820px;
  margin-top: 11px;
  font-size: 19px;
  line-height: 1.45;
}

.feedback-message p {
  margin-top: 12px;
  color: var(--muted);
}

.evidence-panel > :deep(.product-scene) {
  margin: 16px;
}

.transcript-panel blockquote {
  margin: 0;
  padding: 18px;
  color: var(--ink-soft);
  font-size: 15px;
  line-height: 1.7;
}

.detail-context {
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: var(--radius);
}

.detail-context section {
  padding: 16px;
  border-bottom: 1px solid var(--line);
}

.detail-context section:last-child {
  border-bottom: 0;
}

.detail-context dl {
  margin: 12px 0 0;
}

.detail-context dl div {
  display: grid;
  grid-template-columns: 82px minmax(0, 1fr);
  gap: 10px;
  padding: 7px 0;
}

.detail-context dt {
  color: var(--muted);
  font-size: 12px;
}

.detail-context dd {
  min-width: 0;
  margin: 0;
  overflow-wrap: anywhere;
  font-size: 12px;
  font-weight: 650;
}

.full {
  width: 100%;
  margin-top: 12px;
}

.nav-scrim {
  display: none;
}

@media (max-width: 1000px) {
  .clearspace {
    grid-template-columns: 200px minmax(0, 1fr);
  }

  .cs-sidebar {
    width: 200px;
  }

  .cs-overview-grid {
    grid-template-columns: 1fr;
  }

  .detail-layout {
    grid-template-columns: minmax(0, 1fr) 270px;
  }
}

@media (max-width: 720px) {
  .clearspace {
    display: block;
  }

  .cs-sidebar {
    position: fixed;
    top: 0;
    left: 0;
    z-index: 100;
    width: min(86vw, 300px);
    height: 100vh;
    transform: translateX(-105%);
    transition: transform 180ms ease;
    box-shadow: var(--shadow-overlay);
  }

  .cs-sidebar[data-open='true'] {
    transform: translateX(0);
  }

  .nav-scrim {
    position: fixed;
    z-index: 90;
    display: block;
    inset: 0;
    background: rgb(9 15 13 / 42%);
  }

  .cs-topbar {
    position: static;
    height: 52px;
    padding: 0 10px;
  }

  .mobile-menu {
    display: inline-grid;
  }

  .cs-content {
    padding: 20px 14px 60px;
  }

  .heading-actions,
  .detail-heading,
  .detail-heading > div:last-child {
    align-items: stretch;
    flex-direction: column;
  }

  .cs-metrics {
    grid-template-columns: repeat(2, 1fr);
  }

  .cs-metrics article {
    min-height: 106px;
    padding: 14px;
    border-right: 1px solid var(--line);
    border-bottom: 1px solid var(--line);
  }

  .cs-metrics article:last-child {
    grid-column: 1 / -1;
    border-bottom: 0;
  }

  .cs-overview-grid {
    grid-template-columns: 1fr;
  }

  .project-health-grid article {
    grid-template-columns: 32px minmax(0, 1fr) auto;
  }

  .project-health-grid article > span:nth-last-child(-n + 2) {
    display: none;
  }

  .feedback-toolbar {
    grid-template-columns: 1fr auto;
  }

  .feedback-toolbar .select-field {
    grid-column: 1 / -1;
  }

  .filter-drawer {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .table-scroll {
    overflow: visible;
  }

  .data-table,
  .data-table tbody {
    display: block;
  }

  .data-table thead {
    display: none;
  }

  .data-table tr {
    display: grid;
    grid-template-columns: 50px minmax(0, 1fr) auto;
    align-items: center;
    gap: 8px;
    min-height: 76px;
    padding: 10px 12px;
    border-bottom: 1px solid var(--line);
  }

  .data-table td {
    display: none;
    height: auto;
    padding: 0;
    border: 0;
  }

  .data-table td:nth-child(1),
  .data-table td:nth-child(2),
  .data-table td:last-child {
    display: block;
  }

  .data-table td:last-child {
    text-align: right;
  }

  .feedback-table tr {
    grid-template-columns: 50px minmax(0, 1fr) auto;
  }

  .feedback-table td:last-child {
    display: block;
  }

  .detail-heading {
    display: flex;
  }

  .detail-layout {
    grid-template-columns: 1fr;
  }

  .detail-context {
    order: -1;
  }
}

@media (prefers-reduced-motion: reduce) {
  .cs-sidebar {
    transition: none;
  }
}
</style>
