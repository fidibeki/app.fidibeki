<script setup lang="ts">
const route = useRoute()
await navigateTo(`/projects/${String(route.params.id || '')}`, { replace: true })
</script>
