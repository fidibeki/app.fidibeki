import { Pressable, StyleSheet, Text, useColorScheme, View } from 'react-native'
import { useSafeAreaInsets } from 'react-native-safe-area-context'
import { useMemo } from 'react'
import { useFeedbackContext } from './context'
import { FeedbackSheet } from './sheet'
import type { FidibekiFeedbackLauncherProps } from './types'

export function FidibekiFeedbackLauncher({
  accessibilityLabel,
  testID = 'fidibeki-launcher',
}: FidibekiFeedbackLauncherProps) {
  const context = useFeedbackContext()
  const definition = context.session?.configuration.definition
  const localization = definition?.localization
  const appearance = definition?.appearance
  const systemMode = useColorScheme()
  const insets = useSafeAreaInsets()
  const colors = useMemo(() => {
    const configuredMode = appearance?.theme === 'light' || appearance?.theme === 'dark' ? appearance.theme : systemMode
    const dark = (context.theme.mode ?? configuredMode) === 'dark'
    return {
      accent: context.theme.accentColor ?? appearance?.accent_color ?? (dark ? '#50d0ba' : '#087f6b'),
      onAccent: appearance?.accent_foreground ?? (dark ? '#071411' : '#ffffff'),
    }
  }, [appearance?.accent_color, appearance?.accent_foreground, appearance?.theme, context.theme, systemMode])
  const isEdge = definition?.launcher_variant === 'edge_tab'
  const isLeft = appearance?.position === 'bottom-left'

  return (
    <View
      collapsable={false}
      pointerEvents="box-none"
      style={[styles.layer, { bottom: Math.max(16, insets.bottom + 8) }]}
    >
      {(context.eligible || context.status === 'error') && definition && localization ? (
        <Pressable
          accessibilityLabel={accessibilityLabel ?? localization.launcher_label}
          accessibilityRole="button"
          onPress={() => void context.open()}
          style={[
            styles.launcher,
            isEdge ? styles.edge : styles.floating,
            { alignSelf: isLeft ? 'flex-start' : 'flex-end' },
            isEdge ? (isLeft ? styles.edgeLeft : styles.edgeRight) : null,
            { backgroundColor: colors.accent },
          ]}
          testID={testID}
        >
          <Text numberOfLines={2} style={[styles.label, { color: colors.onAccent }]}>
            {localization.launcher_label}
          </Text>
        </Pressable>
      ) : null}
      <FeedbackSheet />
    </View>
  )
}

const styles = StyleSheet.create({
  layer: {
    position: 'absolute',
    left: 0,
    right: 0,
    zIndex: 1000,
  },
  launcher: {
    minHeight: 48,
    maxWidth: 240,
    paddingHorizontal: 16,
    paddingVertical: 12,
    alignItems: 'center',
    justifyContent: 'center',
  },
  floating: { borderRadius: 8 },
  edge: {
    minHeight: 44,
    borderRadius: 6,
    paddingHorizontal: 14,
  },
  edgeLeft: {
    borderTopLeftRadius: 0,
    borderBottomLeftRadius: 0,
  },
  edgeRight: {
    borderTopRightRadius: 0,
    borderBottomRightRadius: 0,
  },
  label: {
    fontSize: 15,
    fontWeight: '700',
    lineHeight: 20,
    textAlign: 'center',
  },
})
