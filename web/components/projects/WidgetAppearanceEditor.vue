<script setup lang="ts">
import { RotateCcw } from '@lucide/vue'
import type { WidgetAppearance, WidgetAppearanceOverrides, WidgetPosition, WidgetTheme } from '~/types/api'
import { contrastRatio, resolveWidgetAppearance } from '~/utils/widget-appearance'

const props = defineProps<{
  modelValue: WidgetAppearanceOverrides
  parent?: WidgetAppearance
  allowLogoReset?: boolean
  errors?: Record<string, string>
  fieldPathPrefix?: string
}>()
const emit = defineEmits<{
  'update:modelValue': [value: WidgetAppearanceOverrides]
  'field-edited': [field: string]
}>()
const { t } = useI18n()
const { decimal } = useRegionalFormat()
const identifier = useId()

const effective = computed(() => resolveWidgetAppearance(props.parent, props.modelValue))
const logoReset = computed(() => props.modelValue.logo_url === '')
const accentContrast = computed(() => decimal(
  contrastRatio(effective.value.accent_color, effective.value.accent_foreground),
  { minimumFractionDigits: 2, maximumFractionDigits: 2 },
))

function fieldPath(field: string) {
  return props.fieldPathPrefix ? `${props.fieldPathPrefix}.${field}` : field
}

function fieldError(field: string) {
  return props.errors?.[field] || ''
}

function fieldErrorID(field: string) {
  return `${identifier}-${field.replaceAll('_', '-')}-error`
}

function fieldAttributes(field: string) {
  const error = fieldError(field)
  return {
    'data-field-path': fieldPath(field),
    'aria-invalid': error ? true : undefined,
    'aria-describedby': error ? fieldErrorID(field) : undefined,
  }
}

function update(field: keyof WidgetAppearanceOverrides, patch: Partial<WidgetAppearanceOverrides>) {
  emit('update:modelValue', { ...props.modelValue, ...patch })
  emit('field-edited', fieldPath(field))
}

function setChoice(field: 'position' | 'theme', value: string) {
  const next = { ...props.modelValue }
  if (!value) delete next[field]
  else if (field === 'position') next.position = value as WidgetPosition
  else next.theme = value as WidgetTheme
  emit('update:modelValue', next)
  emit('field-edited', fieldPath(field))
}

function setColor(field: 'accent_color' | 'surface_color', value: string) {
  if (field === 'surface_color') {
    emit('update:modelValue', {
      ...props.modelValue,
      surface_color: value,
      text_color: props.modelValue.text_color || effective.value.text_color || '#17211f',
    })
    emit('field-edited', fieldPath('surface_color'))
    emit('field-edited', fieldPath('text_color'))
    return
  }
  update('accent_color', { accent_color: value })
}

function resetColor(field: 'accent_color' | 'surface_color') {
  const next = { ...props.modelValue }
  if (field === 'surface_color') {
    delete next.surface_color
    delete next.text_color
  } else delete next.accent_color
  emit('update:modelValue', next)
  emit('field-edited', fieldPath(field))
  if (field === 'surface_color') emit('field-edited', fieldPath('text_color'))
}

function setLogoReset(enabled: boolean) {
  const next = { ...props.modelValue }
  if (enabled) next.logo_url = ''
  else delete next.logo_url
  emit('update:modelValue', next)
  emit('field-edited', fieldPath('logo_url'))
}
</script>

<template>
  <div class="appearance-editor">
    <label>
      {{ t('projects.appearance.position') }}
      <select
        class="app-select"
        :value="modelValue.position || ''"
        v-bind="fieldAttributes('position')"
        @change="setChoice('position', ($event.target as HTMLSelectElement).value)"
      >
        <option value="">{{ t('projects.appearance.inherit') }}</option>
        <option value="bottom-left">{{ t('projects.appearance.positions.bottomLeft') }}</option>
        <option value="bottom-right">{{ t('projects.appearance.positions.bottomRight') }}</option>
      </select>
      <small v-if="fieldError('position')" :id="fieldErrorID('position')" class="appearance-field-error">{{ fieldError('position') }}</small>
    </label>
    <label>
      {{ t('projects.appearance.theme') }}
      <select
        class="app-select"
        :value="modelValue.theme || ''"
        v-bind="fieldAttributes('theme')"
        @change="setChoice('theme', ($event.target as HTMLSelectElement).value)"
      >
        <option value="">{{ t('projects.appearance.inherit') }}</option>
        <option value="light">{{ t('projects.appearance.themes.light') }}</option>
        <option value="dark">{{ t('projects.appearance.themes.dark') }}</option>
        <option value="auto">{{ t('projects.appearance.themes.auto') }}</option>
      </select>
      <small v-if="fieldError('theme')" :id="fieldErrorID('theme')" class="appearance-field-error">{{ fieldError('theme') }}</small>
    </label>
    <label>
      {{ t('projects.appearance.accentColor') }}
      <span class="appearance-color" :data-invalid="Boolean(fieldError('accent_color'))">
        <input
          type="color"
          :value="modelValue.accent_color || effective.accent_color"
          :aria-label="t('projects.appearance.accentSwatch')"
          v-bind="fieldAttributes('accent_color')"
          @input="setColor('accent_color', ($event.target as HTMLInputElement).value)"
        />
        <code>{{ modelValue.accent_color || t('projects.appearance.inherited', { value: effective.accent_color }) }}</code>
        <button v-if="modelValue.accent_color" type="button" :title="t('projects.appearance.inheritAccent')" @click="resetColor('accent_color')"><RotateCcw :size="13" /></button>
      </span>
      <small v-if="fieldError('accent_color')" :id="fieldErrorID('accent_color')" class="appearance-field-error">{{ fieldError('accent_color') }}</small>
    </label>
    <label>
      {{ t('projects.appearance.surfaceColor') }}
      <span class="appearance-color" :data-invalid="Boolean(fieldError('surface_color'))">
        <input
          type="color"
          :value="modelValue.surface_color || effective.surface_color || '#ffffff'"
          :aria-label="t('projects.appearance.surfaceSwatch')"
          v-bind="fieldAttributes('surface_color')"
          @input="setColor('surface_color', ($event.target as HTMLInputElement).value)"
        />
        <code>{{ modelValue.surface_color || t('projects.appearance.inherited', { value: effective.surface_color || '#ffffff' }) }}</code>
        <button v-if="modelValue.surface_color" type="button" :title="t('projects.appearance.inheritSurface')" @click="resetColor('surface_color')"><RotateCcw :size="13" /></button>
      </span>
      <small v-if="fieldError('surface_color')" :id="fieldErrorID('surface_color')" class="appearance-field-error">{{ fieldError('surface_color') }}</small>
    </label>
    <label>
      {{ t('projects.appearance.textColor') }}
      <span class="appearance-color" :data-invalid="Boolean(fieldError('text_color'))">
        <input
          type="color"
          :value="modelValue.text_color || effective.text_color || '#17211f'"
          :aria-label="t('projects.appearance.textSwatch')"
          v-bind="fieldAttributes('text_color')"
          @input="update('text_color', { surface_color: modelValue.surface_color || effective.surface_color || '#ffffff', text_color: ($event.target as HTMLInputElement).value })"
        />
        <code>{{ modelValue.text_color || t('projects.appearance.inherited', { value: effective.text_color || '#17211f' }) }}</code>
        <button v-if="modelValue.text_color" type="button" :title="t('projects.appearance.inheritSurface')" @click="resetColor('surface_color')"><RotateCcw :size="13" /></button>
      </span>
      <small v-if="fieldError('text_color')" :id="fieldErrorID('text_color')" class="appearance-field-error">{{ fieldError('text_color') }}</small>
    </label>
    <div class="appearance-accent-preview wide">
      <span :style="{ background: effective.accent_color, color: effective.accent_foreground }">{{ t('projects.appearance.foregroundSample') }}</span>
      <p>
        <b>{{ t('projects.appearance.foregroundPreview') }}</b>
        <code>{{ effective.accent_foreground }}</code>
        <small>{{ t('projects.appearance.contrastRatio', { ratio: accentContrast }) }}</small>
      </p>
    </div>
    <label class="wide">
      {{ t('projects.appearance.logoURL') }}
      <input
        class="app-input"
        type="url"
        maxlength="2048"
        placeholder="https://cdn.example.com/logo.svg"
        :value="logoReset ? '' : modelValue.logo_url || ''"
        :disabled="logoReset"
        v-bind="fieldAttributes('logo_url')"
        @input="update('logo_url', { logo_url: ($event.target as HTMLInputElement).value })"
      />
      <small v-if="fieldError('logo_url')" :id="fieldErrorID('logo_url')" class="appearance-field-error">{{ fieldError('logo_url') }}</small>
    </label>
    <label v-if="allowLogoReset" class="appearance-reset wide"><input type="checkbox" :checked="logoReset" @change="setLogoReset(($event.target as HTMLInputElement).checked)" /><span>{{ t('projects.appearance.hideInheritedLogo') }}</span></label>
  </div>
</template>

<style scoped>
.appearance-editor { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
.appearance-editor > label { display: flex; min-width: 0; flex-direction: column; gap: 6px; color: var(--muted); font-size: 9px; font-weight: 700; }
.appearance-editor > label.wide { grid-column: 1 / -1; }
.appearance-color { display: grid; height: 38px; grid-template-columns: 38px minmax(0, 1fr) 34px; align-items: center; overflow: hidden; border: 1px solid var(--line-strong); border-radius: 5px; }
.appearance-color input { width: 38px; height: 38px; padding: 3px; background: transparent; border: 0; border-right: 1px solid var(--line); }
.appearance-color code { min-width: 0; overflow: hidden; padding: 0 8px; color: var(--muted); font-size: 8px; text-overflow: ellipsis; white-space: nowrap; }
.appearance-color button { display: grid; width: 34px; height: 36px; place-items: center; background: transparent; color: var(--muted); border: 0; border-left: 1px solid var(--line); }
.appearance-color[data-invalid='true'],
.appearance-editor :is(input, select)[aria-invalid='true'] { border-color: var(--red); box-shadow: 0 0 0 2px var(--red-glow); }
.appearance-field-error { color: var(--red); font-size: 9px; font-weight: 650; line-height: 1.4; }
.appearance-accent-preview { display: flex; min-height: 54px; align-items: center; gap: 10px; padding: 8px 10px; background: var(--surface-soft); border: 1px solid var(--line); border-radius: 5px; }
.appearance-accent-preview > span { display: grid; width: 38px; height: 38px; flex: 0 0 38px; place-items: center; border-radius: 4px; font-size: 12px; font-weight: 800; }
.appearance-accent-preview p { display: grid; min-width: 0; grid-template-columns: auto minmax(0, 1fr); gap: 3px 8px; margin: 0; }
.appearance-accent-preview b { color: var(--ink-soft); font-size: 9px; }
.appearance-accent-preview code { color: var(--muted); font-size: 8px; }
.appearance-accent-preview small { grid-column: 1 / -1; color: var(--muted); font-size: 8px; font-weight: 500; }
.appearance-reset { align-items: center; flex-direction: row !important; color: var(--ink-soft) !important; }
@media (max-width: 600px) { .appearance-editor { grid-template-columns: 1fr; } .appearance-editor > label.wide { grid-column: auto; } }
</style>
