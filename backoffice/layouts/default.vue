<script setup lang="ts">
import { Building2, ChevronDown, House, ListTodo, LogOut, ShieldAlert } from '@lucide/vue'

const route = useRoute()
const { session, clear } = usePlatformAuth()
const navigation = [
  { label: 'Overview', to: '/', icon: House },
  { label: 'Workspaces', to: '/workspaces', icon: Building2 },
  { label: 'Queues', to: '/queues', icon: ListTodo },
  { label: 'Privacy', to: '/privacy', icon: ShieldAlert },
]
const displayName = computed(() => session.value.admin?.email.split('@')[0] || 'Platform admin')

function isActive(to: string) {
  return to === '/' ? route.path === '/' : route.path.startsWith(to)
}

async function logout() {
  try {
    await $fetch('/admin/auth/logout', { method: 'POST', credentials: 'include' })
  } finally {
    clear()
    await navigateTo('/login')
  }
}
</script>

<template>
  <div class="backoffice-shell">
    <header class="backoffice-topbar">
      <NuxtLink to="/" class="backoffice-brand" aria-label="Fidibeki platform administration">
        <span>F</span><b>fidibeki</b><em>Platform</em>
      </NuxtLink>
      <p class="backoffice-context">Administration</p>
      <details class="backoffice-account-menu">
        <summary><i>{{ displayName.slice(0, 1).toUpperCase() }}</i><span>{{ displayName }}</span><ChevronDown :size="13" /></summary>
        <div>
          <p><b>{{ displayName }}</b><span>{{ session.admin?.email }}</span></p>
          <button type="button" @click="logout"><LogOut :size="15" /> Sign out</button>
        </div>
      </details>
    </header>

    <aside class="backoffice-rail">
      <nav aria-label="Platform navigation">
        <NuxtLink v-for="item in navigation" :key="item.to" :to="item.to" :data-active="isActive(item.to)">
          <component :is="item.icon" :size="20" /><span>{{ item.label }}</span>
        </NuxtLink>
      </nav>
    </aside>

    <main class="backoffice-main"><slot /></main>
  </div>
</template>
