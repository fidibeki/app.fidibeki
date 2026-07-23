<script setup lang="ts">
import { AlertTriangle, MonitorPlay, RotateCcw } from '@lucide/vue'
import 'rrweb-player/dist/style.css'

const props = defineProps<{
  url: string
  viewportWidth?: number
  viewportHeight?: number
}>()
const { $apiFetch } = useNuxtApp()
const { t, locale } = useI18n()
const { message: apiErrorMessage } = useLocalizedAPIError()
const { integer, plural } = useRegionalFormat()
const target = ref<HTMLElement | null>(null)
const state = ref<'loading' | 'ready' | 'error'>('loading')
const errorMessage = ref('')
const eventCount = ref(0)
type ReplayPlayerInstance = {
  $destroy: () => void
  $set: (props: { width: number, height: number }) => void
  triggerResize: () => void
}
let player: ReplayPlayerInstance | null = null
let resizeObserver: ResizeObserver | null = null
let renderedWidth = 0
let resizeFrame = 0

function dimensionsForWidth(width: number) {
  const sourceWidth = props.viewportWidth && props.viewportWidth > 0 ? props.viewportWidth : 1280
  const sourceHeight = props.viewportHeight && props.viewportHeight > 0
    ? props.viewportHeight
    : Math.round(sourceWidth * 0.5625)
  const playerWidth = Math.max(1, Math.floor(width))
  return {
    width: playerWidth,
    height: Math.max(1, Math.round(playerWidth * sourceHeight / sourceWidth)),
  }
}

function resizePlayer(width: number) {
  const dimensions = dimensionsForWidth(width)
  if (!player || Math.abs(dimensions.width - renderedWidth) < 1) return
  renderedWidth = dimensions.width
  player.$set(dimensions)
  cancelAnimationFrame(resizeFrame)
  resizeFrame = requestAnimationFrame(() => player?.triggerResize())
}

function labelReplayControls() {
  if (!target.value) return
  const buttons = target.value.querySelectorAll<HTMLButtonElement>('.rr-controller__btns > button')
  const playPause = buttons.item(0)
  const fullscreen = buttons.item(buttons.length - 1)
  playPause?.setAttribute('aria-label', t('feedback.replay.playPause'))
  fullscreen?.setAttribute('aria-label', t('feedback.replay.fullscreen'))
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
    const styles = getComputedStyle(target.value)
    const availableWidth = target.value.clientWidth
      - Number.parseFloat(styles.paddingLeft)
      - Number.parseFloat(styles.paddingRight)
    const dimensions = dimensionsForWidth(availableWidth)
    renderedWidth = dimensions.width
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
    resizeObserver = new ResizeObserver(entries => resizePlayer(entries[0]?.contentRect.width || 0))
    resizeObserver.observe(target.value)
  } catch (error) {
    errorMessage.value = apiErrorMessage(error, 'feedback.errors.replay')
    state.value = 'error'
  }
}

onMounted(loadReplay)
watch(() => props.url, loadReplay)
watch(locale, () => nextTick(labelReplayControls))
onBeforeUnmount(() => {
  resizeObserver?.disconnect()
  cancelAnimationFrame(resizeFrame)
  player?.$destroy?.()
})
</script>

<template>
  <div class="replay-player">
    <div v-if="state === 'loading'" class="replay-player-state"><span class="app-spinner" /><h3>{{ t('feedback.replay.loading') }}</h3></div>
    <div v-else-if="state === 'error'" class="replay-player-state"><AlertTriangle :size="24" /><h3>{{ t('feedback.replay.unavailable') }}</h3><p>{{ errorMessage }}</p><button class="app-button" type="button" @click="loadReplay"><RotateCcw :size="14" /> {{ t('feedback.replay.retry') }}</button></div>
    <div v-show="state === 'ready'" ref="target" class="replay-player-target" />
    <span v-if="state === 'ready'" class="replay-player-count"><MonitorPlay :size="13" /> {{ plural('feedback.replay.events', eventCount, { count: integer(eventCount) }) }}</span>
  </div>
</template>

<style scoped>
.replay-player { position: relative; min-width: 0; min-height: 430px; overflow: hidden; background: #edf2f0; }
.replay-player-state { display: grid; min-height: 430px; place-items: center; align-content: center; gap: 9px; padding: 28px; color: var(--muted); text-align: center; }
.replay-player-state h3, .replay-player-state p { margin: 0; }
.replay-player-state h3 { color: var(--ink); font-size: 13px; }
.replay-player-state p { max-width: 480px; font-size: 10px; line-height: 1.5; }
.replay-player-target { display: grid; min-width: 0; min-height: 430px; place-items: center; padding: 14px; overflow: hidden; }
.replay-player-count { position: absolute; top: 9px; right: 9px; z-index: 3; display: inline-flex; align-items: center; gap: 5px; padding: 5px 7px; background: rgb(255 255 255 / 90%); color: var(--ink-soft); border: 1px solid var(--line); border-radius: 4px; font-size: 9px; }
.replay-player-target :deep(.rr-player) { float: none; box-shadow: none; }
.replay-player-target :deep(.rr-controller) { background: #fff; }
</style>
