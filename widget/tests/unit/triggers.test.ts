import { armTrigger, scrollPercent } from '../../src/triggers'
import { rule } from './fixtures'

describe('widget triggers', () => {
  afterEach(() => vi.useRealTimers())

  it('fires a delay trigger once', () => {
    vi.useFakeTimers()
    const callback = vi.fn()
    armTrigger(rule({ trigger_delay_seconds: 2 }), callback)
    vi.advanceTimersByTime(1_999)
    expect(callback).not.toHaveBeenCalled()
    vi.advanceTimersByTime(1)
    expect(callback).toHaveBeenCalledOnce()
    vi.advanceTimersByTime(10_000)
    expect(callback).toHaveBeenCalledOnce()
  })

  it('cancels a delay trigger', () => {
    vi.useFakeTimers()
    const callback = vi.fn()
    const cleanup = armTrigger(rule({ trigger_delay_seconds: 1 }), callback)
    cleanup()
    vi.runAllTimers()
    expect(callback).not.toHaveBeenCalled()
  })

  it('fires when scroll depth crosses the configured threshold', () => {
    Object.defineProperty(window, 'innerHeight', { configurable: true, value: 100 })
    Object.defineProperty(window, 'scrollY', { configurable: true, value: 0 })
    Object.defineProperty(document.documentElement, 'scrollHeight', { configurable: true, value: 1_100 })
    const callback = vi.fn()
    armTrigger(rule({
      trigger_type: 'scroll', trigger_delay_seconds: null, trigger_scroll_percent: 50,
    }), callback)
    Object.defineProperty(window, 'scrollY', { configurable: true, value: 500 })
    window.dispatchEvent(new Event('scroll'))
    expect(callback).toHaveBeenCalledOnce()
    window.dispatchEvent(new Event('scroll'))
    expect(callback).toHaveBeenCalledOnce()
  })

  it('fires exit intent only through the top edge', () => {
    const callback = vi.fn()
    armTrigger(rule({ trigger_type: 'exit_intent', trigger_delay_seconds: null }), callback)
    document.dispatchEvent(new MouseEvent('mouseout', { clientY: 10, relatedTarget: null }))
    expect(callback).not.toHaveBeenCalled()
    document.dispatchEvent(new MouseEvent('mouseout', { clientY: 0, relatedTarget: null }))
    expect(callback).toHaveBeenCalledOnce()
  })

  it('treats a non-scrollable page as fully viewed', () => {
    Object.defineProperty(window, 'innerHeight', { configurable: true, value: 1_000 })
    Object.defineProperty(document.documentElement, 'scrollHeight', { configurable: true, value: 500 })
    expect(scrollPercent(window, document)).toBe(100)
  })
})
