export const chatStyles = `
:host {
  all: initial;
  --fb-bg: #fff; --fb-fg: #171a19; --fb-muted: #63706b; --fb-line: #d9dfdc;
  --fb-accent: #147d64; --fb-on-accent: #fff; --action-ink: #147d64;
  font: 14px/1.5 "Avenir Next", "Segoe UI", sans-serif;
  color: var(--fb-fg);
  position: fixed; right: 20px;
  bottom: calc(20px + var(--fidibeki-chat-bottom-offset, 0px));
  z-index: 2147483000;
}
:host([data-side=left]) { left: 20px; right: auto; }
* { box-sizing: border-box; }
[hidden] { display: none !important; }
button, textarea { font: inherit; }
button { cursor: pointer; }
button:disabled { cursor: default; opacity: .55; }
button:focus-visible, textarea:focus-visible { outline: 2px solid var(--fb-fg); outline-offset: -4px; }
.launcher { display: flex; align-items: center; gap: 9px; position: relative; min-height: 46px; padding: 11px 16px; border: 0; border-radius: 12px; background: var(--fb-accent); color: var(--fb-on-accent); box-shadow: 0 4px 18px #0003; font-weight: 650; }
.launcher:hover { filter: brightness(1.08); }
.launcher svg { width: 21px; height: 21px; flex: none; }
.badge { min-width: 20px; padding: 1px 6px; border-radius: 12px; background: var(--fb-bg); color: var(--fb-fg); font-size: 12px; text-align: center; }
.panel { position: absolute; bottom: 58px; right: 0; width: 380px; height: min(620px, calc(100dvh - 110px - var(--fidibeki-chat-bottom-offset, 0px))); display: flex; flex-direction: column; background: var(--fb-bg); color: var(--fb-fg); border-radius: 14px; box-shadow: 0 12px 48px #0004; overflow: hidden; }
:host([data-open=true][data-view=feedback]) .launcher { display: none; }
:host([data-view=feedback]) .panel { bottom: 0; height: auto; max-height: calc(var(--viewport-height, 100dvh) - 40px - var(--fidibeki-chat-bottom-offset, 0px)); overflow-y: auto; overscroll-behavior: contain; }
:host([data-side=left]) .panel, :host([data-side=left]) .preview { left: 0; right: auto; }
.head { flex: none; padding: 12px 14px 10px 20px; display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.identity { display: flex; align-items: center; gap: 10px; min-width: 0; }
.brand { width: 28px; height: 28px; object-fit: contain; flex: none; }
.name { font-size: 16px; line-height: 1.3; overflow-wrap: anywhere; }
.icon { flex: none; display: grid; place-items: center; border: 0; border-radius: 6px; background: transparent; color: var(--fb-muted); width: 40px; height: 40px; font-size: 24px; line-height: 1; }
.icon:hover { color: var(--fb-fg); background: color-mix(in srgb, var(--fb-fg) 7%, transparent); }
.tabs { display: flex; flex: none; gap: 24px; padding: 0 20px; border-bottom: 1px solid var(--fb-line); }
.tabs button { min-height: 44px; padding: 10px 0; border: 0; border-bottom: 2px solid transparent; background: transparent; color: var(--fb-muted); font-weight: 600; }
.tabs button[aria-pressed=true] { color: var(--action-ink); border-bottom-color: var(--action-ink); }
.tabs button:hover { color: var(--fb-fg); }
.messages { flex: 1; min-height: 0; overflow: auto; padding: 20px; display: flex; flex-direction: column; gap: 16px; overscroll-behavior: contain; }
.empty { margin: auto; max-width: 29ch; text-align: center; color: var(--fb-muted); }
.empty strong { display: block; color: var(--fb-fg); font-size: 16px; margin-bottom: 8px; }
.empty p { margin: 0; }
.bubble { max-width: 90%; align-self: flex-start; }
.bubble.own { align-self: flex-end; }
.bubble b, .bubble time { display: block; font-size: 11px; font-weight: 500; color: var(--fb-muted); }
.bubble p { padding: 10px 12px; margin: 5px 0; background: color-mix(in srgb, var(--fb-fg) 6%, var(--fb-bg)); border-radius: 8px; white-space: pre-wrap; overflow-wrap: anywhere; }
.bubble.own p { background: color-mix(in srgb, var(--fb-accent) 15%, var(--fb-bg)); }
.compose { flex: none; padding: 12px 20px; border-top: 1px solid var(--fb-line); }
.compose label { display: block; font-size: 12px; margin-bottom: 5px; }
.compose textarea { display: block; width: 100%; min-height: 64px; max-height: 120px; resize: vertical; border: 1px solid var(--fb-line); border-radius: 6px; padding: 8px 10px; background: var(--fb-bg); color: var(--fb-fg); }
.compose footer { display: flex; justify-content: space-between; align-items: center; gap: 8px; margin-top: 9px; }
.compose small { color: var(--fb-muted); font-size: 11px; }
.send { min-height: 42px; padding: 9px 13px; border: 0; border-radius: 6px; background: var(--fb-accent); color: var(--fb-on-accent); }
.link { border: 0; background: transparent; color: var(--action-ink); font-size: 12px; text-decoration: underline; text-underline-offset: 3px; padding: 10px; }
.reset { flex: none; color: var(--fb-muted); padding: 12px 20px; }
.notice { flex: none; font-size: 12px; color: var(--fb-muted); padding: 8px 20px; margin: 0; }
.feedback { flex: none; overflow: visible; }
.feedback-state { padding: 20px; color: var(--fb-muted); }
.tools { display: flex; flex: none; justify-content: space-between; flex-wrap: wrap; padding: 0 10px; }
.preview { position: absolute; right: 0; bottom: 58px; width: 310px; background: var(--fb-bg); color: var(--fb-fg); box-shadow: 0 6px 28px #0003; border-radius: 12px; padding: 16px; }
.preview .dismiss { position: absolute; right: 3px; top: 3px; }
.preview-open { text-align: left; border: 0; background: transparent; color: inherit; padding: 0; width: 100%; }
.preview-open strong { display: block; padding-right: 28px; }
.preview-open p { display: -webkit-box; -webkit-line-clamp: 3; -webkit-box-orient: vertical; overflow: hidden; overflow-wrap: anywhere; margin: 8px 0; }
.preview-open small { color: var(--action-ink); }
@media (max-width: 600px) {
  :host { right: 12px; bottom: calc(12px + env(safe-area-inset-bottom) + var(--fidibeki-chat-bottom-offset, 64px)); }
  :host([data-side=left]) { left: 12px; right: auto; }
  .launcher { width: 52px; height: 52px; min-height: 52px; padding: 0; justify-content: center; border-radius: 50%; }
  .launcher svg { width: 24px; height: 24px; }
  .launcher-label { display: none; }
  .badge { position: absolute; top: -4px; right: -4px; box-shadow: 0 2px 8px #0002; }
  :host([data-open=true]) .launcher { display: none; }
  .panel, :host([data-side=left]) .panel { position: fixed; left: 8px; right: 8px; width: auto; bottom: calc(var(--keyboard-inset, 0px) + max(8px, env(safe-area-inset-bottom))); height: min(620px, calc(var(--viewport-height, 100dvh) - 16px - env(safe-area-inset-top) - env(safe-area-inset-bottom))); }
  :host([data-view=feedback]) .panel { bottom: calc(var(--keyboard-inset, 0px) + max(8px, env(safe-area-inset-bottom))); height: auto; max-height: calc(var(--viewport-height, 100dvh) - 16px - env(safe-area-inset-top) - env(safe-area-inset-bottom)); }
  .preview { max-width: calc(100vw - 24px); bottom: 64px; }
  .compose textarea { font-size: 16px; max-height: 80px; }
  .compose small { max-width: 16ch; }
  :host([data-compact=true]) .head { padding-top: 4px; padding-bottom: 0; }
  :host([data-compact=true]) .tabs button { min-height: 40px; padding: 6px 0; }
  :host([data-compact=true]) .compose { padding-top: 8px; padding-bottom: 4px; }
  :host([data-compact=true]) .compose textarea { min-height: 48px; }
  :host([data-compact=true]) .reset { padding-top: 6px; padding-bottom: 6px; }
}
`
