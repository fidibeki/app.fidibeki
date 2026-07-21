import { cacheIsFresh, fetchConfiguration, matchRule, reportConnection, type ConfigCache } from './config'
import { isSuppressed, suppress } from './suppression'
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
		replaySessionID: string | null
  }): WidgetUnmount
}

interface ReplayCapture {
	readonly sessionID: string
	stop(): Promise<void>
}

interface ReplayModule {
	startReplayCapture(options: {
		rule: WidgetRule
		publicKey: string
		apiBase: URL
		onError(error: unknown): void
	}): Promise<ReplayCapture | null>
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
  let cache: ConfigCache | null = null
  let cleanupTrigger: TriggerCleanup | null = null
  let unmountWidget: WidgetUnmount | null = null
	let replayCapture: ReplayCapture | null = null
	let replayFingerprint = ''
	let replayTransition = 0
  let evaluation = 0

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

  const loadConfiguration = async (force: boolean): Promise<WidgetConfiguration> => {
    const now = Date.now()
    if (!force && cacheIsFresh(cache, now)) return cache.value
    const value = await fetchConfiguration({
      endpoint: configurationEndpoint,
      publicKey: key,
    })
    cache = { value, fetchedAt: now }
    return value
  }

  const evaluate = async (forceRefresh = false) => {
    const currentEvaluation = ++evaluation
    cleanupTrigger?.()
    cleanupTrigger = null
    unmountWidget?.()
    unmountWidget = null
    try {
      const configuration = await loadConfiguration(forceRefresh)
      if (currentEvaluation !== evaluation) return
      const rule = matchRule(configuration.rules, window.location.href)
			if (rule) {
				void reportConnection({ endpoint: connectionEndpoint, publicKey: key, ruleID: rule.id }).catch(() => {})
			}
			await synchronizeReplay(configuration, rule)
			if (currentEvaluation !== evaluation) return
      if (!rule || isSuppressed(sessionStorage, configuration.project_id, rule.id, window.location.href)) return
      cleanupTrigger = armTrigger(rule, () => {
        suppress(sessionStorage, configuration.project_id, rule.id, window.location.href)
        void import(new URL('widget.js', assetBase).href)
          .then((module) => {
            unmountWidget = (module as WidgetModule).mountWidget({
              configuration,
              rule,
              publicKey: key,
              apiBase: assetBase,
              assetBase,
					replaySessionID: replayCapture?.sessionID ?? null,
            })
          })
          .catch((error: unknown) => report(error, 'mount'))
      })
    } catch (error) {
      void report(error, 'configuration')
    }
  }

	const synchronizeReplay = async (configuration: WidgetConfiguration, rule: WidgetRule | null) => {
		const enabled = rule !== null && rule.replay_mode !== 'none' && configuration.features.session_replay
		const fingerprint = enabled ? JSON.stringify({
			id: rule.id,
			mode: rule.replay_mode,
			sampling: rule.replay_sampling_rate,
			duration: rule.replay_max_duration_seconds,
			events: rule.replay_max_events,
			maskInputs: rule.replay_mask_all_inputs,
			maskText: rule.replay_mask_text_selector,
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
				onError: (error) => void report(error, 'replay'),
			})
			if (transition !== replayTransition) {
				if (capture) void capture.stop()
				return
			}
			replayCapture = capture
		} catch (error) {
			void report(error, 'replay')
		}
	}

  installNavigationEvents()
  window.addEventListener('fidibeki:navigation', () => void evaluate())
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible') void evaluate(true)
  })
  window.setInterval(() => void evaluate(true), 5 * 60 * 1000)
  await evaluate()
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
