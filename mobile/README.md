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
import {
  FidibekiFeedbackLauncher,
  FidibekiProvider,
} from '@fidibeki/mobile'
import { createAsyncStorageAdapter } from '@fidibeki/mobile/async-storage'
import { SafeAreaProvider } from 'react-native-safe-area-context'

const storage = createAsyncStorageAdapter()
const sessionProvider = ({ platform, screen, display_locale }) =>
  hostAPI.createFeedbackSession({ platform, screen, display_locale })

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
    <FidibekiFeedbackLauncher />
  </FidibekiProvider>
</SafeAreaProvider>
```

`apiBaseUrl` is the public Fidibeki widget edge that serves the mobile submit and attachment
completion routes. It is separate from the host application's authenticated session endpoint.

The SDK does not use a WebView, request camera or photo-library permissions, collect analytics,
derive a device identifier, or invent a context URL. A screenshot can be added only through an
explicit user action and a host-owned `screenshotProvider`.

The launcher applies the configured position, colors, theme, and native safe-area insets. It does
not fetch `appearance.logo_url`, avoiding an implicit remote image request from the host app.
