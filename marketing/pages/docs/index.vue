<script setup lang="ts">
import { ArrowRight, BookOpen } from '@lucide/vue'
import { docs } from '~/data/docs'

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
      eyebrow="Documentation"
      title="From script tag to first feedback"
      lede="Short, honest guides for getting Fidibeki running: install the widget, shape its behavior with rules, and wire events into your own systems."
    >
      <template #breadcrumbs>
        <Breadcrumbs :items="[{ name: 'Home', path: '/' }, { name: 'Docs', path: '/docs' }]" />
      </template>
    </PageHero>

    <section class="section">
      <div class="container">
        <div class="docs-grid">
          <NuxtLink v-for="(doc, index) in docs" :key="doc.slug" v-reveal="index * 60" :to="`/docs/${doc.slug}`" class="card doc-card">
            <span class="doc-icon"><BookOpen :size="20" aria-hidden="true" /></span>
            <h2>{{ doc.title }}</h2>
            <p>{{ doc.description }}</p>
            <span class="doc-meta">{{ doc.minutes }} min read <ArrowRight :size="14" aria-hidden="true" /></span>
          </NuxtLink>
        </div>

        <div class="card docs-quickstart">
          <h2>The 30-second version</h2>
          <p>Create a project, allow your domain, paste one script tag:</p>
          <pre><code>&lt;script src="https://widget.fidibeki.com/v1/loader.js"
        data-fidibeki-key="pk_live_..." async&gt;&lt;/script&gt;</code></pre>
          <p class="docs-quickstart-note">
            The loader is async, renders in a shadow root, and checks the browser origin against
            your allowed domains. Full details in
            <NuxtLink to="/docs/installation">Install the widget</NuxtLink>.
          </p>
        </div>
      </div>
    </section>

    <CtaSection
      title="Stuck on setup?"
      body="Book a demo and we will install it with you. Or start a free trial and follow the in-app getting started checklist."
    />
  </div>
</template>

<style scoped>
.docs-grid {
  display: grid;
  grid-template-columns: 1fr;
  gap: 1rem;
}

.doc-card {
  display: flex;
  flex-direction: column;
  gap: 0.35rem;
  color: var(--ink);
  transition: border-color 160ms ease, transform 160ms ease;
}

.doc-card:hover {
  text-decoration: none;
  border-color: var(--accent);
  transform: translateY(-3px);
}

.doc-card h2 {
  margin: 0;
  font-size: 1.2rem;
}

.doc-card p {
  margin: 0;
  color: var(--muted);
  font-size: 0.94rem;
}

.doc-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 42px;
  height: 42px;
  margin-bottom: 0.4rem;
  border-radius: 10px;
  background: var(--accent-soft);
  color: var(--accent-text);
}

.doc-meta {
  display: inline-flex;
  align-items: center;
  gap: 0.3rem;
  margin-top: 0.5rem;
  color: var(--accent-text);
  font-size: 0.86rem;
  font-weight: 600;
}

.docs-quickstart {
  margin-top: 2rem;
}

.docs-quickstart h2 {
  font-size: 1.2rem;
}

.docs-quickstart-note {
  margin: 0.9rem 0 0;
  color: var(--muted);
  font-size: 0.92rem;
}

@media (min-width: 800px) {
  .docs-grid {
    grid-template-columns: repeat(3, 1fr);
  }
}
</style>
