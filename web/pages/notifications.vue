<script setup lang="ts">
import { AlertTriangle, Bell, Check, ChevronRight } from '@lucide/vue'
import type { MemberNotification } from '~/types/api'

const { $apiFetch } = useNuxtApp()
const { t } = useI18n()
const { message: apiErrorMessage } = useLocalizedAPIError()
const { dateTime } = useRegionalFormat()
const pendingID = ref('')
const mutationError = ref('')
const { data, status, error } = await useAsyncData('member-notifications', () =>
  $apiFetch<{ notifications: MemberNotification[], unread_count: number }>('/v1/notifications'),
)

async function openNotification(notification: MemberNotification) {
  if (pendingID.value) return
  pendingID.value = notification.id
  mutationError.value = ''
  try {
    if (!notification.read_at) {
      await $apiFetch(`/v1/notifications/${notification.id}/read`, { method: 'PATCH' })
      notification.read_at = new Date().toISOString()
      if (data.value && data.value.unread_count > 0) data.value.unread_count--
    }
    await navigateTo(`/issues/${notification.issue_id}`)
  } catch (notificationError) {
    mutationError.value = apiErrorMessage(notificationError, 'notifications.openFailed')
  } finally {
    pendingID.value = ''
  }
}

function actorName(notification: MemberNotification) {
  return notification.actor?.name
    || notification.actor?.email
    || t('notifications.workspaceMember')
}

function notificationSummary(notification: MemberNotification) {
  const key = notification.kind === 'issue.mentioned'
    ? 'notifications.kinds.mentioned'
    : 'notifications.kinds.unknown'
  return t(key, { actor: actorName(notification) })
}

useHead(() => ({ title: t('notifications.pageTitle') }))
</script>

<template>
  <div class="app-page notifications-page">
    <header class="app-page-head"><div><p class="app-eyebrow">{{ t('notifications.eyebrow') }}</p><h1>{{ t('notifications.title') }}</h1><span>{{ t('notifications.unread', { count: data?.unread_count || 0 }) }}</span></div></header>
    <p v-if="mutationError" class="notifications-error" role="alert"><AlertTriangle :size="15" /> {{ mutationError }}</p>
    <section v-if="status === 'pending'" class="app-surface app-state" aria-live="polite"><div><span class="app-spinner" /><h2>{{ t('notifications.loading') }}</h2></div></section>
    <section v-else-if="error" class="app-surface app-state" role="alert"><div><AlertTriangle :size="24" /><h2>{{ t('notifications.unavailable') }}</h2><p>{{ apiErrorMessage(error, 'notifications.loadFailed') }}</p></div></section>
    <section v-else-if="!data?.notifications.length" class="app-surface app-state notifications-empty"><div><Bell :size="24" /><h2>{{ t('notifications.emptyTitle') }}</h2><p>{{ t('notifications.emptyDescription') }}</p></div></section>
    <section v-else class="app-surface notifications-list" :aria-label="t('notifications.listLabel')">
      <header><span>{{ t('notifications.columns.activity') }}</span><span>{{ t('notifications.columns.issue') }}</span><span>{{ t('notifications.columns.received') }}</span><span /></header>
      <button v-for="notification in data.notifications" :key="notification.id" type="button" :data-unread="!notification.read_at" :disabled="Boolean(pendingID)" @click="openNotification(notification)">
        <span class="notifications-state"><i :title="notification.read_at ? t('notifications.readState') : t('notifications.unreadState')"><MemberAvatar v-if="notification.actor" :member-id="notification.actor.id" :name="notification.actor.name" :email="notification.actor.email" :avatar-version="notification.actor.avatar_version" /><template v-else><Check v-if="notification.read_at" :size="12" /><Bell v-else :size="12" /></template></i><span><b>{{ notificationSummary(notification) }}</b><small>{{ notification.body }}</small></span></span>
        <span><b>{{ notification.public_id }}</b><small>{{ notification.issue_title }}</small></span>
        <time :datetime="notification.created_at">{{ dateTime(notification.created_at) }}</time>
        <ChevronRight :size="15" />
      </button>
    </section>
  </div>
</template>

<style scoped>
.notifications-error { display: flex; align-items: center; gap: 7px; margin: 0 0 12px; padding: 10px 12px; background: var(--red-soft); color: var(--red); border: 1px solid #efc2c6; border-radius: 5px; font-size: 11px; }
.notifications-empty > div { display: grid; max-width: 420px; justify-items: center; gap: 8px; }
.notifications-empty h2, .notifications-empty p { margin: 0; }
.notifications-list { overflow: hidden; }
.notifications-list > header,
.notifications-list > button { display: grid; grid-template-columns: minmax(260px, 1.5fr) minmax(190px, 1fr) 150px 24px; align-items: center; gap: 16px; padding: 0 16px; }
.notifications-list > header { min-height: 38px; background: var(--surface-soft); color: var(--muted); border-bottom: 1px solid var(--line); font-size: 9px; font-weight: 750; text-transform: uppercase; }
.notifications-list > button { width: 100%; min-height: 68px; background: #fff; color: var(--ink-soft); border: 0; border-bottom: 1px solid var(--line); text-align: left; cursor: pointer; }
.notifications-list > button:last-child { border-bottom: 0; }
.notifications-list > button:hover { background: var(--surface-soft); }
.notifications-list > button[data-unread='true'] { box-shadow: inset 3px 0 0 var(--teal); }
.notifications-list > button:disabled { cursor: wait; opacity: .7; }
.notifications-list > button > span:not(.notifications-state) { display: flex; min-width: 0; flex-direction: column; gap: 4px; }
.notifications-list b,
.notifications-list small { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.notifications-list b { font-size: 10px; }
.notifications-list small,
.notifications-list time { color: var(--muted); font-size: 9px; }
.notifications-state { display: flex; min-width: 0; align-items: center; gap: 10px; }
.notifications-state > i { display: grid; width: 28px; height: 28px; flex: 0 0 auto; place-items: center; overflow: hidden; background: var(--surface-soft); color: var(--muted); border: 1px solid var(--line); border-radius: 50%; font-size: 8px; font-style: normal; }
button[data-unread='true'] .notifications-state > i { background: var(--teal-soft); color: var(--teal); border-color: #9ed6ca; }
.notifications-state > span { display: flex; min-width: 0; flex-direction: column; gap: 4px; }
@media (max-width: 760px) { .notifications-list > header { display: none; } .notifications-list > button { grid-template-columns: minmax(0, 1fr) 20px; gap: 8px; padding: 12px; } .notifications-list > button > span:not(.notifications-state), .notifications-list time { grid-column: 1; margin-left: 38px; } .notifications-list > button > svg { grid-column: 2; grid-row: 1 / 4; } }
</style>
