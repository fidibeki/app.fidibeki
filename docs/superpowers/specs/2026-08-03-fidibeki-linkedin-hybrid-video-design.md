# Fidibeki LinkedIn Hybrid Video Design

**Date:** 2026-08-03  
**Status:** Approved direction; production design awaiting final review  
**Channel:** LinkedIn native video  
**Objective:** Make one Fidibeki product boundary understandable in thirty seconds without requiring viewers to read a long post.

## Core idea

The first video teaches one idea only:

> A report tells the team what happened. The team still decides whether it becomes work.

The visual moves from a short Paper Diorama metaphor into the actual Fidibeki interface. The generated opening earns attention; the real product footage proves that the workflow exists.

This is not a general product tour. It does not explain every capture type, AI analysis, replay, integration, plan, or testimonial feature.

## Selected approach

The video is a **thirty-second vertical hybrid**:

1. Four seconds of non-photoreal Paper Diorama animation.
2. Twenty-two seconds of real Fidibeki product capture using synthetic demo data.
3. Four seconds for the product name and one conversation question.

The video must make sense with sound disabled. It uses up to six short text statements rather than narration or paragraph subtitles. Small interface sounds and restrained ambient audio may support the motion, but audio cannot carry essential meaning.

## Format

- Canvas: 1080 × 1920, 9:16 vertical.
- Duration: 30 seconds, with a tolerance of 0.5 seconds.
- Frame rate: 30 fps for the assembled video.
- Delivery: MP4 suitable for native LinkedIn upload.
- Safe area: essential text stays away from the top 160 px, bottom 300 px, and outer 80 px.
- Captions: no spoken narration, so no subtitle track is required.
- Brand: Fidibeki cream, ink, teal, and restrained amber/red status accents.
- Readability: no more than seven words in any overlay and no more than one overlay at a time.

## Shot design

### Shot 1 — the wrong shortcut, 0:00–0:04

**Visual:** A handcrafted Paper Diorama user places a report card onto a small machine. The machine immediately stamps it `ISSUE` and drops it into a paper backlog tray that bends under the weight.

**Overlay:** `A report is not an issue.`

**Motion:** Locked miniature stage, shallow parallax, one quick mechanical stamp. No people, logos, dialogue, photorealism, or tiny decorative text.

**Purpose:** Establish the argument before the viewer sees the product.

### Shot 2 — capture what happened, 0:04–0:10

**Visual:** Match-cut from the paper report card to the real Fidibeki widget inside a neutral synthetic SaaS page. A short report is entered and the reporter deliberately chooses one safe visual context item. No personal email, real customer name, or sensitive page content is visible.

**Overlay:** `First, understand what happened.`

**Product truth:** The report originates in-product and context is reporter-chosen. The shot must not imply that Fidibeki automatically reconstructs the bug.

### Shot 3 — inspect before committing, 0:10–0:18

**Visual:** The real Feedback queue opens with one synthetic report selected. The inspector shows its source, page context, written report, and evidence availability. The cursor briefly reveals the actions to create a new issue or link the report to existing work.

**Overlay:** `Then decide if it is work.`

**Product truth:** Feedback remains feedback until the team deliberately creates or links an issue. Avoid a rapid tour of every filter and evidence tab.

### Shot 4 — track the deliberate decision, 0:18–0:25

**Visual:** The selected report is deliberately linked to an issue. Cut to the real Issue detail screen, showing linked feedback and a concise status transition. The interaction should remain readable at mobile size.

**Overlay:** `Track the decision.`

**Product truth:** Issues have tracked statuses and activity. Do not show a transition that the interface does not support.

### Shot 5 — close the loop, 0:25–0:30

**Visual:** The real secure reporter progress view shows a synthetic opted-in reporter receiving an honest resolved update. End on the Fidibeki wordmark over the cream background.

**Overlay:** `And close the loop.`

**Final question:** `Where does feedback become work?`

**Product truth:** Only an opted-in reporter receives updates. The secure reporter view does not require a product account.

## LinkedIn caption

The caption is intentionally short and does not repeat the video:

> Most feedback tools turn every report into a ticket.
>
> I think that is the wrong boundary.
>
> A report tells you what happened. Your team still decides whether it becomes work.
>
> That decision is why I’m building Fidibeki.

No hashtags, external link, feature list, artificial engagement request, or generic AI claim belongs in the post body. The closing question already exists inside the video.

## Asset plan

### Generated asset

- One Paper Diorama source clip, trimmed to four seconds in the final edit.
- The style reference must use handcrafted layered paper, visible cut edges, tactile shadows, simple geometric figures, and Fidibeki’s restrained palette.
- Higgsfield generation must stay strictly non-photoreal and must not invent product screens, UI text, customer marks, or logos.

### Real product assets

- A local or staging environment with a clean synthetic workspace.
- One neutral demonstration product page with the Fidibeki widget installed.
- One synthetic feedback record with safe report text and one safe context item.
- One synthetic issue linked from that report.
- One synthetic opted-in reporter with secure progress access.
- Fidibeki wordmark in a repository-owned format.

Every visible identifier must be synthetic. Tokens, email addresses, local paths, API hosts, workspace IDs, browser chrome, and developer tooling must be cropped or redacted.

## Capture and assembly workflow

1. Prepare the synthetic workspace and verify the complete workflow manually.
2. Capture each product interaction as a separate clean take at a resolution that remains sharp after vertical cropping.
3. Generate one Paper Diorama source clip after its prompt and credit cost are approved, then trim it to the four-second hook.
4. Select the shortest clean sections from the real product captures; do not accelerate them until labels become unreadable.
5. Assemble the five shots, add the short text overlays, minimal audio, and the final wordmark.
6. Export one vertical MP4 and review it at actual phone size with sound disabled before reviewing audio.

## Failure handling

- If the Paper Diorama clip drifts toward realism, changes the palette, or renders illegible text, remove generated text from the prompt and regenerate the hook only.
- If a real product interaction takes too long, split it at a natural state boundary rather than applying an unreadable speed-up.
- If the reporter view cannot be populated safely, end on the Issue activity showing the reporter update event; do not fabricate a reporter screen.
- If any real identifier appears, reject the take and recapture. Blurring is a fallback, not the primary privacy strategy.
- If the complete workflow cannot stay legible in thirty seconds, remove a transition. Do not extend the video or add explanatory copy.

## Acceptance checks

The video is ready only when all checks pass:

- A viewer can state the core idea after one silent viewing.
- The first frame communicates tension without a text paragraph.
- At least twenty seconds show the real Fidibeki product.
- The video shows a deliberate create/link decision between feedback and an issue.
- The reporter update is shown only for synthetic opt-in data.
- No shot implies automatic bug reproduction, automatic issue creation, unrestricted replay, or AI access to replay.
- No overlay exceeds seven words.
- All overlays remain readable on a phone without pausing.
- No real customer, reporter, token, email, workspace, or infrastructure data is visible.
- The MP4 is thirty seconds, vertical, and understandable with sound disabled.
- Nothing is posted automatically.

## Scope exclusions

- A second video or alternate aspect ratio.
- A full product walkthrough.
- AI narration, talking heads, or lip-sync.
- Native interactivity inside the generated video.
- Pricing, plan comparison, integrations, or testimonial features.
- Automatic publishing, scheduling, or outreach.
