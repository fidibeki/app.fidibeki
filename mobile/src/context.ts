import { createContext, useContext } from 'react'
import type {
  FeedbackFormState,
  FeedbackStatus,
  MobileCaptureSession,
  MobileClientError,
  MobileSubmissionResult,
  MobileTheme,
  PreparedScreenshot,
  ScreenshotProvider,
} from './types'
import type { SystemMessages } from './localization'

export interface FeedbackContextValue {
  session: MobileCaptureSession | null
  visible: boolean
  status: FeedbackStatus
  error: MobileClientError | null
  eligible: boolean
  screen: string
  theme: MobileTheme
  messages: SystemMessages
  screenshotProvider?: ScreenshotProvider
  open(): Promise<void>
  dismiss(): void
  submit(
    state: FeedbackFormState,
    screenshot?: PreparedScreenshot,
  ): Promise<MobileSubmissionResult>
  clearError(): void
}

export const FeedbackContext = createContext<FeedbackContextValue | null>(null)

export function useFeedbackContext(): FeedbackContextValue {
  const context = useContext(FeedbackContext)
  if (!context) {
    throw new Error('Fidibeki components must be rendered inside FidibekiProvider')
  }
  return context
}
