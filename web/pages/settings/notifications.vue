<script setup lang="ts">
import { BellRing, Clock, Save, ShieldCheck } from '@lucide/vue'
import type {
  NotificationCategory,
  NotificationDelivery,
  NotificationMode,
  NotificationPreferences,
  NotificationPreferencesResponse,
} from '~/types/api'
import { preferencePatch, usesDigest, usesWeeklyDigest } from '~/utils/notification-preferences'

const { $apiFetch } = useNuxtApp()
const { t } = useI18n()
const { tenant, workspaceWritable } = useAuth()
const localizedError = useLocalizedAPIError()
const { dateTime } = useRegionalFormat()

const timezoneSuggestions = [
  'UTC',
  'Africa/Luanda',
  'Africa/Maputo',
  'Europe/Lisbon',
  'Europe/London',
  'America/Sao_Paulo',
  'America/New_York',
]
const weekdays = [1, 2, 3, 4, 5, 6, 7]
const hours = Array.from({ length: 24 }, (_, hour) => hour)

const { data, status, error, refresh } = await useAsyncData(
  'notification-preferences',
  () => $apiFetch<NotificationPreferencesResponse>('/v1/notification-preferences'),
)

const preferences = ref<NotificationPreferences | null>(null)
const inheritTimezone = ref(false)
const timezone = ref('')
const saving = ref(false)
const notice = ref('')
const failure = ref('')
const fields = ref<Record<string, string>>({})

function applyResponse(response: NotificationPreferencesResponse) {
  preferences.value = { ...response.notification_preferences }
  inheritTimezone.value = response.notification_preferences.timezone_source !== 'member'
  timezone.value = response.notification_preferences.timezone
}

watch(data, (response) => {
  if (response) applyResponse(response)
}, { immediate: true })

const categories = computed(() => data.value?.categories || [])
const deliveries = computed(() => data.value?.deliveries || [])

function modesFor(category: NotificationCategory): NotificationMode[] {
  return data.value?.supported_modes?.[category] || []
}

const digestEnabled = computed(
  () => Boolean(preferences.value) && usesDigest(preferences.value!, categories.value),
)

const weeklyEnabled = computed(
  () => Boolean(preferences.value) && usesWeeklyDigest(preferences.value!, categories.value),
)

function hourLabel(hour: number) {
  return `${String(hour).padStart(2, '0')}:00`
}

function deliveryWindow(delivery: NotificationDelivery) {
  return `${dateTime(delivery.window_start)} → ${dateTime(delivery.window_end)}`
}

async function savePreferences() {
  if (!preferences.value || !workspaceWritable.value || saving.value) return
  saving.value = true
  notice.value = ''
  failure.value = ''
  fields.value = {}
  try {
    const body = preferencePatch(
      preferences.value, categories.value, timezone.value, inheritTimezone.value,
    )
    const response = await $apiFetch<NotificationPreferencesResponse>(
      '/v1/notification-preferences',
      { method: 'PATCH', body },
    )
    data.value = response
    applyResponse(response)
    notice.value = t('admin.notifications.notices.saved')
  } catch (saveError) {
    fields.value = localizedError.fields(saveError)
    failure.value = Object.keys(fields.value).length
      ? ''
      : localizedError.message(saveError, 'admin.notifications.errors.save')
  } finally {
    saving.value = false
  }
}

useHead({ title: computed(() => t('admin.notifications.pageTitle')) })
</script>

<template>
  <div class="app-page notifications-page">
    <header class="app-page-head">
      <div>
        <p class="app-eyebrow">{{ t('admin.notifications.eyebrow') }}</p>
        <h1>{{ t('admin.notifications.title') }}</h1>
        <span>{{ t('admin.notifications.subtitle', { workspace: tenant?.name || '' }) }}</span>
      </div>
    </header>

    <section v-if="status === 'pending'" class="app-surface app-state">
      <div><BellRing :size="24" /><h2>{{ t('admin.notifications.loading') }}</h2></div>
    </section>

    <section v-else-if="error || !preferences" class="app-surface app-state">
      <div>
        <BellRing :size="24" />
        <h2>{{ t('admin.notifications.unavailable') }}</h2>
        <button class="app-button" type="button" @click="refresh()">
          {{ t('admin.common.tryAgain') }}
        </button>
      </div>
    </section>

    <template v-else>
      <form class="app-surface notifications-surface" @submit.prevent="savePreferences">
        <header>
          <div>
            <p class="app-eyebrow">{{ t('admin.notifications.categoriesEyebrow') }}</p>
            <h2>{{ t('admin.notifications.categoriesTitle') }}</h2>
          </div>
        </header>

        <fieldset :disabled="!workspaceWritable || saving">
          <div v-for="category in categories" :key="category" class="notifications-row">
            <div class="notifications-row-copy">
              <label :for="`notification-${category}`">
                {{ t(`admin.notifications.categories.${category}.label`) }}
              </label>
              <small>{{ t(`admin.notifications.categories.${category}.description`) }}</small>
            </div>
            <div class="notifications-row-control">
              <select
                :id="`notification-${category}`"
                v-model="preferences[category]"
                class="app-select"
              >
                <option v-for="mode in modesFor(category)" :key="mode" :value="mode">
                  {{ t(`admin.notifications.modes.${mode}`) }}
                </option>
              </select>
              <small v-if="fields[category]" class="notifications-field-error">
                {{ fields[category] }}
              </small>
            </div>
          </div>
        </fieldset>

        <div class="notifications-section-heading">
          <h3><Clock :size="15" /> {{ t('admin.notifications.windowTitle') }}</h3>
          <p>{{ t('admin.notifications.windowDescription') }}</p>
        </div>

        <fieldset :disabled="!workspaceWritable || saving">
          <div class="notifications-window-grid">
            <div class="notifications-field">
              <label for="notification-timezone">{{ t('admin.notifications.timezone') }}</label>
              <input
                id="notification-timezone"
                v-model="timezone"
                class="app-input"
                list="notification-timezones"
                maxlength="100"
                :disabled="inheritTimezone"
                :placeholder="t('admin.notifications.timezonePlaceholder')"
              />
              <datalist id="notification-timezones">
                <option v-for="suggestion in timezoneSuggestions" :key="suggestion" :value="suggestion" />
              </datalist>
              <label class="notifications-checkbox">
                <input v-model="inheritTimezone" type="checkbox" />
                <span>{{ t('admin.notifications.timezoneInherit') }}</span>
              </label>
              <small v-if="fields.timezone" class="notifications-field-error">{{ fields.timezone }}</small>
            </div>
            <div class="notifications-field">
              <label for="notification-hour">{{ t('admin.notifications.digestHour') }}</label>
              <select
                id="notification-hour"
                v-model.number="preferences.digest_hour"
                class="app-select"
                :disabled="!digestEnabled"
              >
                <option v-for="hour in hours" :key="hour" :value="hour">{{ hourLabel(hour) }}</option>
              </select>
              <small v-if="fields.digest_hour" class="notifications-field-error">{{ fields.digest_hour }}</small>
            </div>
            <div class="notifications-field">
              <label for="notification-weekday">{{ t('admin.notifications.digestWeekday') }}</label>
              <select
                id="notification-weekday"
                v-model.number="preferences.digest_weekday"
                class="app-select"
                :disabled="!weeklyEnabled"
              >
                <option v-for="weekday in weekdays" :key="weekday" :value="weekday">
                  {{ t(`admin.notifications.weekdays.${weekday}`) }}
                </option>
              </select>
              <small v-if="fields.digest_weekday" class="notifications-field-error">
                {{ fields.digest_weekday }}
              </small>
            </div>
          </div>
        </fieldset>

        <p class="notifications-mandatory">
          <ShieldCheck :size="14" /> {{ t('admin.notifications.mandatoryNotice') }}
        </p>

        <div class="notifications-form-footer">
          <p v-if="!workspaceWritable">{{ t('admin.notifications.readOnly') }}</p>
          <p v-else-if="failure" class="notifications-failure" role="alert">{{ failure }}</p>
          <p v-else-if="notice" class="notifications-notice" role="status">{{ notice }}</p>
          <button
            class="app-button app-button--primary"
            type="submit"
            :disabled="!workspaceWritable || saving"
          >
            <Save :size="14" />
            {{ saving ? t('admin.common.saving') : t('admin.notifications.save') }}
          </button>
        </div>
      </form>

      <section class="app-surface notifications-surface">
        <header>
          <div>
            <p class="app-eyebrow">{{ t('admin.notifications.deliveriesEyebrow') }}</p>
            <h2>{{ t('admin.notifications.deliveriesTitle') }}</h2>
          </div>
        </header>
        <p class="notifications-deliveries-description">
          {{ t('admin.notifications.deliveriesDescription') }}
        </p>
        <p v-if="!deliveries.length" class="notifications-empty">
          {{ t('admin.notifications.deliveriesEmpty') }}
        </p>
        <ul v-else class="notifications-deliveries">
          <li v-for="delivery in deliveries" :key="`${delivery.cadence}-${delivery.window_start}`">
            <div class="notifications-delivery-head">
              <span :class="['notifications-status', `notifications-status--${delivery.status}`]">
                {{ t(`admin.notifications.deliveryStatuses.${delivery.status}`) }}
              </span>
              <span>{{ t(`admin.notifications.cadences.${delivery.cadence}`) }}</span>
              <span>{{ t('admin.notifications.deliveryEvents', { count: delivery.event_count }) }}</span>
            </div>
            <small>{{ deliveryWindow(delivery) }}</small>
            <small v-if="delivery.reason" class="notifications-delivery-reason">{{ delivery.reason }}</small>
          </li>
        </ul>
      </section>
    </template>
  </div>
</template>

<style scoped>
.notifications-page {
  max-width: 920px;
}
.notifications-surface {
  display: grid;
  gap: 18px;
  padding: 24px;
}
.notifications-surface > header {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  justify-content: space-between;
  align-items: flex-start;
}
.notifications-surface h2 {
  margin: 0;
  font-size: 18px;
}
fieldset {
  border: 0;
  margin: 0;
  padding: 0;
  display: grid;
  gap: 14px;
}
.notifications-row {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  align-items: center;
  justify-content: space-between;
  border-top: 1px solid var(--line);
  padding-top: 14px;
}
.notifications-row:first-child {
  border-top: 0;
  padding-top: 0;
}
.notifications-row-copy {
  display: grid;
  gap: 4px;
  min-width: 220px;
  flex: 1 1 260px;
}
.notifications-row-copy label {
  font-weight: 600;
  font-size: 14px;
}
.notifications-row-copy small,
.notifications-deliveries-description,
.notifications-empty {
  color: var(--muted);
  font-size: 13px;
  line-height: 19px;
}
.notifications-row-control {
  display: grid;
  gap: 4px;
  flex: 0 1 220px;
  min-width: 180px;
}
.notifications-section-heading {
  display: grid;
  gap: 4px;
  border-top: 1px solid var(--line);
  padding-top: 18px;
}
.notifications-section-heading h3 {
  display: flex;
  align-items: center;
  gap: 6px;
  margin: 0;
  font-size: 15px;
}
.notifications-section-heading p {
  margin: 0;
  color: var(--muted);
  font-size: 13px;
}
.notifications-window-grid {
  display: grid;
  gap: 14px;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
}
.notifications-field {
  display: grid;
  gap: 6px;
  align-content: start;
}
.notifications-field > label:first-child {
  font-weight: 600;
  font-size: 13px;
}
.notifications-checkbox {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  color: var(--muted);
}
.notifications-field-error,
.notifications-failure {
  color: var(--red);
  font-size: 12px;
}
.notifications-notice {
  color: var(--green);
  font-size: 13px;
}
.notifications-mandatory {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 0;
  padding: 12px 14px;
  border: 1px solid var(--line);
  border-radius: 6px;
  background: var(--surface-subtle);
  color: var(--muted);
  font-size: 13px;
  line-height: 19px;
}
.notifications-form-footer {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  align-items: center;
  justify-content: flex-end;
}
.notifications-form-footer p {
  margin: 0;
  margin-right: auto;
  font-size: 13px;
}
.notifications-deliveries {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: 12px;
}
.notifications-deliveries li {
  display: grid;
  gap: 4px;
  border-top: 1px solid var(--line);
  padding-top: 12px;
}
.notifications-deliveries li:first-child {
  border-top: 0;
  padding-top: 0;
}
.notifications-delivery-head {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  align-items: center;
  font-size: 13px;
}
.notifications-deliveries small,
.notifications-delivery-reason {
  color: var(--muted);
  font-size: 12px;
}
.notifications-status {
  border-radius: 999px;
  padding: 2px 10px;
  font-size: 12px;
  font-weight: 600;
  background: var(--surface-subtle);
  color: var(--muted);
}
.notifications-status--sent {
  background: var(--green-soft);
  color: var(--green);
}
.notifications-status--suppressed,
.notifications-status--failed {
  background: var(--amber-soft);
  color: var(--amber-ink);
}
@media (max-width: 640px) {
  .notifications-row {
    align-items: stretch;
  }
  .notifications-row-control {
    flex: 1 1 100%;
  }
  .notifications-form-footer {
    justify-content: stretch;
  }
  .notifications-form-footer button {
    width: 100%;
    justify-content: center;
  }
}
</style>
