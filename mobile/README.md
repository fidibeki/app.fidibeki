# `@fidibeki/mobile`

Native iOS and Android feedback capture for Fidibeki.

Install the SDK with its native safe-area peer:

```sh
pnpm add @fidibeki/mobile react-native-safe-area-context
```

The host backend mints each short-lived capture session. Never place a tenant API key in a
JavaScript bundle, iOS application, Android application, mobile configuration file, or
`sessionProvider` response.

```tsx
import { FidibekiProvider, useFidibekiFeedback } from '@fidibeki/mobile'
import { createAsyncStorageAdapter } from '@fidibeki/mobile/async-storage'
import { Pressable, Text } from 'react-native'
import { SafeAreaProvider } from 'react-native-safe-area-context'

const storage = createAsyncStorageAdapter()
const sessionProvider = ({ platform, screen, display_locale }) =>
  hostAPI.createFeedbackSession({ platform, screen, display_locale })

function AppContent() {
  const feedback = useFidibekiFeedback()

  return (
    <>
      <YourApp />
      <Pressable
        accessibilityLabel="Give feedback"
        disabled={!feedback.eligible}
        onPress={() => void feedback.open()}
      >
        <Text>💬</Text>
      </Pressable>
    </>
  )
}

<SafeAreaProvider>
  <FidibekiProvider
    apiBaseUrl="https://widget.fidibeki.com"
    appVersion="1.2.3"
    locale="en"
    osVersion={String(Platform.Version)}
    screen="/checkout/review"
    sessionProvider={sessionProvider}
    storage={storage}
  >
    <AppContent />
  </FidibekiProvider>
</SafeAreaProvider>
```

`apiBaseUrl` is the public Fidibeki widget edge that serves the mobile submit and attachment
completion routes. It is separate from the host application's authenticated session endpoint.

The SDK does not use a WebView, request camera or photo-library permissions, collect analytics,
derive a device identifier, or invent a context URL. A screenshot can be added only through an
explicit user action and a host-owned `screenshotProvider`.

The SDK never injects a launcher. The host application owns the trigger's placement, appearance,
copy, and interaction pattern, and calls `useFidibekiFeedback().open()` to present the native
sheet. The provider applies the configured sheet colors, theme, and native safe-area insets. It
does not fetch `appearance.logo_url`, avoiding an implicit remote image request from the host app.

The sheet opens at 75% of the safe viewport, expands around the keyboard, and keeps its header and
submit action fixed while the form scrolls. Rating blocks render as cumulative stars but continue
to submit numeric `1..5` values. Optional comment and contact sections use explicit progressive
disclosures, and a user-selected screenshot remains a compact attachment action.

Closing without submitting preserves the in-memory draft and keeps the host trigger eligible, so
the same sheet can reopen immediately without minting a new capture session. Recurrence is written
only after Fidibeki accepts the feedback; the configured session, cooldown, or never policy then
controls subsequent eligibility.

Web presentation metadata such as `presentation` and `launcher_variant` does not control mobile
rendering. The host-selected active definition supplies the response blocks, localization,
recurrence, privacy, and sheet appearance.
