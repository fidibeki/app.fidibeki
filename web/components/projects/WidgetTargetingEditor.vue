<script setup lang="ts">
import { CircleCheck, CircleX, Globe2, Plus, Search, Trash2 } from '@lucide/vue'
import type { MatcherType, WidgetTargetConditionInput } from '~/types/api'
import {
  testTargetingURL,
  validateTargeting,
  type TargetingFormState,
  type TargetingMode,
  type TargetingTestDiagnostic,
  type TargetingTestResult,
} from '~/utils/widget-targeting'

const state = defineModel<TargetingFormState>({ required: true })
const props = withDefaults(defineProps<{ showErrors?: boolean }>(), { showErrors: false })
const emit = defineEmits<{ tested: [diagnostic: TargetingTestDiagnostic | null] }>()
const { t } = useI18n()
const testURL = ref('')
const testResult = ref<TargetingTestResult | null>(null)
const errors = computed(() => validateTargeting(state.value))
const matcherOptions: Array<{ value: MatcherType; labelKey: string }> = [
  { value: 'url_exact', labelKey: 'onboarding.targeting.exactURL' },
  { value: 'url_prefix', labelKey: 'onboarding.targeting.urlPrefix' },
  { value: 'url_regex', labelKey: 'onboarding.targeting.regularExpression' },
]
const targetingErrorKeys: Record<string, string> = {
  'Choose where this rule should appear.': 'choose',
  'Add between 1 and 20 include conditions.': 'includeCount',
  'Use no more than 20 exclude conditions.': 'excludeCount',
  'All pages must be the only include condition.': 'allPagesOnly',
  'All pages cannot be excluded.': 'allPagesExcluded',
  'All pages cannot have a value.': 'allPagesValue',
  'Enter a matcher value.': 'matcherValue',
  'Use no more than 2048 characters.': 'valueLength',
  'Enter a valid safe regular expression.': 'invalidRegex',
  'Enter an absolute HTTP(S) URL.': 'absoluteURL',
}

watch(state, resetTest, { deep: true })
watch(testURL, resetTest)

function selectMode(mode: Exclude<TargetingMode, null>) {
  if (mode === 'all_pages') {
    state.value = {
      mode,
      includes: [{ matcher_type: 'all_pages', matcher_value: null }],
      excludes: state.value.excludes,
    }
    return
  }
  const includes = state.value.includes.filter(condition => condition.matcher_type !== 'all_pages')
  state.value = {
    mode,
    includes: includes.length ? includes : [emptyCondition()],
    excludes: state.value.excludes,
  }
}

function addCondition(kind: 'includes' | 'excludes') {
  if (state.value[kind].length >= 20) return
  state.value[kind].push(emptyCondition())
}

function removeCondition(kind: 'includes' | 'excludes', index: number) {
  state.value[kind].splice(index, 1)
}

function changeMatcher(condition: WidgetTargetConditionInput) {
  condition.matcher_value = null
}

function conditionError(collection: 'include_conditions' | 'exclude_conditions', index: number) {
  return errors.value[`${collection}[${index}].matcher_value`]
    || errors.value[`${collection}[${index}].matcher_type`]
}

function targetingErrorMessage(error: string) {
  return t(`onboarding.targeting.errors.${targetingErrorKeys[error] || 'condition'}`)
}

function testResultMessage(result: TargetingTestResult) {
  return t(
    `onboarding.targeting.testResults.${result.status}`,
    result.status === 'excluded' ? { index: result.conditionIndex } : {},
  )
}

function runTest() {
  testResult.value = testTargetingURL(testURL.value, state.value)
  emit('tested', { url: testURL.value.trim(), result: testResult.value })
}

function resetTest() {
  testResult.value = null
  emit('tested', null)
}

function emptyCondition(): WidgetTargetConditionInput {
  return { matcher_type: 'url_exact', matcher_value: null }
}
</script>

<template>
  <div class="targeting-editor">
    <div class="targeting-mode" role="radiogroup" :aria-label="t('onboarding.targeting.targetPages')">
      <button type="button" role="radio" :aria-checked="state.mode === 'all_pages'" :data-active="state.mode === 'all_pages'" @click="selectMode('all_pages')">
        <Globe2 :size="14" /> {{ t('onboarding.targeting.allPages') }}
      </button>
      <button type="button" role="radio" :aria-checked="state.mode === 'specific_pages'" :data-active="state.mode === 'specific_pages'" @click="selectMode('specific_pages')">
        <Search :size="14" /> {{ t('onboarding.targeting.specificPages') }}
      </button>
    </div>
    <p v-if="props.showErrors && errors.targeting" class="targeting-error" role="alert">{{ t('onboarding.targeting.errors.choose') }}</p>

    <template v-if="state.mode">
      <div v-if="state.mode === 'all_pages'" class="targeting-all-pages"><Globe2 :size="14" /><span>{{ t('onboarding.targeting.everyAllowedPage') }}</span></div>
      <div v-else class="targeting-group">
        <header><div><b>{{ t('onboarding.targeting.include') }}</b><span>{{ state.includes.length }}/20</span></div><button type="button" :disabled="state.includes.length >= 20" @click="addCondition('includes')"><Plus :size="14" /> {{ t('onboarding.targeting.addInclude') }}</button></header>
        <div v-for="(condition, index) in state.includes" :key="`include-${index}`" class="targeting-condition">
          <select v-model="condition.matcher_type" class="app-select" :aria-label="t('onboarding.targeting.includeMatcher', { index: index + 1 })" @change="changeMatcher(condition)">
            <option v-for="option in matcherOptions" :key="option.value" :value="option.value">{{ t(option.labelKey) }}</option>
          </select>
          <input v-model="condition.matcher_value" class="app-input" :aria-label="t('onboarding.targeting.includeValue', { index: index + 1 })" :placeholder="condition.matcher_type === 'url_regex' ? '^https://example\\.com/docs' : 'https://example.com/docs'" maxlength="2048" required />
          <button type="button" class="targeting-remove" :aria-label="t('onboarding.targeting.removeInclude', { index: index + 1 })" :title="t('onboarding.targeting.removeInclude', { index: index + 1 })" @click="removeCondition('includes', index)"><Trash2 :size="14" /></button>
          <p v-if="props.showErrors && conditionError('include_conditions', index)" class="targeting-error">{{ targetingErrorMessage(conditionError('include_conditions', index)!) }}</p>
        </div>
        <p v-if="props.showErrors && errors.include_conditions" class="targeting-error" role="alert">{{ t('onboarding.targeting.errors.includes') }}</p>
      </div>

      <div class="targeting-group">
        <header><div><b>{{ t('onboarding.targeting.exclude') }}</b><span>{{ state.excludes.length }}/20</span></div><button type="button" :disabled="state.excludes.length >= 20" @click="addCondition('excludes')"><Plus :size="14" /> {{ t('onboarding.targeting.addExclude') }}</button></header>
        <div v-for="(condition, index) in state.excludes" :key="`exclude-${index}`" class="targeting-condition">
          <select v-model="condition.matcher_type" class="app-select" :aria-label="t('onboarding.targeting.excludeMatcher', { index: index + 1 })" @change="changeMatcher(condition)">
            <option v-for="option in matcherOptions" :key="option.value" :value="option.value">{{ t(option.labelKey) }}</option>
          </select>
          <input v-model="condition.matcher_value" class="app-input" :aria-label="t('onboarding.targeting.excludeValue', { index: index + 1 })" :placeholder="condition.matcher_type === 'url_regex' ? '^https://example\\.com/account' : 'https://example.com/account'" maxlength="2048" required />
          <button type="button" class="targeting-remove" :aria-label="t('onboarding.targeting.removeExclude', { index: index + 1 })" :title="t('onboarding.targeting.removeExclude', { index: index + 1 })" @click="removeCondition('excludes', index)"><Trash2 :size="14" /></button>
          <p v-if="props.showErrors && conditionError('exclude_conditions', index)" class="targeting-error">{{ targetingErrorMessage(conditionError('exclude_conditions', index)!) }}</p>
        </div>
        <p v-if="props.showErrors && errors.exclude_conditions" class="targeting-error" role="alert">{{ t('onboarding.targeting.errors.excludes') }}</p>
      </div>

      <div class="targeting-tester">
        <label>{{ t('onboarding.targeting.testURL') }}<input v-model="testURL" class="app-input" type="url" placeholder="https://example.com/docs?source=test" /></label>
        <button class="app-button" type="button" @click="runTest"><Search :size="14" /> {{ t('onboarding.targeting.test') }}</button>
        <p v-if="testResult" :data-status="testResult.status" role="status">
          <CircleCheck v-if="testResult.status === 'eligible'" :size="14" />
          <CircleX v-else :size="14" />
          {{ testResultMessage(testResult) }}
        </p>
      </div>
    </template>
  </div>
</template>

<style scoped>
.targeting-editor { display: flex; flex-direction: column; gap: 14px; }
.targeting-mode { display: inline-grid; width: min(100%, 360px); grid-template-columns: 1fr 1fr; padding: 3px; background: #eef2f1; border: 1px solid var(--line); border-radius: 5px; }
.targeting-mode button { display: flex; min-height: 34px; align-items: center; justify-content: center; gap: 7px; padding: 0 10px; background: transparent; color: var(--muted); border: 0; border-radius: 3px; font-size: 9px; font-weight: 750; }
.targeting-mode button[data-active='true'] { background: #fff; color: var(--ink); box-shadow: 0 1px 3px rgb(20 42 36 / 12%); }
.targeting-all-pages { display: flex; min-height: 38px; align-items: center; gap: 8px; padding: 0 10px; background: #f7faf9; color: var(--ink-soft); border: 1px solid var(--line); border-radius: 5px; font-size: 9px; font-weight: 700; }
.targeting-group { display: flex; flex-direction: column; gap: 7px; }
.targeting-group > header { display: flex; min-height: 28px; align-items: center; justify-content: space-between; gap: 10px; }
.targeting-group > header div { display: flex; align-items: center; gap: 6px; }
.targeting-group > header b { font-size: 9px; }
.targeting-group > header span { color: var(--muted); font-size: 8px; }
.targeting-group > header button { display: flex; min-height: 28px; align-items: center; gap: 5px; padding: 0 7px; background: transparent; color: var(--teal); border: 0; border-radius: 4px; font-size: 8px; font-weight: 750; }
.targeting-group > header button:hover { background: var(--teal-soft); }
.targeting-condition { display: grid; grid-template-columns: minmax(118px, .34fr) minmax(0, 1fr) 32px; gap: 7px; align-items: start; }
.targeting-condition .app-select,
.targeting-condition .app-input { min-width: 0; height: 36px; font-size: 9px; }
.targeting-remove { display: grid; width: 32px; height: 36px; place-items: center; background: transparent; color: var(--muted); border: 1px solid var(--line); border-radius: 4px; }
.targeting-remove:hover { color: var(--red); border-color: #e4adb4; }
.targeting-condition .targeting-error { grid-column: 2 / -1; }
.targeting-error { margin: 0; color: var(--red); font-size: 8px; }
.targeting-tester { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 7px; align-items: end; padding-top: 12px; border-top: 1px solid var(--line); }
.targeting-tester label { display: flex; flex-direction: column; gap: 6px; color: var(--muted); font-size: 9px; font-weight: 700; }
.targeting-tester > p { display: flex; grid-column: 1 / -1; align-items: center; gap: 6px; margin: 2px 0 0; color: var(--muted); font-size: 8px; }
.targeting-tester > p[data-status='eligible'] { color: var(--green); }
.targeting-tester > p[data-status='excluded'],
.targeting-tester > p[data-status='invalid'] { color: var(--red); }
@media (max-width: 600px) {
  .targeting-mode { width: 100%; }
  .targeting-condition { grid-template-columns: minmax(0, 1fr) 32px; }
  .targeting-condition .app-select { grid-column: 1; }
  .targeting-condition .app-input { grid-column: 1; }
  .targeting-condition .targeting-remove { grid-column: 2; grid-row: 1 / span 2; height: 100%; }
  .targeting-condition .targeting-error { grid-column: 1 / -1; }
}
</style>
