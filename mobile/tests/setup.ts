import React from 'react'
import { vi } from 'vitest'
;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

vi.mock('react-native', () => {
  let reducedMotion = false
  let colorScheme: 'light' | 'dark' | null = 'light'
  let appStateListener: ((state: string) => void) | undefined
  let reduceMotionListener: ((enabled: boolean) => void) | undefined
  const platform = { OS: 'ios' as 'ios' | 'android', Version: 'test' }
  const host = (name: string) =>
    React.forwardRef<unknown, Record<string, unknown>>(function Host(props, ref) {
      return React.createElement(name, { ...props, ref }, props.children as React.ReactNode)
    })
  return {
    AccessibilityInfo: {
      isReduceMotionEnabled: vi.fn(async () => reducedMotion),
      addEventListener: vi.fn((_event: string, listener: (enabled: boolean) => void) => {
        reduceMotionListener = listener
        return { remove: vi.fn() }
      }),
    },
    AppState: {
      addEventListener: vi.fn((_event: string, listener: (state: string) => void) => {
        appStateListener = listener
        return { remove: vi.fn() }
      }),
    },
    KeyboardAvoidingView: host('KeyboardAvoidingView'),
    Linking: { openURL: vi.fn(async () => undefined) },
    Modal: host('Modal'),
    Platform: platform,
    Pressable: host('Pressable'),
    SafeAreaView: host('SafeAreaView'),
    ScrollView: host('ScrollView'),
    StyleSheet: {
      create: <T>(styles: T) => styles,
      hairlineWidth: 1,
    },
    Switch: host('Switch'),
    Text: host('Text'),
    TextInput: host('TextInput'),
    useColorScheme: () => colorScheme,
    View: host('View'),
    __emitAppState: (state: string) => appStateListener?.(state),
    __setPlatform: (nextPlatform: 'ios' | 'android') => {
      platform.OS = nextPlatform
    },
    __setReducedMotion: (enabled: boolean) => {
      reducedMotion = enabled
      reduceMotionListener?.(enabled)
    },
    __setColorScheme: (nextColorScheme: 'light' | 'dark' | null) => {
      colorScheme = nextColorScheme
    },
  }
})

vi.mock('@react-native-async-storage/async-storage', () => {
  const values = new Map<string, string>()
  return {
    default: {
      getItem: vi.fn(async (key: string) => values.get(key) ?? null),
      setItem: vi.fn(async (key: string, value: string) => {
        values.set(key, value)
      }),
      removeItem: vi.fn(async (key: string) => {
        values.delete(key)
      }),
    },
  }
})

vi.mock('react-native-safe-area-context', () => {
  const host = (name: string) =>
    React.forwardRef<unknown, Record<string, unknown>>(function Host(props, ref) {
      return React.createElement(name, { ...props, ref }, props.children as React.ReactNode)
    })
  return {
    SafeAreaProvider: host('SafeAreaProvider'),
    SafeAreaView: host('SafeAreaView'),
    useSafeAreaInsets: () => ({ top: 0, right: 0, bottom: 34, left: 0 }),
  }
})
