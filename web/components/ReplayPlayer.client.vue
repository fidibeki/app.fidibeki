<script setup lang="ts">
import { AlertTriangle, MonitorPlay, RotateCcw } from '@lucide/vue'
import 'rrweb-player/dist/style.css'
import { dimensionsForViewport, dimensionsForWidth as fitDimensionsForWidth } from '~/utils/replay-player-geometry'

const props = defineProps<{
  url: string
  viewportWidth?: number
  viewportHeight?: number
}>()
const { $apiFetch } = useNuxtApp()
const { t, locale } = useI18n()
const { message: apiErrorMessage } = useLocalizedAPIError()
const { integer, plural } = useRegionalFormat()
const root = ref<HTMLElement | null>(null)
const target = ref<HTMLElement | null>(null)
const state = ref<'loading' | 'ready' | 'error'>('loading')
const errorMessage = ref('')
const eventCount = ref(0)
const isFullscreen = ref(false)
const fullscreenSupported = typeof document !== 'undefined' && Boolean(document.fullscreenEnabled)
type ReplayPlayerInstance = {
  $destroy: () => void
  $set: (props: { width: number, height: number }) => void
  triggerResize: () => void
}
let player: ReplayPlayerInstance | null = null
let resizeObserver: ResizeObserver | null = null
let renderedWidth = 0
let renderedHeight = 0
let resizeFrame = 0

function dimensionsForWidth(width: number) {
  return fitDimensionsForWidth(props, width)
}

function currentDimensions() {
  if (!target.value) return null
  const styles = getComputedStyle(target.value)
  const availableWidth = target.value.clientWidth
    - Number.parseFloat(styles.paddingLeft)
    - Number.parseFloat(styles.paddingRight)
  if (!isFullscreen.value) return dimensionsForWidth(availableWidth)
  const controllerHeight = target.value.querySelector<HTMLElement>('.rr-controller')?.offsetHeight || 0
  const availableHeight = target.value.clientHeight
    - Number.parseFloat(styles.paddingTop)
    - Number.parseFloat(styles.paddingBottom)
    - controllerHeight
  return dimensionsForViewport(props, availableWidth, availableHeight)
}

function applyDimensions() {
  const dimensions = currentDimensions()
  if (!player || !dimensions) return
  if (Math.abs(dimensions.width - renderedWidth) < 1 && Math.abs(dimensions.height - renderedHeight) < 1) return
  renderedWidth = dimensions.width
  renderedHeight = dimensions.height
  player.$set(dimensions)
  cancelAnimationFrame(resizeFrame)
  resizeFrame = requestAnimationFrame(() => player?.triggerResize())
}

async function toggleFullscreen() {
  if (!root.value || !fullscreenSupported) return
  try {
    if (document.fullscreenElement === root.value) {
      await document.exitFullscreen()
    } else {
      await root.value.requestFullscreen()
    }
  } catch {
    // Fullscreen request denied (e.g. missing user-activation) - leave state untouched.
  }
}

function onFullscreenChange() {
  isFullscreen.value = document.fullscreenElement === root.value
  // rrweb-player keeps its own legacy fullscreenchange listener that reacts to ANY fullscreen
  // element on the page (it checks document.webkitIsFullScreen, not element identity) and can
  // overwrite our dimensions with its own stale cache in the same microtask flush. Deferring to
  // requestAnimationFrame guarantees we run after that settles, so our sizing always wins.
  requestAnimationFrame(applyDimensions)
}

function onControllerClick(event: MouseEvent) {
  if (!target.value) return
  const buttons = target.value.querySelectorAll<HTMLButtonElement>('.rr-controller__btns > button')
  const fullscreenButton = buttons.item(buttons.length - 1)
  if (!fullscreenButton || !(event.target instanceof Node) || !fullscreenButton.contains(event.target)) return
  event.stopImmediatePropagation()
  event.preventDefault()
  toggleFullscreen()
}

function labelReplayControls() {
  if (!target.value) return
  const buttons = target.value.querySelectorAll<HTMLButtonElement>('.rr-controller__btns > button')
  const playPause = buttons.item(0)
  const fullscreen = buttons.item(buttons.length - 1)
  playPause?.setAttribute('aria-label', t('feedback.replay.playPause'))
  fullscreen?.setAttribute('aria-label', t('feedback.replay.fullscreen'))
  fullscreen?.setAttribute('aria-pressed', String(isFullscreen.value))
  target.value.querySelector<HTMLInputElement>('.rr-controller input[type="checkbox"]')
    ?.setAttribute('aria-label', t('feedback.replay.skipInactive'))
}

async function loadReplay() {
  state.value = 'loading'
  errorMessage.value = ''
  player?.$destroy?.()
  player = null
  if (target.value) target.value.replaceChildren()
  try {
    const payload = await $apiFetch<unknown>(props.url)
    if (!Array.isArray(payload) || payload.length === 0) {
      throw new Error('Replay stream did not contain rrweb events.')
    }
    eventCount.value = payload.length
    const { default: RRWebPlayer } = await import('rrweb-player')
    state.value = 'ready'
    await nextTick()
    if (!target.value) return
    const dimensions = currentDimensions() || dimensionsForWidth(target.value.clientWidth)
    renderedWidth = dimensions.width
    renderedHeight = dimensions.height
    player = new RRWebPlayer({
      target: target.value,
      props: {
        events: payload as never[],
        ...dimensions,
        autoPlay: false,
        showController: true,
        skipInactive: true,
        maxScale: 1,
      },
    }) as unknown as ReplayPlayerInstance
    labelReplayControls()
    resizeObserver?.disconnect()
    resizeObserver = new ResizeObserver(() => applyDimensions())
    if (root.value) resizeObserver.observe(root.value)
  } catch (error) {
    errorMessage.value = apiErrorMessage(error, 'feedback.errors.replay')
    state.value = 'error'
  }
}

onMounted(() => {
  loadReplay()
  target.value?.addEventListener('click', onControllerClick, true)
  document.addEventListener('fullscreenchange', onFullscreenChange)
})
watch(() => props.url, loadReplay)
watch(locale, () => nextTick(labelReplayControls))
watch(isFullscreen, () => labelReplayControls())
onBeforeUnmount(() => {
  target.value?.removeEventListener('click', onControllerClick, true)
  document.removeEventListener('fullscreenchange', onFullscreenChange)
  if (document.fullscreenElement === root.value) document.exitFullscreen().catch(() => {})
  resizeObserver?.disconnect()
  cancelAnimationFrame(resizeFrame)
  player?.$destroy?.()
})
</script>

<template>
  <div ref="root" class="replay-player" :class="{ 'replay-player--fullscreen': isFullscreen }">
    <div v-if="state === 'loading'" class="replay-player-state"><span class="app-spinner" /><h3>{{ t('feedback.replay.loading') }}</h3></div>
    <div v-else-if="state === 'error'" class="replay-player-state"><AlertTriangle :size="24" /><h3>{{ t('feedback.replay.unavailable') }}</h3><p>{{ errorMessage }}</p><button class="app-button" type="button" @click="loadReplay"><RotateCcw :size="14" /> {{ t('feedback.replay.retry') }}</button></div>
    <div v-show="state === 'ready'" ref="target" class="replay-player-target" />
    <span v-if="state === 'ready'" class="replay-player-count"><MonitorPlay :size="13" /> {{ plural('feedback.replay.events', eventCount, { count: integer(eventCount) }) }}</span>
  </div>
</template>

<style scoped>
.replay-player { position: relative; min-width: 0; min-height: 430px; overflow: hidden; background: var(--surface-soft); }
.replay-player-state { display: grid; min-height: 430px; place-items: center; align-content: center; gap: 9px; padding: 28px; color: var(--muted); text-align: center; }
.replay-player-state h3, .replay-player-state p { margin: 0; }
.replay-player-state h3 { color: var(--ink); font-size: 13px; }
.replay-player-state p { max-width: 480px; font-size: 10px; line-height: 1.5; }
.replay-player-target { display: grid; min-width: 0; min-height: 430px; place-items: center; padding: 14px; overflow: hidden; }
.replay-player-count { position: absolute; top: 9px; right: 9px; z-index: 3; display: inline-flex; align-items: center; gap: 5px; padding: 5px 7px; background: var(--surface); color: var(--ink-soft); border: 1px solid var(--line); border-radius: 4px; font-size: 9px; }
.replay-player-target :deep(.rr-player) { float: none; box-shadow: none; }
/* rrweb-player ships light-only controller chrome (near-black glyphs and
   timestamps, a white progress gutter). Restating it in theme tokens keeps the
   controls legible on the dark surface too. */
.replay-player-target :deep(.rr-controller) { background: var(--surface); color: var(--ink); }
.replay-player-target :deep(.rr-timeline__time) { color: var(--ink-soft); }
.replay-player-target :deep(.rr-controller__btns button) { color: var(--ink); }
.replay-player-target :deep(.rr-controller__btns button svg) { fill: currentColor; }
.replay-player-target :deep(.rr-controller__btns button.active) { color: var(--on-accent); }
.replay-player-target :deep(.rr-progress) { background: var(--surface-soft); border-top-color: var(--surface); border-bottom-color: var(--surface); }
.replay-player--fullscreen { display: flex; min-height: 0; background: #000; }
.replay-player--fullscreen .replay-player-target { flex: 1; min-height: 0; height: 100%; }
.replay-player--fullscreen .replay-player-target :deep(.rr-player) { max-width: 100%; max-height: 100%; }
</style>
