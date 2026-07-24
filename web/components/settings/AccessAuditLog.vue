<script setup lang="ts">
import { AlertTriangle, Download, ShieldAlert, ShieldCheck } from '@lucide/vue'
import type { AccessEvent, AccessEventPage, AccessEventSignal } from '~/types/api'
import { triggerBlobDownload } from '~/utils/download'

const { $apiFetch } = useNuxtApp()
const { can } = useAuth()
const { t } = useI18n()
const localizedError = useLocalizedAPIError()
const { dateTime, integer } = useRegionalFormat()

const canReadAudit = computed(() => can('audit:read'))

const resourceType = ref('')
const actorType = ref('')
const outcome = ref('')
const dateFrom = ref('')
const dateTo = ref('')
const exportPending = ref(false)
const exportError = ref('')

const supportAccessOnly = computed({
  get: () => actorType.value === 'platform_admin',
  set: (value: boolean) => { actorType.value = value ? 'platform_admin' : '' },
})

function buildQuery(cursor?: string) {
  const query: Record<string, string> = {}
  if (resourceType.value) query.resource_type = resourceType.value
  if (actorType.value) query.actor_type = actorType.value
  if (outcome.value) query.outcome = outcome.value
  if (dateFrom.value) query.from = new Date(dateFrom.value).toISOString()
  if (dateTo.value) query.to = new Date(dateTo.value).toISOString()
  if (cursor) query.cursor = cursor
  return query
}

const filterKey = computed(() => JSON.stringify(buildQuery()))

const { data, status, error, refresh } = await useAsyncData('access-audit-log', async () => {
  if (!canReadAudit.value) return { events: [], signals: [], pagination: { has_more: false, limit: 50 } } satisfies AccessEventPage
  return $apiFetch<AccessEventPage>('/v1/access-events', { query: buildQuery() })
}, { watch: [filterKey] })

const events = ref<AccessEvent[]>([])
const nextCursor = ref<string | undefined>(undefined)
const hasMore = ref(false)
const loadingMore = ref(false)

watch(data, (value) => {
  events.value = value?.events || []
  nextCursor.value = value?.pagination.next_cursor
  hasMore.value = Boolean(value?.pagination.has_more)
}, { immediate: true })

async function loadMore() {
  if (!nextCursor.value || loadingMore.value) return
  loadingMore.value = true
  try {
    const page = await $apiFetch<AccessEventPage>('/v1/access-events', { query: buildQuery(nextCursor.value) })
    events.value = [...events.value, ...page.events]
    nextCursor.value = page.pagination.next_cursor
    hasMore.value = page.pagination.has_more
  } catch (loadError) {
    exportError.value = localizedError.message(loadError, 'admin.accessAudit.error')
  } finally {
    loadingMore.value = false
  }
}

async function exportCSV() {
  if (exportPending.value) return
  exportPending.value = true
  exportError.value = ''
  try {
    const blob = await $apiFetch<Blob>('/v1/access-events', {
      query: { ...buildQuery(), format: 'csv' }, responseType: 'blob',
    })
    triggerBlobDownload(blob, 'fidibeki-access-events.csv')
  } catch (downloadError) {
    exportError.value = localizedError.message(downloadError, 'admin.accessAudit.error')
  } finally {
    exportPending.value = false
  }
}

function actorLabel(event: AccessEvent) {
  const id = event.actor_id || event.actor_member_id || event.actor_api_key_id || event.actor_platform_admin_id || ''
  const shortID = id ? id.slice(0, 8) : t('admin.accessAudit.unknownActor')
  return `${t(`admin.accessAudit.actorType.${event.actor_type}`)} · ${shortID}`
}

function isSupportAccess(event: AccessEvent) {
  return event.actor_type === 'platform_admin'
}

function supportCaseReference(event: AccessEvent) {
  const value = event.data?.support_case_reference
  return typeof value === 'string' && value ? value : t('admin.accessAudit.unknownActor')
}

function supportReason(event: AccessEvent) {
  const value = event.data?.support_reason
  return typeof value === 'string' ? value : ''
}

function outcomeTone(value: string) {
  if (value === 'success') return 'success'
  if (value === 'denied') return 'danger'
  return 'pending'
}

function resourceLabel(event: AccessEvent) {
  return event.resource_type ? t(`admin.accessAudit.resourceType.${event.resource_type}`) : '—'
}

function signalDetail(signal: AccessEventSignal) {
  if (signal.type === 'bulk_access') {
    return t('admin.accessAudit.signalDetail.bulkAccess', {
      distinct: integer(signal.distinct_resources ?? 0),
      denied: integer(signal.denied_count ?? 0),
    })
  }
  if (signal.type === 'support_access_volume') {
    return t('admin.accessAudit.signalDetail.supportAccessVolume', {
      distinct: integer(signal.distinct_resources ?? 0),
    })
  }
  return signal.detail ? t(`admin.accessAudit.signalDetail.${signal.detail}`) : '—'
}
</script>

<template>
  <div class="access-audit">
    <section v-if="!canReadAudit" class="app-surface access-audit-empty">
      <ShieldCheck :size="23" /><h3>{{ t('admin.accessAudit.accessRequired') }}</h3>
    </section>
    <template v-else>
      <section class="app-surface access-audit-section">
        <header>
          <div><p class="app-eyebrow">{{ t('admin.accessAudit.eyebrow') }}</p><h2>{{ t('admin.accessAudit.title') }}</h2></div>
          <button class="app-button" :disabled="exportPending" @click="exportCSV">
            <Download :size="15" />{{ exportPending ? t('admin.common.queuing') : t('admin.accessAudit.exportCSV') }}
          </button>
        </header>
        <div class="access-audit-filters">
          <label>{{ t('admin.accessAudit.filters.resourceType') }}
            <select v-model="resourceType" class="app-select">
              <option value="">{{ t('admin.accessAudit.filters.all') }}</option>
              <option value="feedback_attachment">{{ t('admin.accessAudit.resourceType.feedback_attachment') }}</option>
              <option value="replay_session">{{ t('admin.accessAudit.resourceType.replay_session') }}</option>
              <option value="privacy_export">{{ t('admin.accessAudit.resourceType.privacy_export') }}</option>
            </select>
          </label>
          <label>{{ t('admin.accessAudit.filters.actorType') }}
            <select v-model="actorType" class="app-select">
              <option value="">{{ t('admin.accessAudit.filters.all') }}</option>
              <option value="human">{{ t('admin.accessAudit.actorType.human') }}</option>
              <option value="api_key">{{ t('admin.accessAudit.actorType.api_key') }}</option>
              <option value="platform_admin">{{ t('admin.accessAudit.actorType.platform_admin') }}</option>
              <option value="system">{{ t('admin.accessAudit.actorType.system') }}</option>
            </select>
          </label>
          <label>{{ t('admin.accessAudit.filters.outcome') }}
            <select v-model="outcome" class="app-select">
              <option value="">{{ t('admin.accessAudit.filters.all') }}</option>
              <option value="success">{{ t('admin.accessAudit.outcome.success') }}</option>
              <option value="denied">{{ t('admin.accessAudit.outcome.denied') }}</option>
              <option value="not_found">{{ t('admin.accessAudit.outcome.not_found') }}</option>
            </select>
          </label>
          <label>{{ t('admin.accessAudit.filters.from') }}<input v-model="dateFrom" class="app-input" type="date" /></label>
          <label>{{ t('admin.accessAudit.filters.to') }}<input v-model="dateTo" class="app-input" type="date" /></label>
          <label class="access-audit-support-filter">
            <input v-model="supportAccessOnly" type="checkbox" />
            {{ t('admin.accessAudit.filters.supportAccessOnly') }}
          </label>
        </div>

        <p v-if="exportError" class="access-audit-alert" role="alert"><AlertTriangle :size="15" />{{ exportError }}</p>

        <div v-if="status === 'pending'" class="access-audit-empty"><span class="app-spinner" /><h3>{{ t('admin.accessAudit.loading') }}</h3></div>
        <div v-else-if="error" class="access-audit-empty">
          <AlertTriangle :size="22" /><h3>{{ t('admin.accessAudit.unavailable') }}</h3>
          <p>{{ localizedError.message(error, 'admin.accessAudit.error') }}</p>
          <button class="app-button" @click="refresh()">{{ t('admin.common.tryAgain') }}</button>
        </div>
        <div v-else-if="!events.length" class="access-audit-empty"><ShieldAlert :size="22" /><h3>{{ t('admin.accessAudit.none') }}</h3></div>
        <template v-else>
          <div class="access-audit-table">
            <div class="access-audit-row access-audit-row--head">
              <span>{{ t('admin.accessAudit.columns.timestamp') }}</span>
              <span>{{ t('admin.accessAudit.columns.actor') }}</span>
              <span>{{ t('admin.accessAudit.columns.resource') }}</span>
              <span>{{ t('admin.accessAudit.columns.action') }}</span>
              <span>{{ t('admin.accessAudit.columns.outcome') }}</span>
            </div>
            <div v-for="event in events" :key="event.id" class="access-audit-row">
              <span>{{ dateTime(event.created_at) }}</span>
              <span>
                {{ actorLabel(event) }}
                <small v-if="isSupportAccess(event)" class="access-audit-support-badge" :title="supportReason(event)">
                  <ShieldAlert :size="11" />{{ t('admin.accessAudit.supportAccess') }} · {{ supportCaseReference(event) }}
                </small>
              </span>
              <span>{{ resourceLabel(event) }}<small v-if="event.resource_id">{{ event.resource_id }}</small></span>
              <span>{{ event.action || '—' }}</span>
              <span class="access-audit-status" :data-tone="outcomeTone(event.outcome)"><i />{{ t(`admin.accessAudit.outcome.${event.outcome}`) }}</span>
            </div>
          </div>
          <footer v-if="hasMore" class="access-audit-footer">
            <button class="app-button" :disabled="loadingMore" @click="loadMore">
              {{ loadingMore ? t('admin.common.queuing') : t('admin.accessAudit.loadMore') }}
            </button>
          </footer>
        </template>
      </section>

      <section v-if="data?.signals.length" class="app-surface access-audit-section">
        <header><div><p class="app-eyebrow">{{ t('admin.accessAudit.signalsEyebrow') }}</p><h2>{{ t('admin.accessAudit.signals') }}</h2></div></header>
        <div class="access-audit-table access-audit-signals-table">
          <div class="access-audit-row access-audit-row--head">
            <span>{{ t('admin.accessAudit.columns.actor') }}</span>
            <span>{{ t('admin.accessAudit.columns.signalType') }}</span>
            <span>{{ t('admin.accessAudit.columns.events') }}</span>
            <span>{{ t('admin.accessAudit.columns.detail') }}</span>
            <span>{{ t('admin.accessAudit.columns.lastAccess') }}</span>
          </div>
          <div v-for="signal in data?.signals" :key="`${signal.type}:${signal.actor_type}:${signal.actor_id}`" class="access-audit-row">
            <span>{{ t(`admin.accessAudit.actorType.${signal.actor_type}`) }} · {{ signal.actor_id.slice(0, 8) }}</span>
            <span>{{ t(`admin.accessAudit.signalType.${signal.type}`) }}</span>
            <span>{{ integer(signal.event_count) }}</span>
            <span>{{ signalDetail(signal) }}</span>
            <span>{{ dateTime(signal.last_access_at) }}</span>
          </div>
        </div>
      </section>
    </template>
  </div>
</template>

<style scoped>
.access-audit { display: grid; gap: 16px; }
.access-audit-section { overflow: hidden; }
.access-audit-section > header { display: flex; min-height: 68px; align-items: center; justify-content: space-between; gap: 14px; padding: 11px 16px; border-bottom: 1px solid var(--line); }
.access-audit-section h2 { margin: 3px 0 0; font-size: 14px; }
.access-audit-filters { display: flex; flex-wrap: wrap; gap: 12px; padding: 14px 16px; background: var(--surface-soft); border-bottom: 1px solid var(--line); }
.access-audit-filters label { display: flex; flex-direction: column; gap: 6px; color: var(--ink-soft); font-size: 10px; font-weight: 750; }
.access-audit-support-filter { flex-direction: row !important; align-items: center; gap: 6px !important; align-self: flex-end; }
.access-audit-support-badge { display: inline-flex; align-items: center; gap: 4px; margin-top: 3px; max-width: 220px; overflow: hidden; color: var(--red); font-size: 9px; font-weight: 750; text-overflow: ellipsis; white-space: nowrap; }
.access-audit-alert { display: flex; min-height: 38px; align-items: center; gap: 8px; margin: 0; padding: 8px 11px; background: var(--red-soft); color: var(--red); border-radius: 4px; font-size: 10px; font-weight: 700; }
.access-audit-empty { display: grid; place-items: center; gap: 6px; padding: 40px 16px; color: var(--muted); text-align: center; }
.access-audit-empty h3 { margin: 0; font-size: 12px; color: var(--ink-soft); }
.access-audit-table { width: 100%; overflow-x: auto; }
.access-audit-row { display: grid; grid-template-columns: minmax(150px, .9fr) minmax(150px, 1fr) minmax(150px, 1fr) 110px 120px; min-height: 52px; align-items: center; gap: 14px; padding: 7px 15px; border-bottom: 1px solid var(--line); color: var(--muted); font-size: 10px; }
.access-audit-signals-table .access-audit-row { grid-template-columns: minmax(160px, 1fr) 140px 90px minmax(160px, 1.2fr) 140px; }
.access-audit-row:last-child { border-bottom: 0; }
.access-audit-row--head { min-height: 36px; padding-top: 0; padding-bottom: 0; background: #f8faf9; font-size: 9px; font-weight: 750; text-transform: uppercase; }
.access-audit-row > span { min-width: 0; }
.access-audit-row small { display: block; margin-top: 3px; color: var(--muted); font-size: 9px; }
.access-audit-status { display: inline-flex; align-items: center; gap: 6px; color: var(--ink-soft); font-size: 10px; font-weight: 700; text-transform: capitalize; }
.access-audit-status i { width: 7px; height: 7px; background: #b98a26; border-radius: 50%; }
.access-audit-status[data-tone='success'] i { background: var(--green); }
.access-audit-status[data-tone='danger'] i { background: var(--red); }
.access-audit-footer { display: flex; justify-content: center; padding: 14px 16px; }
@media (max-width: 700px) {
  .access-audit-section > header { align-items: flex-start; flex-wrap: wrap; }
  .access-audit-row { min-width: 760px; }
  .access-audit-signals-table .access-audit-row { min-width: 740px; }
}
</style>
