<script setup lang="ts">
import {
  Activity,
  ArrowLeft,
  ArrowUp,
  Bell,
  CalendarDays,
  ChartNoAxesCombined,
  ChevronDown,
  CircleAlert,
  Filter,
  Layers3,
  PanelRightClose,
  Search,
  Sparkles,
  Telescope,
  TrendingDown,
  TrendingUp,
  X,
} from '@lucide/vue'
import {
  feedbackItems,
  movements,
  projects,
  satisfactionSeries,
  sentimentSeries,
  volumeSeries,
} from '~/data/design-fixtures'
import type { FeedbackItem, ViewSlug } from '~/types/design'
import { defaultFilters } from '~/utils/design'

const props = defineProps<{ view: ViewSlug }>()

const {
  filters,
  filteredItems,
  selected,
  selectedId,
  notice,
  select,
  confirm,
} = usePrototypeState()

type Range = '7d' | '30d' | '90d'
type TrackId = 'volume' | 'satisfaction' | 'sentiment'

const range = ref<Range>('30d')
const activeTrack = ref<TrackId>('volume')
const focusOpen = ref(false)
const moreFiltersOpen = ref(false)

const ranges: { value: Range; label: string; period: string }[] = [
  { value: '7d', label: '7 days', period: 'Jul 15 - Jul 21' },
  { value: '30d', label: '30 days', period: 'Jun 22 - Jul 21' },
  { value: '90d', label: '90 days', period: 'Apr 23 - Jul 21' },
]

const tracks = computed(() => [
  {
    id: 'volume' as const,
    label: 'Feedback volume',
    value: '1,284',
    unit: 'responses',
    delta: '+18%',
    direction: 'up' as const,
    context: 'vs previous period',
    color: '#087f73',
    fill: '#dcefeb',
    values: seriesForRange(volumeSeries),
  },
  {
    id: 'satisfaction' as const,
    label: 'Satisfaction',
    value: '4.1',
    unit: 'out of 5',
    delta: '+0.3',
    direction: 'up' as const,
    context: 'vs previous period',
    color: '#2b63c6',
    fill: '#e1e9f8',
    values: seriesForRange(satisfactionSeries),
  },
  {
    id: 'sentiment' as const,
    label: 'Positive sentiment',
    value: '61%',
    unit: 'of analyzed feedback',
    delta: '-4%',
    direction: 'down' as const,
    context: 'vs previous period',
    color: '#247c55',
    fill: '#deeee6',
    values: seriesForRange(sentimentSeries),
  },
])

const currentPeriod = computed(() => ranges.find((item) => item.value === range.value)!.period)
const activeFilterCount = computed(() => {
  const defaults = defaultFilters()
  return Object.entries(filters).filter(([key, value]) => value !== defaults[key as keyof typeof defaults]).length
})
const visibleItems = computed(() => {
  if (filters.date === 'today') return filteredItems.value.filter((item) => item.receivedAt.startsWith('2026-07-21'))
  return filteredItems.value
})

const topicCounts = computed(() => {
  const counts = new Map<string, number>()
  for (const item of feedbackItems) counts.set(item.topic, (counts.get(item.topic) ?? 0) + 1)
  return [...counts.entries()].sort((left, right) => right[1] - left[1]).slice(0, 5)
})

function seriesForRange(values: number[]) {
  if (range.value === '7d') return values.slice(-7)
  return values
}

function chartPoints(values: number[], width = 720, height = 108): string {
  const min = Math.min(...values)
  const max = Math.max(...values)
  const spread = max - min || 1
  const step = width / Math.max(values.length - 1, 1)

  return values
    .map((value, index) => `${Math.round(index * step)},${Math.round(height - ((value - min) / spread) * (height - 18) - 9)}`)
    .join(' ')
}

function chartArea(values: number[], width = 720, height = 108): string {
  return `0,${height} ${chartPoints(values, width, height)} ${width},${height}`
}

function openEvidence(item: FeedbackItem) {
  select(item.id)
  focusOpen.value = true
}

function openMovement(label: string) {
  filters.query = label
  const item = feedbackItems.find((entry) => entry.topic === label)
  if (item) openEvidence(item)
}

function filterTopic(topic: string) {
  filters.query = filters.query === topic ? '' : topic
}

function resetFilters() {
  Object.assign(filters, defaultFilters())
}

function statusLabel(status: FeedbackItem['status']) {
  return status.charAt(0).toUpperCase() + status.slice(1)
}
</script>

<template>
  <main class="observatory">
    <header class="product-bar">
      <NuxtLink class="product-mark" to="/designs/signal-observatory/overview" aria-label="Fidibeki signals overview">
        <span class="product-symbol"><Activity :size="18" aria-hidden="true" /></span>
        <span class="product-wordmark">fidibeki</span>
      </NuxtLink>

      <div class="tenant-context">
        <span>Acme Studio</span>
        <ChevronDown :size="14" aria-hidden="true" />
      </div>

      <div class="product-actions">
        <span class="trial-state desktop-only">Pro trial · 9 days left</span>
        <button class="icon-button" type="button" title="Notifications" @click="confirm('No new notifications')">
          <Bell :size="18" aria-hidden="true" />
          <span class="sr-only">Notifications</span>
        </button>
        <button class="account-avatar" type="button" title="Account menu" @click="confirm('Account menu is fixture-only')">AD</button>
      </div>
    </header>

    <nav class="product-rail" aria-label="Signal Observatory navigation">
      <div class="rail-links">
        <NuxtLink to="/designs/signal-observatory/overview" :data-active="view === 'overview'">
          <ChartNoAxesCombined :size="16" aria-hidden="true" />
          Signals
        </NuxtLink>
        <NuxtLink to="/designs/signal-observatory/feedback" :data-active="view === 'feedback'">
          <Telescope :size="16" aria-hidden="true" />
          Evidence
        </NuxtLink>
        <button type="button" @click="confirm('Topic management is fixture-only')">
          <Layers3 :size="16" aria-hidden="true" />
          Topics
        </button>
      </div>

      <div class="rail-scope">
        <button type="button" @click="confirm('Project comparison is fixture-only')">
          All projects
          <ChevronDown :size="14" aria-hidden="true" />
        </button>
      </div>
    </nav>

    <section v-if="view === 'overview'" class="observatory-body overview-view">
      <div class="view-heading">
        <div>
          <p class="eyebrow"><span class="live-dot" /> Product health · Live</p>
          <h1>Product signals</h1>
          <p>Feedback movement across Storefront, Account Portal, and Docs.</p>
        </div>

        <div class="period-controls">
          <div class="range-control" aria-label="Signal time range">
            <button
              v-for="item in ranges"
              :key="item.value"
              type="button"
              :data-active="range === item.value"
              @click="range = item.value"
            >
              {{ item.value.toUpperCase() }}
            </button>
          </div>
          <button class="period-button" type="button" @click="confirm('Date range is fixture-only')">
            <CalendarDays :size="16" aria-hidden="true" />
            <span class="desktop-only">{{ currentPeriod }}</span>
            <ChevronDown :size="14" aria-hidden="true" />
          </button>
        </div>
      </div>

      <div class="mobile-track-control" aria-label="Selected signal track">
        <button
          v-for="track in tracks"
          :key="track.id"
          type="button"
          :data-active="activeTrack === track.id"
          @click="activeTrack = track.id"
        >
          {{ track.label.replace('Feedback ', '') }}
        </button>
      </div>

      <section class="signal-canvas" aria-labelledby="signal-canvas-title">
        <div class="canvas-head">
          <div>
            <span class="section-label">Aligned signals</span>
            <h2 id="signal-canvas-title">Current period</h2>
          </div>
          <div class="axis-key" aria-hidden="true">
            <span>{{ currentPeriod.split(' - ')[0] }}</span>
            <span>Period midpoint</span>
            <span>{{ currentPeriod.split(' - ')[1] }}</span>
          </div>
        </div>

        <button
          v-for="track in tracks"
          :key="track.id"
          class="signal-track"
          type="button"
          :data-active="activeTrack === track.id"
          @click="activeTrack = track.id"
        >
          <span class="track-summary">
            <span class="track-name">{{ track.label }}</span>
            <span class="track-value">{{ track.value }}</span>
            <span class="track-unit">{{ track.unit }}</span>
            <span class="track-delta" :data-direction="track.direction">
              <component :is="track.direction === 'up' ? TrendingUp : TrendingDown" :size="14" aria-hidden="true" />
              {{ track.delta }}
            </span>
            <span class="track-context">{{ track.context }}</span>
          </span>

          <span class="chart-wrap">
            <svg viewBox="0 0 720 108" role="img" :aria-label="`${track.label} trend for ${currentPeriod}`" preserveAspectRatio="none">
              <line v-for="x in [0, 180, 360, 540, 720]" :key="`x-${x}`" :x1="x" y1="0" :x2="x" y2="108" />
              <line v-for="y in [18, 54, 90]" :key="`y-${y}`" x1="0" :y1="y" x2="720" :y2="y" />
              <polygon :points="chartArea(track.values)" :fill="track.fill" />
              <polyline :points="chartPoints(track.values)" :stroke="track.color" />
              <circle
                cx="720"
                :cy="chartPoints(track.values).split(' ').at(-1)?.split(',')[1]"
                r="5"
                :fill="track.color"
                stroke="#fff"
                stroke-width="3"
              />
            </svg>
          </span>
        </button>
      </section>

      <section class="movement-register" aria-labelledby="movement-heading">
        <div class="register-heading">
          <div>
            <span class="section-label">Movement register</span>
            <h2 id="movement-heading">Material changes</h2>
          </div>
          <span class="analysis-state"><Sparkles :size="14" aria-hidden="true" /> AI analyzed 1,237 responses</span>
        </div>

        <div class="movement-list">
          <button
            v-for="(movement, index) in movements"
            :key="movement.label"
            type="button"
            :data-tone="movement.tone"
            @click="openMovement(movement.label)"
          >
            <span class="movement-index">0{{ index + 1 }}</span>
            <span class="movement-icon">
              <TrendingDown v-if="movement.tone === 'critical'" :size="18" aria-hidden="true" />
              <TrendingUp v-else-if="movement.tone === 'positive'" :size="18" aria-hidden="true" />
              <CircleAlert v-else :size="18" aria-hidden="true" />
            </span>
            <span class="movement-copy">
              <strong>{{ movement.label }}</strong>
              <span>{{ movement.detail }}</span>
            </span>
            <span class="evidence-count">{{ feedbackItems.filter((item) => item.topic === movement.label).length }} evidence</span>
            <ArrowUp class="movement-arrow" :size="16" aria-hidden="true" />
          </button>
        </div>
      </section>

      <section class="topic-band" aria-labelledby="topic-heading">
        <div class="register-heading">
          <div>
            <span class="section-label">Topic distribution</span>
            <h2 id="topic-heading">What customers mention</h2>
          </div>
          <NuxtLink to="/designs/signal-observatory/feedback">View all evidence <ArrowUp :size="15" aria-hidden="true" /></NuxtLink>
        </div>

        <div class="topic-spectrum">
          <button
            v-for="([topic, count], index) in topicCounts"
            :key="topic"
            type="button"
            :style="{ '--topic-width': `${Math.max(28, 100 - index * 14)}%` }"
            @click="openMovement(topic)"
          >
            <span>{{ topic }}</span>
            <span class="topic-line"><i /></span>
            <strong>{{ count }}</strong>
          </button>
        </div>
      </section>
    </section>

    <section v-else-if="view === 'feedback'" class="observatory-body evidence-view">
      <div class="view-heading">
        <div>
          <p class="eyebrow"><Telescope :size="14" aria-hidden="true" /> Signal evidence</p>
          <h1>Feedback chronology</h1>
          <p>{{ visibleItems.length }} of {{ feedbackItems.length }} responses in the current period.</p>
        </div>

        <div class="period-controls">
          <button class="period-button" type="button" @click="confirm('Comparison is fixture-only')">
            Previous period
            <ChevronDown :size="14" aria-hidden="true" />
          </button>
          <button class="period-button" type="button" @click="confirm('Date range is fixture-only')">
            <CalendarDays :size="16" aria-hidden="true" />
            <span class="desktop-only">Jun 22 - Jul 21</span>
          </button>
        </div>
      </div>

      <div class="filter-bar">
        <label class="search-field">
          <span class="sr-only">Search feedback</span>
          <Search :size="16" aria-hidden="true" />
          <input v-model="filters.query" type="search" placeholder="Search evidence" />
          <button v-if="filters.query" type="button" title="Clear search" @click="filters.query = ''">
            <X :size="15" aria-hidden="true" />
            <span class="sr-only">Clear search</span>
          </button>
        </label>

        <label>
          <span class="sr-only">Project</span>
          <select v-model="filters.project">
            <option value="all">All projects</option>
            <option v-for="project in projects" :key="project.slug" :value="project.slug">{{ project.name }}</option>
          </select>
        </label>

        <label>
          <span class="sr-only">Sentiment</span>
          <select v-model="filters.sentiment">
            <option value="all">All sentiment</option>
            <option value="positive">Positive</option>
            <option value="neutral">Neutral</option>
            <option value="negative">Negative</option>
          </select>
        </label>

        <button class="more-filter-button" type="button" :aria-expanded="moreFiltersOpen" @click="moreFiltersOpen = !moreFiltersOpen">
          <Filter :size="16" aria-hidden="true" />
          Filters
          <span v-if="activeFilterCount" class="filter-count">{{ activeFilterCount }}</span>
        </button>

        <button v-if="activeFilterCount" class="reset-button" type="button" @click="resetFilters">Reset</button>
      </div>

      <div v-if="moreFiltersOpen" class="expanded-filters">
        <label class="mobile-only-filter">Project
          <select v-model="filters.project">
            <option value="all">All projects</option>
            <option v-for="project in projects" :key="project.slug" :value="project.slug">{{ project.name }}</option>
          </select>
        </label>
        <label class="mobile-only-filter">Sentiment
          <select v-model="filters.sentiment">
            <option value="all">All sentiment</option>
            <option value="positive">Positive</option>
            <option value="neutral">Neutral</option>
            <option value="negative">Negative</option>
          </select>
        </label>
        <label>Status
          <select v-model="filters.status">
            <option value="all">Any status</option>
            <option value="new">New</option>
            <option value="reviewed">Reviewed</option>
            <option value="linked">Linked</option>
            <option value="resolved">Resolved</option>
          </select>
        </label>
        <label>Rating
          <select v-model="filters.rating">
            <option value="all">Any rating</option>
            <option v-for="rating in 5" :key="rating" :value="String(rating)">{{ rating }} out of 5</option>
          </select>
        </label>
        <label>Evidence
          <select v-model="filters.attachment">
            <option value="all">Any evidence</option>
            <option value="screenshot">Screenshot</option>
            <option value="audio">Audio</option>
            <option value="video">Video</option>
            <option value="replay">Replay</option>
          </select>
        </label>
        <label>Moderation
          <select v-model="filters.moderation">
            <option value="all">Any moderation</option>
            <option value="flagged">Flagged</option>
            <option value="clear">Clear</option>
          </select>
        </label>
        <label>Issue
          <select v-model="filters.issue">
            <option value="all">Any issue state</option>
            <option value="linked">Linked</option>
            <option value="unlinked">Unlinked</option>
          </select>
        </label>
        <label>Date
          <select v-model="filters.date">
            <option value="today">Today</option>
            <option value="7d">Last 7 days</option>
            <option value="30d">Last 30 days</option>
          </select>
        </label>
        <label>Sort
          <select v-model="filters.sort">
            <option value="newest">Newest first</option>
            <option value="oldest">Oldest first</option>
            <option value="rating-low">Rating low-high</option>
            <option value="rating-high">Rating high-low</option>
          </select>
        </label>
      </div>

      <div class="topic-filters" aria-label="Filter by topic">
        <span>Topics</span>
        <button
          v-for="[topic, count] in topicCounts"
          :key="topic"
          type="button"
          :data-active="filters.query === topic"
          @click="filterTopic(topic)"
        >
          {{ topic }} <span>{{ count }}</span>
        </button>
      </div>

      <section class="chronology" aria-label="Feedback evidence">
        <div class="chronology-head desktop-only" aria-hidden="true">
          <span>Received</span>
          <span>Signal</span>
          <span>Topic</span>
          <span>Sentiment</span>
          <span>Project</span>
          <span>Evidence</span>
        </div>

        <button
          v-for="item in visibleItems"
          :key="item.id"
          class="evidence-row"
          type="button"
          :data-selected="focusOpen && selectedId === item.id"
          @click="openEvidence(item)"
        >
          <span class="received-cell">
            <strong>{{ item.age }}</strong>
            <span>{{ item.id }}</span>
          </span>
          <span class="signal-cell">
            <RatingMark :rating="item.rating" compact />
            <span>
              <strong>{{ item.excerpt }}</strong>
              <small>{{ item.page }}</small>
            </span>
          </span>
          <span class="topic-cell">{{ item.topic }}</span>
          <span class="sentiment-cell" :data-sentiment="item.sentiment">
            <i /> {{ item.sentiment }}
          </span>
          <span class="project-cell">{{ item.project }}</span>
          <span class="attachment-cell"><AttachmentIcons :attachments="item.attachments" /></span>
        </button>

        <div v-if="visibleItems.length === 0" class="empty-evidence">
          <Search :size="24" aria-hidden="true" />
          <strong>No evidence matches these filters</strong>
          <button type="button" @click="resetFilters">Reset filters</button>
        </div>
      </section>
    </section>

    <section v-else class="observatory-body detail-view">
      <div class="detail-heading">
        <NuxtLink to="/designs/signal-observatory/feedback" class="back-link">
          <ArrowLeft :size="16" aria-hidden="true" /> Feedback chronology
        </NuxtLink>
        <div class="detail-title-row">
          <div>
            <span class="section-label">{{ selected.id }} · {{ selected.project }}</span>
            <h1>{{ selected.topic }}</h1>
          </div>
          <div class="detail-actions">
            <button class="secondary-button" type="button" @click="confirm('Feedback marked as reviewed locally')">Mark reviewed</button>
            <button class="primary-button" type="button" @click="confirm('Issue draft created locally')">Create issue</button>
          </div>
        </div>
      </div>

      <div class="detail-layout">
        <div class="detail-evidence">
          <section class="feedback-statement" aria-labelledby="statement-heading">
            <div class="statement-meta">
              <RatingMark :rating="selected.rating" />
              <span class="status-pill" :class="`status-${selected.status}`">{{ statusLabel(selected.status) }}</span>
              <span class="sentiment-cell" :data-sentiment="selected.sentiment"><i /> {{ selected.sentiment }}</span>
            </div>
            <h2 id="statement-heading">“{{ selected.cleanedText }}”</h2>
            <p>{{ selected.reporter }} · {{ selected.age }} ago</p>
          </section>

          <section class="evidence-section" aria-labelledby="visual-evidence-heading">
            <div class="evidence-section-head">
              <div>
                <span class="section-label">Visual evidence</span>
                <h2 id="visual-evidence-heading">Storefront checkout</h2>
              </div>
              <span v-if="selected.replayReady" class="status-pill status-ready">Replay ready</span>
            </div>
            <ProductScene :replay="selected.replayReady" />
          </section>

          <section class="transcript-section" aria-labelledby="transcript-heading">
            <div>
              <span class="section-label">Reporter transcript</span>
              <h2 id="transcript-heading">Customer context</h2>
            </div>
            <p v-if="selected.transcript">{{ selected.transcript }}</p>
            <p v-else class="muted">No audio or video transcript attached.</p>
          </section>
        </div>

        <aside class="detail-metadata" aria-label="Feedback metadata">
          <div class="metadata-block">
            <span class="section-label">Signal</span>
            <dl>
              <div><dt>Sentiment score</dt><dd>{{ selected.sentimentScore }}</dd></div>
              <div><dt>AI processing</dt><dd><span class="status-pill" :class="`status-${selected.aiStatus}`">{{ selected.aiStatus }}</span></dd></div>
              <div><dt>Moderation</dt><dd>{{ selected.moderationFlags.length ? selected.moderationFlags.join(', ') : 'Clear' }}</dd></div>
              <div><dt>Linked issue</dt><dd>{{ selected.issue ?? 'Unlinked' }}</dd></div>
            </dl>
          </div>
          <div class="metadata-block">
            <span class="section-label">Environment</span>
            <dl>
              <div><dt>Page</dt><dd>{{ selected.page }}</dd></div>
              <div><dt>Browser</dt><dd>{{ selected.browser }}</dd></div>
              <div><dt>Device</dt><dd>{{ selected.device }}</dd></div>
              <div><dt>Viewport</dt><dd>{{ selected.viewport }}</dd></div>
            </dl>
          </div>
          <div class="metadata-block">
            <span class="section-label">Reporter</span>
            <dl>
              <div><dt>Name</dt><dd>{{ selected.reporter }}</dd></div>
              <div><dt>Email</dt><dd>{{ selected.email || 'Not provided' }}</dd></div>
            </dl>
          </div>
        </aside>
      </div>
    </section>

    <Transition name="focus-pane">
      <aside v-if="focusOpen && view !== 'feedback-detail'" class="focus-pane" aria-label="Selected feedback evidence">
        <header class="focus-header">
          <div>
            <span class="section-label">Selected evidence</span>
            <strong>{{ selected.id }} · {{ selected.project }}</strong>
          </div>
          <button class="icon-button" type="button" title="Close focus pane" @click="focusOpen = false">
            <PanelRightClose :size="19" aria-hidden="true" />
            <span class="sr-only">Close focus pane</span>
          </button>
        </header>

        <div class="focus-scroll">
          <div class="focus-status">
            <RatingMark :rating="selected.rating" />
            <span class="status-pill" :class="`status-${selected.status}`">{{ statusLabel(selected.status) }}</span>
            <span class="sentiment-cell" :data-sentiment="selected.sentiment"><i /> {{ selected.sentiment }}</span>
          </div>

          <div class="focus-copy">
            <span class="section-label">{{ selected.topic }}</span>
            <h2>{{ selected.cleanedText }}</h2>
            <p>{{ selected.reporter }} · {{ selected.age }} ago</p>
          </div>

          <ProductScene v-if="selected.attachments.includes('screenshot') || selected.replayReady" :replay="selected.replayReady" />

          <div class="focus-metadata">
            <div><span>Page</span><strong>{{ selected.page }}</strong></div>
            <div><span>Environment</span><strong>{{ selected.browser }} · {{ selected.device }}</strong></div>
            <div><span>Issue</span><strong>{{ selected.issue ?? 'Unlinked' }}</strong></div>
            <div><span>AI processing</span><strong>{{ selected.aiStatus }}</strong></div>
          </div>
        </div>

        <footer class="focus-actions">
          <button class="secondary-button" type="button" @click="confirm('Feedback marked as reviewed locally')">Mark reviewed</button>
          <NuxtLink class="primary-button" to="/designs/signal-observatory/feedback-detail">Open full detail</NuxtLink>
        </footer>
      </aside>
    </Transition>

    <div v-if="notice" class="prototype-toast" role="status">{{ notice }}</div>
  </main>
</template>

<style scoped>
.observatory {
  --so-canvas: #f4f7f8;
  --so-surface: #fff;
  --so-ink: #152321;
  --so-muted: #637370;
  --so-line: #d7e0e2;
  --so-grid: #e8edef;
  min-height: calc(100vh - 48px);
  background: var(--so-canvas);
  color: var(--so-ink);
  font-variant-numeric: tabular-nums;
}

.product-bar {
  display: flex;
  min-height: 54px;
  align-items: center;
  padding: 0 28px;
  background: #fff;
  border-bottom: 1px solid var(--so-line);
}

.product-mark,
.tenant-context,
.product-actions,
.rail-links,
.rail-links a,
.rail-links button,
.rail-scope button,
.period-controls,
.period-button,
.eyebrow,
.analysis-state,
.register-heading a,
.back-link,
.statement-meta,
.detail-actions {
  display: flex;
  align-items: center;
}

.product-mark {
  gap: 9px;
  padding-right: 22px;
}

.product-symbol {
  display: grid;
  width: 28px;
  height: 28px;
  place-items: center;
  background: #163f3a;
  color: #65d4c1;
  border-radius: 5px;
}

.product-wordmark {
  font-size: 16px;
  font-weight: 800;
}

.tenant-context {
  min-height: 32px;
  gap: 7px;
  padding-left: 22px;
  border-left: 1px solid var(--so-line);
  color: #3f4f4c;
  font-size: 13px;
  font-weight: 650;
}

.product-actions {
  gap: 4px;
  margin-left: auto;
}

.trial-state {
  margin-right: 10px;
  color: var(--so-muted);
  font-size: 12px;
}

.account-avatar {
  display: grid;
  width: 32px;
  height: 32px;
  margin-left: 5px;
  padding: 0;
  place-items: center;
  background: #e6efed;
  color: #25554f;
  border: 1px solid #cbdad7;
  border-radius: 50%;
  font-size: 11px;
  font-weight: 800;
}

.product-rail {
  display: flex;
  min-height: 46px;
  align-items: stretch;
  padding: 0 28px;
  background: #fff;
  border-bottom: 1px solid var(--so-line);
}

.rail-links {
  min-width: 0;
  gap: 3px;
}

.rail-links a,
.rail-links button {
  position: relative;
  min-height: 45px;
  gap: 7px;
  padding: 0 13px;
  background: transparent;
  color: var(--so-muted);
  border: 0;
  font-size: 13px;
  font-weight: 650;
}

.rail-links a::after,
.rail-links button::after {
  position: absolute;
  right: 12px;
  bottom: -1px;
  left: 12px;
  height: 2px;
  background: transparent;
  content: '';
}

.rail-links a[data-active='true'] {
  color: var(--so-ink);
}

.rail-links a[data-active='true']::after {
  background: var(--teal);
}

.rail-scope {
  display: flex;
  align-items: center;
  margin-left: auto;
}

.rail-scope button {
  min-height: 34px;
  gap: 8px;
  padding: 0 10px;
  background: #f6f8f8;
  border: 1px solid var(--so-line);
  border-radius: 5px;
  font-size: 12px;
  font-weight: 650;
}

.observatory-body {
  width: min(100%, 1500px);
  margin: 0 auto;
  padding: 30px 36px 54px;
}

.view-heading {
  display: flex;
  min-height: 74px;
  align-items: flex-start;
  justify-content: space-between;
  gap: 24px;
  margin-bottom: 24px;
}

.view-heading h1,
.detail-title-row h1 {
  margin-top: 5px;
  font-size: 25px;
  font-weight: 740;
  line-height: 1.15;
}

.view-heading > div:first-child > p:last-child {
  margin-top: 6px;
  color: var(--so-muted);
}

.eyebrow {
  gap: 7px;
  color: #49635f;
  font-size: 11px;
  font-weight: 750;
  text-transform: uppercase;
}

.live-dot {
  width: 7px;
  height: 7px;
  background: #2c9a6a;
  border: 2px solid #dff2e8;
  border-radius: 50%;
  box-sizing: content-box;
}

.period-controls {
  gap: 8px;
}

.range-control {
  display: flex;
  height: 36px;
  padding: 3px;
  background: #e9eeee;
  border-radius: 5px;
}

.range-control button {
  min-width: 48px;
  padding: 0 8px;
  background: transparent;
  color: var(--so-muted);
  border: 0;
  border-radius: 4px;
  font-size: 11px;
  font-weight: 750;
}

.range-control button[data-active='true'] {
  background: #fff;
  color: var(--so-ink);
  box-shadow: 0 1px 2px rgb(19 42 38 / 8%);
}

.period-button {
  min-height: 36px;
  gap: 8px;
  padding: 0 11px;
  background: #fff;
  border: 1px solid var(--so-line);
  border-radius: 5px;
  color: #3c4b49;
  font-size: 12px;
  font-weight: 650;
}

.mobile-track-control {
  display: none;
}

.signal-canvas {
  background: #fff;
  border: 1px solid var(--so-line);
  border-radius: 6px;
}

.canvas-head {
  display: grid;
  min-height: 66px;
  grid-template-columns: 248px minmax(0, 1fr);
  align-items: center;
  padding: 0 20px;
  border-bottom: 1px solid var(--so-line);
}

.canvas-head h2,
.register-heading h2,
.evidence-section-head h2,
.transcript-section h2 {
  margin-top: 3px;
  font-size: 16px;
  font-weight: 720;
}

.axis-key {
  display: flex;
  justify-content: space-between;
  color: #879390;
  font-size: 10px;
}

.signal-track {
  display: grid;
  width: 100%;
  min-height: 150px;
  grid-template-columns: 248px minmax(0, 1fr);
  align-items: stretch;
  padding: 0 20px;
  background: #fff;
  border: 0;
  border-bottom: 1px solid var(--so-line);
  text-align: left;
}

.signal-track:last-child {
  border-bottom: 0;
}

.signal-track:hover,
.signal-track[data-active='true'] {
  background: #fbfdfd;
}

.track-summary {
  display: flex;
  flex-direction: column;
  justify-content: center;
  padding-right: 28px;
}

.track-name {
  margin-bottom: 9px;
  color: var(--so-muted);
  font-size: 12px;
  font-weight: 680;
}

.track-value {
  color: #112a26;
  font-size: 26px;
  font-weight: 760;
  line-height: 1;
}

.track-unit,
.track-context {
  color: #7b8986;
  font-size: 10px;
}

.track-unit {
  margin-top: 4px;
}

.track-delta {
  display: inline-flex;
  width: max-content;
  align-items: center;
  gap: 4px;
  margin-top: 11px;
  color: #247c55;
  font-size: 11px;
  font-weight: 750;
}

.track-delta[data-direction='down'] {
  color: #b23a46;
}

.chart-wrap {
  display: flex;
  min-width: 0;
  align-items: center;
  border-left: 1px solid var(--so-grid);
}

.chart-wrap svg {
  width: 100%;
  height: 108px;
  overflow: visible;
}

.chart-wrap line {
  stroke: var(--so-grid);
  stroke-width: 1;
}

.chart-wrap polygon {
  opacity: 0.6;
}

.chart-wrap polyline {
  fill: none;
  stroke-width: 2.4;
  vector-effect: non-scaling-stroke;
}

.movement-register,
.topic-band {
  margin-top: 26px;
  background: #fff;
  border: 1px solid var(--so-line);
  border-radius: 6px;
}

.register-heading {
  display: flex;
  min-height: 70px;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 0 20px;
  border-bottom: 1px solid var(--so-line);
}

.analysis-state {
  gap: 6px;
  color: var(--indigo);
  font-size: 11px;
  font-weight: 650;
}

.movement-list button {
  display: grid;
  width: 100%;
  min-height: 74px;
  grid-template-columns: 34px 38px minmax(0, 1fr) auto 20px;
  align-items: center;
  gap: 12px;
  padding: 10px 20px;
  background: #fff;
  border: 0;
  border-bottom: 1px solid var(--so-line);
  text-align: left;
}

.movement-list button:last-child {
  border-bottom: 0;
}

.movement-list button:hover {
  background: #f7faf9;
}

.movement-index {
  color: #98a29f;
  font-size: 10px;
}

.movement-icon {
  display: grid;
  width: 32px;
  height: 32px;
  place-items: center;
  background: var(--red-soft);
  color: var(--red);
  border-radius: 5px;
}

.movement-list button[data-tone='positive'] .movement-icon {
  background: var(--green-soft);
  color: var(--green);
}

.movement-list button[data-tone='warning'] .movement-icon {
  background: var(--amber-soft);
  color: var(--amber);
}

.movement-copy {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 3px;
}

.movement-copy strong {
  font-size: 13px;
}

.movement-copy span {
  overflow: hidden;
  color: var(--so-muted);
  font-size: 12px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.evidence-count {
  color: var(--so-muted);
  font-size: 11px;
  font-weight: 650;
}

.movement-arrow,
.register-heading a svg {
  transform: rotate(45deg);
}

.movement-arrow {
  color: #71807d;
}

.register-heading a {
  gap: 7px;
  color: var(--blue);
  font-size: 12px;
  font-weight: 680;
}

.topic-spectrum {
  padding: 12px 20px 16px;
}

.topic-spectrum button {
  display: grid;
  width: 100%;
  min-height: 40px;
  grid-template-columns: 160px minmax(0, 1fr) 28px;
  align-items: center;
  gap: 14px;
  padding: 0;
  background: transparent;
  border: 0;
  color: #40504d;
  font-size: 12px;
  text-align: left;
}

.topic-line {
  height: 5px;
  overflow: hidden;
  background: #edf1f2;
  border-radius: 2px;
}

.topic-line i {
  display: block;
  width: var(--topic-width);
  height: 100%;
  background: #549b91;
}

.topic-spectrum button:hover .topic-line i {
  background: var(--teal);
}

.filter-bar {
  display: flex;
  min-height: 62px;
  align-items: center;
  gap: 8px;
  padding: 10px 12px;
  background: #fff;
  border: 1px solid var(--so-line);
  border-bottom: 0;
  border-radius: 6px 6px 0 0;
}

.search-field {
  display: flex;
  width: min(340px, 32vw);
  min-height: 40px;
  align-items: center;
  gap: 8px;
  padding: 0 10px;
  background: #f7f9f9;
  border: 1px solid var(--so-line);
  border-radius: 5px;
}

.search-field svg {
  color: #71807d;
}

.search-field input {
  width: 100%;
  min-width: 0;
  background: transparent;
  border: 0;
  outline: 0;
}

.search-field button {
  display: grid;
  width: 28px;
  height: 28px;
  padding: 0;
  place-items: center;
  background: transparent;
  border: 0;
}

.filter-bar select,
.expanded-filters select {
  min-height: 40px;
  padding: 0 30px 0 10px;
  background: #fff;
  border: 1px solid var(--so-line);
  border-radius: 5px;
  font-size: 12px;
}

.more-filter-button,
.reset-button {
  display: inline-flex;
  min-height: 40px;
  align-items: center;
  gap: 7px;
  padding: 0 11px;
  background: #fff;
  border: 1px solid var(--so-line);
  border-radius: 5px;
  font-size: 12px;
  font-weight: 650;
}

.filter-count {
  display: grid;
  min-width: 20px;
  height: 20px;
  padding: 0 5px;
  place-items: center;
  background: var(--teal-soft);
  color: #096a61;
  border-radius: 10px;
  font-size: 10px;
}

.reset-button {
  color: var(--blue);
  border-color: transparent;
}

.expanded-filters {
  display: grid;
  grid-template-columns: repeat(6, minmax(120px, 1fr));
  gap: 10px;
  padding: 14px;
  background: #f8fafa;
  border: 1px solid var(--so-line);
  border-bottom: 0;
}

.expanded-filters label {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 5px;
  color: var(--so-muted);
  font-size: 10px;
  font-weight: 700;
  text-transform: uppercase;
}

.expanded-filters .mobile-only-filter {
  display: none;
}

.expanded-filters select {
  width: 100%;
  min-width: 0;
  text-transform: none;
}

.topic-filters {
  display: flex;
  min-height: 48px;
  align-items: center;
  gap: 7px;
  padding: 7px 14px;
  overflow-x: auto;
  background: #fff;
  border: 1px solid var(--so-line);
  border-bottom: 0;
  scrollbar-width: none;
}

.topic-filters > span {
  margin-right: 3px;
  color: var(--so-muted);
  font-size: 10px;
  font-weight: 750;
  text-transform: uppercase;
}

.topic-filters button {
  min-height: 30px;
  padding: 0 9px;
  background: #f6f8f8;
  border: 1px solid var(--so-line);
  border-radius: 4px;
  color: #43524f;
  font-size: 11px;
  white-space: nowrap;
}

.topic-filters button span {
  margin-left: 3px;
  color: #889491;
}

.topic-filters button[data-active='true'] {
  background: var(--teal-soft);
  color: #086a61;
  border-color: #a5d9d0;
}

.chronology {
  background: #fff;
  border: 1px solid var(--so-line);
  border-radius: 0 0 6px 6px;
}

.chronology-head,
.evidence-row {
  display: grid;
  grid-template-columns: 90px minmax(260px, 1.8fr) minmax(120px, 0.7fr) 105px minmax(112px, 0.65fr) 118px;
  gap: 12px;
  align-items: center;
}

.chronology-head {
  min-height: 38px;
  padding: 0 14px;
  color: #7d8987;
  border-bottom: 1px solid var(--so-line);
  font-size: 10px;
  font-weight: 750;
  text-transform: uppercase;
}

.evidence-row {
  width: 100%;
  min-height: 70px;
  padding: 9px 14px;
  background: #fff;
  border: 0;
  border-bottom: 1px solid var(--so-line);
  text-align: left;
}

.evidence-row:last-of-type {
  border-bottom: 0;
}

.evidence-row:hover,
.evidence-row[data-selected='true'] {
  background: #f0f7f5;
  box-shadow: inset 3px 0 var(--teal);
}

.received-cell,
.signal-cell > span,
.focus-copy,
.focus-header > div {
  display: flex;
  min-width: 0;
  flex-direction: column;
}

.received-cell strong {
  font-size: 11px;
}

.received-cell span,
.signal-cell small {
  margin-top: 3px;
  color: #85918e;
  font-size: 10px;
}

.signal-cell {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 10px;
}

.signal-cell strong {
  overflow: hidden;
  font-size: 12px;
  font-weight: 670;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.topic-cell,
.project-cell {
  overflow: hidden;
  color: #485854;
  font-size: 11px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.sentiment-cell {
  display: inline-flex;
  width: max-content;
  align-items: center;
  gap: 6px;
  color: #5f6e6b;
  font-size: 11px;
  text-transform: capitalize;
}

.sentiment-cell i {
  width: 7px;
  height: 7px;
  background: #8a9693;
  border-radius: 50%;
}

.sentiment-cell[data-sentiment='positive'] i {
  background: var(--green);
}

.sentiment-cell[data-sentiment='negative'] i {
  background: var(--red);
}

.empty-evidence {
  display: flex;
  min-height: 220px;
  align-items: center;
  justify-content: center;
  flex-direction: column;
  gap: 10px;
  color: var(--so-muted);
}

.empty-evidence button {
  min-height: 36px;
  padding: 0 12px;
  background: #fff;
  color: var(--blue);
  border: 1px solid var(--so-line);
  border-radius: 5px;
}

.detail-heading {
  margin-bottom: 26px;
}

.back-link {
  width: max-content;
  min-height: 36px;
  gap: 8px;
  color: var(--blue);
  font-size: 12px;
  font-weight: 670;
}

.detail-title-row {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 20px;
  padding-top: 15px;
}

.detail-actions {
  gap: 8px;
}

.detail-layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 320px;
  gap: 24px;
}

.detail-evidence,
.detail-metadata {
  background: #fff;
  border: 1px solid var(--so-line);
  border-radius: 6px;
}

.feedback-statement,
.evidence-section,
.transcript-section,
.metadata-block {
  padding: 24px;
  border-bottom: 1px solid var(--so-line);
}

.feedback-statement:last-child,
.evidence-section:last-child,
.transcript-section:last-child,
.metadata-block:last-child {
  border-bottom: 0;
}

.statement-meta {
  gap: 8px;
}

.feedback-statement h2 {
  max-width: 840px;
  margin-top: 19px;
  font-size: 20px;
  font-weight: 660;
  line-height: 1.45;
}

.feedback-statement p {
  margin-top: 12px;
  color: var(--so-muted);
  font-size: 12px;
}

.evidence-section-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 16px;
}

.transcript-section {
  display: grid;
  grid-template-columns: 180px minmax(0, 1fr);
  gap: 26px;
}

.transcript-section > p {
  padding-left: 22px;
  color: #42524f;
  border-left: 2px solid #acd7d0;
  line-height: 1.7;
}

.detail-metadata {
  align-self: start;
}

.metadata-block dl {
  margin: 12px 0 0;
}

.metadata-block dl > div {
  display: grid;
  min-height: 43px;
  grid-template-columns: 1fr auto;
  align-items: center;
  gap: 12px;
  border-bottom: 1px solid #edf1f1;
}

.metadata-block dl > div:last-child {
  border-bottom: 0;
}

.metadata-block dt {
  color: var(--so-muted);
  font-size: 11px;
}

.metadata-block dd {
  max-width: 180px;
  margin: 0;
  overflow-wrap: anywhere;
  font-size: 11px;
  font-weight: 680;
  text-align: right;
}

.focus-pane {
  position: fixed;
  z-index: 70;
  top: 48px;
  right: 0;
  bottom: 0;
  display: grid;
  width: clamp(480px, 42vw, 680px);
  grid-template-rows: auto minmax(0, 1fr) auto;
  background: #fff;
  border-left: 1px solid var(--so-line);
  box-shadow: -20px 0 45px rgb(28 52 48 / 13%);
}

.focus-header {
  display: flex;
  min-height: 64px;
  align-items: center;
  justify-content: space-between;
  padding: 8px 18px 8px 22px;
  border-bottom: 1px solid var(--so-line);
}

.focus-header strong {
  margin-top: 4px;
  font-size: 13px;
}

.focus-scroll {
  min-height: 0;
  padding: 22px;
  overflow-y: auto;
}

.focus-status {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}

.focus-copy {
  gap: 7px;
  padding: 22px 0;
}

.focus-copy h2 {
  font-size: 19px;
  font-weight: 660;
  line-height: 1.45;
}

.focus-copy p {
  color: var(--so-muted);
  font-size: 12px;
}

.focus-metadata {
  margin-top: 20px;
  border-top: 1px solid var(--so-line);
}

.focus-metadata > div {
  display: grid;
  min-height: 48px;
  grid-template-columns: 110px minmax(0, 1fr);
  align-items: center;
  gap: 16px;
  border-bottom: 1px solid var(--so-line);
  font-size: 11px;
}

.focus-metadata span {
  color: var(--so-muted);
}

.focus-metadata strong {
  overflow-wrap: anywhere;
}

.focus-actions {
  display: flex;
  min-height: 68px;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
  padding: 12px 18px;
  background: #f8fafa;
  border-top: 1px solid var(--so-line);
}

.focus-pane-enter-active,
.focus-pane-leave-active {
  transition: transform 180ms ease, opacity 180ms ease;
}

.focus-pane-enter-from,
.focus-pane-leave-to {
  opacity: 0;
  transform: translateX(22px);
}

@media (max-width: 980px) {
  .observatory-body {
    padding-right: 22px;
    padding-left: 22px;
  }

  .expanded-filters {
    grid-template-columns: repeat(3, minmax(130px, 1fr));
  }

  .chronology-head,
  .evidence-row {
    grid-template-columns: 80px minmax(230px, 1.5fr) minmax(110px, 0.65fr) 95px 112px;
  }

  .attachment-cell,
  .chronology-head span:last-child {
    display: none;
  }

  .detail-layout {
    grid-template-columns: minmax(0, 1fr) 280px;
  }
}

@media (max-width: 720px) {
  .observatory {
    min-height: calc(100vh - 86px);
  }

  .product-bar {
    min-height: 50px;
    padding: 0 14px;
  }

  .product-mark {
    padding-right: 12px;
  }

  .product-wordmark {
    display: none;
  }

  .tenant-context {
    padding-left: 12px;
  }

  .product-rail {
    min-height: 44px;
    padding: 0 10px;
    overflow-x: auto;
  }

  .rail-links {
    flex: 1;
  }

  .rail-links a,
  .rail-links button {
    min-height: 43px;
    padding: 0 9px;
  }

  .rail-links button:last-child,
  .rail-scope {
    display: none;
  }

  .observatory-body {
    padding: 20px 14px 88px;
  }

  .view-heading {
    min-height: 0;
    align-items: stretch;
    flex-direction: column;
    gap: 16px;
    margin-bottom: 18px;
  }

  .view-heading h1,
  .detail-title-row h1 {
    font-size: 21px;
  }

  .period-controls {
    justify-content: space-between;
  }

  .range-control {
    flex: 1;
  }

  .range-control button {
    flex: 1;
  }

  .mobile-track-control {
    display: flex;
    margin-bottom: 10px;
    padding: 3px;
    overflow-x: auto;
    background: #e5ebeb;
    border-radius: 5px;
  }

  .mobile-track-control button {
    min-width: max-content;
    min-height: 36px;
    flex: 1;
    padding: 0 10px;
    background: transparent;
    border: 0;
    border-radius: 4px;
    color: var(--so-muted);
    font-size: 11px;
    font-weight: 680;
  }

  .mobile-track-control button[data-active='true'] {
    background: #fff;
    color: var(--so-ink);
  }

  .canvas-head {
    min-height: 58px;
    grid-template-columns: 1fr;
    padding: 0 14px;
  }

  .axis-key {
    display: none;
  }

  .signal-track {
    display: none;
    min-height: 262px;
    grid-template-columns: 1fr;
    padding: 0 14px 14px;
  }

  .signal-track[data-active='true'] {
    display: grid;
  }

  .track-summary {
    padding: 15px 0 4px;
  }

  .track-name {
    margin-bottom: 7px;
  }

  .track-value {
    font-size: 24px;
  }

  .chart-wrap {
    min-height: 110px;
    border-top: 1px solid var(--so-grid);
    border-left: 0;
  }

  .movement-register,
  .topic-band {
    margin-top: 18px;
  }

  .register-heading {
    min-height: 64px;
    padding: 10px 14px;
  }

  .analysis-state,
  .register-heading a {
    display: none;
  }

  .movement-list button {
    min-height: 76px;
    grid-template-columns: 32px minmax(0, 1fr) 16px;
    gap: 10px;
    padding: 10px 14px;
  }

  .movement-index,
  .evidence-count {
    display: none;
  }

  .movement-copy span {
    white-space: normal;
  }

  .topic-spectrum {
    padding: 8px 14px 12px;
  }

  .topic-spectrum button {
    grid-template-columns: 112px minmax(0, 1fr) 20px;
    gap: 8px;
  }

  .filter-bar {
    display: grid;
    grid-template-columns: 1fr auto;
    border-radius: 6px 6px 0 0;
  }

  .search-field {
    width: 100%;
    grid-column: 1 / -1;
  }

  .filter-bar > label:not(.search-field) {
    display: none;
  }

  .reset-button {
    justify-self: end;
  }

  .expanded-filters {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .expanded-filters .mobile-only-filter {
    display: flex;
  }

  .topic-filters {
    margin: 0;
  }

  .chronology {
    border-radius: 0 0 6px 6px;
  }

  .evidence-row {
    position: relative;
    display: grid;
    min-height: 126px;
    grid-template-columns: minmax(0, 1fr) auto;
    grid-template-areas:
      'signal signal'
      'received sentiment'
      'topic project';
    gap: 8px 12px;
    padding: 14px;
  }

  .received-cell {
    grid-area: received;
    flex-direction: row;
    align-items: center;
    gap: 7px;
  }

  .received-cell span {
    margin: 0;
  }

  .signal-cell {
    grid-area: signal;
    align-items: flex-start;
  }

  .signal-cell strong {
    display: -webkit-box;
    overflow: hidden;
    text-overflow: initial;
    white-space: normal;
    -webkit-box-orient: vertical;
    -webkit-line-clamp: 2;
  }

  .topic-cell {
    grid-area: topic;
  }

  .sentiment-cell {
    grid-area: sentiment;
    justify-self: end;
  }

  .project-cell {
    grid-area: project;
    justify-self: end;
  }

  .attachment-cell {
    display: none;
  }

  .detail-heading {
    margin-bottom: 18px;
  }

  .detail-title-row {
    align-items: stretch;
    flex-direction: column;
  }

  .detail-actions {
    display: grid;
    grid-template-columns: 1fr 1fr;
  }

  .detail-layout {
    grid-template-columns: 1fr;
    gap: 14px;
  }

  .feedback-statement,
  .evidence-section,
  .transcript-section,
  .metadata-block {
    padding: 18px;
  }

  .feedback-statement h2 {
    font-size: 17px;
  }

  .transcript-section {
    grid-template-columns: 1fr;
    gap: 14px;
  }

  .transcript-section > p {
    padding: 0 0 0 14px;
  }

  .focus-pane {
    z-index: 90;
    top: 86px;
    width: 100vw;
    border-left: 0;
  }

  .focus-scroll {
    padding: 18px 14px;
  }

  .focus-actions {
    display: grid;
    grid-template-columns: 1fr 1fr;
  }
}

@media (prefers-reduced-motion: reduce) {
  .focus-pane-enter-active,
  .focus-pane-leave-active {
    transition: none;
  }
}
</style>
