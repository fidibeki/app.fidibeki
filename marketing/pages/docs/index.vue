<script setup lang="ts">
const { copy } = useMarketingI18n()

import { ArrowUpRight, Check, Copy } from '@lucide/vue'
import { docs } from '~/data/docs'

const quickstartSnippet = `<script
  src="https://widget.fidibeki.com/v1/loader.js"
  data-fidibeki-key="pk_live_..."
  async
><\/script>`

const copyState = ref<'idle' | 'copied' | 'error'>('idle')
const canCopy = ref(false)
onMounted(() => {
  canCopy.value = Boolean(navigator.clipboard?.writeText)
})

async function copySnippet() {
  try {
    await navigator.clipboard.writeText(quickstartSnippet)
    copyState.value = 'copied'
  } catch {
    copyState.value = 'error'
  }
}

useSiteMeta({
  title: 'Documentation',
  description:
    'Install the Fidibeki widget, configure widget rules and targeting, and verify signed webhook deliveries. Everything you need to get running.',
  path: '/docs',
  breadcrumbs: [
    { name: 'Home', path: '/' },
    { name: 'Docs', path: '/docs' },
  ],
})
</script>

<template>
  <div>
    <PageHero
      :eyebrow="copy('Documentation')"
      :title="copy('From script tag to first feedback')"
      :lede="
        copy(
          'Install the widget, choose when it appears, and connect feedback to your team\'s workflow.',
        )
      "
    >
      <template #breadcrumbs
        ><Breadcrumbs
          :items="[
            { name: 'Home', path: '/' },
            { name: 'Docs', path: '/docs' },
          ]"
      /></template>
    </PageHero>
    <section
      class="section quickstart-section"
      aria-labelledby="quickstart-title"
    >
      <div class="container quickstart-layout">
        <div class="quickstart-copy">
          <p class="quickstart-label">{{ copy('Quick start') }}</p>
          <h2 id="quickstart-title">{{ copy('One script tag.') }}</h2>
          <p>
            {{
              copy(
                'Create a project, allow your domain, then add the widget to your site.',
              )
            }}
          </p>
          <NuxtLinkLocale to="/docs/installation" class="quickstart-link">
            {{ copy('Read the installation guide') }}
            <ArrowUpRight :size="16" aria-hidden="true" />
          </NuxtLinkLocale>
        </div>
        <div class="quickstart-example">
          <div class="snippet-frame">
            <div class="snippet-toolbar">
              <span>{{ copy('HTML') }}</span>
              <button v-if="canCopy" type="button" @click="copySnippet">
                <Check
                  v-if="copyState === 'copied'"
                  :size="15"
                  aria-hidden="true"
                />
                <Copy v-else :size="15" aria-hidden="true" />
                {{ copy(copyState === 'copied' ? 'Copied' : 'Copy code') }}
              </button>
            </div>
            <CodeBlock :code="quickstartSnippet" language="html" />
          </div>
          <p
            class="copy-status"
            :class="{ 'copy-status-error': copyState === 'error' }"
            role="status"
          >
            {{
              copy(
                copyState === 'error'
                  ? "Couldn't copy. Select and copy the code above."
                  : copyState === 'copied'
                    ? 'Code copied to clipboard.'
                    : '',
              )
            }}
          </p>
          <p class="quickstart-note">
            {{
              copy(
                'The loader is async, renders in a shadow root, and checks the browser origin against your allowed domains.',
              )
            }}
          </p>
        </div>
      </div>
    </section>
    <section class="section guides-section" aria-labelledby="guides-title">
      <div class="container">
        <h2 id="guides-title">{{ copy('Guides') }}</h2>
        <div class="docs-guides">
          <NuxtLinkLocale
            v-for="doc in docs"
            :key="doc.slug"
            :to="`/docs/${doc.slug}`"
            class="doc-entry"
          >
            <div class="doc-heading">
              <h3>{{ copy(doc.title) }}</h3>
              <ArrowUpRight :size="20" aria-hidden="true" />
            </div>
            <p>{{ copy(doc.description) }}</p>
            <span class="doc-time">{{
              copy('{minutes} min read', { minutes: copy(doc.minutes) })
            }}</span>
          </NuxtLinkLocale>
        </div>
      </div>
    </section>
    <CtaSection
      :title="copy('Stuck on setup?')"
      :body="
        copy(
          'Book a demo and we will install it with you. Or start a free trial and follow the in-app getting started checklist.',
        )
      "
    />
  </div>
</template>
<style scoped>
.quickstart-section {
  padding-top: 24px;
  padding-bottom: 40px;
}
.quickstart-layout {
  display: grid;
  grid-template-columns: minmax(0, 0.8fr) minmax(0, 1.2fr);
  gap: 80px;
  align-items: start;
}
.quickstart-copy {
  padding-top: 20px;
}
.quickstart-label {
  color: var(--muted);
  font-size: 13px;
  margin-bottom: 16px;
}
.quickstart-copy h2 {
  font-size: 30px;
  margin-bottom: 20px;
}
.quickstart-copy > p:not(.quickstart-label) {
  max-width: 340px;
  color: var(--muted);
  margin-bottom: 24px;
}
.quickstart-link {
  display: inline-flex;
  gap: 12px;
  align-items: center;
  font-size: 14px;
}
.quickstart-link svg {
  flex-shrink: 0;
}
.quickstart-example {
  min-width: 0;
}
.snippet-frame {
  background: var(--surface-soft);
  border-radius: 10px;
}
.snippet-toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  min-height: 64px;
  padding: 12px 28px 0;
  color: var(--muted);
  font-size: 12px;
}
.snippet-toolbar button {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  min-height: 44px;
  padding: 8px 0 8px 12px;
  background: none;
  border: 0;
  color: var(--ink-soft);
  cursor: pointer;
  font-size: 12px;
}
.snippet-toolbar button:hover {
  color: var(--ink);
  text-decoration: underline;
  text-underline-offset: 4px;
}
.snippet-frame :deep(pre) {
  background: transparent;
  margin: 0;
  padding: 12px 28px 28px;
  border-radius: 0;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  font-size: 13px;
  line-height: 1.9;
}
.snippet-frame :deep(code) {
  font-size: inherit;
}
.quickstart-note {
  color: var(--muted);
  font-size: 13px;
  margin: 16px 0 0;
  max-width: 580px;
}
.copy-status {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip-path: inset(50%);
  white-space: nowrap;
}
.copy-status-error {
  position: static;
  width: auto;
  height: auto;
  margin: 12px 0 0;
  clip-path: none;
  white-space: normal;
  font-size: 13px;
}
.guides-section {
  padding-top: 40px;
}
.guides-section h2 {
  font-size: 28px;
  margin-bottom: 36px;
}
.docs-guides {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 44px 80px;
}
.doc-entry {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
}
.doc-heading {
  display: flex;
  width: 100%;
  align-items: flex-start;
  justify-content: space-between;
  gap: 24px;
}
.doc-heading h3 {
  font-size: 22px;
  margin: 0;
}
.doc-heading svg {
  flex-shrink: 0;
  margin-top: 3px;
}
.doc-entry p {
  color: var(--muted);
  font-size: 15px;
  margin: 14px 0 16px;
  max-width: 480px;
}
.doc-time {
  color: var(--muted);
  font-size: 12px;
  margin-top: auto;
}
.doc-entry:hover {
  text-decoration: none;
}
.doc-entry:hover h3 {
  text-decoration: underline;
  text-decoration-thickness: 1px;
  text-underline-offset: 5px;
}
@media (max-width: 899px) {
  .quickstart-layout {
    grid-template-columns: minmax(0, 1fr);
    gap: 28px;
  }
  .quickstart-copy {
    padding-top: 0;
  }
  .quickstart-copy > p:not(.quickstart-label) {
    max-width: 520px;
  }
  .docs-guides {
    gap: 36px;
  }
}
@media (max-width: 600px) {
  .quickstart-section {
    padding-top: 12px;
    padding-bottom: 28px;
  }
  .guides-section {
    padding-top: 28px;
  }
  .docs-guides {
    grid-template-columns: minmax(0, 1fr);
    gap: 36px;
  }
  .snippet-toolbar {
    padding-inline: 20px;
  }
  .snippet-frame :deep(pre) {
    padding: 12px 20px 24px;
    font-size: 12px;
  }
  .doc-heading h3 {
    font-size: 21px;
  }
}
</style>
