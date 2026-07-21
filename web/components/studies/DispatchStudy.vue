<script setup lang="ts">
import {
  Archive,
  Bell,
  Check,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  CircleAlert,
  CircleUserRound,
  ExternalLink,
  FolderKanban,
  Inbox,
  LayoutList,
  Link2,
  Menu,
  MoreHorizontal,
  PanelRightOpen,
  Plus,
  RotateCcw,
  Search,
  Settings,
  ShieldAlert,
  Sparkles,
  Tag,
  Trash2,
} from '@lucide/vue'
import { feedbackItems, overviewMetrics, projects } from '~/data/design-fixtures'
import type { FeedbackItem, ViewSlug } from '~/types/design'
import { conceptPath } from '~/utils/design'

const props = defineProps<{ view: ViewSlug }>()
const route = useRoute()
const router = useRouter()
const state = reactive(usePrototypeState())
const mobileNavOpen = ref(false)
const inspectorTab = ref<'feedback' | 'evidence' | 'replay'>('feedback')

if (typeof route.query.id === 'string') state.select(route.query.id)

const queueViews = [
  { name: 'Open', count: 18, icon: Inbox, filter: 'all' },
  { name: 'Moderation', count: 3, icon: ShieldAlert, filter: 'flagged' },
  { name: 'AI failed', count: 2, icon: CircleAlert, filter: 'failed' },
  { name: 'Has replay', count: 12, icon: PanelRightOpen, filter: 'replay' },
  { name: 'Unlinked', count: 9, icon: Link2, filter: 'unlinked' },
  { name: 'Archived', count: 46, icon: Archive, filter: 'archived' },
]

const activeQueue = ref('Open')

function applyQueue(item: (typeof queueViews)[number]) {
  activeQueue.value = item.name
  state.filters.moderation = item.filter === 'flagged' ? 'flagged' : 'all'
  state.filters.attachment = item.filter === 'replay' ? 'replay' : 'all'
  state.filters.issue = item.filter === 'unlinked' ? 'unlinked' : 'all'
}

function selectItem(item: FeedbackItem) {
  state.select(item.id)
  inspectorTab.value = 'feedback'
}

function openDetail(item = state.selected) {
  router.push({ path: conceptPath('dispatch', 'feedback-detail'), query: { id: item.id } })
}

function moveSelection(direction: number) {
  const items = state.filteredItems
  const index = items.findIndex((item) => item.id === state.selectedId)
  const next = items[Math.min(items.length - 1, Math.max(0, index + direction))]
  if (next) state.select(next.id)
}
</script>

<template>
  <div class="dispatch">
    <header class="dispatch-topbar">
      <button class="icon-button mobile-nav-button" title="Open navigation" @click="mobileNavOpen = true"><Menu :size="19" /><span class="sr-only">Open navigation</span></button>
      <div class="dispatch-brand"><span>F</span><b>fidibeki</b></div>
      <button class="dispatch-tenant"><span>AS</span><b>Acme Studio</b><ChevronDown :size="14" /></button>
      <label class="global-search"><Search :size="15" /><input v-model="state.filters.query" placeholder="Search feedback" /></label>
      <span class="trial-status"><i /> Pro trial · 9 days</span>
      <button class="icon-button" title="Notifications"><Bell :size="17" /><span class="sr-only">Notifications</span></button>
      <button class="profile-button"><CircleUserRound :size="19" /><span class="desktop-only">Marta</span></button>
    </header>

    <aside class="global-rail" :data-open="mobileNavOpen">
      <nav aria-label="Primary navigation">
        <button :data-active="view !== 'overview'" title="Feedback" @click="router.push(conceptPath('dispatch', 'feedback'))"><Inbox :size="20" /><span>Feedback</span></button>
        <button title="Issues"><LayoutList :size="20" /><span>Issues</span></button>
        <button title="Projects"><FolderKanban :size="20" /><span>Projects</span></button>
        <button title="Settings"><Settings :size="20" /><span>Settings</span></button>
      </nav>
      <button class="rail-help" title="More"><MoreHorizontal :size="20" /><span>More</span></button>
    </aside>

    <div v-if="mobileNavOpen" class="dispatch-scrim" @click="mobileNavOpen = false" />

    <aside v-if="view !== 'feedback-detail'" class="queue-rail">
      <div class="queue-heading"><span>Feedback queues</span><button title="Create saved view"><Plus :size="16" /><span class="sr-only">Create saved view</span></button></div>
      <nav aria-label="Feedback queues">
        <button v-for="item in queueViews" :key="item.name" :data-active="activeQueue === item.name" @click="applyQueue(item)">
          <component :is="item.icon" :size="15" /><span>{{ item.name }}</span><b>{{ item.count }}</b>
        </button>
      </nav>
      <p class="queue-section">Projects</p>
      <nav aria-label="Projects">
        <button v-for="project in projects" :key="project.slug" @click="state.filters.project = project.slug">
          <span class="project-dot" :style="{ background: project.accent }" /><span>{{ project.name }}</span><b>{{ Math.round(project.feedbackCount / 10) }}</b>
        </button>
      </nav>
      <div class="queue-tip"><Sparkles :size="15" /><p><b>2 AI jobs failed</b><span>Review before the next sync.</span></p></div>
    </aside>

    <main class="dispatch-main" :class="{ 'is-detail': view === 'feedback-detail' }">
      <template v-if="view === 'overview'">
        <div class="dispatch-page-head">
          <div><p class="section-label">Operations</p><h1>Good afternoon, Marta</h1><span>Six feedback items need a decision today.</span></div>
          <select class="select-field" aria-label="Date range"><option>Last 30 days</option><option>Last 7 days</option></select>
        </div>

        <section class="metric-ledger" aria-label="Workspace metrics">
          <article v-for="metric in overviewMetrics" :key="metric.label"><span>{{ metric.label }}</span><strong>{{ metric.value }}</strong><small>{{ metric.delta }} vs previous</small></article>
        </section>

        <section class="worklist">
          <header><div><h2>Work requiring attention</h2><p>Ordered by impact and age</p></div><span>6 items</span></header>
          <button v-for="item in feedbackItems.slice(0, 6)" :key="item.id" @click="selectItem(item)">
            <span class="priority-marker" :class="item.sentiment" />
            <RatingMark :rating="item.rating" compact />
            <span class="work-copy"><b>{{ item.excerpt }}</b><small>{{ item.id }} · {{ item.project }} · {{ item.topic }}</small></span>
            <AttachmentIcons :attachments="item.attachments" />
            <span v-if="item.issue" class="issue-label">{{ item.issue }}</span><span v-else class="muted">Unlinked</span>
            <span class="muted">{{ item.age }}</span>
            <ChevronRight :size="16" />
          </button>
        </section>

        <section class="dispatch-projects">
          <header><h2>Projects</h2><span>Collection health</span></header>
          <div v-for="project in projects" :key="project.slug">
            <span class="project-dot large" :style="{ background: project.accent }" /><p><b>{{ project.name }}</b><span>{{ project.domain }}</span></p><span class="status-pill" :class="project.status === 'collecting' ? 'status-ready' : 'status-failed'">{{ project.status }}</span><b>{{ project.feedbackCount }}</b><span class="muted">{{ project.satisfaction }} avg.</span>
          </div>
        </section>
      </template>

      <template v-else-if="view === 'feedback'">
        <section class="stream-panel">
          <header class="stream-header">
            <div><h1>{{ activeQueue }}</h1><span>{{ state.filteredItems.length }} feedback items</span></div>
            <div><button class="icon-button" title="Create filter"><Tag :size="17" /><span class="sr-only">Create filter</span></button><select v-model="state.filters.sort" class="compact-select" aria-label="Sort"><option value="newest">Newest</option><option value="oldest">Oldest</option><option value="rating-low">Lowest rating</option></select></div>
          </header>
          <div class="stream-filter-row">
            <select v-model="state.filters.status" aria-label="Status filter"><option value="all">All status</option><option value="new">New</option><option value="reviewed">Reviewed</option><option value="linked">Linked</option><option value="resolved">Resolved</option></select>
            <select v-model="state.filters.rating" aria-label="Rating filter"><option value="all">Any rating</option><option value="1">1 star</option><option value="2">2 stars</option><option value="3">3 stars</option><option value="4">4 stars</option><option value="5">5 stars</option></select>
            <select v-model="state.filters.sentiment" aria-label="Sentiment filter"><option value="all">Any sentiment</option><option value="negative">Negative</option><option value="neutral">Neutral</option><option value="positive">Positive</option></select>
          </div>

          <div v-if="state.selectedIds.length" class="bulk-bar">
            <b>{{ state.selectedIds.length }} selected</b>
            <button @click="state.confirm('Selected feedback archived locally')"><Archive :size="14" /> Archive</button>
            <button @click="state.confirm('Issue linkage opened locally')"><Link2 :size="14" /> Link issue</button>
            <button @click="state.confirm('AI reprocessing queued locally')"><RotateCcw :size="14" /> Reprocess</button>
            <button class="danger" @click="state.confirm('Selected feedback marked as spam locally')"><Trash2 :size="14" /> Spam</button>
          </div>

          <div class="feedback-stream" role="list">
            <article v-for="item in state.filteredItems" :key="item.id" role="listitem" :data-selected="state.selectedId === item.id" @click="selectItem(item)">
              <label class="row-check" @click.stop><input type="checkbox" :checked="state.selectedIds.includes(item.id)" :aria-label="`Select ${item.id}`" @change="state.toggleSelected(item.id)" /></label>
              <RatingMark :rating="item.rating" compact />
              <div class="stream-copy"><strong>{{ item.excerpt }}</strong><span>{{ item.id }} · {{ item.project }} · {{ item.page }}</span></div>
              <AttachmentIcons :attachments="item.attachments" />
              <span class="status-pill" :class="`status-${item.aiStatus}`">{{ item.aiStatus }}</span>
              <span v-if="item.issue" class="issue-label">{{ item.issue }}</span><span v-else class="muted">Unlinked</span>
              <time>{{ item.age }}</time>
              <button class="mobile-detail-button" title="Open detail" @click.stop="openDetail(item)"><ChevronRight :size="17" /><span class="sr-only">Open {{ item.id }}</span></button>
            </article>
          </div>
        </section>

        <aside class="dispatch-inspector">
          <header>
            <div><span class="section-label">{{ state.selected.project }}</span><h2>{{ state.selected.id }}</h2></div>
            <div><button title="Previous feedback" @click="moveSelection(-1)"><ChevronLeft :size="16" /><span class="sr-only">Previous feedback</span></button><button title="Next feedback" @click="moveSelection(1)"><ChevronRight :size="16" /><span class="sr-only">Next feedback</span></button><button title="Open full detail" @click="openDetail()"><ExternalLink :size="16" /><span class="sr-only">Open full detail</span></button></div>
          </header>
          <div class="inspector-summary"><RatingMark :rating="state.selected.rating" /><div><b>{{ state.selected.topic }}</b><span>{{ state.selected.age }} ago · {{ state.selected.page }}</span></div><span class="status-pill" :class="`status-${state.selected.status}`">{{ state.selected.status }}</span></div>
          <nav class="inspector-tabs" aria-label="Feedback detail sections"><button :data-active="inspectorTab === 'feedback'" @click="inspectorTab = 'feedback'">Feedback</button><button :data-active="inspectorTab === 'evidence'" @click="inspectorTab = 'evidence'">Evidence</button><button :data-active="inspectorTab === 'replay'" @click="inspectorTab = 'replay'">Replay</button></nav>
          <div v-if="inspectorTab === 'feedback'" class="inspector-body">
            <p class="feedback-quote">{{ state.selected.cleanedText }}</p>
            <dl><div><dt>Reporter</dt><dd>{{ state.selected.reporter }}</dd></div><div><dt>Sentiment</dt><dd><span class="status-pill" :class="`status-${state.selected.sentiment}`">{{ state.selected.sentiment }}</span></dd></div><div><dt>Issue</dt><dd>{{ state.selected.issue ?? 'Not linked' }}</dd></div><div><dt>Browser</dt><dd>{{ state.selected.browser }}</dd></div></dl>
          </div>
          <div v-else-if="inspectorTab === 'evidence'" class="inspector-body evidence-body"><ProductScene /><p v-if="state.selected.transcript">“{{ state.selected.transcript }}”</p><p v-else class="muted">No transcript attached.</p></div>
          <div v-else class="inspector-body evidence-body"><ProductScene :replay="true" /><p>{{ state.selected.replayReady ? 'Replay ready · 01:24 captured' : 'Replay is still processing.' }}</p></div>
          <footer><button class="secondary-button" @click="state.confirm('Feedback archived locally')"><Archive :size="15" /> Archive</button><button class="primary-button" @click="state.confirm('Issue draft opened locally')"><Plus :size="15" /> Create issue</button></footer>
        </aside>
      </template>

      <template v-else>
        <header class="detail-topline"><button class="quiet-button" @click="router.push(conceptPath('dispatch', 'feedback'))"><ChevronLeft :size="16" /> Back to queue</button><div><button class="secondary-button" @click="state.confirm('Feedback archived locally')"><Archive :size="15" /> Archive</button><button class="primary-button" @click="state.confirm('Issue draft opened locally')"><Plus :size="15" /> Create issue</button></div></header>
        <section class="dispatch-detail">
          <header><div><span class="section-label">{{ state.selected.project }} · {{ state.selected.page }}</span><h1>{{ state.selected.cleanedText }}</h1><p>{{ state.selected.id }} · received {{ state.selected.age }} ago</p></div><RatingMark :rating="state.selected.rating" /></header>
          <div class="dispatch-detail-grid">
            <div class="detail-evidence"><div class="evidence-title"><h2>Captured session</h2><span class="status-pill" :class="state.selected.replayReady ? 'status-ready' : 'status-processing'">{{ state.selected.replayReady ? 'Replay ready' : 'Processing' }}</span></div><ProductScene :replay="state.selected.replayReady" /><div class="transcript-strip"><b>Transcript</b><p>{{ state.selected.transcript || 'No spoken feedback was attached.' }}</p></div></div>
            <aside class="case-facts"><section><h2>Triage</h2><dl><div><dt>Status</dt><dd><span class="status-pill" :class="`status-${state.selected.status}`">{{ state.selected.status }}</span></dd></div><div><dt>Sentiment</dt><dd>{{ state.selected.sentiment }} · {{ state.selected.sentimentScore }}</dd></div><div><dt>Topic</dt><dd>{{ state.selected.topic }}</dd></div><div><dt>Issue</dt><dd>{{ state.selected.issue ?? 'Not linked' }}</dd></div></dl></section><section><h2>Environment</h2><dl><div><dt>Reporter</dt><dd>{{ state.selected.reporter }}</dd></div><div><dt>Browser</dt><dd>{{ state.selected.browser }}</dd></div><div><dt>Device</dt><dd>{{ state.selected.device }}</dd></div><div><dt>Viewport</dt><dd>{{ state.selected.viewport }}</dd></div></dl></section></aside>
          </div>
        </section>
      </template>
    </main>
    <div v-if="state.notice" class="prototype-toast" role="status">{{ state.notice }}</div>
  </div>
</template>

<style scoped>
.dispatch {
  --topbar: 56px;
  --global-rail: 62px;
  --queue-rail: 216px;
  display: grid;
  min-height: calc(100vh - 48px);
  grid-template-columns: var(--global-rail) var(--queue-rail) minmax(0, 1fr);
  grid-template-rows: var(--topbar) minmax(0, 1fr);
  background: #f6f8f7;
}

.dispatch-topbar {
  position: sticky;
  top: 48px;
  z-index: 60;
  display: flex;
  grid-column: 1 / -1;
  align-items: center;
  gap: 10px;
  padding: 0 12px;
  background: #fff;
  border-bottom: 1px solid var(--line);
}

.dispatch-brand {
  display: flex;
  width: 150px;
  align-items: center;
  gap: 8px;
}

.dispatch-brand span {
  display: grid;
  width: 28px;
  height: 28px;
  place-items: center;
  background: var(--ink);
  color: #fff;
  border-radius: 5px;
  font-weight: 800;
}

.dispatch-tenant {
  display: flex;
  min-height: 38px;
  align-items: center;
  gap: 8px;
  padding: 0 9px;
  background: #f5f7f6;
  border: 1px solid var(--line);
  border-radius: 5px;
  font-size: 12px;
}

.dispatch-tenant > span {
  display: grid;
  width: 24px;
  height: 24px;
  place-items: center;
  background: var(--teal-soft);
  color: var(--teal);
  border-radius: 4px;
  font-size: 9px;
  font-weight: 800;
}

.global-search {
  display: flex;
  width: min(360px, 32vw);
  height: 38px;
  align-items: center;
  gap: 8px;
  margin-left: 8px;
  padding: 0 10px;
  background: #f5f7f6;
  border: 1px solid var(--line);
  border-radius: 5px;
}

.global-search input {
  width: 100%;
  border: 0;
  outline: 0;
  background: transparent;
}

.trial-status {
  display: flex;
  align-items: center;
  gap: 7px;
  margin-left: auto;
  color: var(--ink-soft);
  font-size: 11px;
  font-weight: 650;
}

.trial-status i {
  width: 7px;
  height: 7px;
  background: var(--teal);
  border-radius: 50%;
}

.profile-button {
  display: flex;
  min-height: 38px;
  align-items: center;
  gap: 7px;
  padding: 0 9px;
  background: transparent;
  border: 1px solid transparent;
  border-radius: 5px;
}

.mobile-nav-button {
  display: none;
}

.global-rail {
  position: sticky;
  top: 104px;
  z-index: 50;
  display: flex;
  height: calc(100vh - 104px);
  grid-column: 1;
  grid-row: 2;
  flex-direction: column;
  align-items: center;
  padding: 12px 7px;
  background: #f1f4f3;
  border-right: 1px solid var(--line);
}

.global-rail nav {
  display: flex;
  width: 100%;
  flex-direction: column;
  gap: 5px;
}

.global-rail button {
  display: flex;
  width: 100%;
  min-height: 48px;
  align-items: center;
  justify-content: center;
  flex-direction: column;
  gap: 3px;
  padding: 4px;
  background: transparent;
  color: var(--muted);
  border: 0;
  border-radius: 5px;
}

.global-rail button span {
  font-size: 9px;
  font-weight: 700;
}

.global-rail button:hover,
.global-rail button[data-active='true'] {
  background: #dfeae7;
  color: var(--teal);
}

.rail-help {
  margin-top: auto;
}

.queue-rail {
  position: sticky;
  top: 104px;
  height: calc(100vh - 104px);
  grid-column: 2;
  grid-row: 2;
  overflow-y: auto;
  padding: 14px 10px;
  background: #f9faf9;
  border-right: 1px solid var(--line);
}

.queue-heading {
  display: flex;
  height: 35px;
  align-items: center;
  justify-content: space-between;
  padding: 0 7px;
  color: var(--muted);
  font-size: 10px;
  font-weight: 750;
  text-transform: uppercase;
}

.queue-heading button {
  display: grid;
  width: 28px;
  height: 28px;
  place-items: center;
  background: transparent;
  border: 0;
  border-radius: 4px;
}

.queue-rail nav {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.queue-rail nav button {
  display: grid;
  min-height: 37px;
  grid-template-columns: 18px minmax(0, 1fr) auto;
  align-items: center;
  gap: 7px;
  padding: 0 8px;
  background: transparent;
  color: var(--ink-soft);
  border: 0;
  border-radius: 5px;
  text-align: left;
}

.queue-rail nav button:hover,
.queue-rail nav button[data-active='true'] {
  background: #e7efed;
  color: var(--ink);
}

.queue-rail nav button[data-active='true'] {
  font-weight: 750;
}

.queue-rail nav button b {
  color: var(--muted);
  font-size: 10px;
}

.queue-section {
  margin: 23px 8px 7px;
  color: var(--muted);
  font-size: 10px;
  font-weight: 750;
  text-transform: uppercase;
}

.project-dot {
  width: 8px;
  height: 8px;
  border-radius: 2px;
}

.project-dot.large {
  width: 28px;
  height: 28px;
  border-radius: 5px;
}

.queue-tip {
  display: flex;
  gap: 8px;
  margin-top: 24px;
  padding: 10px;
  background: var(--amber-soft);
  color: var(--amber);
  border: 1px solid #efd39f;
  border-radius: 5px;
}

.queue-tip p {
  display: flex;
  flex-direction: column;
  font-size: 10px;
}

.queue-tip span {
  margin-top: 2px;
  color: #705736;
}

.dispatch-main {
  display: grid;
  min-width: 0;
  grid-column: 3;
  grid-row: 2;
  grid-template-columns: minmax(470px, 1fr) minmax(330px, 40%);
}

.dispatch-main.is-detail,
.dispatch-main:has(.metric-ledger) {
  display: block;
}

.dispatch-page-head,
.metric-ledger,
.worklist,
.dispatch-projects {
  width: min(1180px, calc(100% - 48px));
  margin-right: auto;
  margin-left: auto;
}

.dispatch-page-head {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 16px;
  padding: 30px 0 20px;
}

.dispatch-page-head h1 {
  margin-top: 5px;
  font-size: 24px;
}

.dispatch-page-head > div > span {
  display: block;
  margin-top: 5px;
  color: var(--muted);
}

.metric-ledger {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  background: #fff;
  border: 1px solid var(--line);
  border-radius: 6px;
}

.metric-ledger article {
  display: grid;
  min-height: 102px;
  grid-template-columns: minmax(0, 1fr) auto;
  align-content: center;
  gap: 6px 14px;
  padding: 16px 20px;
  border-right: 1px solid var(--line);
}

.metric-ledger article:last-child { border-right: 0; }
.metric-ledger article > span { color: var(--muted); }
.metric-ledger strong { grid-row: 1 / span 2; grid-column: 2; font-size: 28px; }
.metric-ledger small { color: var(--green); }

.worklist,
.dispatch-projects {
  margin-top: 16px;
  overflow: hidden;
  background: #fff;
  border: 1px solid var(--line);
  border-radius: 6px;
}

.worklist > header,
.dispatch-projects > header {
  display: flex;
  min-height: 60px;
  align-items: center;
  justify-content: space-between;
  padding: 12px 16px;
  border-bottom: 1px solid var(--line);
}

.worklist h2,
.dispatch-projects h2 { font-size: 15px; }
.worklist header p,
.dispatch-projects header span { margin-top: 2px; color: var(--muted); font-size: 11px; }
.worklist header > span { padding: 3px 8px; background: var(--surface-soft); border-radius: 999px; font-size: 11px; font-weight: 700; }

.worklist > button {
  display: grid;
  width: 100%;
  min-height: 58px;
  grid-template-columns: 4px 44px minmax(220px, 1fr) 90px 80px 60px 18px;
  align-items: center;
  gap: 9px;
  padding: 7px 12px;
  background: transparent;
  border: 0;
  border-bottom: 1px solid var(--line);
  text-align: left;
}

.worklist > button:last-child { border-bottom: 0; }
.worklist > button:hover { background: #f1f6f4; }
.priority-marker { width: 3px; height: 28px; background: var(--amber); border-radius: 2px; }
.priority-marker.negative { background: var(--red); }
.priority-marker.positive { background: var(--green); }

.work-copy { display: flex; min-width: 0; flex-direction: column; }
.work-copy b,
.work-copy small { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.work-copy small { margin-top: 2px; color: var(--muted); font-size: 10px; }
.issue-label { color: var(--blue); font-size: 11px; font-weight: 750; }

.dispatch-projects { margin-bottom: 56px; }
.dispatch-projects > div { display: grid; min-height: 54px; grid-template-columns: 30px minmax(160px, 1fr) 100px 70px 70px; align-items: center; gap: 12px; padding: 8px 16px; border-bottom: 1px solid var(--line); }
.dispatch-projects > div:last-child { border-bottom: 0; }
.dispatch-projects p { display: flex; flex-direction: column; }
.dispatch-projects p span { color: var(--muted); font-size: 10px; }

.stream-panel {
  min-width: 0;
  background: #fff;
  border-right: 1px solid var(--line);
}

.stream-header {
  display: flex;
  min-height: 68px;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 10px 14px;
  border-bottom: 1px solid var(--line);
}

.stream-header h1 { font-size: 18px; }
.stream-header > div > span { color: var(--muted); font-size: 11px; }
.stream-header > div:last-child { display: flex; align-items: center; gap: 4px; }
.compact-select,
.stream-filter-row select { height: 34px; padding: 0 9px; background: #fff; border: 1px solid var(--line); border-radius: 4px; font-size: 11px; }

.stream-filter-row {
  display: flex;
  min-height: 50px;
  align-items: center;
  gap: 7px;
  padding: 8px 14px;
  overflow-x: auto;
  border-bottom: 1px solid var(--line);
}

.bulk-bar {
  display: flex;
  min-height: 46px;
  align-items: center;
  gap: 5px;
  padding: 6px 12px;
  background: #e8f2ef;
  border-bottom: 1px solid #c9ded8;
}

.bulk-bar b { margin-right: auto; font-size: 11px; }
.bulk-bar button { display: inline-flex; min-height: 31px; align-items: center; gap: 5px; padding: 0 8px; background: #fff; border: 1px solid var(--line); border-radius: 4px; font-size: 10px; font-weight: 700; }
.bulk-bar button.danger { color: var(--red); }

.feedback-stream article {
  display: grid;
  min-height: 56px;
  grid-template-columns: 24px 42px minmax(190px, 1fr) 78px 82px 66px 48px 24px;
  align-items: center;
  gap: 8px;
  padding: 6px 10px;
  border-bottom: 1px solid var(--line);
  cursor: pointer;
}

.feedback-stream article:hover,
.feedback-stream article[data-selected='true'] { background: #edf5f3; }
.feedback-stream article[data-selected='true'] { box-shadow: inset 3px 0 var(--teal); }
.row-check { display: grid; place-items: center; }
.row-check input { width: 15px; height: 15px; accent-color: var(--teal); }
.stream-copy { display: flex; min-width: 0; flex-direction: column; }
.stream-copy strong,
.stream-copy span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.stream-copy strong { font-size: 12px; }
.stream-copy span { margin-top: 2px; color: var(--muted); font-size: 9px; }
.feedback-stream time { color: var(--muted); font-size: 10px; }
.mobile-detail-button { display: grid; width: 28px; height: 28px; place-items: center; padding: 0; background: transparent; border: 0; border-radius: 4px; }

.dispatch-inspector {
  position: sticky;
  top: 104px;
  display: flex;
  height: calc(100vh - 104px);
  min-width: 0;
  flex-direction: column;
  background: #fbfcfb;
}

.dispatch-inspector > header {
  display: flex;
  min-height: 68px;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 10px 14px;
  border-bottom: 1px solid var(--line);
}

.dispatch-inspector h2 { margin-top: 3px; font-size: 18px; }
.dispatch-inspector > header > div:last-child { display: flex; }
.dispatch-inspector > header button { display: grid; width: 32px; height: 32px; place-items: center; padding: 0; background: transparent; border: 0; border-radius: 4px; }
.dispatch-inspector > header button:hover { background: var(--surface-soft); }

.inspector-summary { display: grid; min-height: 66px; grid-template-columns: 44px minmax(0, 1fr) auto; align-items: center; gap: 9px; padding: 9px 14px; border-bottom: 1px solid var(--line); }
.inspector-summary > div { display: flex; min-width: 0; flex-direction: column; }
.inspector-summary span { color: var(--muted); font-size: 10px; }

.inspector-tabs { display: grid; grid-template-columns: repeat(3, 1fr); padding: 0 12px; border-bottom: 1px solid var(--line); }
.inspector-tabs button { min-height: 42px; background: transparent; color: var(--muted); border: 0; border-bottom: 2px solid transparent; font-size: 11px; font-weight: 700; }
.inspector-tabs button[data-active='true'] { color: var(--teal); border-bottom-color: var(--teal); }

.inspector-body { flex: 1; min-height: 0; overflow-y: auto; padding: 18px; }
.feedback-quote { color: var(--ink); font-size: 16px; line-height: 1.6; }
.inspector-body dl { margin: 22px 0 0; }
.inspector-body dl div { display: grid; grid-template-columns: 82px minmax(0, 1fr); gap: 10px; padding: 9px 0; border-top: 1px solid var(--line); }
.inspector-body dt { color: var(--muted); font-size: 11px; }
.inspector-body dd { margin: 0; overflow-wrap: anywhere; font-size: 11px; font-weight: 650; }
.evidence-body { display: flex; flex-direction: column; gap: 13px; }
.evidence-body :deep(.product-scene) { min-height: 220px; }
.evidence-body p { line-height: 1.6; }

.dispatch-inspector > footer { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; padding: 12px; background: #fff; border-top: 1px solid var(--line); }

.detail-topline { display: flex; min-height: 58px; align-items: center; justify-content: space-between; padding: 0 24px; background: #fff; border-bottom: 1px solid var(--line); }
.detail-topline > div { display: flex; gap: 7px; }
.dispatch-detail { width: min(1160px, calc(100% - 48px)); margin: 0 auto; padding: 28px 0 64px; }
.dispatch-detail > header { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 24px; align-items: start; }
.dispatch-detail > header h1 { max-width: 820px; margin-top: 8px; font-size: 24px; line-height: 1.35; }
.dispatch-detail > header p { margin-top: 7px; color: var(--muted); }
.dispatch-detail-grid { display: grid; grid-template-columns: minmax(0, 1fr) 300px; gap: 16px; margin-top: 22px; align-items: start; }
.detail-evidence { overflow: hidden; background: #fff; border: 1px solid var(--line); border-radius: 6px; }
.evidence-title { display: flex; min-height: 58px; align-items: center; justify-content: space-between; padding: 12px 16px; border-bottom: 1px solid var(--line); }
.evidence-title h2,
.case-facts h2 { font-size: 14px; }
.detail-evidence > :deep(.product-scene) { margin: 16px; }
.transcript-strip { padding: 16px; border-top: 1px solid var(--line); }
.transcript-strip p { margin-top: 7px; color: var(--ink-soft); line-height: 1.6; }
.case-facts { background: #fff; border: 1px solid var(--line); border-radius: 6px; }
.case-facts section { padding: 15px; border-bottom: 1px solid var(--line); }
.case-facts section:last-child { border-bottom: 0; }
.case-facts dl { margin: 10px 0 0; }
.case-facts dl div { display: grid; grid-template-columns: 75px minmax(0, 1fr); gap: 10px; padding: 7px 0; }
.case-facts dt { color: var(--muted); font-size: 11px; }
.case-facts dd { margin: 0; overflow-wrap: anywhere; font-size: 11px; font-weight: 650; }

.dispatch-scrim { display: none; }

@media (max-width: 1100px) {
  .dispatch { --queue-rail: 190px; }
  .dispatch-main { grid-template-columns: minmax(430px, 1fr) 340px; }
  .feedback-stream article { grid-template-columns: 22px 40px minmax(170px, 1fr) 70px 70px 24px; }
  .feedback-stream article > :nth-child(6),
  .feedback-stream article > :nth-child(7) { display: none; }
}

@media (max-width: 720px) {
  .dispatch { display: block; min-height: calc(100vh - 86px); }
  .dispatch-topbar { position: static; height: 52px; padding: 0 8px; }
  .mobile-nav-button { display: inline-grid; }
  .dispatch-brand b,
  .dispatch-tenant b,
  .trial-status,
  .global-search { display: none; }
  .dispatch-brand { width: auto; }
  .dispatch-tenant { margin-left: auto; }
  .global-rail { position: fixed; top: 0; left: 0; z-index: 110; width: 84px; height: 100vh; transform: translateX(-110%); transition: transform 180ms ease; box-shadow: var(--shadow-overlay); }
  .global-rail[data-open='true'] { transform: translateX(0); }
  .dispatch-scrim { position: fixed; z-index: 100; display: block; inset: 0; background: rgb(9 15 13 / 42%); }
  .queue-rail { position: static; display: flex; height: 50px; padding: 6px 8px; overflow-x: auto; border-right: 0; border-bottom: 1px solid var(--line); }
  .queue-heading,
  .queue-section,
  .queue-tip,
  .queue-rail nav[aria-label='Projects'] { display: none; }
  .queue-rail nav { flex-direction: row; gap: 5px; }
  .queue-rail nav button { display: flex; min-width: max-content; min-height: 36px; gap: 6px; padding: 0 10px; background: #fff; border: 1px solid var(--line); }
  .queue-rail nav button b { margin-left: 2px; }
  .dispatch-main { display: block; }
  .dispatch-page-head,
  .metric-ledger,
  .worklist,
  .dispatch-projects { width: calc(100% - 28px); }
  .dispatch-page-head { align-items: stretch; flex-direction: column; padding-top: 20px; }
  .metric-ledger { grid-template-columns: 1fr 1fr; }
  .metric-ledger article { min-height: 86px; padding: 12px; border-bottom: 1px solid var(--line); }
  .metric-ledger article:last-child { grid-column: 1 / -1; border-bottom: 0; }
  .metric-ledger strong { font-size: 23px; }
  .worklist > button { grid-template-columns: 4px 42px minmax(0, 1fr) 18px; }
  .worklist > button > :nth-child(n + 4):not(:last-child) { display: none; }
  .dispatch-projects > div { grid-template-columns: 28px minmax(0, 1fr) auto; }
  .dispatch-projects > div > :nth-child(n + 4) { display: none; }
  .stream-header { min-height: 62px; }
  .feedback-stream article { grid-template-columns: 23px 42px minmax(0, 1fr) 28px; min-height: 70px; }
  .feedback-stream article > :nth-child(n + 4):not(:last-child) { display: none; }
  .stream-copy strong { white-space: normal; line-height: 1.3; }
  .dispatch-inspector { display: none; }
  .bulk-bar { overflow-x: auto; }
  .bulk-bar button { min-width: max-content; }
  .detail-topline { align-items: stretch; flex-direction: column; gap: 8px; padding: 10px 14px; }
  .detail-topline > div { display: grid; grid-template-columns: 1fr 1fr; }
  .dispatch-detail { width: calc(100% - 28px); padding-top: 20px; }
  .dispatch-detail > header h1 { font-size: 20px; }
  .dispatch-detail-grid { grid-template-columns: 1fr; }
  .case-facts { order: -1; }
}

@media (prefers-reduced-motion: reduce) {
  .global-rail { transition: none; }
}
</style>
