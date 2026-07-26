<script setup lang="ts">
import {
  Bug,
  Hash,
  Headset,
  Lightbulb,
  ListChecks,
  MailCheck,
  MessageSquarePlus,
  MonitorPlay,
  Quote,
  ShieldCheck,
  Sparkles,
  Telescope,
  Users,
  WandSparkles,
  Webhook,
} from '@lucide/vue'

const props = withDefaults(defineProps<{ name: string; size?: number }>(), { size: 22 })

const iconMap: Record<string, unknown> = {
  'bug': Bug,
  'hash': Hash,
  'headset': Headset,
  'lightbulb': Lightbulb,
  'list-checks': ListChecks,
  'mail-check': MailCheck,
  'message-square-plus': MessageSquarePlus,
  'monitor-play': MonitorPlay,
  'quote': Quote,
  'shield-check': ShieldCheck,
  'sparkles': Sparkles,
  'telescope': Telescope,
  'users': Users,
  'wand-sparkles': WandSparkles,
  'webhook': Webhook,
}

const icon = computed(() => iconMap[props.name] ?? Sparkles)
</script>

<template>
  <component :is="icon" :size="size" aria-hidden="true" />
</template>
