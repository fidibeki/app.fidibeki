<script setup lang="ts">
import { Tag, UserRound, X } from '@lucide/vue'
import type { Member, TriageAssignee, TriagePriority, TriageTag } from '~/types/api'

const props = defineProps<{
  priority?: TriagePriority | null
  assignee?: TriageAssignee | null
  tags: TriageTag[]
  tagOptions: TriageTag[]
  members: Member[]
  editable: boolean
  pending?: boolean
}>()

const emit = defineEmits<{
  setPriority: [priority: TriagePriority | null]
  setAssignee: [memberID: string | null]
  toggleTag: [tagID: string, selected: boolean]
}>()

const priorities: Array<{ value: TriagePriority, label: string }> = [
  { value: 'low', label: 'Low' },
  { value: 'medium', label: 'Medium' },
  { value: 'high', label: 'High' },
  { value: 'urgent', label: 'Urgent' },
]

const selectedTagIDs = computed(() => new Set(props.tags.map(tag => tag.id)))
const assigneeName = computed(() => props.assignee?.name || props.assignee?.email || 'Unassigned')

function onAssigneeChange(event: Event) {
  const value = (event.target as HTMLSelectElement).value
  emit('setAssignee', value || null)
}

function onTagChange(tagID: string, event: Event) {
  emit('toggleTag', tagID, (event.target as HTMLInputElement).checked)
}
</script>

<template>
  <section class="operational-triage" aria-labelledby="operational-triage-title">
    <header>
      <div>
        <span class="app-eyebrow">Operational triage</span>
        <h3 id="operational-triage-title">Ownership and priority</h3>
      </div>
      <span v-if="pending" class="app-spinner" aria-label="Saving triage" />
    </header>

    <template v-if="editable">
      <div class="triage-field">
        <span>Priority</span>
        <div class="triage-priority-control" role="group" aria-label="Priority">
          <button
            v-for="item in priorities"
            :key="item.value"
            type="button"
            :data-priority="item.value"
            :aria-label="`${item.label} priority`"
            :aria-pressed="priority === item.value"
            :disabled="pending"
            @click="emit('setPriority', item.value)"
          >{{ item.label }}</button>
          <button
            class="triage-clear"
            type="button"
            title="Clear priority"
            aria-label="Clear priority"
            :disabled="pending || !priority"
            @click="emit('setPriority', null)"
          ><X :size="13" /></button>
        </div>
      </div>

      <label class="triage-field">
        <span><UserRound :size="13" /> Assignee</span>
        <select :value="assignee?.id || ''" aria-label="Assignee" :disabled="pending" @change="onAssigneeChange">
          <option value="">Unassigned</option>
          <option v-for="member in members" :key="member.id" :value="member.id">
            {{ member.name || member.email }}
          </option>
        </select>
      </label>

      <fieldset class="triage-field triage-tag-picker">
        <legend><Tag :size="13" /> Tags</legend>
        <div>
          <label v-for="tag in tagOptions" :key="tag.id" :style="{ borderColor: tag.color }">
            <input
              type="checkbox"
              :checked="selectedTagIDs.has(tag.id)"
              :aria-label="`${tag.name} tag`"
              :disabled="pending"
              @change="onTagChange(tag.id, $event)"
            >
            <span :style="{ backgroundColor: tag.color }" />{{ tag.name }}
          </label>
          <span v-if="tagOptions.length === 0" class="triage-empty">No tags configured</span>
        </div>
      </fieldset>
    </template>

    <dl v-else class="triage-readonly">
      <div><dt>Priority</dt><dd>{{ priority ? priorities.find(item => item.value === priority)?.label : 'Unset' }}</dd></div>
      <div><dt>Assignee</dt><dd>{{ assigneeName }}</dd></div>
      <div><dt>Tags</dt><dd><span v-for="tag in tags" :key="tag.id" :style="{ borderColor: tag.color }">{{ tag.name }}</span><template v-if="tags.length === 0">None</template></dd></div>
    </dl>
  </section>
</template>

<style scoped>
.operational-triage { padding: 16px 0 18px; border-top: 1px solid var(--line); border-bottom: 1px solid var(--line); }
.operational-triage > header { display: flex; min-height: 34px; align-items: flex-start; justify-content: space-between; gap: 12px; margin-bottom: 13px; }
.operational-triage h3 { margin: 3px 0 0; font-size: 12px; }
.triage-field { display: grid; gap: 7px; margin: 0 0 13px; padding: 0; border: 0; }
.triage-field > span,
.triage-field legend { display: flex; align-items: center; gap: 5px; padding: 0; color: var(--muted); font-size: 9px; font-weight: 750; text-transform: uppercase; }
.triage-field select { width: 100%; height: 34px; padding: 0 9px; background: #fff; color: var(--ink); border: 1px solid var(--line); border-radius: 4px; font-size: 10px; }
.triage-priority-control { display: grid; min-height: 34px; grid-template-columns: repeat(4, minmax(0, 1fr)) 32px; }
.triage-priority-control button { min-width: 0; padding: 0 4px; background: #fff; color: var(--muted); border: 1px solid var(--line); border-right: 0; font-size: 9px; font-weight: 750; }
.triage-priority-control button:first-child { border-radius: 4px 0 0 4px; }
.triage-priority-control button:last-child { border-right: 1px solid var(--line); border-radius: 0 4px 4px 0; }
.triage-priority-control button[aria-pressed='true'] { position: relative; z-index: 1; color: #fff; border-color: #315b52; background: #315b52; }
.triage-priority-control button[data-priority='urgent'][aria-pressed='true'] { border-color: #a63e4a; background: #a63e4a; }
.triage-priority-control button[data-priority='high'][aria-pressed='true'] { border-color: #bd641f; background: #bd641f; }
.triage-priority-control button[data-priority='medium'][aria-pressed='true'] { border-color: #8a711d; background: #8a711d; }
.triage-priority-control .triage-clear { display: grid; place-items: center; color: var(--muted); }
.triage-tag-picker > div { display: flex; flex-wrap: wrap; gap: 6px; }
.triage-tag-picker label { display: inline-flex; min-height: 28px; align-items: center; gap: 5px; padding: 0 7px; background: #fff; color: var(--ink-soft); border: 1px solid; border-radius: 4px; font-size: 9px; }
.triage-tag-picker input { width: 13px; height: 13px; margin: 0; accent-color: var(--teal); }
.triage-tag-picker label > span { width: 7px; height: 7px; border-radius: 50%; }
.triage-empty { color: var(--muted); font-size: 10px; }
.triage-readonly { margin: 0; }
.triage-readonly div { display: grid; min-height: 34px; grid-template-columns: 82px minmax(0, 1fr); align-items: center; gap: 10px; border-bottom: 1px solid var(--line); }
.triage-readonly div:last-child { border: 0; }
.triage-readonly dt { color: var(--muted); font-size: 10px; }
.triage-readonly dd { display: flex; min-width: 0; flex-wrap: wrap; justify-content: flex-end; gap: 5px; margin: 0; font-size: 10px; text-align: right; }
.triage-readonly dd span { padding: 3px 6px; border: 1px solid; border-radius: 4px; }
</style>
