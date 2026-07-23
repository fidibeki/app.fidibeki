<script setup lang="ts">
import { loopStages } from '~/data/site'
</script>

<template>
  <ol class="loop" aria-label="The Fidibeki loop">
    <li
      v-for="(stage, index) in loopStages"
      :key="stage.key"
      v-reveal="index * 80"
      class="loop-stage"
    >
      <span class="loop-index" aria-hidden="true">{{ index + 1 }}</span>
      <span class="loop-label">{{ stage.label }}</span>
      <span class="loop-blurb">{{ stage.blurb }}</span>
    </li>
  </ol>
</template>

<style scoped>
.loop {
  --loop-gap: 0.9rem;
  display: grid;
  grid-template-columns: 1fr;
  gap: var(--loop-gap);
  list-style: none;
  margin: 0;
  padding: 0;
  counter-reset: stage;
}

.loop-stage {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 0.3rem;
  padding: 1.1rem 1.2rem;
  border: 1px solid var(--line);
  border-radius: var(--radius-lg);
  background: var(--surface);
  transition: border-color 160ms ease, transform 160ms ease;
}

.loop-stage:hover {
  border-color: var(--accent);
  transform: translateY(-2px);
}

.loop-index {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 1.6rem;
  height: 1.6rem;
  border-radius: 999px;
  background: var(--accent-soft);
  color: var(--accent-text);
  font-size: 0.8rem;
  font-weight: 700;
  font-family: var(--font-display);
}

.loop-label {
  font-family: var(--font-display);
  font-weight: 640;
  font-size: 1.05rem;
}

.loop-blurb {
  color: var(--muted);
  font-size: 0.88rem;
  line-height: 1.5;
}

@media (min-width: 900px) {
  .loop {
    grid-template-columns: repeat(5, 1fr);
  }

  .loop-stage:not(:last-child)::after {
    content: '→';
    position: absolute;
    right: calc(-1 * var(--loop-gap) + 2px);
    top: 1.05rem;
    color: var(--accent);
    font-weight: 700;
  }
}
</style>
