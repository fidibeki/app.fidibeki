<script setup lang="ts">
interface TurnstileAPI {
  render: (container: HTMLElement, options: Record<string, unknown>) => string
  reset: (widgetID: string) => void
  remove: (widgetID: string) => void
}

const props = defineProps<{ siteKey: string }>()
const { t } = useI18n()
const emit = defineEmits<{
  verified: [token: string]
  expired: []
  failed: []
}>()

const container = ref<HTMLElement | null>(null)
let widgetID = ''
let loader: Promise<TurnstileAPI> | null = null

function browserTurnstile() {
  return (window as typeof window & { turnstile?: TurnstileAPI }).turnstile
}

function loadTurnstile() {
  const ready = browserTurnstile()
  if (ready) return Promise.resolve(ready)
  if (loader) return loader

  loader = new Promise<TurnstileAPI>((resolve, reject) => {
    const existing = document.querySelector<HTMLScriptElement>('script[data-fidibeki-turnstile]')
    const script = existing || document.createElement('script')
    const fail = () => {
      script.remove()
      loader = null
      reject(new Error('Turnstile could not be loaded'))
    }
    const finish = () => {
      const api = browserTurnstile()
      if (api) resolve(api)
      else fail()
    }
    script.addEventListener('load', finish, { once: true })
    script.addEventListener('error', fail, { once: true })
    if (!existing) {
      script.src = 'https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit'
      script.async = true
      script.defer = true
      script.dataset.fidibekiTurnstile = 'true'
      document.head.appendChild(script)
    }
  })
  return loader
}

async function renderWidget() {
  if (!props.siteKey || !container.value || widgetID) return
  try {
    const api = await loadTurnstile()
    widgetID = api.render(container.value, {
      sitekey: props.siteKey,
      action: 'signup',
      theme: 'light',
      size: 'flexible',
      callback: (token: string) => emit('verified', token),
      'expired-callback': () => emit('expired'),
      'error-callback': () => emit('failed'),
    })
  } catch {
    emit('failed')
  }
}

function reset() {
  const api = browserTurnstile()
  if (api && widgetID) api.reset(widgetID)
}

async function retry() {
  const api = browserTurnstile()
  if (api && widgetID) {
    api.reset(widgetID)
    return
  }
  loader = null
  document.querySelector<HTMLScriptElement>('script[data-fidibeki-turnstile]')?.remove()
  await nextTick()
  await renderWidget()
}

onMounted(async () => {
  await nextTick()
  await renderWidget()
})
onBeforeUnmount(() => {
  const api = browserTurnstile()
  if (api && widgetID) api.remove(widgetID)
  widgetID = ''
})

defineExpose({ reset, retry })
</script>

<template>
  <div v-if="siteKey" ref="container" class="turnstile-widget" :aria-label="t('auth.securityVerification')" />
</template>

<style scoped>
.turnstile-widget { min-height: 65px; }
</style>
