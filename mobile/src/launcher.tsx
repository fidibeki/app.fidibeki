import { Pressable, StyleSheet, Text, View } from 'react-native'
import { useMemo } from 'react'
import { useFeedbackContext } from './context'
import { FeedbackSheet } from './sheet'
import type { FidibekiFeedbackLauncherProps } from './types'

export function FidibekiFeedbackLauncher({
  accessibilityLabel,
  testID = 'fidibeki-launcher',
}: FidibekiFeedbackLauncherProps) {
  const context = useFeedbackContext()
  const localization = context.session?.configuration.definition.localization
  const colors = useMemo(() => {
    const dark = context.theme.mode === 'dark'
    return {
      accent: context.theme.accentColor ?? (dark ? '#50d0ba' : '#087f6b'),
      onAccent: dark ? '#071411' : '#ffffff',
    }
  }, [context.theme])

  return (
    <View collapsable={false}>
      {context.eligible && localization ? (
        <Pressable
          accessibilityLabel={accessibilityLabel ?? localization.launcher_label}
          accessibilityRole="button"
          onPress={() => void context.open()}
          style={[styles.launcher, { backgroundColor: colors.accent }]}
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
  launcher: {
    minHeight: 48,
    maxWidth: 240,
    borderRadius: 6,
    paddingHorizontal: 16,
    paddingVertical: 12,
    alignItems: 'center',
    justifyContent: 'center',
  },
  label: {
    fontSize: 15,
    fontWeight: '700',
    lineHeight: 20,
    textAlign: 'center',
  },
})
