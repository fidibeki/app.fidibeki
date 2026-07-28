import {
  FidibekiFeedbackLauncher,
  FidibekiProvider,
  useFidibekiFeedback,
  type ScreenshotAsset,
  type SessionProvider,
} from '@fidibeki/mobile'
import { createAsyncStorageAdapter } from '@fidibeki/mobile/async-storage'
import { StatusBar } from 'expo-status-bar'
import { Platform, Pressable, StyleSheet, Text, View } from 'react-native'
import { SafeAreaProvider, SafeAreaView } from 'react-native-safe-area-context'

const localHost = Platform.OS === 'android' ? '10.0.2.2' : '127.0.0.1'
const widgetBaseUrl = process.env.EXPO_PUBLIC_FIDIBEKI_WIDGET_BASE_URL ?? `http://${localHost}:43136`
const sessionURL = process.env.EXPO_PUBLIC_FIDIBEKI_SESSION_URL ?? `http://${localHost}:43136/session`
const storage = createAsyncStorageAdapter()

const sessionProvider: SessionProvider = async (request) => {
  // This endpoint belongs to the host application and authenticates its own signed-in user.
  // It keeps the least-privilege Fidibeki API key on the host backend.
  const response = await fetch(sessionURL, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({
      ...request,
      application_id: request.platform === 'ios' ? 'com.fidibeki.mobileexample' : 'com.fidibeki.mobileexample',
      widget_definition_key: 'mobile-example',
    }),
  })
  if (!response.ok) throw new Error(`Session endpoint returned ${response.status}`)
  return response.json()
}

async function screenshotProvider(): Promise<ScreenshotAsset> {
  const response = await fetch(`${widgetBaseUrl}/fixture.png`)
  if (!response.ok) throw new Error(`Screenshot fixture returned ${response.status}`)
  const blob = await response.blob()
  return {
    uri: 'memory://host-owned-example-screenshot',
    contentType: 'image/png',
    size: blob.size,
    read: async () => blob,
  }
}

function ProductScreen() {
  const feedback = useFidibekiFeedback()
  return (
    <SafeAreaView style={styles.safeArea}>
      <StatusBar style="dark" />
      <View style={styles.header}>
        <Text style={styles.brand}>Fidibeki Mobile Example</Text>
        <Text accessibilityLiveRegion="polite" style={styles.status}>
          {feedback.status}
        </Text>
      </View>
      <View style={styles.content}>
        <Text style={styles.title}>Checkout review</Text>
        <Text style={styles.body}>
          This native screen uses a host-minted capture session and explicit screenshot provider.
        </Text>
        <Pressable accessibilityRole="button" onPress={() => void feedback.open()} style={styles.productButton}>
          <Text style={styles.productButtonText}>Send product feedback</Text>
        </Pressable>
        <FidibekiFeedbackLauncher />
      </View>
    </SafeAreaView>
  )
}

export default function App() {
  return (
    <SafeAreaProvider>
      <FidibekiProvider
        apiBaseUrl={widgetBaseUrl}
        appVersion="0.1.0"
        deviceLocale="en-PT"
        locale="en"
        osVersion={String(Platform.Version)}
        screen="/checkout/review"
        screenshotProvider={screenshotProvider}
        sessionProvider={sessionProvider}
        storage={storage}
        theme={{ accentColor: '#087f6b' }}
      >
        <ProductScreen />
      </FidibekiProvider>
    </SafeAreaProvider>
  )
}

const styles = StyleSheet.create({
  safeArea: { flex: 1, backgroundColor: '#ffffff' },
  header: {
    minHeight: 60,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: '#ccd5d2',
    paddingHorizontal: 20,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 16,
  },
  brand: { flex: 1, color: '#17211f', fontSize: 17, fontWeight: '700' },
  status: { color: '#53635f', fontSize: 13 },
  content: { flex: 1, padding: 24, justifyContent: 'center', gap: 18 },
  title: { color: '#17211f', fontSize: 28, fontWeight: '700' },
  body: { color: '#53635f', fontSize: 16, lineHeight: 24 },
  productButton: {
    minHeight: 48,
    borderRadius: 6,
    backgroundColor: '#17211f',
    paddingHorizontal: 16,
    alignItems: 'center',
    justifyContent: 'center',
  },
  productButtonText: { color: '#ffffff', fontSize: 15, fontWeight: '700' },
})
