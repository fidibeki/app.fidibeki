<script setup lang="ts">
import { permissionLabel, type PermissionPreset } from '~/data/permissions'

type PermissionSelection = PermissionPreset | 'custom'

defineProps<{
  groups: Array<{ label: string, permissions: readonly string[] }>
  permissions: string[]
  activePreset: PermissionSelection
  label: string
}>()

const emit = defineEmits<{
  applyPreset: [preset: PermissionPreset]
  togglePermission: [permission: string]
}>()

const presets: PermissionPreset[] = ['admin', 'editor', 'viewer']
</script>

<template>
  <section class="permission-editor" :aria-label="label">
    <header>
      <div><b>Permissions</b><span>{{ activePreset === 'custom' ? 'Custom selection' : `${activePreset} preset` }}</span></div>
      <div class="permission-presets" role="group" aria-label="Permission preset">
        <button
          v-for="preset in presets"
          :key="preset"
          type="button"
          :data-active="activePreset === preset"
          :aria-pressed="activePreset === preset"
          @click="emit('applyPreset', preset)"
        >{{ preset }}</button>
      </div>
    </header>
    <div class="permission-groups">
      <fieldset v-for="group in groups" :key="group.label">
        <legend>{{ group.label }} <small>{{ group.permissions.filter(permission => permissions.includes(permission)).length }}/{{ group.permissions.length }}</small></legend>
        <label v-for="permission in group.permissions" :key="permission" :data-granted="permissions.includes(permission)">
          <input type="checkbox" :checked="permissions.includes(permission)" @change="emit('togglePermission', permission)" />
          <span>{{ permissionLabel(permission) }}</span>
        </label>
      </fieldset>
    </div>
  </section>
</template>

<style scoped>
.permission-editor { margin-top: 16px; border-top: 1px solid var(--line); border-bottom: 1px solid var(--line); }
.permission-editor > header { display: flex; min-height: 50px; align-items: center; justify-content: space-between; gap: 14px; }
.permission-editor > header > div:first-child { display: flex; flex-direction: column; gap: 2px; }
.permission-editor > header b { color: var(--ink); font-size: 10px; }
.permission-editor > header span { color: var(--muted); font-size: 9px; text-transform: capitalize; }
.permission-presets { display: inline-grid; grid-template-columns: repeat(3, 1fr); padding: 2px; background: #edf2f0; border: 1px solid var(--line); border-radius: 5px; }
.permission-presets button { min-width: 62px; min-height: 29px; padding: 0 9px; background: transparent; color: var(--muted); border: 0; border-radius: 3px; font-size: 9px; font-weight: 750; text-transform: capitalize; }
.permission-presets button[data-active='true'] { background: #fff; color: var(--teal); box-shadow: 0 1px 2px rgb(20 42 36 / 12%); }
.permission-groups { padding-bottom: 8px; columns: 3; column-gap: 20px; }
.permission-groups fieldset { min-width: 0; margin: 0; padding: 12px 0 10px; break-inside: avoid; border: 0; border-top: 1px solid #e8edeb; }
.permission-groups legend { width: 100%; padding: 0 0 6px; color: var(--ink); font-size: 9px; font-weight: 800; }
.permission-groups legend small { float: right; color: var(--muted); font-size: 8px; font-weight: 650; }
.permission-groups label { display: flex; min-height: 27px; align-items: center; gap: 7px; color: var(--muted); font-size: 9px; text-transform: capitalize; }
.permission-groups label[data-granted='true'] { color: var(--ink-soft); font-weight: 650; }
.permission-groups input { width: 15px; height: 15px; flex: 0 0 15px; margin: 0; accent-color: var(--teal); }
.permission-groups label:focus-within { color: var(--teal); }

@media (max-width: 650px) {
  .permission-editor > header { align-items: stretch; flex-direction: column; gap: 9px; padding: 12px 0; }
  .permission-presets { width: 100%; }
  .permission-presets button { min-width: 0; }
  .permission-groups { columns: 1; }
}
</style>
