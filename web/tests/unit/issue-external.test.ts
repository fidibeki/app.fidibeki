import { describe, expect, it } from 'vitest'
import type { IssueExternalLink, IssueTrackerConnection } from '../../types/api'
import {
  creatableProviders,
  externalDestinationLabel,
  externalLinkFromError,
  liveExternalLink,
  mergeExternalLink,
} from '../../utils/issue-external'

function connection(overrides: Partial<IssueTrackerConnection> = {}): IssueTrackerConnection {
  return {
    id: 'connection-1',
    provider: 'jira',
    site_url: 'https://acme.atlassian.net',
    account_label: 'Ops Bot',
    credential_fingerprint: 'sha256:abcd',
    destination_id: '10000',
    destination_key: 'OPS',
    destination_name: 'Operations',
    issue_type_id: '10001',
    issue_type_name: 'Task',
    sub_destination_id: '',
    sub_destination_name: '',
    is_enabled: true,
    auto_create: false,
    validated_at: '2026-07-25T10:00:00Z',
    last_error: '',
    created_at: '2026-07-25T10:00:00Z',
    updated_at: '2026-07-25T10:00:00Z',
    ...overrides,
  }
}

function link(overrides: Partial<IssueExternalLink> = {}): IssueExternalLink {
  return {
    id: 'link-1',
    issue_id: 'issue-1',
    provider: 'jira',
    status: 'created',
    request_title: 'Checkout is confusing',
    request_description: '',
    request_priority: 'high',
    destination_id: '10000',
    destination_name: 'Operations',
    issue_type_id: '10001',
    external_id: '20001',
    external_key: 'OPS-1',
    external_url: 'https://acme.atlassian.net/browse/OPS-1',
    last_error: '',
    attempt_count: 1,
    created_at: '2026-07-25T10:05:00Z',
    updated_at: '2026-07-25T10:05:00Z',
    ...overrides,
  }
}

describe('issue external trackers', () => {
  it('treats pending and created attempts as occupying the provider slot', () => {
    const links = [link({ status: 'pending', external_key: '', external_url: '' })]

    expect(liveExternalLink(links, 'jira')?.id).toBe('link-1')
    expect(liveExternalLink(links, 'linear')).toBeUndefined()
  })

  it('frees the provider slot once an attempt has failed', () => {
    const links = [link({ status: 'failed', last_error: 'Jira is temporarily unavailable' })]

    expect(liveExternalLink(links, 'jira')).toBeUndefined()
    expect(creatableProviders([connection()], links)).toHaveLength(1)
  })

  it('offers only enabled, fully configured providers without a live attempt', () => {
    const providers = [
      connection({ id: 'jira-off', is_enabled: false }),
      connection({ id: 'jira-unconfigured', issue_type_id: '' }),
      connection({ id: 'linear-unconfigured', provider: 'linear', destination_id: '' }),
      connection({ id: 'linear-ready', provider: 'linear', destination_id: 'team-1', issue_type_id: '' }),
    ]

    expect(creatableProviders(providers, []).map(entry => entry.id)).toEqual(['linear-ready'])
  })

  it('hides a provider that already holds an external issue for this record', () => {
    const providers = [connection(), connection({ id: 'linear-ready', provider: 'linear', destination_id: 'team-1', issue_type_id: '' })]

    expect(creatableProviders(providers, [link()]).map(entry => entry.id)).toEqual(['linear-ready'])
  })

  it('describes the destination with the detail each provider needs', () => {
    expect(externalDestinationLabel(connection())).toBe('Operations · Task')
    expect(externalDestinationLabel(connection({
      provider: 'linear', issue_type_name: '', destination_name: 'Operations',
      sub_destination_name: 'Q3',
    }))).toBe('Operations · Q3')
    expect(externalDestinationLabel(connection({
      provider: 'linear', issue_type_name: '', sub_destination_name: '',
    }))).toBe('Operations')
  })

  it('recovers the persisted attempt from a rejected request', () => {
    const failed = link({ status: 'failed', last_error: 'Jira is temporarily unavailable' })

    expect(externalLinkFromError({ data: { error: { details: { external_link: failed } } } }))
      .toEqual(failed)
    expect(externalLinkFromError({ response: { _data: { error: { details: { external_link: failed } } } } }))
      .toEqual(failed)
    expect(externalLinkFromError({ data: { error: { code: 'validation_error' } } })).toBeNull()
    expect(externalLinkFromError('boom')).toBeNull()
  })

  it('replaces a retried attempt instead of listing it twice', () => {
    const failed = link({ status: 'failed', last_error: 'unavailable', external_key: '' })
    const succeeded = link({ status: 'created', attempt_count: 2 })

    const merged = mergeExternalLink([failed, link({ id: 'link-2', provider: 'linear' })], succeeded)

    expect(merged).toHaveLength(2)
    expect(merged[0].status).toBe('created')
    expect(merged[0].attempt_count).toBe(2)
  })
})
