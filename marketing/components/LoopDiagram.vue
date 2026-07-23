<script setup lang="ts">
import { loopStages } from '~/data/site'

const stages = loopStages
const values = [100, 82, 66, 52, 40]
const total = values.reduce((sum, value) => sum + value, 0)

const height = 200
const width = 880
const gap = 4
const usableWidth = width - gap * (stages.length - 1)

function segmentWidth(index: number) {
  return (values[index] / total) * usableWidth
}

function leftX(index: number) {
  let x = 0
  for (let i = 0; i < index; i += 1) {
    x += segmentWidth(i) + gap
  }
  return x
}

function topY(index: number) {
  const taper = 72
  return taper * (index / (stages.length - 1))
}

function bottomY(index: number) {
  return height - topY(index)
}

function pathForSegment(index: number) {
  const x = leftX(index)
  const w = segmentWidth(index)
  const t = topY(index)
  const b = bottomY(index)
  const nextT = topY(index + 1)
  const nextB = bottomY(index + 1)
  const r = 10

  return `
    M ${x + r} ${t}
    L ${x + w - r} ${nextT}
    Q ${x + w} ${nextT} ${x + w} ${nextT + r}
    L ${x + w} ${nextB - r}
    Q ${x + w} ${nextB} ${x + w - r} ${nextB}
    L ${x + r} ${b}
    Q ${x} ${b} ${x} ${b - r}
    L ${x} ${t + r}
    Q ${x} ${t} ${x + r} ${t}
    Z
  `
}
</script>

<template>
  <figure class="funnel" aria-label="The Fidibeki loop">
    <svg
      :viewBox="`0 0 ${width} ${height}`"
      role="img"
      aria-labelledby="funnel-title"
      preserveAspectRatio="xMidYMid meet"
    >
      <title id="funnel-title">
        From report to resolution: collect, understand, decide, track, close the loop.
      </title>
      <g v-for="(stage, index) in stages" :key="stage.key" class="funnel-group">
        <path :d="pathForSegment(index)" class="funnel-segment" />
        <text
          :x="leftX(index) + segmentWidth(index) / 2"
          :y="height / 2 - 8"
          text-anchor="middle"
          class="funnel-label"
        >
          {{ stage.label }}
        </text>
        <text
          :x="leftX(index) + segmentWidth(index) / 2"
          :y="height / 2 + 16"
          text-anchor="middle"
          class="funnel-number"
        >
          {{ index + 1 }}
        </text>
      </g>
    </svg>
    <figcaption class="visually-hidden">
      <ol>
        <li v-for="stage in stages" :key="stage.key">
          {{ stage.label }}: {{ stage.blurb }}
        </li>
      </ol>
    </figcaption>
  </figure>
</template>

<style scoped>
.funnel {
  margin: 0;
  padding: 0;
  max-width: 64rem;
  margin-inline: auto;
}

.funnel svg {
  display: block;
  width: 100%;
  height: auto;
  overflow: visible;
}

.funnel-segment {
  fill: var(--surface);
  stroke: var(--line);
  stroke-width: 1.5;
  transition: fill 180ms ease, stroke 180ms ease, filter 180ms ease;
}

.funnel-group:hover .funnel-segment {
  fill: var(--accent-soft);
  stroke: var(--accent);
  filter: drop-shadow(0 4px 10px rgba(0, 0, 0, 0.06));
}

.funnel-group:nth-child(1) .funnel-segment {
  fill: var(--surface);
}

.funnel-group:nth-child(2) .funnel-segment {
  fill: color-mix(in srgb, var(--surface) 92%, var(--accent) 8%);
}

.funnel-group:nth-child(3) .funnel-segment {
  fill: color-mix(in srgb, var(--surface) 84%, var(--accent) 16%);
}

.funnel-group:nth-child(4) .funnel-segment {
  fill: color-mix(in srgb, var(--surface) 76%, var(--accent) 24%);
}

.funnel-group:nth-child(5) .funnel-segment {
  fill: color-mix(in srgb, var(--surface) 68%, var(--accent) 32%);
}

.funnel-label {
  font-family: var(--font-display);
  font-weight: 640;
  font-size: 1.05rem;
  fill: var(--ink);
  pointer-events: none;
}

.funnel-number {
  font-family: var(--font-display);
  font-weight: 700;
  font-size: 0.85rem;
  fill: var(--accent-text);
  opacity: 0.8;
  pointer-events: none;
}

.visually-hidden {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  border: 0;
}

@media (max-width: 760px) {
  .funnel-label {
    font-size: 0.72rem;
  }

  .funnel-number {
    font-size: 0.6rem;
  }
}
</style>
