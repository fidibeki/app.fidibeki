import {
  cacheIsFresh,
  fetchConfiguration,
  matchRule,
  reportConnection,
  type ConfigCache,
  type TargetingRegexModule,
} from './config'
import {
  createFunnelTracker,
  createPageViewTracker,
  forgetVisitorID,
  resolveVisitorID,
  type FunnelEventType,
  type FunnelTracker,
  type PageViewIdentity,
} from './analytics'
import { createAnalyticsOutbox, type AnalyticsOutbox } from './analytics-outbox'
import {
  createAnalyticsConsentController,
  type AnalyticsConsentAssertion,
  type AnalyticsConsentController,
} from './analytics-consent'
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
import type { PrivacyChoice, PrivacyChoices, WidgetConfiguration, WidgetRule } from './types'

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
    privacy: PrivacyChoices
    onClosed(): void
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
			consentToken: string
			onError(error: unknown): void
	}): Promise<ReplayCapture | null>
}

interface ConsentPromptPurposeDescriptor {
  purpose: 'widget_analytics' | 'session_replay'
  consent: WidgetRule['analytics_consent'] | WidgetRule['replay_consent']
  locale: string
  onAccept(): Promise<void>
  onReject(): Promise<void>
}

interface ConsentPromptModule {
  mountConsentPrompt(options: {
    purposes: ConsentPromptPurposeDescriptor[]
    runExclusive?<T>(action: () => Promise<T>): Promise<T>
  }): WidgetUnmount
}

interface PrivacyControlsModule {
  mountPrivacyControls(options: {
    rule: WidgetRule
    configuration: WidgetConfiguration
    choices(): PrivacyChoice[]
    subscribe(listener: () => void): () => void
    onRemoved(): void
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
  let consentPromptUnmount: WidgetUnmount | null = null
  let consentPromptFingerprint = ''
  let consentPromptTransition = 0
  let consentDecisionDepth = 0
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
  // The page-view identity is in-memory only (no storage, no network) and is always safe to
  // track. The persistent visitor identifier and funnel outbox are created lazily, strictly after
  // an active widget_analytics grant is confirmed for the current rule — see
  // ensureAnalyticsFunnel/synchronizeAnalyticsConsent below (issue #58).
  const pageViews = createPageViewTracker()
  let currentPageView: PageViewIdentity | null = null
  let analyticsConsent: AnalyticsConsentController | null = null
  let analyticsConsentProjectID = ''
  let currentAnalyticsRule: WidgetRule | null = null
  let analyticsFunnel: FunnelTracker | null = null
  let analyticsOutbox: AnalyticsOutbox | null = null
  let analyticsGrantedRuleID: string | null = null
  let analyticsGrantToken = ''
  let liveFeedbackWidgets = 0
  let privacyControlUnmount: WidgetUnmount | null = null
  let privacyControlFingerprint = ''
  let privacyControlTransition = 0

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
        notifyPrivacyChoices()
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

  const ensureAnalyticsConsent = (configuration: WidgetConfiguration) => {
    if (analyticsConsent && analyticsConsentProjectID === configuration.project_id) return analyticsConsent
    analyticsConsentProjectID = configuration.project_id
    analyticsConsent = createAnalyticsConsentController({
      endpoint: new URL('widget/analytics-consents', assetBase),
      publicKey: key,
      projectID: configuration.project_id,
      storage: durableStorage,
      onChanged: (state) => {
        if (state === 'revoked') wipeAnalyticsIdentity()
        notifyPrivacyChoices()
        if (!cache) return
        const grantedRule = currentAnalyticsRule
        void synchronizeAnalyticsConsent(cache.value, grantedRule).then(() => {
          // evaluate() records "matched" before the visitor has answered the prompt, so the gate
          // in recordAnalytics drops it and the funnel would open on an impression the backend
          // cannot attribute to any matched event. Re-emit it the moment the grant lands.
          // FunnelTracker.record is idempotent per page view and rule, so replaying it here is a
          // no-op whenever evaluate() already succeeded.
          if (state === 'granted' && grantedRule) recordAnalytics('matched', grantedRule)
        })
      },
    })
    return analyticsConsent
  }

  // The persistent visitor identifier and outbox are created the first time any rule has an
  // active widget_analytics grant, and never before. Once created they are reused across rule
  // changes on the same project (the identifier is inherently browser/project scoped, not
  // rule scoped), until an explicit withdrawal wipes them via wipeAnalyticsIdentity.
  const ensureAnalyticsFunnel = (): FunnelTracker => {
    if (!analyticsFunnel) {
      analyticsFunnel = createFunnelTracker(resolveVisitorID(durableStorage))
    }
    if (!analyticsOutbox) {
      analyticsOutbox = createAnalyticsOutbox({
        endpoint: analyticsEndpoint,
        publicKey: key,
        ownerID: widgetSessionID,
        storage: durableStorage,
        sendBeacon: typeof navigator.sendBeacon === 'function' ? navigator.sendBeacon.bind(navigator) : undefined,
        onError: (error) => void report(error, 'analytics'),
      })
    }
    return analyticsFunnel
  }

  const wipeAnalyticsIdentity = () => {
    analyticsGrantedRuleID = null
    analyticsGrantToken = ''
    analyticsFunnel = null
    const outbox = analyticsOutbox
    analyticsOutbox = null
    outbox?.destroy()
    forgetVisitorID(durableStorage)
  }

  const privacyListeners = new Set<() => void>()
  const notifyPrivacyChoices = () => {
    for (const listener of privacyListeners) listener()
    void synchronizePrivacyControl()
  }

  // Only Fidibeki-managed grants are withdrawable from our own UI. When the customer runs their
  // own CMP ('customer_cmp') the decision and its withdrawal belong to that CMP, and the widget
  // must not offer a second, competing control; 'none' has no consent to withdraw at all.
  const privacyChoices = (): PrivacyChoice[] => {
    const configuration = cache?.value
    if (!configuration) return []
    const choices: PrivacyChoice[] = []
    const analyticsController = analyticsConsent
    const analyticsRule = currentAnalyticsRule
    if (
      analyticsController && analyticsRule &&
      analyticsRule.analytics_consent.mode === 'fidibeki_managed' &&
      analyticsController.status() === 'active'
    ) {
      const locale = analyticsConsentLocale(configuration, analyticsRule)
      const notice = locale ? analyticsRule.analytics_consent.notices[locale] : undefined
      if (notice) {
        choices.push({
          purpose: 'widget_analytics',
          title: notice.title,
          description: notice.description,
          controllerName: analyticsRule.analytics_consent.controller_name,
          retentionDays: analyticsRule.analytics_consent.retention_days,
          privacyPolicyURL: analyticsRule.analytics_consent.privacy_policy_url,
          withdrawLabel: notice.withdraw_label,
          withdraw: () => analyticsController.revoke(),
        })
      }
    }
    const replayController = replayConsent
    const replayRule = currentReplayRule
    if (
      replayController && replayRule &&
      replayRule.replay_consent.mode === 'fidibeki_managed' &&
      replayController.status() === 'active'
    ) {
      const locale = replayConsentLocale(configuration, replayRule)
      const notice = locale ? replayRule.replay_consent.notices[locale] : undefined
      if (notice) {
        choices.push({
          purpose: 'session_replay',
          title: notice.title,
          description: notice.description,
          controllerName: replayRule.replay_consent.controller_name,
          retentionDays: replayRule.replay_consent.retention_days,
          privacyPolicyURL: replayRule.replay_consent.privacy_policy_url,
          withdrawLabel: notice.withdraw_label,
          withdraw: () => replayController.revoke(),
        })
      }
    }
    return choices
  }

  const privacy: PrivacyChoices = {
    choices: privacyChoices,
    subscribe(listener) {
      privacyListeners.add(listener)
      return () => {
        privacyListeners.delete(listener)
      }
    },
  }

  // Exactly one withdrawal path is on the page at a time. While a feedback panel is mounted it owns
  // the path through its privacy column; the moment it is gone — never triggered, dismissed,
  // submitted, or suppressed on this page view — the standing control takes over. Withdrawal must
  // stay as easy as granting was, and a suppressed widget must not be able to strand it.
  const synchronizePrivacyControl = async () => {
    const configuration = cache?.value
    const rule = currentAnalyticsRule ?? currentReplayRule
    // A control that is already up stays up even once its list empties: it is showing the outcome
    // of the withdrawal that emptied it, and removes itself when that outcome has been read.
    const wanted = liveFeedbackWidgets === 0 && configuration && rule &&
      (privacyControlUnmount !== null || privacyChoices().length > 0)
    const fingerprint = wanted ? `${rule.id}:${configuration.project_id}` : ''
    if (fingerprint === privacyControlFingerprint) return
    const transition = ++privacyControlTransition
    privacyControlFingerprint = fingerprint
    privacyControlUnmount?.()
    privacyControlUnmount = null
    if (!fingerprint || !rule || !configuration) return
    try {
      const module = (await import(
        new URL('privacy-controls.js', assetBase).href
      )) as PrivacyControlsModule
      if (transition !== privacyControlTransition) return
      privacyControlUnmount = module.mountPrivacyControls({
        rule,
        configuration,
        choices: privacyChoices,
        subscribe: privacy.subscribe,
        onRemoved: () => {
          privacyControlUnmount = null
          privacyControlFingerprint = ''
        },
      })
    } catch (error) {
      void report(error, 'privacy_controls')
    }
  }

  const holdFeedbackWidget = () => {
    liveFeedbackWidgets++
    void synchronizePrivacyControl()
  }

  const releaseFeedbackWidget = () => {
    liveFeedbackWidgets = Math.max(0, liveFeedbackWidgets - 1)
    void synchronizePrivacyControl()
  }

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
    if (rule.id !== analyticsGrantedRuleID || !currentPageView) return
    const event = ensureAnalyticsFunnel().record(eventType, rule, currentPageView, analyticsGrantToken)
    if (!event) return
    analyticsOutbox?.enqueue(event)
  }

  // The "submitted" conversion event is optionally embedded directly in the feedback submission
  // request rather than the outbox (see widget.ts). It uses the same gate as every other funnel
  // event: without an active grant for the current rule it returns null and feedback submission
  // proceeds without any analytics attachment (issue #58).
  const recordSubmissionAnalytics = (rule: WidgetRule) => {
    if (rule.id !== analyticsGrantedRuleID || !currentPageView) return null
    return ensureAnalyticsFunnel().record('submitted', rule, currentPageView, analyticsGrantToken)
  }

  const evaluate = async (forceRefresh = false) => {
    const currentEvaluation = ++evaluation
    const evaluationURL = window.location.href
    currentPageView = pageViews.visit(evaluationURL)
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
			await synchronizeAnalyticsConsent(configuration, rule)
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
            holdFeedbackWidget()
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
              submissionAnalytics: () => recordSubmissionAnalytics(rule),
              privacy,
              onClosed: releaseFeedbackWidget,
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
    } finally {
      // Covers every path that leaves no feedback panel behind — no rule, suppressed, or still
      // waiting on a trigger that may never fire.
      if (currentEvaluation === evaluation) void synchronizePrivacyControl()
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
      currentPageView = pageViews.visit(currentURL)
      const targeted = await matchRule([rule], currentURL, {
        loadRegex: () => import(new URL('targeting-regex.js', assetBase).href) as Promise<TargetingRegexModule>,
        onError: (error) => void report(error, 'targeting'),
      })
      if (explicitGenerations.get(target) !== generation) return explicitResult(false, 'not_found')
      if (!targeted) return explicitResult(false, 'not_targeted')
      const privacyLocation = privacySafeLocation(currentURL, rule)
      if (!privacyLocation) return explicitResult(false, 'not_targeted')
      await synchronizeAnalyticsConsent(configuration, rule)
      if (explicitGenerations.get(target) !== generation) return explicitResult(false, 'not_found')
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
      holdFeedbackWidget()
      const unmount = module.mountWidget({
        configuration, rule, publicKey: key, apiBase: assetBase, assetBase,
        pageURL: privacyLocation.target,
        replaySessionID: replayCapture?.sessionID ?? null,
        privacy,
        onClosed: releaseFeedbackWidget,
        target, hostID,
        onVisible: () => recordAnalytics('impression', rule),
        onInteraction: () => recordAnalytics('interaction', rule),
        submissionAnalytics: () => recordSubmissionAnalytics(rule),
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
    } finally {
      void synchronizePrivacyControl()
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
    // Read the grant synchronously after setRule: a concurrent evaluation for another rule (or
    // no rule) may retarget the shared controller while the prompt synchronization awaits.
    const grant = consent.activeGrant()
    await synchronizeConsentPrompt(configuration, rule)
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

  // Runs a prompt decision while deferring prompt re-synchronization: onChanged handlers still
  // update grant gating, replay capture, and the privacy surfaces immediately, but the dialog is
  // only re-rendered once the whole batch (e.g. "accept all" spanning both purposes) has settled,
  // so the user never sees an intermediate prompt for a purpose they are still deciding.
  const runConsentDecision = async <T>(action: () => Promise<T>): Promise<T> => {
    consentDecisionDepth++
    try {
      return await action()
    } finally {
      consentDecisionDepth--
      if (consentDecisionDepth === 0 && cache) void synchronizeConsentPrompt(cache.value, currentAnalyticsRule)
    }
  }

  // One prompt for every undecided Fidibeki-managed purpose on the current rule. Only the
  // presentation is shared: each purpose keeps its own controller, storage, and evidence endpoint
  // (issue #58). The dialog asks once and then leaves the host page for good — withdrawal lives in
  // the feedback widget's privacy column and the standing control (privacyChoices).
  const synchronizeConsentPrompt = async (configuration: WidgetConfiguration, rule: WidgetRule | null) => {
    if (consentDecisionDepth > 0) return
    const analyticsController = ensureAnalyticsConsent(configuration)
    const replayController = ensureReplayConsent(configuration)
    analyticsController.setRule(rule)
    replayController.setRule(rule)
    const purposes: ConsentPromptPurposeDescriptor[] = []
    if (rule && rule.analytics_consent.mode === 'fidibeki_managed' && analyticsController.status() === 'new') {
      const locale = analyticsConsentLocale(configuration, rule)
      if (locale) {
        purposes.push({
          purpose: 'widget_analytics',
          consent: rule.analytics_consent,
          locale,
          onAccept: () => analyticsController.grant({
            purpose: 'widget_analytics',
            noticeVersion: rule.analytics_consent.notice_version,
            locale,
            source: 'fidibeki_managed',
          }).then(() => {}),
          onReject: () => analyticsController.deny(locale),
        })
      }
    }
    if (rule && rule.replay_consent.mode === 'fidibeki_managed' && replayController.status() === 'new') {
      const locale = replayConsentLocale(configuration, rule)
      if (locale) {
        purposes.push({
          purpose: 'session_replay',
          consent: rule.replay_consent,
          locale,
          onAccept: () => replayController.grant({
            purpose: 'session_replay',
            noticeVersion: rule.replay_consent.notice_version,
            locale,
            source: 'fidibeki_managed',
          }).then(() => {}),
          onReject: () => replayController.deny(locale),
        })
      }
    }
    const fingerprint = rule && purposes.length > 0
      ? [rule.id, ...purposes.map(descriptor =>
          `${descriptor.purpose}:${descriptor.consent.notice_version}:${descriptor.locale}`)].join('|')
      : ''
    if (fingerprint === consentPromptFingerprint) return
    const transition = ++consentPromptTransition
    consentPromptFingerprint = fingerprint
    consentPromptUnmount?.()
    consentPromptUnmount = null
    if (!fingerprint) return
    try {
      const module = (await import(new URL('consent-prompt.js', assetBase).href)) as ConsentPromptModule
      if (transition !== consentPromptTransition) return
      consentPromptUnmount = module.mountConsentPrompt({
        purposes,
        runExclusive: runConsentDecision,
      })
    } catch (error) {
      void report(error, 'consent_prompt')
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

  const synchronizeAnalyticsConsent = async (configuration: WidgetConfiguration, rule: WidgetRule | null) => {
    currentAnalyticsRule = rule
    const consent = ensureAnalyticsConsent(configuration)
    consent.setRule(rule)
    // Read the grant synchronously after setRule: a concurrent evaluation for another rule (or
    // no rule) may retarget the shared controller while the prompt synchronization awaits.
    const grant = rule !== null && rule.analytics_consent.mode !== 'none' ? consent.activeGrant() : null
    if (grant && rule) {
      analyticsGrantedRuleID = rule.id
      analyticsGrantToken = grant.token
    } else {
      analyticsGrantedRuleID = null
      analyticsGrantToken = ''
    }
    await synchronizeConsentPrompt(configuration, rule)
  }

  installAnalyticsConsentAPI(
    async (assertion) => {
      const configuration = await loadConfiguration(false)
      const controller = ensureAnalyticsConsent(configuration)
      if (!currentAnalyticsRule) throw new Error('no analytics-enabled rule is active on this page')
      controller.setRule(currentAnalyticsRule)
      const grant = await controller.grant(assertion)
      if (currentAnalyticsRule) {
        analyticsGrantedRuleID = currentAnalyticsRule.id
        analyticsGrantToken = grant.token
      }
      return grant
    },
    async () => {
      if (!analyticsConsent) return
      await analyticsConsent.revoke()
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
      analyticsOutbox?.flushOnHide()
      return
    }
    void analyticsOutbox?.flush()
    void evaluate(true)
  })
  window.addEventListener('pagehide', () => analyticsOutbox?.flushOnHide())
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

function installAnalyticsConsentAPI(
  grantConsent: (assertion: AnalyticsConsentAssertion) => Promise<unknown>,
  revokeConsent: () => Promise<void>,
): void {
  const namespace = (window as unknown as { Fidibeki?: Record<string, unknown> }).Fidibeki ?? {}
  namespace.analytics = { grantConsent, revokeConsent }
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

function analyticsConsentLocale(configuration: WidgetConfiguration, rule: WidgetRule): string | null {
  const supported = rule.analytics_consent.supported_locales
    .map(locale => locale.trim().toLowerCase())
    .filter(locale => rule.analytics_consent.notices[locale] !== undefined)
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
