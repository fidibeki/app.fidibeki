<script setup lang="ts">
import { RotateCcw } from '@lucide/vue'
import type { WidgetAppearance, WidgetAppearanceOverrides, WidgetPosition, WidgetTheme } from '~/types/api'
import { resolveWidgetAppearance } from '~/utils/widget-appearance'

const props = defineProps<{
  modelValue: WidgetAppearanceOverrides
  parent?: WidgetAppearance
  allowLogoReset?: boolean
}>()
const emit = defineEmits<{ 'update:modelValue': [value: WidgetAppearanceOverrides] }>()

const effective = computed(() => resolveWidgetAppearance(props.parent, props.modelValue))
const logoReset = computed(() => props.modelValue.logo_url === '')

function update(patch: Partial<WidgetAppearanceOverrides>) {
  emit('update:modelValue', { ...props.modelValue, ...patch })
}

function setChoice(field: 'position' | 'theme', value: string) {
  const next = { ...props.modelValue }
  if (!value) delete next[field]
  else if (field === 'position') next.position = value as WidgetPosition
  else next.theme = value as WidgetTheme
  emit('update:modelValue', next)
}

function setColor(field: 'accent_color' | 'surface_color', value: string) {
  if (field === 'surface_color') {
    update({ surface_color: value, text_color: props.modelValue.text_color || effective.value.text_color || '#17211f' })
    return
  }
  update({ accent_color: value })
}

function resetColor(field: 'accent_color' | 'surface_color') {
  const next = { ...props.modelValue }
  if (field === 'surface_color') {
    delete next.surface_color
    delete next.text_color
  } else delete next.accent_color
  emit('update:modelValue', next)
}

function setLogoReset(enabled: boolean) {
  const next = { ...props.modelValue }
  if (enabled) next.logo_url = ''
  else delete next.logo_url
  emit('update:modelValue', next)
}
</script>

<template>
  <div class="appearance-editor">
    <label>Position<select class="app-select" :value="modelValue.position || ''" @change="setChoice('position', ($event.target as HTMLSelectElement).value)"><option value="">Inherit</option><option value="bottom-left">Bottom left</option><option value="bottom-right">Bottom right</option></select></label>
    <label>Theme<select class="app-select" :value="modelValue.theme || ''" @change="setChoice('theme', ($event.target as HTMLSelectElement).value)"><option value="">Inherit</option><option value="light">Light</option><option value="dark">Dark</option><option value="auto">System</option></select></label>
    <label>Accent color<span class="appearance-color"><input type="color" :value="modelValue.accent_color || effective.accent_color" aria-label="Accent color swatch" @input="setColor('accent_color', ($event.target as HTMLInputElement).value)" /><code>{{ modelValue.accent_color || `Inherited ${effective.accent_color}` }}</code><button v-if="modelValue.accent_color" type="button" title="Inherit accent color" @click="resetColor('accent_color')"><RotateCcw :size="13" /></button></span></label>
    <label>Surface color<span class="appearance-color"><input type="color" :value="modelValue.surface_color || effective.surface_color || '#ffffff'" aria-label="Surface color swatch" @input="setColor('surface_color', ($event.target as HTMLInputElement).value)" /><code>{{ modelValue.surface_color || `Inherited ${effective.surface_color || '#ffffff'}` }}</code><button v-if="modelValue.surface_color" type="button" title="Inherit surface and text colors" @click="resetColor('surface_color')"><RotateCcw :size="13" /></button></span></label>
    <label>Text color<span class="appearance-color"><input type="color" :value="modelValue.text_color || effective.text_color || '#17211f'" aria-label="Text color swatch" @input="update({ surface_color: modelValue.surface_color || effective.surface_color || '#ffffff', text_color: ($event.target as HTMLInputElement).value })" /><code>{{ modelValue.text_color || `Inherited ${effective.text_color || '#17211f'}` }}</code><button v-if="modelValue.text_color" type="button" title="Inherit surface and text colors" @click="resetColor('surface_color')"><RotateCcw :size="13" /></button></span></label>
    <label class="wide">Logo URL<input class="app-input" type="url" maxlength="2048" placeholder="https://cdn.example.com/logo.svg" :value="logoReset ? '' : modelValue.logo_url || ''" :disabled="logoReset" @input="update({ logo_url: ($event.target as HTMLInputElement).value })" /></label>
    <label v-if="allowLogoReset" class="appearance-reset wide"><input type="checkbox" :checked="logoReset" @change="setLogoReset(($event.target as HTMLInputElement).checked)" /><span>Hide inherited logo</span></label>
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
.appearance-reset { align-items: center; flex-direction: row !important; color: var(--ink-soft) !important; }
@media (max-width: 600px) { .appearance-editor { grid-template-columns: 1fr; } .appearance-editor > label.wide { grid-column: auto; } }
</style>
