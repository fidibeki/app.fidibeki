import {
  cacheIsFresh,
  fetchConfiguration,
  matchRule,
  reportConnection,
  type ConfigCache,
  type TargetingRegexModule,
} from './config'
import {
  createAnalyticsTracker,
  type FunnelEventType,
} from './analytics'
import { createAnalyticsOutbox } from './analytics-outbox'
import { getSessionID, isSuppressed, presentationRecurrencePolicy, recordWidgetState } from './suppression'
import { armObjectivePolicy, recordObjectiveVisit } from './policy'
import { privacySafeLocation } from './privacy-location'
import {
  createReplayConsentController,
  type ReplayConsentAssertion,
  type ReplayConsentController,
} from './replay-consent'
import { storageOrMemory, type StorageLike } from './storage'
import { armTrigger, type TriggerCleanup } from './triggers'
import type { WidgetConfiguration, WidgetRule } from './types'

type WidgetUnmount = () => void

interface WidgetModule {
  mountWidget(options: {
    configuration: WidgetConfiguration
    rule: WidgetRule
    publicKey: string
    apiBase: URL
    assetBase: URL
    pageURL: string
    replaySessionID: string | null
    onVisible(): void
    onInteraction(): void
    submissionAnalytics(): import('./analytics').FunnelEvent | null
    onSubmitted(): void
    onDismissed(): void
    target?: HTMLElement
    hostID?: string
  }): WidgetUnmount
}

interface ExplicitMountResult {
  mounted: boolean
  reason?: 'not_found' | 'inactive' | 'not_targeted' | 'suppressed'
  unmount(): void
}

interface ReplayCapture {
	readonly sessionID: string
	stop(): Promise<void>
	revoke?(): void
}

interface ReplayModule {
	startReplayCapture(options: {
		rule: WidgetRule
			publicKey: string
			apiBase: URL
			visitorID: string
			consentToken: string
			onError(error: unknown): void
	}): Promise<ReplayCapture | null>
}

interface ReplayConsentPromptModule {
  mountReplayConsentPrompt(options: {
    consent: WidgetRule['replay_consent']
    locale: string
    state: 'new' | 'active'
    onAccept(): Promise<void>
    onReject(): Promise<void>
    onWithdraw(): Promise<void>
  }): WidgetUnmount
}

interface ErrorModule {
  captureWidgetError(error: unknown, options: { dsn: string; context: Record<string, unknown> }): void
}

const script = findLoaderScript()
const publicKey = script?.dataset.fidibekiKey?.trim() ?? ''

if (script && publicKey) {
  void bootstrap(script, publicKey)
}

async function bootstrap(loaderScript: HTMLScriptElement, key: string): Promise<void> {
  const assetBase = new URL('.', loaderScript.src)
  const configurationEndpoint = new URL('widget/config', assetBase)
  const connectionEndpoint = new URL('widget/connected', assetBase)
  const analyticsEndpoint = new URL('widget/events', assetBase)
  let cache: ConfigCache | null = null
  let cleanupTrigger: TriggerCleanup | null = null
  let unmountWidget: WidgetUnmount | null = null
	let replayCapture: ReplayCapture | null = null
  let replayConsent: ReplayConsentController | null = null
  let replayConsentProjectID = ''
  let currentReplayRule: WidgetRule | null = null
  let replayConsentPromptUnmount: WidgetUnmount | null = null
  let replayConsentPromptFingerprint = ''
  let replayConsentPromptTransition = 0
  let replayFingerprint = ''
  let replayTransition = 0
  let replayRevokedThroughTransition = 0
  let evaluation = 0
  let automaticRuleID: string | null = null
  let configurationRequest: Promise<WidgetConfiguration> | null = null
  const explicitRuleIDs = new Set<string>()
  const explicitTargets = new Map<HTMLElement, { reference: string; unmount: WidgetUnmount | null }>()
  const explicitGenerations = new WeakMap<HTMLElement, number>()
  const durableStorage = storageOrMemory(() => window.localStorage)
  const sessionStorage = storageOrMemory(() => window.sessionStorage)
  const widgetSessionID = getSessionID(sessionStorage)
  const analytics = createAnalyticsTracker({ storage: durableStorage })

  const ensureReplayConsent = (configuration: WidgetConfiguration) => {
    if (replayConsent && replayConsentProjectID === configuration.project_id) return replayConsent
    replayConsentProjectID = configuration.project_id
    replayConsent = createReplayConsentController({
      endpoint: new URL('replay/consents', assetBase),
      publicKey: key,
      projectID: configuration.project_id,
      storage: durableStorage,
      onChanged: (state) => {
        if (state === 'revoked') {
          replayRevokedThroughTransition = ++replayTransition
          replayFingerprint = ''
          const capture = replayCapture
          replayCapture = null
          if (capture?.revoke) capture.revoke()
          else if (capture) void capture.stop()
        }
        if (cache) void synchronizeReplay(cache.value, currentReplayRule)
      },
    })
    return replayConsent
  }

  const report = async (error: unknown, phase: string) => {
    const dsn = cache?.value.error_reporting_dsn
    if (!dsn) return
    try {
      const module = (await import(new URL('error-reporting.js', assetBase).href)) as ErrorModule
      module.captureWidgetError(error, { dsn, context: { phase, project_id: cache?.value.project_id } })
    } catch {
      // Error reporting must never interfere with the host page.
    }
  }

  const analyticsOutbox = createAnalyticsOutbox({
    endpoint: analyticsEndpoint,
    publicKey: key,
    ownerID: widgetSessionID,
    storage: durableStorage,
    sendBeacon: typeof navigator.sendBeacon === 'function' ? navigator.sendBeacon.bind(navigator) : undefined,
    onError: (error) => void report(error, 'analytics'),
  })

  const loadConfiguration = async (force: boolean): Promise<WidgetConfiguration> => {
    const now = Date.now()
    if (!force && cacheIsFresh(cache, now)) return cache.value
    if (!force && configurationRequest) return configurationRequest
    const request = fetchConfiguration({ endpoint: configurationEndpoint, publicKey: key })
    configurationRequest = request
    try {
      const value = await request
      cache = { value, fetchedAt: now }
      return value
    } finally {
      if (configurationRequest === request) configurationRequest = null
    }
  }

  const recordAnalytics = (eventType: FunnelEventType, rule: WidgetRule) => {
    const event = analytics.record(eventType, rule)
    if (!event) return
    analyticsOutbox.enqueue(event)
  }

  const evaluate = async (forceRefresh = false) => {
    const currentEvaluation = ++evaluation
    const evaluationURL = window.location.href
    analytics.visit(evaluationURL)
    cleanupTrigger?.()
    cleanupTrigger = null
    unmountWidget?.()
    unmountWidget = null
    automaticRuleID = null
    try {
      const configuration = await loadConfiguration(forceRefresh)
      if (currentEvaluation !== evaluation) return
      const declaredReferences = new Set(Array.from(
        document.querySelectorAll<HTMLElement>('fidibeki-widget[rule]'),
        element => element.getAttribute('rule')?.trim() ?? '',
      ))
      const definitions = (configuration.definitions ?? configuration.rules).filter(rule =>
        definitionIsActive(rule) && !explicitRuleIDs.has(rule.id) && !explicitRuleIDs.has(rule.definition_key ?? '') &&
        !declaredReferences.has(rule.id) && !declaredReferences.has(rule.definition_key ?? ''))
      const rule = await matchRule(definitions, evaluationURL, {
        loadRegex: () => import(new URL('targeting-regex.js', assetBase).href) as Promise<TargetingRegexModule>,
        onError: (error) => void report(error, 'targeting'),
      })
			if (currentEvaluation !== evaluation) return
			if (rule) {
				recordAnalytics('matched', rule)
				void reportConnection({ endpoint: connectionEndpoint, publicKey: key, ruleID: rule.id }).catch(() => {})
			}
      await synchronizeReplay(configuration, rule)
			if (currentEvaluation !== evaluation) return
      if (!rule) return
      const privacyLocation = privacySafeLocation(evaluationURL, rule)
      if (!privacyLocation) return
      const visitCount = recordObjectiveVisit(durableStorage, {
        projectID: configuration.project_id,
        rawURL: evaluationURL,
        sessionID: widgetSessionID,
      })
      if (ruleIsSuppressed(durableStorage, configuration, rule, evaluationURL, widgetSessionID)) return
      let triggerReady = false
      let policyReady = rule.objective === undefined && rule.presentation === undefined
      let mounting = false
      const mountWhenEligible = () => {
        if (!triggerReady || !policyReady || mounting) return
        mounting = true
        void import(new URL('widget.js', assetBase).href)
          .then((module) => {
            if (currentEvaluation !== evaluation) return
            automaticRuleID = rule.id
            unmountWidget = (module as WidgetModule).mountWidget({
              configuration,
              rule,
              publicKey: key,
              apiBase: assetBase,
              assetBase,
              pageURL: privacyLocation.target,
					replaySessionID: replayCapture?.sessionID ?? null,
              onVisible: () => recordAnalytics('impression', rule),
              onInteraction: () => recordAnalytics('interaction', rule),
              submissionAnalytics: () => analytics.record('submitted', rule),
              onSubmitted: () => persistState(durableStorage, configuration, rule, evaluationURL, widgetSessionID, 'submitted'),
              onDismissed: () => {
					recordAnalytics('dismissed', rule)
					persistState(durableStorage, configuration, rule, evaluationURL, widgetSessionID, 'dismissed')
				},
            })
          })
          .catch((error: unknown) => report(error, 'mount'))
      }
      const cleanupLegacyTrigger = armTrigger(rule, () => {
        triggerReady = true
        mountWhenEligible()
      })
      const cleanupPolicy = policyReady
        ? () => undefined
        : armObjectivePolicy(rule, visitCount, () => {
            policyReady = true
            mountWhenEligible()
          })
      cleanupTrigger = () => {
        cleanupLegacyTrigger()
        cleanupPolicy()
      }
    } catch (error) {
      void report(error, 'configuration')
    }
  }

  const mountExplicit = async (target: HTMLElement, reference: string): Promise<ExplicitMountResult> => {
    const generation = (explicitGenerations.get(target) ?? 0) + 1
    explicitGenerations.set(target, generation)
    const normalizedReference = reference.trim()
    const previous = explicitTargets.get(target)
    previous?.unmount?.()
    explicitTargets.set(target, { reference: normalizedReference, unmount: null })
    if (!normalizedReference) return explicitResult(false, 'not_found')
    try {
      const configuration = await loadConfiguration(false)
      if (explicitGenerations.get(target) !== generation) return explicitResult(false, 'not_found')
      const definitions = configuration.definitions ?? configuration.rules
      const rule = definitions.find(candidate =>
        candidate.id === normalizedReference || candidate.definition_key === normalizedReference)
      if (!rule) return explicitResult(false, 'not_found')
      if (!definitionIsActive(rule)) return explicitResult(false, 'inactive')
      explicitRuleIDs.add(rule.id)
      if (rule.definition_key) explicitRuleIDs.add(rule.definition_key)
      if (automaticRuleID === rule.id) {
        unmountWidget?.()
        unmountWidget = null
        automaticRuleID = null
      }
      const currentURL = window.location.href
      analytics.visit(currentURL)
      const targeted = await matchRule([rule], currentURL, {
        loadRegex: () => import(new URL('targeting-regex.js', assetBase).href) as Promise<TargetingRegexModule>,
        onError: (error) => void report(error, 'targeting'),
      })
      if (explicitGenerations.get(target) !== generation) return explicitResult(false, 'not_found')
      if (!targeted) return explicitResult(false, 'not_targeted')
      const privacyLocation = privacySafeLocation(currentURL, rule)
      if (!privacyLocation) return explicitResult(false, 'not_targeted')
      await synchronizeReplay(configuration, rule)
      if (explicitGenerations.get(target) !== generation) return explicitResult(false, 'not_found')
      recordAnalytics('matched', rule)
      recordObjectiveVisit(durableStorage, {
        projectID: configuration.project_id,
        rawURL: currentURL,
        sessionID: widgetSessionID,
      })
      void reportConnection({ endpoint: connectionEndpoint, publicKey: key, ruleID: rule.id }).catch(() => {})
      if (ruleIsSuppressed(durableStorage, configuration, rule, currentURL, widgetSessionID)) {
        return explicitResult(false, 'suppressed')
      }
      const module = (await import(new URL('widget.js', assetBase).href)) as WidgetModule
      if (explicitGenerations.get(target) !== generation) return explicitResult(false, 'not_found')
      const hostID = `fidibeki-widget-host-${randomID()}`
      const unmount = module.mountWidget({
        configuration, rule, publicKey: key, apiBase: assetBase, assetBase,
        pageURL: privacyLocation.target,
        replaySessionID: replayCapture?.sessionID ?? null,
        target, hostID,
        onVisible: () => recordAnalytics('impression', rule),
        onInteraction: () => recordAnalytics('interaction', rule),
        submissionAnalytics: () => analytics.record('submitted', rule),
        onSubmitted: () => persistState(durableStorage, configuration, rule, currentURL, widgetSessionID, 'submitted'),
        onDismissed: () => {
          recordAnalytics('dismissed', rule)
          persistState(durableStorage, configuration, rule, currentURL, widgetSessionID, 'dismissed')
        },
      })
      explicitTargets.set(target, { reference: normalizedReference, unmount })
      return { mounted: true, unmount: () => unmountExplicitTarget(target) }
    } catch (error) {
      void report(error, 'mount')
      return explicitResult(false, 'not_found')
    }
  }

  const unmountExplicitTarget = (target: HTMLElement) => {
    explicitGenerations.set(target, (explicitGenerations.get(target) ?? 0) + 1)
    explicitTargets.get(target)?.unmount?.()
    explicitTargets.delete(target)
  }

  installExplicitMounting(mountExplicit, unmountExplicitTarget)

	const synchronizeReplay = async (configuration: WidgetConfiguration, rule: WidgetRule | null) => {
    currentReplayRule = rule
    const consent = ensureReplayConsent(configuration)
    consent.setRule(rule)
    await synchronizeReplayConsentPrompt(configuration, rule, consent)
    const grant = consent.activeGrant()
		const enabled = rule !== null && rule.replay_mode !== 'none' &&
      configuration.features.session_replay && grant !== null
		const fingerprint = enabled ? JSON.stringify({
			id: rule.id,
			mode: rule.replay_mode,
			sampling: rule.replay_sampling_rate,
			duration: rule.replay_max_duration_seconds,
			events: rule.replay_max_events,
			maskInputs: rule.replay_mask_all_inputs,
			maskText: rule.replay_mask_text_selector,
			recordCanvas: rule.replay_record_canvas,
			canvasFPS: rule.replay_canvas_fps,
      consent: grant.token,
		}) : ''
		if (fingerprint === replayFingerprint) return
		const transition = ++replayTransition
		const previous = replayCapture
		replayCapture = null
		replayFingerprint = fingerprint
		if (previous) void previous.stop()
		if (!enabled || !rule) return
		try {
			const module = (await import(new URL('replay.js', assetBase).href)) as ReplayModule
			const capture = await module.startReplayCapture({
				rule,
					publicKey: key,
					apiBase: assetBase,
					visitorID: analytics.visitorID,
          consentToken: grant.token,
					onError: (error) => void report(error, 'replay'),
				})
				if (transition !== replayTransition) {
					if (capture?.revoke && transition < replayRevokedThroughTransition) capture.revoke()
					else if (capture) void capture.stop()
					return
				}
			replayCapture = capture
		} catch (error) {
			void report(error, 'replay')
		}
	}

  const synchronizeReplayConsentPrompt = async (
    configuration: WidgetConfiguration,
    rule: WidgetRule | null,
    controller: ReplayConsentController,
  ) => {
    const status = controller.status()
    const visibleState = status === 'new' ? 'new' : status === 'active' ? 'active' : null
    const locale = rule ? replayConsentLocale(configuration, rule) : null
    const fingerprint = rule?.replay_consent.mode === 'fidibeki_managed' && visibleState && locale
      ? `${rule.id}:${rule.replay_consent.notice_version}:${locale}:${visibleState}`
      : ''
    if (fingerprint === replayConsentPromptFingerprint) return
    const transition = ++replayConsentPromptTransition
    replayConsentPromptFingerprint = fingerprint
    replayConsentPromptUnmount?.()
    replayConsentPromptUnmount = null
    if (!fingerprint || !rule || !locale || !visibleState) return
    try {
      const module = (await import(
        new URL('replay-consent-prompt.js', assetBase).href
      )) as ReplayConsentPromptModule
      if (transition !== replayConsentPromptTransition) return
      replayConsentPromptUnmount = module.mountReplayConsentPrompt({
        consent: rule.replay_consent,
        locale,
        state: visibleState,
        onAccept: () => controller.grant({
          purpose: 'session_replay',
          noticeVersion: rule.replay_consent.notice_version,
          locale,
          source: 'fidibeki_managed',
        }).then(() => {}),
        onReject: () => controller.deny(locale),
        onWithdraw: () => controller.revoke(),
      })
    } catch (error) {
      void report(error, 'replay_consent_prompt')
    }
  }

  installReplayConsentAPI(
    async (assertion) => {
      const configuration = await loadConfiguration(false)
      const controller = ensureReplayConsent(configuration)
      if (!currentReplayRule) throw new Error('no replay-enabled rule is active on this page')
      controller.setRule(currentReplayRule)
      return controller.grant(assertion)
    },
    async () => {
      if (!replayConsent) return
      await replayConsent.revoke()
    },
  )

  installNavigationEvents()
  window.addEventListener('fidibeki:navigation', () => {
    void evaluate()
    for (const [target, mounted] of explicitTargets) {
      if (target.isConnected) void mountExplicit(target, mounted.reference)
      else unmountExplicitTarget(target)
    }
  })
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'hidden') {
      analyticsOutbox.flushOnHide()
      return
    }
    void analyticsOutbox.flush()
    void evaluate(true)
  })
  window.addEventListener('pagehide', () => analyticsOutbox.flushOnHide())
  window.setInterval(() => void evaluate(true), 5 * 60 * 1000)
  await evaluate()
}

function explicitResult(mounted: false, reason: NonNullable<ExplicitMountResult['reason']>): ExplicitMountResult {
  return { mounted, reason, unmount() {} }
}

function definitionIsActive(rule: WidgetRule): boolean {
  return rule.definition_status === undefined || rule.definition_status === 'active'
}

function installReplayConsentAPI(
  grantConsent: (assertion: ReplayConsentAssertion) => Promise<unknown>,
  revokeConsent: () => Promise<void>,
): void {
  const namespace = (window as unknown as { Fidibeki?: Record<string, unknown> }).Fidibeki ?? {}
  namespace.replay = { grantConsent, revokeConsent }
  ;(window as unknown as { Fidibeki: Record<string, unknown> }).Fidibeki = namespace
}

function installExplicitMounting(
  mount: (target: HTMLElement, reference: string) => Promise<ExplicitMountResult>,
  unmount: (target: HTMLElement) => void,
): void {
  const namespace = (window as unknown as { Fidibeki?: Record<string, unknown> }).Fidibeki ?? {}
  namespace.mount = (target: HTMLElement, options: { rule: string }) => mount(target, options.rule)
  ;(window as unknown as { Fidibeki: Record<string, unknown> }).Fidibeki = namespace
  if (customElements.get('fidibeki-widget')) return
  customElements.define('fidibeki-widget', class extends HTMLElement {
    static observedAttributes = ['rule']
    private scheduledMount = 0
    connectedCallback() { this.scheduleMount() }
    disconnectedCallback() { unmount(this) }
    attributeChangedCallback() {
      if (this.isConnected) this.scheduleMount()
    }
    private scheduleMount() {
      const scheduled = ++this.scheduledMount
      queueMicrotask(() => {
        if (this.isConnected && scheduled === this.scheduledMount) {
          void mount(this, this.getAttribute('rule') ?? '')
        }
      })
    }
  })
}

function randomID(): string {
  return globalThis.crypto?.randomUUID?.() ?? `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`
}

function replayConsentLocale(configuration: WidgetConfiguration, rule: WidgetRule): string | null {
  const supported = rule.replay_consent.supported_locales
    .map(locale => locale.trim().toLowerCase())
    .filter(locale => rule.replay_consent.notices[locale] !== undefined)
  const candidates = [
    document.documentElement.lang,
    ...navigator.languages,
    configuration.default_locale ?? '',
  ].map(locale => locale.trim().replace(/_/g, '-').toLowerCase()).filter(Boolean)
  for (const candidate of candidates) {
    if (supported.includes(candidate)) return candidate
    const primary = candidate.split('-', 1)[0]
    const compatible = supported.find(locale => locale === primary || locale.split('-', 1)[0] === primary)
    if (compatible) return compatible
  }
  return supported[0] ?? null
}

function ruleIsSuppressed(
  store: StorageLike,
  configuration: WidgetConfiguration,
  rule: WidgetRule,
  rawURL: string,
  sessionID: string,
): boolean {
  const recurrence = presentationRecurrencePolicy(
    rule.presentation,
    rule.recurrence_mode ?? 'cooldown',
    rule.recurrence_days ?? 30,
  )
  return isSuppressed(store, {
    projectID: configuration.project_id,
    ruleID: rule.id,
    rawURL,
    sessionID,
    recurrenceMode: recurrence.mode,
    recurrenceDays: recurrence.days,
  })
}

function persistState(
  store: StorageLike,
  configuration: WidgetConfiguration,
  rule: WidgetRule,
  rawURL: string,
  sessionID: string,
  state: 'submitted' | 'dismissed',
): void {
  recordWidgetState(store, {
    projectID: configuration.project_id,
    ruleID: rule.id,
    rawURL,
    sessionID,
    state,
  })
}

function findLoaderScript(): HTMLScriptElement | null {
  if (document.currentScript instanceof HTMLScriptElement) return document.currentScript
  const scripts = Array.from(document.querySelectorAll<HTMLScriptElement>('script[data-fidibeki-key]'))
  return scripts.find((candidate) => candidate.src.includes('/v1/loader.js')) ?? null
}

function installNavigationEvents(): void {
  const marker = '__fidibekiNavigationInstalled__'
  const markedWindow = window as unknown as Window & Record<string, unknown>
  if (markedWindow[marker]) return
  markedWindow[marker] = true
  const dispatch = () => window.dispatchEvent(new Event('fidibeki:navigation'))
  for (const method of ['pushState', 'replaceState'] as const) {
    const original = history[method]
    history[method] = function (...arguments_: Parameters<History[typeof method]>) {
      const result = original.apply(this, arguments_)
      dispatch()
      return result
    }
  }
  window.addEventListener('popstate', dispatch)
}
