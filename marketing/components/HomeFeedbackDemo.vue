<script setup lang="ts">
const { copy } = useMarketingI18n()

import {
  ArrowRight,
  ArrowUp,
  ArrowUpRight,
  Check,
  Inbox,
  ListTodo,
  MessageCircle,
  Pause,
  Pencil,
  Play,
  Reply,
  RotateCcw,
} from '@lucide/vue'

// One bounded story, started by the visitor. Keep every step readable throughout;
// motion only shows the handoff, and never advances to another screen.
const links = useAppLinks()
const ready = ref(false)
const draft = ref('')
const submitted = ref('')
const step = ref(0)
const playback = ref<'playing' | 'paused' | 'complete'>('complete')
const reducedMotion = ref(false)
const inView = ref(true)
const pageVisible = ref(true)
const feedbackInput = ref<HTMLTextAreaElement | null>(null)
const journey = ref<HTMLElement | null>(null)
const demoRoot = ref<HTMLElement | null>(null)
const samples = [
  {
    label: 'Report a bug',
    kind: 'bug',
    text: "The checkout button isn't working on my phone.",
  },
  {
    label: 'Suggest an idea',
    kind: 'idea',
    text: "I'd love to save my filters so I can pick up where I left off.",
  },
  {
    label: 'Ask a question',
    kind: 'question',
    text: 'Can I invite the rest of my team to this project?',
  },
]
const scenario = computed(
  () =>
    samples.find((sample) => copy(sample.text) === submitted.value)?.kind ??
    'feedback',
)
const isQuestion = computed(() => scenario.value === 'question')
const steps = computed(() => [
  {
    title: isQuestion.value
      ? 'Your question reaches the team.'
      : 'Your feedback reaches the team.',
    detail: isQuestion.value
      ? 'A conversation starts right inside the product.'
      : 'It arrives in Fidibeki, ready for someone to pick up.',
    icon: Inbox,
  },
  {
    title: isQuestion.value
      ? 'Someone picks up the conversation.'
      : 'Your team takes it from here.',
    detail: isQuestion.value
      ? 'Your team reads the question and writes a reply.'
      : 'They connect the report to an issue and track the work.',
    icon: isQuestion.value ? MessageCircle : ListTodo,
  },
  {
    title: isQuestion.value
      ? 'The answer comes back to you.'
      : 'You hear what changed.',
    detail: isQuestion.value
      ? 'The reply appears in the same website conversation.'
      : 'If you opted in, the team can send you an update.',
    icon: Reply,
  },
])
const exampleReply = computed(
  () =>
    ({
      bug: 'The checkout button works on mobile again. Thanks for catching that.',
      idea: 'You can now save your filters and pick up where you left off. Thanks for the idea.',
      question: 'Yes! Open your workspace settings to invite your teammates.',
      feedback:
        "We've made an update based on your feedback. Thanks for helping us improve.",
    })[scenario.value as 'bug' | 'idea' | 'question' | 'feedback'],
)

let timer: ReturnType<typeof setTimeout> | undefined
let remaining = 2200
let startedAt = 0
let observer: IntersectionObserver | undefined
let motionQuery: MediaQueryList | undefined

function stopTimer() {
  if (timer === undefined) return
  clearTimeout(timer)
  timer = undefined
  remaining = Math.max(0, remaining - (performance.now() - startedAt))
}
function schedule() {
  stopTimer()
  if (
    !submitted.value ||
    playback.value !== 'playing' ||
    !inView.value ||
    !pageVisible.value
  )
    return
  startedAt = performance.now()
  timer = setTimeout(() => {
    timer = undefined
    if (step.value < 2) {
      step.value++
      remaining = step.value === 1 ? 2800 : 1800
      schedule()
    } else {
      playback.value = 'complete'
    }
  }, remaining)
}
function startStory() {
  stopTimer()
  step.value = reducedMotion.value ? 2 : 0
  playback.value = reducedMotion.value ? 'complete' : 'playing'
  remaining = 2200
  schedule()
}
function togglePlayback() {
  if (playback.value === 'playing') {
    stopTimer()
    playback.value = 'paused'
  } else {
    playback.value = 'playing'
    schedule()
  }
}
function useSample(text: string) {
  draft.value = copy(text)
  feedbackInput.value?.focus()
}
async function previewFeedback() {
  if (!draft.value.trim()) return
  submitted.value = draft.value.trim()
  startStory()
  await nextTick()
  journey.value?.focus({ preventScroll: true })
}
async function resetDemo() {
  stopTimer()
  submitted.value = ''
  playback.value = 'complete'
  await nextTick()
  feedbackInput.value?.focus({ preventScroll: true })
}
function onVisibilityChange() {
  pageVisible.value = !document.hidden
  schedule()
}
function onMotionChange() {
  reducedMotion.value = motionQuery?.matches ?? false
  if (reducedMotion.value && submitted.value) {
    stopTimer()
    step.value = 2
    playback.value = 'complete'
  }
}
onMounted(() => {
  ready.value = true
  motionQuery = window.matchMedia('(prefers-reduced-motion: reduce)')
  onMotionChange()
  motionQuery.addEventListener('change', onMotionChange)
  pageVisible.value = !document.hidden
  document.addEventListener('visibilitychange', onVisibilityChange)
  observer = new IntersectionObserver(
    ([entry]) => {
      inView.value = !!entry?.isIntersecting && entry.intersectionRatio >= 0.15
      schedule()
    },
    { threshold: 0.15 },
  )
  if (demoRoot.value) observer.observe(demoRoot.value)
})
onBeforeUnmount(() => {
  stopTimer()
  observer?.disconnect()
  motionQuery?.removeEventListener('change', onMotionChange)
  document.removeEventListener('visibilitychange', onVisibilityChange)
})
</script>

<template>
  <section class="home-opening" aria-labelledby="home-title">
    <div
      ref="demoRoot"
      class="home-prompt"
      :class="{ 'demo-is-sent': submitted }"
    >
      <h1 id="home-title">{{ copy('What could be better?') }}</h1>
      <p class="home-intro">
        {{ copy('Website feedback and live chat, in one widget.') }}
      </p>
      <form
        v-if="!submitted"
        class="home-composer"
        @submit.prevent="previewFeedback"
      >
        <label for="home-feedback" class="home-visually-hidden">{{
          copy('Try an example feedback report')
        }}</label>
        <textarea
          id="home-feedback"
          ref="feedbackInput"
          v-model="draft"
          :disabled="!ready"
          maxlength="500"
          rows="2"
          :placeholder="
            copy('A small frustration. An idea. Something worth fixing.')
          "
          aria-describedby="demo-disclaimer"
        />
        <div class="home-composer-bottom">
          <span
            ><MessageCircle :size="16" aria-hidden="true" />
            {{ copy('Try Fidibeki') }}</span
          >
          <button
            class="home-send"
            type="submit"
            :disabled="!ready || !draft.trim()"
            :aria-label="copy('Preview feedback flow')"
          >
            <ArrowUp :size="20" aria-hidden="true" />
          </button>
        </div>
      </form>
      <div
        v-else
        ref="journey"
        class="feedback-journey"
        role="region"
        :aria-label="copy('Your feedback journey')"
        tabindex="-1"
        :data-playback="playback"
        :data-step="step"
      >
        <div class="journey-message">
          <div class="journey-message-heading">
            <span>{{
              copy(isQuestion ? 'Your question' : 'Your feedback')
            }}</span>
            <div class="journey-message-controls">
              <button
                v-if="!reducedMotion"
                type="button"
                class="journey-control"
                :aria-label="
                  copy(
                    playback === 'complete'
                      ? 'Replay animation'
                      : playback === 'playing'
                        ? 'Pause animation'
                        : 'Resume animation',
                  )
                "
                @click="
                  playback === 'complete' ? startStory() : togglePlayback()
                "
              >
                <RotateCcw
                  v-if="playback === 'complete'"
                  :size="13"
                  aria-hidden="true"
                /><Pause
                  v-else-if="playback === 'playing'"
                  :size="13"
                  aria-hidden="true"
                /><Play v-else :size="13" aria-hidden="true" />{{
                  copy(
                    playback === 'complete'
                      ? 'Replay'
                      : playback === 'playing'
                        ? 'Pause'
                        : 'Resume',
                  )
                }}</button
              ><button
                type="button"
                class="journey-control"
                :aria-label="copy('Edit your feedback')"
                @click="resetDemo"
              >
                <Pencil :size="13" aria-hidden="true" /> {{ copy('Edit') }}
              </button>
            </div>
          </div>
          <blockquote>{{ submitted }}</blockquote>
        </div>
        <ol class="journey-track">
          <li
            v-for="(item, index) in steps"
            :key="index"
            :class="{
              'journey-reached': index <= step,
              'journey-current': index === step,
              'journey-passed': index < step || playback === 'complete',
            }"
            :aria-current="index === step ? 'step' : undefined"
          >
            <span class="journey-node" aria-hidden="true"
              ><Check
                v-if="index < step || playback === 'complete'"
                :size="15" /><component :is="item.icon" v-else :size="15"
            /></span>
            <div class="journey-copy">
              <h2>{{ copy(item.title) }}</h2>
              <p>{{ copy(item.detail) }}</p>
            </div>
          </li>
        </ol>
        <div
          class="journey-reply"
          :class="{ 'journey-reply-visible': step === 2 }"
          :aria-hidden="step !== 2"
        >
          <div class="journey-reply-heading">
            <span class="journey-reply-sender">{{ copy('Your team') }}</span>
            <span class="journey-reply-example">{{
              copy('Example reply')
            }}</span>
          </div>
          <p>{{ copy(exampleReply) }}</p>
        </div>
        <div class="journey-toolbar">
          <span>{{
            copy(
              playback === 'complete'
                ? 'The whole loop, in one place.'
                : 'Following your feedback…',
            )
          }}</span>
        </div>
        <p class="home-visually-hidden" role="status">
          {{ copy(steps[step]?.title) }}
          {{ copy(step === 2 ? exampleReply : '') }}
        </p>
      </div>
      <div
        v-if="!submitted"
        class="home-suggestions"
        role="group"
        :aria-label="copy('Example feedback')"
      >
        <button
          v-for="sample in samples"
          :key="sample.label"
          type="button"
          :disabled="!ready"
          @click="useSample(sample.text)"
        >
          {{ copy(sample.label) }}
          <ArrowUpRight :size="13" aria-hidden="true" />
        </button>
      </div>
      <div v-else class="journey-after">
        <button class="journey-control" type="button" @click="resetDemo">
          {{ copy('Try another example') }}
          <ArrowRight :size="14" aria-hidden="true" /></button
        ><a :href="links.signUp"
          >{{ copy('Try it with your product') }}
          <ArrowUpRight :size="14" aria-hidden="true"
        /></a>
      </div>
      <p id="demo-disclaimer" class="home-demo-note">
        {{
          copy(
            submitted
              ? 'An example of the workflow. Nothing has been sent.'
              : 'An interactive example. Nothing you type is sent.',
          )
        }}
      </p>
    </div>
  </section>
</template>

<style scoped>
.feedback-journey {
  margin-top: 31px;
  border: 1px solid var(--home-line);
  border-radius: 22px;
  text-align: left;
  overflow: hidden;
}
.feedback-journey:focus {
  outline: none;
}
.journey-message {
  padding: 15px 24px 21px;
  border-bottom: 1px solid var(--home-line);
}
.journey-message-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  color: var(--home-muted);
  font-size: 12px;
}
.journey-message-controls {
  display: flex;
  align-items: center;
  gap: 18px;
}
.journey-control {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  min-height: 32px;
  padding: 0 2px;
  border: 0;
  background: transparent;
  color: var(--home-muted);
  font-size: 12px;
  font-weight: 400;
  cursor: pointer;
}
.journey-control:hover {
  color: var(--home-ink);
}
.journey-message blockquote {
  margin: 6px 0 0;
  max-height: 112px;
  overflow-y: auto;
  overflow-wrap: anywhere;
  color: var(--home-ink);
  font-size: 18px;
  line-height: 1.5;
  letter-spacing: -0.015em;
}
.journey-track {
  list-style: none;
  margin: 0;
  padding: 24px 24px 0;
}
.journey-track li {
  position: relative;
  display: grid;
  grid-template-columns: 29px 1fr;
  gap: 14px;
  min-height: 77px;
  color: var(--home-muted);
}
.journey-track li:last-child {
  min-height: 63px;
}
.journey-track li:not(:last-child)::after {
  content: '';
  position: absolute;
  left: 14px;
  top: 35px;
  bottom: 6px;
  width: 1px;
  background: var(--home-line);
}
.journey-track li:not(:last-child)::before {
  content: '';
  position: absolute;
  z-index: 1;
  left: 14px;
  top: 35px;
  bottom: 6px;
  width: 1px;
  background: var(--home-ink);
  transform: scaleY(0);
  transform-origin: top;
  transition: transform 600ms cubic-bezier(0.16, 1, 0.3, 1);
}
.journey-track li.journey-passed::before {
  transform: scaleY(1);
}
.journey-node {
  position: relative;
  display: grid;
  place-items: center;
  width: 29px;
  height: 29px;
  border: 1px solid var(--home-line);
  border-radius: 50%;
  color: var(--home-muted);
  background: var(--home-canvas);
  transition:
    background-color 280ms,
    color 280ms,
    border-color 280ms;
}
.journey-current .journey-node {
  background: var(--home-ink);
  color: var(--home-canvas);
  border-color: var(--home-ink);
}
.journey-passed .journey-node {
  color: var(--home-ink);
  background: var(--home-soft);
}
.journey-copy {
  padding-top: 3px;
}
.journey-copy h2 {
  color: var(--home-muted);
  font-size: 16px;
  font-weight: 500;
  letter-spacing: -0.015em;
  line-height: 1.4;
  transition: color 280ms;
}
.journey-reached .journey-copy h2 {
  color: var(--home-ink);
}
.journey-copy p {
  margin-top: 4px;
  color: var(--home-muted);
  font-size: 13px;
  line-height: 1.5;
}
.journey-reply {
  min-height: 86px;
  margin: 0 24px 0 67px;
  padding: 15px 18px;
  border-radius: 10px;
  background: var(--home-soft);
  opacity: 0;
  transform: translateY(5px);
  transition:
    opacity 400ms cubic-bezier(0.16, 1, 0.3, 1),
    transform 400ms cubic-bezier(0.16, 1, 0.3, 1);
}
.journey-reply-visible {
  opacity: 1;
  transform: translateY(0);
}
.journey-reply-heading {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 4px 12px;
  line-height: 1.4;
}
.journey-reply-sender {
  color: var(--home-ink);
  font-size: 12px;
  font-weight: 500;
}
.journey-reply-example {
  color: var(--home-muted);
  font-size: 11px;
}
.journey-reply p {
  margin-top: 8px;
  color: var(--home-ink);
  font-size: 14px;
  line-height: 1.45;
}
.journey-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  min-height: 53px;
  padding: 8px 24px;
  margin-top: 8px;
  font-size: 11px;
  color: var(--home-muted);
}
.journey-after {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
  margin-top: 14px;
}
.journey-after > a {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  min-height: 32px;
  font-size: 12px;
}
@media (max-width: 600px) {
  .feedback-journey {
    margin-top: 26px;
    border-radius: 18px;
  }
  .journey-message {
    padding: 11px 17px 16px;
  }
  .journey-message blockquote {
    font-size: 16px;
  }
  .journey-message-controls {
    gap: 12px;
  }
  .journey-track {
    padding: 20px 17px 0;
  }
  .journey-track li {
    gap: 11px;
    min-height: 80px;
  }
  .journey-track li:last-child {
    min-height: 66px;
  }
  .journey-copy h2 {
    font-size: 14px;
  }
  .journey-copy p {
    font-size: 12px;
  }
  .journey-reply {
    margin-inline: 57px 17px;
    padding: 13px 14px;
    min-height: 100px;
  }
  .journey-reply p {
    font-size: 13px;
  }
  .journey-toolbar {
    padding-inline: 17px;
  }
  .journey-after {
    flex-wrap: wrap;
    gap: 2px 14px;
    justify-content: center;
  }
}
</style>
