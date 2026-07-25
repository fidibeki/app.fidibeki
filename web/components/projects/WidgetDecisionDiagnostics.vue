<script setup lang="ts">
import { CheckCircle2, Clock3, RotateCcw } from '@lucide/vue'
import { evaluateObjectivePolicy } from '../../../contracts/widget-objective-policy'
import type {
  RecurrenceMode,
  TriggerType,
  WidgetObjective,
  WidgetPresentation,
} from '~/types/api'
import {
  decisionGuardrailKey,
  decisionReasonKey,
  defaultDecisionSignals,
  humanizePolicyToken,
  recurrenceSummaryMessage,
  triggerSummaryMessage,
  type PolicyMessage,
} from '~/utils/widget-policy'

const props = defineProps<{
  objective: WidgetObjective
  presentation: WidgetPresentation
  triggerType: TriggerType
  triggerDelaySeconds: number | null
  triggerScrollPercent: number | null
  recurrenceMode: RecurrenceMode
  recurrenceDays: number | null
}>()

const { t } = useI18n()
const { integer, plural } = useRegionalFormat()
const activeSeconds = ref(0)
const signals = reactive(defaultDecisionSignals())
const decision = computed(() => evaluateObjectivePolicy(props.objective, props.presentation, {
  ...signals,
  activeTimeMs: activeSeconds.value * 1_000,
}))
const progress = computed(() => decision.value.requiredScore === 0
  ? 100
  : Math.min(100, Math.round((decision.value.score / decision.value.requiredScore) * 100)))
const trigger = computed(() => policyText(triggerSummaryMessage(
  props.triggerType,
  props.triggerDelaySeconds,
  props.triggerScrollPercent,
)))
const recurrence = computed(() => policyText(recurrenceSummaryMessage(
  props.recurrenceMode,
  props.recurrenceDays,
  props.presentation,
)))
const guardrails = computed(() => decision.value.guardrails
  .map(guardrail => translateToken(decisionGuardrailKey(guardrail), guardrail))
  .join(', '))
const reasons = computed(() => decision.value.reasons.length
  ? decision.value.reasons.map(reason => translateToken(decisionReasonKey(reason), reason)).join(', ')
  : t('projects.decision.noSignals'))

function policyText(message: PolicyMessage) {
  return message.count === undefined
    ? t(message.key, message.params)
    : plural(message.key, message.count, message.params)
}

function translateToken(key: string, token: string) {
  return key ? t(key) : humanizePolicyToken(token)
}

function reset() {
  activeSeconds.value = 0
  Object.assign(signals, defaultDecisionSignals())
}
</script>

<template>
  <div class="decision-diagnostics">
    <header>
      <div><h3>{{ t('projects.decision.title') }}</h3><span :data-eligible="decision.eligible"><CheckCircle2 v-if="decision.eligible" :size="14" /><Clock3 v-else :size="14" />{{ decision.eligible ? t('projects.decision.eligible') : t('projects.decision.waiting') }}</span></div>
      <button type="button" :title="t('projects.decision.reset')" :aria-label="t('projects.decision.reset')" @click="reset"><RotateCcw :size="14" /></button>
    </header>
    <div class="decision-meter"><span :style="{ width: `${progress}%` }" /><small>{{ t('projects.decision.score', { score: integer(decision.score), required: decision.requiredScore ? integer(decision.requiredScore) : t('projects.decision.openThreshold') }) }}</small></div>
    <div class="decision-signal-grid" role="group" :aria-label="t('projects.decision.signalsLabel')">
      <label><span>{{ t('projects.decision.activeTime') }} <small>{{ t('projects.decision.seconds', { value: integer(activeSeconds) }) }}</small></span><input v-model.number="activeSeconds" type="range" min="0" max="120" step="5" /></label>
      <label><span>{{ t('projects.decision.scrollDepth') }} <small>{{ t('projects.decision.percent', { value: integer(signals.scrollDepth) }) }}</small></span><input v-model.number="signals.scrollDepth" type="range" min="0" max="100" step="5" /></label>
      <label>{{ t('projects.decision.interactions') }}<input v-model.number="signals.interactionCount" class="app-input" type="number" min="0" max="20" /></label>
      <label>{{ t('projects.decision.visits') }}<input v-model.number="signals.visitCount" class="app-input" type="number" min="1" max="10" /></label>
    </div>
    <div class="decision-toggle-grid">
      <label><input v-model="signals.hasTitle" type="checkbox" /><span>{{ t('projects.decision.toggles.title') }}</span></label>
      <label><input v-model="signals.hasCanonical" type="checkbox" /><span>{{ t('projects.decision.toggles.canonical') }}</span></label>
      <label><input v-model="signals.hasDescription" type="checkbox" /><span>{{ t('projects.decision.toggles.description') }}</span></label>
      <label><input v-model="signals.explicitEngagement" type="checkbox" /><span>{{ t('projects.decision.toggles.engaged') }}</span></label>
      <label><input v-model="signals.explicitContent" type="checkbox" /><span>{{ t('projects.decision.toggles.content') }}</span></label>
      <label><input v-model="signals.explicitFriction" type="checkbox" /><span>{{ t('projects.decision.toggles.friction') }}</span></label>
    </div>
    <dl>
      <div><dt>{{ t('projects.decision.policy') }}</dt><dd>{{ guardrails }}</dd></div>
      <div><dt>{{ t('projects.decision.trigger') }}</dt><dd>{{ trigger }}</dd></div>
      <div><dt>{{ t('projects.decision.recurrence') }}</dt><dd>{{ recurrence }}</dd></div>
      <div><dt>{{ t('projects.decision.signals') }}</dt><dd>{{ reasons }}</dd></div>
    </dl>
  </div>
</template>

<style scoped>
.decision-diagnostics { display: grid; gap: 13px; }
.decision-diagnostics > header { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.decision-diagnostics > header > div { display: flex; align-items: center; gap: 10px; }
.decision-diagnostics h3 { margin: 0; font-size: 11px; }
.decision-diagnostics header span { display: inline-flex; align-items: center; gap: 5px; color: var(--amber); font-size: 9px; font-weight: 750; }
.decision-diagnostics header span[data-eligible='true'] { color: var(--green); }
.decision-diagnostics header button { display: grid; width: 30px; height: 30px; place-items: center; background: transparent; color: var(--muted); border: 1px solid var(--line); border-radius: 4px; }
.decision-meter { position: relative; height: 7px; overflow: visible; background: var(--surface-soft); border-radius: 2px; }
.decision-meter > span { display: block; height: 100%; background: var(--teal); border-radius: inherit; transition: width .16s ease; }
.decision-meter small { position: absolute; right: 0; bottom: 10px; color: var(--muted); font-size: 8px; font-variant-numeric: tabular-nums; }
.decision-signal-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px 12px; }
.decision-signal-grid label { display: flex; min-width: 0; flex-direction: column; gap: 6px; color: var(--muted); font-size: 9px; font-weight: 700; }
.decision-signal-grid label > span { display: flex; justify-content: space-between; gap: 8px; }
.decision-signal-grid small { color: var(--ink-soft); font-variant-numeric: tabular-nums; }
.decision-signal-grid input[type='range'] { width: 100%; accent-color: var(--teal); }
.decision-toggle-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 6px; }
.decision-toggle-grid label { display: flex; min-height: 34px; align-items: center; gap: 6px; padding: 0 8px; color: var(--ink-soft); border: 1px solid var(--line); border-radius: 4px; font-size: 8px; font-weight: 700; }
.decision-toggle-grid input { margin: 0; }
.decision-diagnostics dl { display: grid; margin: 0; border-top: 1px solid var(--line); }
.decision-diagnostics dl > div { display: grid; grid-template-columns: 74px minmax(0, 1fr); gap: 10px; padding: 7px 0; border-bottom: 1px solid var(--line); }
.decision-diagnostics dt { color: var(--muted); font-size: 8px; font-weight: 750; text-transform: uppercase; }
.decision-diagnostics dd { min-width: 0; margin: 0; color: var(--ink-soft); font-size: 9px; line-height: 1.4; }
@media (max-width: 600px) {
  .decision-signal-grid { grid-template-columns: 1fr; }
  .decision-toggle-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
</style>
