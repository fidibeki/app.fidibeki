# Replay capture and private content

Replay preserves ordinary page text, the native CSS cascade, responsive rules, fonts,
SVG geometry, and static image resources. Class and ID attributes remain available to
CSS selectors. Stylesheet changes during the session are recorded as well.

Privacy filtering runs before events enter the upload buffer:

- Form values, editable content, and private text regions are masked.
- Recognizable email addresses and phone or payment numbers are redacted in text.
- Arbitrary data attributes, navigation links, comments, and generated CSS text are removed.
- Signed resource URLs, avatar/private media paths, inline image data, and canvas pixels are excluded.
- Page URLs retain the existing rule-safe projection without query parameters or fragments.

Names, addresses, messages, and other application-specific personal content cannot be
reliably identified from arbitrary page text. Mark those elements before displaying the
content, or configure **Additional private text selector** in the project's replay rule:

```html
<nav>Projects · Feedback · Settings</nav>
<span data-fidibeki-mask>Jane Doe</span>
<section data-fidibeki-mask><!-- Private account details or messages --></section>
<div data-fidibeki-block><!-- Excluded content, including private images --></div>
```

`data-private`, `data-sensitive`, `.rr-mask`, and common identity elements such as
`.user-name` are also masked. Masking follows ancestor elements and shadow hosts.
The legacy `data-fidibeki-safe-text` marker never overrides a private region or automatic
contact redaction. An invalid custom selector masks all text until corrected.

Keep personal values out of CSS selectors and styling attributes. Mark private imagery
with `data-fidibeki-block`; static-looking resource names cannot prove an image is public.
Unreadable external stylesheets, signed assets, canvas content, and generated CSS text
may remain absent. This capture mode does not promise automatic anonymization of
unmarked personal content.

## Deployment and verification

Deploy the backend and workers before the widget bundle. Older servers reject the new
readable-text, native-style, and stylesheet-mutation events. Existing consent requirements,
sampling, retention, and input masking still apply. Stored recordings cannot recover text
or styling discarded by the previous recorder.

The widget's replay browser tests compare source and replay layouts on desktop and mobile,
exercise live text/CSS changes, and check that private values never enter uploads. They emit
`captured-replay.json` artifacts that can also be passed to the production Go verifier:

```sh
cd backend
FIDIBEKI_REPLAY_FIXTURE=/absolute/path/to/captured-replay.json \
  go test ./internal/replay -run TestReplayBrowserCapture -count=1
```
