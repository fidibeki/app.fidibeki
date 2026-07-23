<script setup lang="ts">
import {
  Archive,
  ArrowUpRight,
  Check,
  ChevronDown,
  CircleDot,
  ListFilter,
  Link2,
  Link2Off,
  Plus,
  RotateCcw,
  ShieldAlert,
  Tag,
} from '@lucide/vue'
import type { FeedbackItem, Member, TriagePriority, TriageTag } from '~/types/api'

const props = defineProps<{
  count: number
  selectedItems: FeedbackItem[]
  members: Member[]
  tags: TriageTag[]
  pending: boolean
  aiEnabled: boolean
  showAIUpgrade: boolean
  issueActionsAvailable: boolean
  canCreateOrLink: boolean
  canUnlink: boolean
  issueActionReason?: string
}>()
const { t } = useI18n()

const emit = defineEmits<{
  mutate: [body: Record<string, unknown>, actionKey: string, parameters?: Record<string, unknown>]
  reprocess: []
  createIssue: []
  linkIssue: []
  unlinkIssue: []
}>()

const compact = ref(false)
const compactMenuOpen = ref(false)
const tagPopover = ref<HTMLDetailsElement>()
const issuePopover = ref<HTMLDetailsElement>()
let mediaQuery: MediaQueryList | undefined

const priorityValues: TriagePriority[] = ['low', 'medium', 'high', 'urgent']
const priorities = computed(() => priorityValues.map(value => ({
  value,
  label: t(`feedback.priority.${value}`),
})))

function syncCompact(event: MediaQueryList | MediaQueryListEvent) {
  compact.value = event.matches
  if (!event.matches) compactMenuOpen.value = false
}

onMounted(() => {
  mediaQuery = window.matchMedia('(max-width: 700px)')
  syncCompact(mediaQuery)
  mediaQuery.addEventListener('change', syncCompact)
})

onBeforeUnmount(() => mediaQuery?.removeEventListener('change', syncCompact))

function choosePriority(event: Event) {
  const select = event.target as HTMLSelectElement
  const value = select.value
  select.value = ''
  if (!value) return
  mutate({ priority: value }, value === 'none' ? 'bulkPriorityCleared' : 'bulkPrioritized')
}

function chooseAssignee(event: Event) {
  const select = event.target as HTMLSelectElement
  const value = select.value
  select.value = ''
  if (!value) return
  mutate(
    value === 'unassign' ? { unassign: true } : { assignee_id: value },
    value === 'unassign' ? 'bulkUnassigned' : 'bulkAssigned',
  )
}

function allHaveTag(tagID: string) {
  return props.selectedItems.length > 0
    && props.selectedItems.every(item => item.tags.some(tag => tag.id === tagID))
}

function toggleTag(tag: TriageTag) {
  const remove = allHaveTag(tag.id)
  mutate(
    remove ? { remove_tag_ids: [tag.id] } : { add_tag_ids: [tag.id] },
    remove ? 'bulkTagRemoved' : 'bulkTagged',
    { tag: tag.name },
  )
}

function mutate(body: Record<string, unknown>, actionKey: string, parameters?: Record<string, unknown>) {
  compactMenuOpen.value = false
  if (tagPopover.value) tagPopover.value.open = false
  if (issuePopover.value) issuePopover.value.open = false
  emit('mutate', body, actionKey, parameters)
}

function reprocess() {
  compactMenuOpen.value = false
  emit('reprocess')
}

function issueAction(action: 'createIssue' | 'linkIssue' | 'unlinkIssue') {
  compactMenuOpen.value = false
  if (issuePopover.value) issuePopover.value.open = false
  if (action === 'createIssue') emit('createIssue')
  else if (action === 'linkIssue') emit('linkIssue')
  else emit('unlinkIssue')
}
</script>

<template>
  <section class="feedback-bulk-bar" :aria-label="t('feedback.bulk.label')">
    <b>{{ t('feedback.bulk.selected', { count }, count) }}</b>

    <template v-if="!compact">
      <details v-if="issueActionsAvailable" ref="issuePopover" class="bulk-popover bulk-issue-popover">
        <summary><CircleDot :size="14" /> {{ t('feedback.bulk.issue') }} <ChevronDown :size="13" /></summary>
        <div>
          <p v-if="issueActionReason">{{ issueActionReason }}</p>
          <button type="button" :disabled="pending || !canCreateOrLink" :title="issueActionReason" @click="issueAction('createIssue')"><Plus :size="14" /> {{ t('feedback.actions.createIssue') }}</button>
          <button type="button" :disabled="pending || !canCreateOrLink" :title="issueActionReason" @click="issueAction('linkIssue')"><Link2 :size="14" /> {{ t('feedback.actions.linkIssue') }}</button>
          <button type="button" :disabled="pending || !canUnlink" @click="issueAction('unlinkIssue')"><Link2Off :size="14" /> {{ t('feedback.actions.unlink') }}</button>
        </div>
      </details>
      <select :aria-label="t('feedback.bulk.priority')" :disabled="pending" value="" @change="choosePriority">
        <option value="" disabled>{{ t('feedback.triage.priority') }}</option>
        <option v-for="priority in priorities" :key="priority.value" :value="priority.value">{{ priority.label }}</option>
        <option value="none">{{ t('feedback.priority.clear') }}</option>
      </select>
      <select :aria-label="t('feedback.bulk.assignee')" :disabled="pending" value="" @change="chooseAssignee">
        <option value="" disabled>{{ t('feedback.bulk.assign') }}</option>
        <option v-for="member in members" :key="member.id" :value="member.id">{{ member.name || member.email }}</option>
        <option value="unassign">{{ t('feedback.bulk.unassign') }}</option>
      </select>
      <details ref="tagPopover" class="bulk-popover bulk-tag-popover">
        <summary><Tag :size="14" /> {{ t('feedback.bulk.tags') }} <ChevronDown :size="13" /></summary>
        <div>
          <button
            v-for="tag in tags"
            :key="tag.id"
            type="button"
            :aria-label="t(allHaveTag(tag.id) ? 'feedback.bulk.removeTag' : 'feedback.bulk.addTag', { tag: tag.name })"
            :disabled="pending"
            @click="toggleTag(tag)"
          ><span :style="{ backgroundColor: tag.color }" /><Check v-if="allHaveTag(tag.id)" :size="13" />{{ tag.name }}</button>
          <span v-if="tags.length === 0">{{ t('feedback.triage.noTags') }}</span>
        </div>
      </details>
      <button type="button" :disabled="pending" @click="mutate({ status: 'archived' }, 'bulkArchived')"><Archive :size="14" /> {{ t('feedback.actions.archive') }}</button>
      <button class="danger" type="button" :disabled="pending" @click="mutate({ status: 'spam' }, 'bulkSpam')"><ShieldAlert :size="14" /> {{ t('feedback.actions.spam') }}</button>
      <button v-if="aiEnabled" type="button" :disabled="pending" @click="reprocess"><RotateCcw :size="14" /> {{ t('feedback.actions.reprocessAI') }}</button>
      <NuxtLink v-else-if="showAIUpgrade" to="/billing">{{ t('feedback.actions.upgradeAI') }} <ArrowUpRight :size="13" /></NuxtLink>
    </template>

    <div v-else class="bulk-compact-actions">
      <button
        class="bulk-compact-trigger"
        type="button"
        :aria-label="t('feedback.bulk.actionsLabel')"
        :aria-expanded="compactMenuOpen"
        :disabled="pending"
        @click="compactMenuOpen = !compactMenuOpen"
      ><ListFilter :size="15" /> {{ t('feedback.bulk.actions') }} <ChevronDown :size="13" /></button>
      <div v-if="compactMenuOpen" class="bulk-compact-menu">
        <fieldset v-if="issueActionsAvailable" class="bulk-issue-actions">
          <legend>{{ t('feedback.bulk.issue') }}</legend>
          <p v-if="issueActionReason">{{ issueActionReason }}</p>
          <button type="button" :disabled="pending || !canCreateOrLink" @click="issueAction('createIssue')"><Plus :size="14" /> {{ t('feedback.actions.createIssue') }}</button>
          <button type="button" :disabled="pending || !canCreateOrLink" @click="issueAction('linkIssue')"><Link2 :size="14" /> {{ t('feedback.actions.linkIssue') }}</button>
          <button type="button" :disabled="pending || !canUnlink" @click="issueAction('unlinkIssue')"><Link2Off :size="14" /> {{ t('feedback.actions.unlink') }}</button>
        </fieldset>
        <label>{{ t('feedback.triage.priority') }}<select :aria-label="t('feedback.bulk.priority')" :disabled="pending" value="" @change="choosePriority"><option value="" disabled>{{ t('feedback.bulk.choosePriority') }}</option><option v-for="priority in priorities" :key="priority.value" :value="priority.value">{{ priority.label }}</option><option value="none">{{ t('feedback.priority.clear') }}</option></select></label>
        <label>{{ t('feedback.triage.assignee') }}<select :aria-label="t('feedback.bulk.assignee')" :disabled="pending" value="" @change="chooseAssignee"><option value="" disabled>{{ t('feedback.bulk.chooseAssignee') }}</option><option v-for="member in members" :key="member.id" :value="member.id">{{ member.name || member.email }}</option><option value="unassign">{{ t('feedback.bulk.unassign') }}</option></select></label>
        <fieldset><legend>{{ t('feedback.bulk.tags') }}</legend><button v-for="tag in tags" :key="tag.id" type="button" :aria-label="t(allHaveTag(tag.id) ? 'feedback.bulk.removeTag' : 'feedback.bulk.addTag', { tag: tag.name })" :disabled="pending" @click="toggleTag(tag)"><span :style="{ backgroundColor: tag.color }" /><Check v-if="allHaveTag(tag.id)" :size="13" />{{ tag.name }}</button><span v-if="tags.length === 0">{{ t('feedback.triage.noTags') }}</span></fieldset>
        <div class="bulk-compact-commands">
          <button type="button" :disabled="pending" @click="mutate({ status: 'archived' }, 'bulkArchived')"><Archive :size="14" /> {{ t('feedback.actions.archive') }}</button>
          <button class="danger" type="button" :disabled="pending" @click="mutate({ status: 'spam' }, 'bulkSpam')"><ShieldAlert :size="14" /> {{ t('feedback.actions.spam') }}</button>
          <button v-if="aiEnabled" type="button" :disabled="pending" @click="reprocess"><RotateCcw :size="14" /> {{ t('feedback.actions.reprocessAI') }}</button>
          <NuxtLink v-else-if="showAIUpgrade" to="/billing">{{ t('feedback.actions.upgradeAI') }} <ArrowUpRight :size="13" /></NuxtLink>
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
.feedback-bulk-bar { position: relative; z-index: 40; display: flex; height: 45px; align-items: center; gap: 6px; padding: 6px 12px; background: #17342f; color: #fff; }
.feedback-bulk-bar b { margin-right: auto; font-size: 11px; white-space: nowrap; }
.feedback-bulk-bar button,
.feedback-bulk-bar select,
.feedback-bulk-bar summary,
.feedback-bulk-bar a { display: inline-flex; height: 31px; min-width: 0; align-items: center; gap: 5px; padding: 0 8px; background: #274b44; color: #fff; border: 1px solid #42635d; border-radius: 4px; font-size: 10px; text-decoration: none; }
.feedback-bulk-bar select { max-width: 112px; cursor: pointer; }
.feedback-bulk-bar button,
.feedback-bulk-bar summary { cursor: pointer; }
.feedback-bulk-bar button:disabled,
.feedback-bulk-bar select:disabled { cursor: wait; opacity: .62; }
.feedback-bulk-bar button.danger { background: #64323a; border-color: #85505a; }
.bulk-popover { position: relative; }
.bulk-popover summary { list-style: none; }
.bulk-popover summary::-webkit-details-marker { display: none; }
.bulk-popover > div { position: absolute; top: 36px; right: 0; z-index: 70; display: grid; min-width: 180px; gap: 4px; padding: 6px; background: #fff; color: var(--ink); border: 1px solid var(--line); border-radius: 5px; box-shadow: var(--shadow-overlay); }
.bulk-popover > div button { justify-content: flex-start; background: #fff; color: var(--ink); border: 0; }
.bulk-popover > div button:hover { background: var(--surface-soft); }
.bulk-popover > div button:disabled { cursor: not-allowed; opacity: .5; }
.bulk-issue-popover > div > p { max-width: 220px; margin: 4px 6px 7px; color: var(--muted); font-size: 9px; line-height: 1.45; }
.bulk-popover > div button > span,
.bulk-compact-menu fieldset button > span { width: 7px; height: 17px; border-radius: 2px; }
.bulk-popover > div > span { padding: 8px; color: var(--muted); font-size: 10px; }
.bulk-compact-actions { position: relative; }
.bulk-compact-menu { position: absolute; top: 38px; right: 0; z-index: 80; display: grid; width: min(350px, calc(100vw - 20px)); grid-template-columns: 1fr 1fr; gap: 10px; padding: 12px; background: #fff; color: var(--ink); border: 1px solid var(--line); border-radius: 5px; box-shadow: var(--shadow-overlay); }
.bulk-compact-menu label { display: grid; gap: 5px; color: var(--muted); font-size: 9px; font-weight: 750; text-transform: uppercase; }
.bulk-compact-menu label select { width: 100%; max-width: none; background: #fff; color: var(--ink); border-color: var(--line); }
.bulk-compact-menu fieldset { display: flex; grid-column: 1 / -1; flex-wrap: wrap; gap: 5px; margin: 0; padding: 0; border: 0; }
.bulk-compact-menu legend { width: 100%; margin-bottom: 5px; color: var(--muted); font-size: 9px; font-weight: 750; text-transform: uppercase; }
.bulk-compact-menu fieldset button { height: 29px; background: #fff; color: var(--ink); border-color: var(--line); }
.bulk-compact-menu fieldset > span { color: var(--muted); font-size: 10px; }
.bulk-compact-menu .bulk-issue-actions p { width: 100%; margin: 0 0 3px; color: var(--muted); font-size: 9px; line-height: 1.45; }
.bulk-compact-menu .bulk-issue-actions button { flex: 1 1 92px; justify-content: center; }
.bulk-compact-commands { display: grid; grid-column: 1 / -1; grid-template-columns: 1fr 1fr; gap: 6px; padding-top: 4px; border-top: 1px solid var(--line); }
.bulk-compact-commands button,
.bulk-compact-commands a { justify-content: center; }

@media (max-width: 700px) {
  .feedback-bulk-bar { padding: 6px 10px; }
}
</style>
