<script setup lang="ts">
type Incident = { id: string; public_id: string; awareness_at: string; detection_source: string; controller_role: string; containment_summary: string; authority_notification_deadline_at: string }
type Chronology = { id: string; entry_kind: string; summary: string; occurred_at: string }
type Assessment = { id: string; authority_notification_decision: string; high_risk_to_individuals: string; data_subject_communication_decision: string; assessed_at: string }
type Detail = { incident: Incident; chronology: Chronology[]; assessments: Assessment[] }

const route = useRoute()
const { $platformApi } = useNuxtApp()
const id = String(route.params.id)
const { data, error } = await useAsyncData(`privacy-incident:${id}`, () => $platformApi<Detail>(`/admin/breach-incidents/${id}`), { server: false })
</script>

<template>
  <section v-if="data">
    <header class="page-head"><div><p class="eyebrow">Privacy incident</p><h1 class="title">{{ data.incident.public_id }}</h1><p class="subtitle">Aware since {{ new Date(data.incident.awareness_at).toLocaleString() }} · {{ data.incident.controller_role }}</p></div><NuxtLink class="button secondary" to="/privacy">All incidents</NuxtLink></header>
    <section class="panel"><p class="eyebrow">Containment</p><h2>Current recorded summary</h2><p class="muted">{{ data.incident.containment_summary }}</p></section>
    <section class="panel"><p class="eyebrow">Chronology</p><div v-if="data.chronology.length" class="table-wrap"><table class="table"><thead><tr><th>Event</th><th>Summary</th><th>Recorded</th></tr></thead><tbody><tr v-for="entry in data.chronology" :key="entry.id"><td>{{ entry.entry_kind.replaceAll('_', ' ') }}</td><td>{{ entry.summary }}</td><td>{{ new Date(entry.occurred_at).toLocaleString() }}</td></tr></tbody></table></div><p v-else class="empty-state">No chronology entries have been recorded.</p></section>
    <section class="panel"><p class="eyebrow">Assessments</p><div v-if="data.assessments.length" class="table-wrap"><table class="table"><thead><tr><th>Authority</th><th>Individual risk</th><th>Communication</th><th>Assessed</th></tr></thead><tbody><tr v-for="assessment in data.assessments" :key="assessment.id"><td>{{ assessment.authority_notification_decision.replaceAll('_', ' ') }}</td><td>{{ assessment.high_risk_to_individuals.replaceAll('_', ' ') }}</td><td>{{ assessment.data_subject_communication_decision.replaceAll('_', ' ') }}</td><td>{{ new Date(assessment.assessed_at).toLocaleString() }}</td></tr></tbody></table></div><p v-else class="empty-state">No assessments have been recorded.</p></section>
  </section>
  <section v-else><h1 class="title">Incident unavailable</h1><p class="error">{{ error ? 'The incident could not be loaded.' : 'Loading…' }}</p></section>
</template>
