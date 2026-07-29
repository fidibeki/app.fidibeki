<script setup lang="ts">
import { AlertTriangle, CheckCircle2, Mail, Pause, Plus, Save } from '@lucide/vue'
import type {
  EmailFeedbackCampaign,
  EmailFeedbackCampaignInput,
  EmailFeedbackCampaignStatus,
  WidgetRule,
} from '~/types/api'
import { apiFieldErrors } from '~/utils/api-fetch'

const props = defineProps<{
  projectId: string
  defaultLocale: string
  rules: WidgetRule[]
  controllerReady: boolean
}>()

const { $apiFetch } = useNuxtApp()
const { can, canWrite, workspaceWritable } = useAuth()
const { t } = useI18n()
const { message: apiErrorMessage, fields: localizedFieldErrors } = useLocalizedAPIError()

const selectedID = ref('')
const creating = ref(false)
const pending = ref(false)
const notice = ref('')
const formError = ref('')
const fieldErrors = ref<Record<string, string>>({})

const { data, status, error, refresh } = await useAsyncData(
  () => `email-feedback-campaigns:${props.projectId}`,
  () => can('project:read')
    ? $apiFetch<{ campaigns: EmailFeedbackCampaign[] }>(
        `/v1/email-feedback-campaigns?project_id=${encodeURIComponent(props.projectId)}`,
      )
    : Promise.resolve({ campaigns: [] }),
  { watch: [() => props.projectId] },
)

const campaigns = computed(() => data.value?.campaigns || [])
const selected = computed(() => campaigns.value.find(campaign => campaign.id === selectedID.value))
const usableRules = computed(() => props.rules.filter(rule => rule.definition_status !== 'archived'))
const readinessReasons = computed(() => localReadinessReasons())
const form = reactive<EmailFeedbackCampaignInput>(emptyCampaign())
const apiExample = computed(() => selected.value
  ? `curl -X POST "$FIDIBEKI_API/v1/email-feedback/invitations" \\\n  -H "Authorization: Bearer $FIDIBEKI_API_KEY" \\\n  -H "Content-Type: application/json" \\\n  -d '{"campaign_id":"${selected.value.id}","external_event_id":"order_123","recipient_email":"customer@example.com"}'`
  : '')

watch(campaigns, (value) => {
  if (creating.value) return
  const current = value.find(campaign => campaign.id === selectedID.value) || value[0]
  if (current) selectCampaign(current)
  else startCampaign()
}, { immediate: true })

function emptyCampaign(): EmailFeedbackCampaignInput {
  const rule = usableRules.value?.find(candidate => candidate.definition_status === 'active')
    || usableRules.value?.[0]
  return {
    project_id: props.projectId,
    widget_rule_id: rule?.id || '',
    campaign_key: '',
    name: '',
    status: 'draft',
    default_locale: props.defaultLocale || 'en',
    subject: t('projects.emailCampaigns.defaults.subject'),
    introduction: t('projects.emailCampaigns.defaults.introduction'),
    invitation_ttl_days: 30,
  }
}

function selectCampaign(campaign: EmailFeedbackCampaign) {
  selectedID.value = campaign.id
  creating.value = false
  formError.value = ''
  fieldErrors.value = {}
  Object.assign(form, {
    project_id: campaign.project_id,
    widget_rule_id: campaign.widget_rule_id,
    campaign_key: campaign.campaign_key,
    name: campaign.name,
    status: campaign.status,
    default_locale: campaign.default_locale,
    subject: campaign.subject,
    introduction: campaign.introduction,
    invitation_ttl_days: campaign.invitation_ttl_days,
  })
}

function startCampaign() {
  selectedID.value = ''
  creating.value = true
  formError.value = ''
  fieldErrors.value = {}
  Object.assign(form, emptyCampaign())
}

function localReadinessReasons() {
  const reasons: string[] = []
  const rule = props.rules.find(candidate => candidate.id === form.widget_rule_id)
  if (!props.controllerReady) reasons.push('controller_profile_incomplete')
  if (!rule || rule.definition_status !== 'active' || !rule.enabled) {
    reasons.push('widget_definition_inactive')
  }
  if (!rule?.localizations?.[form.default_locale]) reasons.push('localization_missing')
  return reasons
}

function statusIcon(value: EmailFeedbackCampaignStatus) {
  return value === 'active' ? CheckCircle2 : Pause
}

async function save() {
  if (!canWrite('project:update') || pending.value) return
  pending.value = true
  formError.value = ''
  fieldErrors.value = {}
  try {
    const path = creating.value
      ? '/v1/email-feedback-campaigns'
      : `/v1/email-feedback-campaigns/${selectedID.value}`
    const response = await $apiFetch<{ campaign: EmailFeedbackCampaign }>(path, {
      method: creating.value ? 'POST' : 'PATCH',
      body: {
        ...form,
        campaign_key: form.campaign_key.trim().toLowerCase(),
        name: form.name.trim(),
        default_locale: form.default_locale.trim().toLowerCase().replaceAll('_', '-'),
        subject: form.subject.trim(),
        introduction: form.introduction.trim(),
        invitation_ttl_days: Number(form.invitation_ttl_days),
      },
    })
    selectedID.value = response.campaign.id
    creating.value = false
    notice.value = t('projects.emailCampaigns.saved')
    await refresh()
  } catch (saveError) {
    fieldErrors.value = apiFieldErrors(saveError, localizedFieldErrors(saveError))
    formError.value = apiErrorMessage(saveError, 'projects.emailCampaigns.errors.save')
  } finally {
    pending.value = false
    window.setTimeout(() => { notice.value = '' }, 3000)
  }
}
</script>

<template>
  <section id="email-feedback-campaigns" class="email-campaigns app-surface">
    <header>
      <div>
        <p class="app-eyebrow">{{ t('projects.emailCampaigns.eyebrow') }}</p>
        <h2><Mail :size="16" /> {{ t('projects.emailCampaigns.title') }}</h2>
        <p>{{ t('projects.emailCampaigns.description') }}</p>
      </div>
      <button
        v-if="canWrite('project:update')"
        type="button"
        class="app-button"
        @click="startCampaign"
      >
        <Plus :size="14" /> {{ t('projects.emailCampaigns.new') }}
      </button>
    </header>

    <div v-if="status === 'pending'" class="email-campaign-state">
      <span class="app-spinner" /> {{ t('projects.emailCampaigns.loading') }}
    </div>
    <p v-else-if="error" class="email-campaign-state email-campaign-error" role="alert">
      <AlertTriangle :size="15" />
      {{ apiErrorMessage(error, 'projects.emailCampaigns.errors.load') }}
    </p>
    <div v-else class="email-campaign-layout">
      <nav :aria-label="t('projects.emailCampaigns.listLabel')">
        <button
          v-for="campaign in campaigns"
          :key="campaign.id"
          type="button"
          :data-active="campaign.id === selectedID"
          @click="selectCampaign(campaign)"
        >
          <component :is="statusIcon(campaign.status)" :size="14" />
          <span>
            <b>{{ campaign.name }}</b>
            <small>{{ t(`projects.emailCampaigns.status.${campaign.status}`) }}</small>
          </span>
        </button>
        <p v-if="!campaigns.length && !creating">{{ t('projects.emailCampaigns.empty') }}</p>
      </nav>

      <form @submit.prevent="save">
        <fieldset :disabled="!workspaceWritable || selected?.status === 'archived'">
          <div class="email-campaign-grid">
            <label>
              {{ t('projects.emailCampaigns.fields.name') }}
              <input v-model="form.name" class="app-input" required maxlength="120" />
              <small v-if="fieldErrors.name" class="email-field-error">{{ fieldErrors.name }}</small>
            </label>
            <label>
              {{ t('projects.emailCampaigns.fields.key') }}
              <input
                v-model="form.campaign_key"
                class="app-input"
                required
                maxlength="64"
                pattern="[a-z][a-z0-9-]{2,63}"
              />
              <small v-if="fieldErrors.campaign_key" class="email-field-error">
                {{ fieldErrors.campaign_key }}
              </small>
            </label>
            <label>
              {{ t('projects.emailCampaigns.fields.definition') }}
              <select v-model="form.widget_rule_id" class="app-select" required>
                <option value="" disabled>{{ t('projects.emailCampaigns.fields.chooseDefinition') }}</option>
                <option v-for="rule in usableRules" :key="rule.id" :value="rule.id">
                  {{ rule.name }} · {{ t(`projects.enums.definitionStatus.${rule.definition_status}`) }}
                </option>
              </select>
              <small v-if="fieldErrors.widget_rule_id" class="email-field-error">
                {{ fieldErrors.widget_rule_id }}
              </small>
            </label>
            <label>
              {{ t('projects.emailCampaigns.fields.locale') }}
              <input v-model="form.default_locale" class="app-input" required maxlength="35" />
              <small v-if="fieldErrors.default_locale" class="email-field-error">
                {{ fieldErrors.default_locale }}
              </small>
            </label>
            <label>
              {{ t('projects.emailCampaigns.fields.status') }}
              <select v-model="form.status" class="app-select">
                <option value="draft">{{ t('projects.emailCampaigns.status.draft') }}</option>
                <option value="active">{{ t('projects.emailCampaigns.status.active') }}</option>
                <option value="paused">{{ t('projects.emailCampaigns.status.paused') }}</option>
                <option v-if="!creating" value="archived">
                  {{ t('projects.emailCampaigns.status.archived') }}
                </option>
              </select>
            </label>
            <label>
              {{ t('projects.emailCampaigns.fields.ttl') }}
              <input
                v-model.number="form.invitation_ttl_days"
                class="app-input"
                type="number"
                required
                min="1"
                max="90"
              />
            </label>
            <label class="wide">
              {{ t('projects.emailCampaigns.fields.subject') }}
              <input v-model="form.subject" class="app-input" required maxlength="160" />
            </label>
            <label class="wide">
              {{ t('projects.emailCampaigns.fields.introduction') }}
              <textarea v-model="form.introduction" maxlength="1000" />
            </label>
          </div>

          <div
            class="email-campaign-readiness"
            :data-ready="readinessReasons.length === 0"
            role="status"
          >
            <CheckCircle2 v-if="readinessReasons.length === 0" :size="16" />
            <AlertTriangle v-else :size="16" />
            <p>
              <b>
                {{ readinessReasons.length === 0
                  ? t('projects.emailCampaigns.readiness.ready')
                  : t('projects.emailCampaigns.readiness.blocked') }}
              </b>
              <span v-if="readinessReasons.length">
                {{ readinessReasons.map(reason =>
                  t(`projects.emailCampaigns.readiness.reasons.${reason}`)).join(' · ') }}
              </span>
              <span v-else>{{ t('projects.emailCampaigns.readiness.readyDescription') }}</span>
            </p>
            <NuxtLink
              v-if="readinessReasons.includes('controller_profile_incomplete')"
              class="app-button"
              to="/settings#organization-profile"
            >
              {{ t('projects.emailCampaigns.readiness.controllerAction') }}
            </NuxtLink>
          </div>

          <p v-if="formError" class="email-campaign-error" role="alert">
            <AlertTriangle :size="14" /> {{ formError }}
          </p>
          <p v-if="notice" class="email-campaign-notice" role="status">{{ notice }}</p>

          <footer>
            <p>{{ t('projects.emailCampaigns.activationNotice') }}</p>
            <button
              class="app-button app-button--primary"
              type="submit"
              :disabled="pending || !canWrite('project:update')"
            >
              <Save :size="14" />
              {{ pending ? t('projects.emailCampaigns.saving') : t('projects.emailCampaigns.save') }}
            </button>
          </footer>
        </fieldset>

        <section v-if="apiExample" class="email-campaign-api">
          <h3>{{ t('projects.emailCampaigns.api.title') }}</h3>
          <p>{{ t('projects.emailCampaigns.api.description') }}</p>
          <AppCodeBlock :code="apiExample" language="bash" />
        </section>
      </form>
    </div>
  </section>
</template>

<style scoped>
.email-campaigns { margin: 22px; overflow: hidden; }
.email-campaigns > header { display: flex; min-height: 66px; align-items: center; justify-content: space-between; gap: 16px; padding: 14px 16px; border-bottom: 1px solid var(--line); }
.email-campaigns > header h2 { display: flex; align-items: center; gap: 7px; margin: 2px 0 0; font-size: 13px; }
.email-campaigns > header p:last-child { margin: 4px 0 0; color: var(--muted); font-size: 9px; }
.email-campaign-layout { display: grid; grid-template-columns: minmax(180px, .55fr) minmax(0, 1.7fr); }
.email-campaign-layout > nav { border-right: 1px solid var(--line); background: var(--surface-subtle); }
.email-campaign-layout > nav button { display: flex; width: 100%; min-height: 52px; align-items: center; gap: 9px; padding: 10px 12px; border: 0; border-bottom: 1px solid var(--line); background: transparent; color: inherit; text-align: left; cursor: pointer; }
.email-campaign-layout > nav button[data-active="true"] { background: var(--surface); box-shadow: inset 2px 0 var(--accent); }
.email-campaign-layout > nav span { display: flex; min-width: 0; flex-direction: column; gap: 3px; }
.email-campaign-layout > nav b { overflow: hidden; font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.email-campaign-layout > nav small, .email-campaign-layout > nav p { color: var(--muted); font-size: 9px; }
.email-campaign-layout > nav p { margin: 14px; }
.email-campaign-layout > form { min-width: 0; }
.email-campaign-layout > form > fieldset { box-sizing: border-box; min-width: 0; margin: 0; padding: 16px; border: 0; }
.email-campaign-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 13px; }
.email-campaign-grid label { display: flex; min-width: 0; flex-direction: column; gap: 6px; color: var(--muted); font-size: 9px; font-weight: 700; }
.email-campaign-grid .wide { grid-column: 1 / -1; }
.email-campaign-grid textarea { min-height: 82px; padding: 9px; border: 1px solid var(--line-strong); border-radius: 5px; resize: vertical; color: inherit; background: var(--surface); font: inherit; font-size: 10px; }
.email-campaign-readiness { display: grid; grid-template-columns: auto minmax(0, 1fr) auto; align-items: center; gap: 10px; margin-top: 14px; padding: 11px; border: 1px solid var(--amber-line); background: var(--amber-soft); }
.email-campaign-readiness[data-ready="true"] { border-color: var(--green-line); background: var(--green-soft); }
.email-campaign-readiness p { display: flex; min-width: 0; flex-direction: column; gap: 3px; margin: 0; font-size: 9px; }
.email-campaign-readiness b { font-size: 10px; }
.email-campaign-layout footer { display: flex; align-items: center; justify-content: space-between; gap: 20px; margin-top: 14px; }
.email-campaign-layout footer p { max-width: 62ch; margin: 0; color: var(--muted); font-size: 9px; }
.email-campaign-api { min-width: 0; overflow: hidden; padding: 0 16px 16px; }
.email-campaign-api h3 { margin: 0; font-size: 10px; }
.email-campaign-api p { margin: 4px 0 9px; color: var(--muted); font-size: 9px; }
.email-campaign-api :deep(pre) { box-sizing: border-box; max-width: 100%; margin: 0; padding: 12px; overflow-x: auto; border: 1px solid var(--line); border-radius: 5px; background: var(--surface-subtle); font: 10px/1.55 ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; }
.email-campaign-state { display: flex; align-items: center; gap: 8px; padding: 22px; color: var(--muted); font-size: 10px; }
.email-campaign-error { display: flex; align-items: center; gap: 7px; color: var(--red); font-size: 9px; }
.email-campaign-notice { color: var(--green); font-size: 9px; }
.email-field-error { color: var(--red); }
@media (max-width: 800px) {
  .email-campaign-layout { grid-template-columns: 1fr; }
  .email-campaign-layout > nav { display: flex; overflow-x: auto; border-right: 0; border-bottom: 1px solid var(--line); }
  .email-campaign-layout > nav button { min-width: 180px; border-right: 1px solid var(--line); }
}
@media (max-width: 600px) {
  .email-campaigns { margin: 12px; }
  .email-campaigns > header { align-items: flex-start; flex-direction: column; }
  .email-campaign-grid { grid-template-columns: 1fr; }
  .email-campaign-grid .wide { grid-column: auto; }
  .email-campaign-readiness { grid-template-columns: auto minmax(0, 1fr); }
  .email-campaign-readiness .app-button { grid-column: 1 / -1; }
  .email-campaign-layout footer { align-items: stretch; flex-direction: column; }
  .email-campaign-layout footer .app-button { width: 100%; }
}
</style>
