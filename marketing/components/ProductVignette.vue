<script setup lang="ts">
import { CircleCheck, MailCheck, MonitorPlay, Sparkles } from '@lucide/vue'
</script>

<template>
  <!-- Stylized, brand-accurate depiction of the product workflow: a feedback
       submission, optional AI analysis on eligible plans, a manually linked
       issue, and an update sent to a reporter who opted in.
       Purpose-built illustration. Every element mirrors a real capability. -->
  <div class="vignette" role="img" aria-label="Illustration of how a team can move a feedback report through Fidibeki: a reporter can choose to attach a screenshot and session replay when the rule enables those tools; on eligible plans the report may be analyzed by AI; the team triages it into an issue; and an update is sent only to reporters who opted in.">
    <div class="pane pane-feedback">
      <div class="pane-title">
        <span class="dot" aria-hidden="true"></span>
        Feedback · checkout-web
      </div>
      <p class="feedback-quote">“Payment button does nothing after I apply a discount code.”</p>
      <div class="chips">
        <span class="chip chip-replay"><MonitorPlay :size="13" aria-hidden="true" /> Replay · 2m 14s</span>
        <span class="chip">Screenshot</span>
      </div>
      <div class="ai-row">
        <Sparkles :size="14" aria-hidden="true" class="ai-icon" />
        <div>
          <span class="ai-label">AI analysis</span>
          <span class="ai-body">Sentiment: frustrated · “Discount code disables checkout CTA”</span>
        </div>
      </div>
    </div>

    <div class="pane pane-issue">
      <div class="pane-title">
        <span class="issue-key">ISSUE</span>
        Checkout blocked after discount code
      </div>
      <div class="status-track">
        <span class="status done">Open</span>
        <span class="status done">In progress</span>
        <span class="status current"><CircleCheck :size="13" aria-hidden="true" /> Resolved</span>
      </div>
      <div class="issue-meta">
        <span class="chip chip-priority">Urgent</span>
        <span class="issue-links">3 linked reports</span>
      </div>
    </div>

    <div class="pane pane-email">
      <MailCheck :size="15" aria-hidden="true" class="email-icon" />
      <div>
        <span class="email-title">Reporter update sent</span>
        <span class="email-body">“Good news, the issue you reported has been resolved.”</span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.vignette {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 0.9rem;
  max-width: 30rem;
  font-size: 0.85rem;
}

.pane {
  border: 1px solid var(--line);
  border-radius: var(--radius-lg);
  background: var(--surface);
  box-shadow: var(--shadow-card);
  padding: 1rem 1.1rem;
  /* Notification-style entrance: each pane arrives in sequence. */
  opacity: 0;
  transform: translateY(18px) scale(0.98);
  animation: pane-in 620ms cubic-bezier(0.2, 0.65, 0.25, 1) forwards;
}

.pane-feedback {
  animation-delay: 200ms;
}

.pane-issue {
  margin-left: clamp(0rem, 4vw, 2.5rem);
  animation-delay: 440ms;
}

.pane-email {
  display: flex;
  gap: 0.6rem;
  align-items: flex-start;
  margin-left: clamp(0rem, 8vw, 5rem);
  animation-delay: 680ms;
}

@keyframes pane-in {
  to {
    opacity: 1;
    transform: none;
  }
}

.pane-title {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  font-weight: 640;
  font-family: var(--font-display);
  font-size: 0.9rem;
  margin-bottom: 0.6rem;
}

.dot {
  width: 8px;
  height: 8px;
  border-radius: 999px;
  background: var(--accent);
  animation: dot-pulse 2.6s ease-in-out 1.4s infinite;
}

@keyframes dot-pulse {
  50% {
    box-shadow: 0 0 0 5px color-mix(in srgb, var(--accent) 22%, transparent);
  }
}

.feedback-quote {
  margin: 0 0 0.7rem;
  color: var(--ink-soft);
  font-style: italic;
}

.chips {
  display: flex;
  flex-wrap: wrap;
  gap: 0.4rem;
}

.chip {
  display: inline-flex;
  align-items: center;
  gap: 0.3rem;
  padding: 0.14rem 0.55rem;
  border-radius: 999px;
  background: var(--surface-soft);
  color: var(--ink-soft);
  font-size: 0.75rem;
  font-weight: 600;
}

.chip-replay {
  background: var(--accent-soft);
  color: var(--accent-text);
}

.chip-priority {
  background: var(--accent-soft);
  color: var(--accent-text);
}

.ai-row {
  display: flex;
  gap: 0.5rem;
  align-items: flex-start;
  margin-top: 0.8rem;
  padding-top: 0.8rem;
  border-top: 1px dashed var(--line);
}

.ai-icon {
  color: var(--accent);
  flex-shrink: 0;
  margin-top: 0.15rem;
}

.ai-label {
  display: block;
  font-size: 0.72rem;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--accent-text);
}

.ai-body {
  color: var(--muted);
  font-size: 0.8rem;
}

.issue-key {
  padding: 0.1rem 0.4rem;
  border-radius: 4px;
  background: var(--accent-soft);
  color: var(--accent-text);
  font-size: 0.68rem;
  letter-spacing: 0.06em;
}

.status-track {
  display: flex;
  flex-wrap: wrap;
  gap: 0.35rem;
  margin-bottom: 0.7rem;
}

.status {
  padding: 0.16rem 0.55rem;
  border-radius: 999px;
  border: 1px solid var(--line);
  color: var(--muted);
  font-size: 0.75rem;
  font-weight: 600;
}

.status.done {
  text-decoration: line-through;
  text-decoration-thickness: 1px;
}

.status.current {
  display: inline-flex;
  align-items: center;
  gap: 0.3rem;
  border-color: transparent;
  background: var(--teal-fill);
  color: var(--on-accent);
  text-decoration: none;
}

.issue-meta {
  display: flex;
  align-items: center;
  gap: 0.6rem;
}

.issue-links {
  color: var(--muted);
  font-size: 0.78rem;
}

.email-icon {
  color: var(--accent);
  flex-shrink: 0;
  margin-top: 0.15rem;
}

.email-title {
  display: block;
  font-weight: 640;
  font-family: var(--font-display);
  font-size: 0.86rem;
}

.email-body {
  color: var(--muted);
  font-size: 0.8rem;
}
</style>
