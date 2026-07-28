<script setup lang="ts">
type Incident = { id: string; public_id: string; awareness_at: string; detection_source: string; controller_role: string; confidentiality_impact: boolean; integrity_impact: boolean; availability_impact: boolean; authority_notification_deadline_at: string }

const { $platformApi } = useNuxtApp()
const { data, error, pending, refresh } = await useAsyncData('privacy-incidents', () => $platformApi<{ incidents: Incident[] }>('/admin/breach-incidents'), { server: false })

function deadlineTone(deadline: string) {
  const hours = (new Date(deadline).getTime() - Date.now()) / 3_600_000
  return hours <= 0 ? 'critical' : hours <= 24 ? 'warning' : 'complete'
}

function impact(incident: Incident) {
  return [['Confidentiality', incident.confidentiality_impact], ['Integrity', incident.integrity_impact], ['Availability', incident.availability_impact]].filter(([, active]) => active).map(([label]) => label).join(', ') || 'No impact dimension selected'
}

function refreshIncidents() { return refresh() }
</script>

<template>
  <section>
    <header class="page-head">
      <div><p class="eyebrow">Privacy operations</p><h1 class="title">Privacy incidents</h1><p class="subtitle">Restricted read-only breach register. Incident changes remain in the audited API workflow.</p></div>
      <button class="button secondary" type="button" :disabled="pending" @click="refreshIncidents">{{ pending ? 'Refreshing…' : 'Refresh' }}</button>
    </header>
    <p v-if="error" class="error" role="alert">Incident data is temporarily unavailable.</p>
    <section v-else class="panel"><p class="eyebrow">Incident register</p><div v-if="data?.incidents.length" class="table-wrap"><table class="table"><thead><tr><th>Incident</th><th>Awareness</th><th>Impact</th><th>Authority deadline</th><th>Role</th></tr></thead><tbody><tr v-for="incident in data.incidents" :key="incident.id"><td><NuxtLink :to="`/privacy/${incident.id}`">{{ incident.public_id }}</NuxtLink><br><span class="muted">{{ incident.detection_source.replaceAll('_', ' ') }}</span></td><td>{{ new Date(incident.awareness_at).toLocaleString() }}</td><td>{{ impact(incident) }}</td><td><span class="status" :class="`status--${deadlineTone(incident.authority_notification_deadline_at)}`">{{ new Date(incident.authority_notification_deadline_at).toLocaleString() }}</span></td><td>{{ incident.controller_role }}</td></tr></tbody></table></div><p v-else-if="data" class="empty-state">No privacy incidents have been recorded.</p><p v-else class="empty-state">Loading incident register…</p></section>
  </section>
</template>
