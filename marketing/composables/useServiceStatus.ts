/**
 * Live service status, read from our own public status page.
 *
 * The endpoint is the one the status page itself calls. It is unauthenticated
 * and carries no compatibility guarantee, so every failure mode here resolves
 * to `unknown` rather than to a claim. A status badge that silently keeps
 * saying "Operational" while the endpoint is gone is worse than no badge: it
 * asserts health precisely when it can no longer observe it.
 *
 * Reads never happen during SSR. Prerendered HTML ships the `unknown` state,
 * which renders as a plain link to the status page, so crawlers and no-JS
 * visitors get something true and useful.
 */

export type ServiceStatusLevel = 'operational' | 'degraded' | 'down' | 'unknown'

export interface ServiceStatus {
  level: ServiceStatusLevel
  /** Short human label. Never claims health unless the payload proved it. */
  label: string
}

const REQUEST_TIMEOUT_MS = 5000

const UNKNOWN: ServiceStatus = { level: 'unknown', label: 'Status unavailable' }

interface OverviewShape {
  overallStatus?: { name?: unknown; isOperationalState?: unknown; priority?: unknown } | null
  activeIncidents?: unknown
  activeAnnouncements?: unknown
  scheduledMaintenanceEvents?: unknown
}

function countOf(value: unknown): number {
  return Array.isArray(value) ? value.length : 0
}

/**
 * Maps a payload to a level. Anything unexpected returns `unknown`, and an
 * operational flag contradicted by an open incident is treated as degraded —
 * when the payload disagrees with itself, believe the worse half.
 */
export function readOverview(payload: unknown): ServiceStatus {
  if (!payload || typeof payload !== 'object') return { level: 'unknown', label: 'Status unavailable' }

  const overview = payload as OverviewShape
  const overall = overview.overallStatus
  if (!overall || typeof overall !== 'object') {
    return { level: 'unknown', label: 'Status unavailable' }
  }

  const isOperational = overall.isOperationalState
  if (typeof isOperational !== 'boolean') {
    return { level: 'unknown', label: 'Status unavailable' }
  }

  const name = typeof overall.name === 'string' && overall.name.trim() ? overall.name.trim() : null
  const incidents = countOf(overview.activeIncidents)
  const maintenance = countOf(overview.scheduledMaintenanceEvents)

  if (isOperational && incidents === 0) {
    return {
      level: maintenance > 0 ? 'degraded' : 'operational',
      label: maintenance > 0 ? 'Maintenance scheduled' : (name ?? 'All systems operational'),
    }
  }

  // Not operational, or operational with an open incident.
  return {
    level: isOperational ? 'degraded' : 'down',
    label: name ?? (isOperational ? 'Incident open' : 'Service disruption'),
  }
}

export function useServiceStatus() {
  const config = useRuntimeConfig()
  const baseUrl = String(config.public.statusPageUrl || '').replace(/\/$/, '')
  const pageId = String(config.public.statusPageId || '')

  /** Without both halves of the config there is nothing to poll; the badge stays a link. */
  const configured = Boolean(baseUrl && pageId)

  const status = useState<ServiceStatus>('service-status', () => ({ ...UNKNOWN }))

  async function load() {
    if (!import.meta.client || !configured) return

    const controller = new AbortController()
    const timer = setTimeout(() => controller.abort(), REQUEST_TIMEOUT_MS)
    try {
      const response = await fetch(`${baseUrl}/status-page-api/overview/${pageId}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: '{}',
        signal: controller.signal,
        credentials: 'omit',
        cache: 'no-store',
      })
      // Every failure resets rather than leaving the last good answer in place.
      // The state is shared across the app, so a stale success would otherwise
      // survive a later failure and keep asserting health nobody re-checked.
      status.value = response.ok ? readOverview(await response.json()) : { ...UNKNOWN }
    } catch {
      status.value = { ...UNKNOWN }
    } finally {
      clearTimeout(timer)
    }
  }

  return { status, load, configured, statusPageUrl: baseUrl }
}
