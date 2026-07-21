import type { WidgetRule } from './types'

export type TriggerCleanup = () => void

export interface TriggerEnvironment {
  window: Window
  document: Document
  setTimeout: typeof globalThis.setTimeout
  clearTimeout: typeof globalThis.clearTimeout
}

export function armTrigger(
  rule: WidgetRule,
  onTrigger: () => void,
  environment: TriggerEnvironment = browserEnvironment(),
): TriggerCleanup {
  let fired = false
  const fireOnce = () => {
    if (fired) return
    fired = true
    cleanup()
    onTrigger()
  }
  let cleanup: TriggerCleanup = () => undefined

  if (rule.trigger_type === 'delay') {
    const timeout = environment.setTimeout(fireOnce, Math.max(0, rule.trigger_delay_seconds ?? 0) * 1000)
    cleanup = () => environment.clearTimeout(timeout)
    return cleanup
  }

  if (rule.trigger_type === 'scroll') {
    const threshold = rule.trigger_scroll_percent ?? 100
    const onScroll = () => {
      if (scrollPercent(environment.window, environment.document) >= threshold) fireOnce()
    }
    cleanup = () => environment.window.removeEventListener('scroll', onScroll)
    environment.window.addEventListener('scroll', onScroll, { passive: true })
    onScroll()
    return cleanup
  }

  const onMouseLeave = (event: MouseEvent) => {
    if (event.clientY <= 0 && event.relatedTarget === null) fireOnce()
  }
  cleanup = () => environment.document.removeEventListener('mouseout', onMouseLeave)
  environment.document.addEventListener('mouseout', onMouseLeave)
  return cleanup
}

export function scrollPercent(windowObject: Window, documentObject: Document): number {
  const root = documentObject.documentElement
  const body = documentObject.body
  const height = Math.max(root.scrollHeight, root.offsetHeight, body?.scrollHeight ?? 0, body?.offsetHeight ?? 0)
  const traversable = Math.max(0, height - windowObject.innerHeight)
  if (traversable === 0) return 100
  return Math.min(100, Math.max(0, (windowObject.scrollY / traversable) * 100))
}

function browserEnvironment(): TriggerEnvironment {
  return {
    window,
    document,
    setTimeout: globalThis.setTimeout.bind(globalThis),
    clearTimeout: globalThis.clearTimeout.bind(globalThis),
  }
}
