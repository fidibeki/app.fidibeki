<script setup lang="ts">
import { loopStages } from '~/data/site'

const lastIndex = loopStages.length - 1
</script>

<template>
  <div class="loop-wrap">
    <ol class="loop" aria-label="The Fidibeki loop">
      <li
        v-for="(stage, index) in loopStages"
        :key="stage.key"
        v-reveal="index * 80"
        class="loop-stage"
        :class="{ 'is-final': index === lastIndex }"
        :style="{ '--i': index }"
      >
        <span class="loop-chip">
          <span class="loop-index" aria-hidden="true">{{ String(index + 1).padStart(2, '0') }}</span>
          <span class="loop-label">
            {{ stage.label }}<span v-if="index === lastIndex" class="loop-check" aria-hidden="true">✓</span>
          </span>
        </span>
        <span class="loop-blurb">{{ stage.blurb }}</span>
      </li>
    </ol>
    <div v-reveal="loopStages.length * 80 + 120" class="loop-return" aria-hidden="true">
      <span class="loop-return-label">The reporter hears the outcome</span>
    </div>
  </div>
</template>

<style scoped>
.loop-wrap {
  --loop-gap: 0.9rem;
  --return-line: color-mix(in oklab, var(--accent) 60%, transparent);
  position: relative;
  padding-left: 1.1rem;
}

.loop {
  display: grid;
  grid-template-columns: 1fr;
  gap: var(--loop-gap);
  list-style: none;
  margin: 0;
  padding: 0;
}

.loop-stage {
  display: flex;
  flex-direction: column;
  gap: 0.45rem;
}

.loop-chip {
  display: flex;
  align-items: center;
  gap: 0.55rem;
  padding: 0.85rem 1.1rem;
  border-radius: var(--radius-lg);
  background: var(--surface-soft);
  background: color-mix(in oklab, var(--surface-soft), var(--accent-soft) calc(var(--i) * 18%));
  transition: transform 160ms ease, box-shadow 160ms ease;
}

.loop-stage:hover .loop-chip {
  transform: translateY(-2px);
  box-shadow: var(--shadow-card);
}

.loop-stage.is-final .loop-chip {
  background: linear-gradient(135deg, var(--accent), var(--accent-strong));
  color: var(--on-accent);
}

.loop-index {
  font-family: var(--font-mono);
  font-size: 0.72rem;
  font-weight: 600;
  letter-spacing: 0.06em;
  color: var(--accent-text);
}

.loop-stage.is-final .loop-index {
  color: color-mix(in oklab, var(--on-accent) 80%, transparent);
}

.loop-label {
  font-family: var(--font-display);
  font-weight: 640;
  font-size: clamp(0.85rem, 0.62rem + 0.55vw, 1rem);
  white-space: nowrap;
}

.loop-check {
  margin-left: 0.4rem;
  font-weight: 700;
}

.loop-blurb {
  color: var(--muted);
  font-size: 0.88rem;
  line-height: 1.5;
  padding-inline: 0.2rem;
}

/* Return path — mobile: a dashed rail running back up the left side,
   arrow re-entering at Capture. */
.loop-return {
  position: absolute;
  left: 0;
  top: 1.4rem;
  bottom: 2rem;
  width: 0.8rem;
  border: 1.5px dashed var(--return-line);
  border-right: 0;
  border-radius: 14px 0 0 14px;
}

.loop-return::before {
  content: '';
  position: absolute;
  top: -5px;
  right: -3px;
  border-block: 4.5px solid transparent;
  border-left: 7px solid var(--accent);
}

.loop-return-label {
  display: none;
}

@media (min-width: 900px) {
  .loop-wrap {
    --notch: 16px;
    padding-left: 0;
  }

  .loop {
    grid-template-columns: repeat(5, 1fr);
    gap: 4px;
  }

  .loop-chip {
    border-radius: 0;
    padding: 0.9rem 1.1rem 0.9rem calc(var(--notch) + 0.9rem);
    clip-path: polygon(
      0 0,
      calc(100% - var(--notch)) 0,
      100% 50%,
      calc(100% - var(--notch)) 100%,
      0 100%,
      var(--notch) 50%
    );
  }

  .loop-stage:first-child .loop-chip {
    padding-left: 1.1rem;
    border-radius: var(--radius) 0 0 var(--radius);
    clip-path: polygon(0 0, calc(100% - var(--notch)) 0, 100% 50%, calc(100% - var(--notch)) 100%, 0 100%);
  }

  .loop-stage.is-final .loop-chip {
    border-radius: 0 var(--radius) var(--radius) 0;
    clip-path: polygon(0 0, 100% 0, 100% 100%, 0 100%, var(--notch) 50%);
  }

  /* clip-path swallows box-shadow; keep the lift only. */
  .loop-stage:hover .loop-chip {
    box-shadow: none;
  }

  .loop-blurb {
    padding: 0 0.35rem 0 calc(var(--notch) + 0.2rem);
  }

  .loop-stage:first-child .loop-blurb {
    padding-left: 0.2rem;
  }

  /* Return path — desktop: an arc from Communicate swinging back under the
     pipeline, arrowhead pointing up into Capture. */
  .loop-return {
    position: relative;
    inset: auto;
    width: auto;
    height: 2rem;
    margin: 0.25rem 10% 0;
    border: 1.5px dashed var(--return-line);
    border-top: 0;
    border-radius: 0 0 18px 18px;
  }

  .loop-return::before {
    top: -6px;
    right: auto;
    left: -5.25px;
    border-inline: 4.5px solid transparent;
    border-left-width: 4.5px;
    border-bottom: 7px solid var(--accent);
    border-top: 0;
  }

  .loop-return-label {
    position: absolute;
    left: 50%;
    bottom: 0;
    transform: translate(-50%, 55%);
    display: inline-flex;
    align-items: center;
    gap: 0.4rem;
    padding: 0.3rem 0.8rem;
    border: 1px solid var(--line);
    border-radius: 999px;
    background: var(--surface);
    color: var(--accent-text);
    font-family: var(--font-display);
    font-size: 0.8rem;
    font-weight: 600;
    white-space: nowrap;
  }

  .loop-return-label::before {
    content: '↺';
    font-size: 0.9rem;
  }
}
</style>
