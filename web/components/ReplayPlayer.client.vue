<script setup lang="ts">
import { AlertTriangle, MonitorPlay, RotateCcw } from '@lucide/vue'
import 'rrweb-player/dist/style.css'

const props = defineProps<{
  url: string
  viewportWidth?: number
  viewportHeight?: number
}>()
const { $apiFetch } = useNuxtApp()
const target = ref<HTMLElement | null>(null)
const state = ref<'loading' | 'ready' | 'error'>('loading')
const errorMessage = ref('')
const eventCount = ref(0)
let player: { $destroy?: () => void, triggerResize?: () => void } | null = null
let resizeObserver: ResizeObserver | null = null

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
    await nextTick()
    if (!target.value) return
    const { default: RRWebPlayer } = await import('rrweb-player')
    const width = Math.max(320, props.viewportWidth || 1280)
    const height = Math.max(240, props.viewportHeight || Math.round(width * 0.5625))
    player = new RRWebPlayer({
      target: target.value,
      props: {
        events: payload as never[],
        width,
        height,
        autoPlay: false,
        showController: true,
        skipInactive: true,
        maxScale: 1,
      },
    })
    state.value = 'ready'
    resizeObserver?.disconnect()
    resizeObserver = new ResizeObserver(() => player?.triggerResize?.())
    resizeObserver.observe(target.value)
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : 'Replay could not be loaded.'
    state.value = 'error'
  }
}

onMounted(loadReplay)
watch(() => props.url, loadReplay)
onBeforeUnmount(() => {
  resizeObserver?.disconnect()
  player?.$destroy?.()
})
</script>

<template>
  <div class="replay-player">
    <div v-if="state === 'loading'" class="replay-player-state"><span class="app-spinner" /><h3>Loading replay</h3></div>
    <div v-else-if="state === 'error'" class="replay-player-state"><AlertTriangle :size="24" /><h3>Replay unavailable</h3><p>{{ errorMessage }}</p><button class="app-button" type="button" @click="loadReplay"><RotateCcw :size="14" /> Retry</button></div>
    <div v-show="state === 'ready'" ref="target" class="replay-player-target" />
    <span v-if="state === 'ready'" class="replay-player-count"><MonitorPlay :size="13" /> {{ eventCount.toLocaleString() }} events</span>
  </div>
</template>

<style scoped>
.replay-player { position: relative; min-height: 430px; overflow: hidden; background: #edf2f0; }
.replay-player-state { display: grid; min-height: 430px; place-items: center; align-content: center; gap: 9px; padding: 28px; color: var(--muted); text-align: center; }
.replay-player-state h3, .replay-player-state p { margin: 0; }
.replay-player-state h3 { color: var(--ink); font-size: 13px; }
.replay-player-state p { max-width: 480px; font-size: 10px; line-height: 1.5; }
.replay-player-target { display: grid; min-height: 430px; place-items: center; padding: 14px; overflow: auto; }
.replay-player-count { position: absolute; top: 9px; right: 9px; z-index: 3; display: inline-flex; align-items: center; gap: 5px; padding: 5px 7px; background: rgb(255 255 255 / 90%); color: var(--ink-soft); border: 1px solid var(--line); border-radius: 4px; font-size: 9px; }
.replay-player-target :deep(.rr-player) { max-width: 100%; box-shadow: none; }
.replay-player-target :deep(.rr-controller) { background: #fff; }
</style>
