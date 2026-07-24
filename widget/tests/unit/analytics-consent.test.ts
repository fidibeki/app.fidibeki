import { createAnalyticsConsentController } from '../../src/analytics-consent'
import { MemoryStorage } from '../../src/storage'
import { rule } from './fixtures'

describe('analytics consent orchestration', () => {
  it('keeps analytics dormant until a structured customer grant and revokes the grant immediately', async () => {
    const storage = new MemoryStorage()
    const changed = vi.fn()
    const requests: Array<{ url: string, body: Record<string, unknown> }> = []
    const fetchImpl = vi.fn(async (input: URL | RequestInfo, init: RequestInit = {}) => {
      requests.push({
        url: input.toString(),
        body: JSON.parse(String(init.body)) as Record<string, unknown>,
      })
      if (input.toString().includes('/revoke')) return new Response(null, { status: 204 })
      return new Response(JSON.stringify({
        token: 'grant-token',
        state: 'active',
        expires_at: '2026-08-23T12:00:00Z',
      }), { status: 201, headers: { 'Content-Type': 'application/json' } })
    })
    const analyticsRule = rule({
      analytics_consent: {
        mode: 'customer_cmp',
        notice_version: '2026-07',
        controller_name: '',
        privacy_policy_url: '',
        supported_locales: ['pt-pt'],
        retention_days: 0,
        expiry_days: 30,
        notices: {},
      },
    })
    const controller = createAnalyticsConsentController({
      endpoint: new URL('https://widget.example/v1/widget/analytics-consents'),
      publicKey: 'pk_live_test',
      projectID: 'project-1',
      storage,
      now: () => new Date('2026-07-23T12:00:00Z'),
      createToken: () => 'grant-token',
      fetchImpl,
      onChanged: changed,
    })
    controller.setRule(analyticsRule)

    expect(controller.activeGrant()).toBeNull()
    expect(fetchImpl).not.toHaveBeenCalled()

    const granted = await controller.grant({
      consentId: 'cmp-reference',
      purpose: 'widget_analytics',
      noticeVersion: '2026-07',
      locale: 'pt-PT',
      source: 'customer_cmp',
    })
    expect(granted).toMatchObject({ token: 'grant-token', locale: 'pt-pt' })
    expect(requests[0]?.body).toEqual({
      widget_rule_id: analyticsRule.id,
      purpose: 'widget_analytics',
      notice_version: '2026-07',
      locale: 'pt-pt',
      source: 'customer_cmp',
      consent_id: 'cmp-reference',
      state: 'granted',
      grant_token: 'grant-token',
    })
    expect(controller.activeGrant()).toMatchObject({ token: 'grant-token' })
    await expect(controller.grant({
      consentId: 'cmp-reference',
      purpose: 'widget_analytics',
      noticeVersion: '2026-07',
      locale: 'pt-PT',
      source: 'customer_cmp',
    })).resolves.toMatchObject({ token: 'grant-token' })
    expect(fetchImpl).toHaveBeenCalledTimes(1)

    await controller.revoke()
    expect(requests[1]?.body).toEqual({ token: 'grant-token' })
    expect(controller.activeGrant()).toBeNull()
    expect(changed).toHaveBeenCalledTimes(2)
  })

  it('persists an asynchronous grant only for the rule that requested it', async () => {
    const storage = new MemoryStorage()
    let resolveGrant!: (response: Response) => void
    const fetchImpl = vi.fn(() => new Promise<Response>((resolve) => {
      resolveGrant = resolve
    }))
    const consent = {
      mode: 'customer_cmp' as const,
      notice_version: '2026-07',
      controller_name: '',
      privacy_policy_url: '',
      supported_locales: ['en'],
      retention_days: 0,
      expiry_days: 30,
      notices: {},
    }
    const firstRule = rule({ id: 'rule-a', analytics_consent: consent })
    const secondRule = rule({ id: 'rule-b', analytics_consent: consent })
    const controller = createAnalyticsConsentController({
      endpoint: new URL('https://widget.example/v1/widget/analytics-consents'),
      publicKey: 'pk_live_test',
      projectID: 'project-1',
      storage,
      now: () => new Date('2026-07-23T12:00:00Z'),
      createToken: () => 'grant-token',
      fetchImpl,
    })
    controller.setRule(firstRule)
    const grant = controller.grant({
      consentId: 'cmp-reference',
      purpose: 'widget_analytics',
      noticeVersion: '2026-07',
      locale: 'en',
      source: 'customer_cmp',
    })
    await vi.waitFor(() => expect(fetchImpl).toHaveBeenCalledTimes(1))

    controller.setRule(secondRule)
    resolveGrant(new Response(JSON.stringify({
      token: 'grant-token',
      state: 'active',
      expires_at: '2026-08-23T12:00:00Z',
    }), { status: 201, headers: { 'Content-Type': 'application/json' } }))
    await grant

    const firstKey = 'fidibeki:analytics-consent:v1:project-1:rule-a:2026-07'
    const secondKey = 'fidibeki:analytics-consent:v1:project-1:rule-b:2026-07'
    expect(storage.getItem(firstKey)).not.toBeNull()
    expect(storage.getItem(secondKey)).toBeNull()
    expect(controller.status()).toBe('new')
    controller.setRule(firstRule)
    expect(controller.activeGrant()).toMatchObject({ token: 'grant-token' })
  })

  it('expires grants locally and requires a fresh grant after a notice version change', async () => {
    const storage = new MemoryStorage()
    let now = new Date('2026-07-23T12:00:00Z')
    const fetchImpl = vi.fn(async (_input, init = {}) => {
      const body = JSON.parse(String(init.body)) as { grant_token: string }
      return new Response(JSON.stringify({
        token: body.grant_token,
        state: 'active',
        expires_at: '2026-07-24T12:00:00Z',
      }), { status: 201, headers: { 'Content-Type': 'application/json' } })
    })
    const configured = rule({
      analytics_consent: {
        mode: 'customer_cmp',
        notice_version: '2026-07',
        controller_name: '',
        privacy_policy_url: '',
        supported_locales: ['en'],
        retention_days: 0,
        expiry_days: 1,
        notices: {},
      },
    })
    const controller = createAnalyticsConsentController({
      endpoint: new URL('https://widget.example/v1/widget/analytics-consents'),
      publicKey: 'pk_live_test',
      projectID: 'project-1',
      storage,
      now: () => now,
      createToken: () => 'grant-token',
      fetchImpl,
    })
    controller.setRule(configured)
    await controller.grant({
      consentId: 'cmp-reference',
      purpose: 'widget_analytics',
      noticeVersion: '2026-07',
      locale: 'en',
      source: 'customer_cmp',
    })

    controller.setRule(rule({
      ...configured,
      analytics_consent: { ...configured.analytics_consent, notice_version: '2026-08' },
    }))
    expect(controller.status()).toBe('new')
    expect(controller.activeGrant()).toBeNull()

    controller.setRule(configured)
    now = new Date('2026-07-24T12:00:01Z')
    expect(controller.status()).toBe('new')
    expect(controller.activeGrant()).toBeNull()
  })

  it('rejects a malformed customer assertion without calling the backend', async () => {
    const fetchImpl = vi.fn<typeof fetch>()
    const controller = createAnalyticsConsentController({
      endpoint: new URL('https://widget.example/v1/widget/analytics-consents'),
      publicKey: 'pk_live_test',
      projectID: 'project-1',
      storage: new MemoryStorage(),
      fetchImpl,
    })
    controller.setRule(rule({
      analytics_consent: {
        mode: 'customer_cmp',
        notice_version: '2026-07',
        controller_name: '',
        privacy_policy_url: '',
        supported_locales: ['en'],
        retention_days: 0,
        expiry_days: 30,
        notices: {},
      },
    }))

    await expect(controller.grant({
      purpose: 'widget_analytics',
      noticeVersion: 'outdated',
      locale: 'fr',
      source: 'customer_cmp',
    })).rejects.toThrow('does not match')
    expect(fetchImpl).not.toHaveBeenCalled()
  })

  it('records and remembers a managed rejection without creating an analytics token', async () => {
    const storage = new MemoryStorage()
    const requests: Array<Record<string, unknown>> = []
    const controller = createAnalyticsConsentController({
      endpoint: new URL('https://widget.example/v1/widget/analytics-consents'),
      publicKey: 'pk_live_test',
      projectID: 'project-1',
      storage,
      now: () => new Date('2026-07-23T12:00:00Z'),
      fetchImpl: vi.fn(async (_input, init = {}) => {
        requests.push(JSON.parse(String(init.body)) as Record<string, unknown>)
        return new Response(JSON.stringify({
          state: 'denied',
          expires_at: '2026-08-23T12:00:00Z',
        }), { status: 201, headers: { 'Content-Type': 'application/json' } })
      }),
    })
    controller.setRule(rule({
      analytics_consent: {
        mode: 'fidibeki_managed',
        notice_version: '2026-07',
        controller_name: 'Acme',
        privacy_policy_url: 'https://example.com/privacy',
        supported_locales: ['pt-pt'],
        retention_days: 30,
        expiry_days: 30,
        notices: {
          'pt-pt': {
            title: 'Análise do widget',
            description: 'Ajude-nos a melhorar esta experiência.',
            accept_label: 'Aceitar',
            reject_label: 'Recusar',
            withdraw_label: 'Retirar consentimento',
          },
        },
      },
    }))

    await controller.deny('pt-PT')

    expect(requests).toEqual([{
      widget_rule_id: 'rule-id',
      purpose: 'widget_analytics',
      notice_version: '2026-07',
      locale: 'pt-pt',
      source: 'fidibeki_managed',
      consent_id: '',
      state: 'denied',
      grant_token: '',
    }])
    expect(controller.status()).toBe('denied')
    expect(controller.activeGrant()).toBeNull()
  })

  it('never scopes an analytics grant into the replay consent storage namespace', async () => {
    const storage = new MemoryStorage()
    const controller = createAnalyticsConsentController({
      endpoint: new URL('https://widget.example/v1/widget/analytics-consents'),
      publicKey: 'pk_live_test',
      projectID: 'project-1',
      storage,
      now: () => new Date('2026-07-23T12:00:00Z'),
      createToken: () => 'grant-token',
      fetchImpl: vi.fn(async () => new Response(JSON.stringify({
        token: 'grant-token', state: 'active', expires_at: '2026-08-23T12:00:00Z',
      }), { status: 201, headers: { 'Content-Type': 'application/json' } })),
    })
    controller.setRule(rule({
      analytics_consent: {
        mode: 'customer_cmp', notice_version: '2026-07', controller_name: '',
        privacy_policy_url: '', supported_locales: ['en'], retention_days: 0,
        expiry_days: 30, notices: {},
      },
    }))
    await controller.grant({
      purpose: 'widget_analytics', noticeVersion: '2026-07', locale: 'en', source: 'customer_cmp',
      consentId: 'cmp-reference',
    })
    for (let index = 0; index < storage.length; index++) {
      const key = storage.key(index)
      expect(key).not.toMatch(/replay-consent/)
      expect(key).toMatch(/^fidibeki:analytics-consent:v1:/)
    }
  })
})
