import type { IssueExternalLink, IssueTrackerConnection, IssueTrackerProvider } from '~/types/api'

/**
 * The attempt currently occupying a provider slot for an issue. A failed attempt does not
 * occupy the slot, which is what makes the retry path possible.
 */
export function liveExternalLink(
  links: IssueExternalLink[],
  provider: IssueTrackerProvider,
): IssueExternalLink | undefined {
  return links.find(link => link.provider === provider && link.status !== 'failed')
}

/**
 * Providers whose creation control should be offered: enabled by an administrator, pointed at a
 * validated destination, and not already holding an external issue for this record.
 */
export function creatableProviders(
  connections: IssueTrackerConnection[],
  links: IssueExternalLink[],
): IssueTrackerConnection[] {
  return connections.filter(connection => connection.is_enabled
    && Boolean(connection.destination_id)
    && (connection.provider !== 'jira' || Boolean(connection.issue_type_id))
    && !liveExternalLink(links, connection.provider))
}

/** Human-readable destination for the review dialog: project plus issue type, or team plus project. */
export function externalDestinationLabel(connection: IssueTrackerConnection): string {
  const detail = connection.provider === 'jira'
    ? connection.issue_type_name
    : connection.sub_destination_name
  return detail ? `${connection.destination_name} · ${detail}` : connection.destination_name
}

/**
 * A failed or conflicting creation still returns the attempt it produced, so the panel can show
 * the provider's own error text and a retry control instead of only a generic message.
 */
export function externalLinkFromError(value: unknown): IssueExternalLink | null {
  const root = value && typeof value === 'object' ? value as Record<string, unknown> : null
  if (!root) return null
  const response = root.response as Record<string, unknown> | undefined
  const data = (root.data || response?._data || response?.data || root) as Record<string, unknown>
  const body = data.error as Record<string, unknown> | undefined
  const details = body?.details as Record<string, unknown> | undefined
  const link = details?.external_link
  return link && typeof link === 'object' && 'id' in link ? link as IssueExternalLink : null
}

/** Replaces an attempt in place, keeping the most recent result first. */
export function mergeExternalLink(
  links: IssueExternalLink[],
  link: IssueExternalLink,
): IssueExternalLink[] {
  return [link, ...links.filter(entry => entry.id !== link.id)]
}
