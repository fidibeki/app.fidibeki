import {
  AppState,
  Platform,
  type AppStateStatus,
} from 'react-native'
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
import { MobileClient } from './client'
import { FeedbackContext } from './context'
import { useFeedbackContext } from './context'
import { createConsentInput } from './form-state'
import { systemMessages } from './localization'
import { normalizeScreen, RecurrenceEngine } from './recurrence'
import { validateCaptureSession } from './session'
import {
  MobileClientError,
  type FeedbackFormState,
  type FeedbackStatus,
  type FidibekiProviderProps,
  type MobileCaptureSession,
  type MobileFeedbackRequest,
  type MobilePlatform,
  type MobileSubmissionResult,
  type PreparedScreenshot,
  type UseFidibekiFeedbackResult,
} from './types'
import { createUUID } from './uuid'

function resolvedPlatform(): MobilePlatform {
  if (Platform.OS !== 'ios' && Platform.OS !== 'android') {
    throw new Error('@fidibeki/mobile supports only iOS and Android')
  }
  return Platform.OS
}

export function FidibekiProvider({
  apiBaseUrl,
  sessionProvider,
  storage,
  screen: rawScreen,
  appVersion,
  osVersion,
  locale = 'en',
  deviceLocale,
  appSessionId,
  screenshotProvider,
  theme = {},
  children,
}: FidibekiProviderProps) {
  const screen = useMemo(() => normalizeScreen(rawScreen), [rawScreen])
  const client = useMemo(() => new MobileClient({ apiBaseUrl }), [apiBaseUrl])
  const recurrence = useMemo(
    () => new RecurrenceEngine(storage, appSessionId ? { appSessionId } : {}),
    [appSessionId, storage],
  )
  const [session, setSession] = useState<MobileCaptureSession | null>(null)
  const sessionRef = useRef<MobileCaptureSession | null>(null)
  const [status, setStatus] = useState<FeedbackStatus>('loading')
  const [error, setError] = useState<MobileClientError | null>(null)
  const [eligible, setEligible] = useState(false)
  const [visible, setVisible] = useState(false)
  const feedbackID = useRef(createUUID())
  const mounted = useRef(true)

  useEffect(() => {
    sessionRef.current = session
  }, [session])

  const acquireSession = useCallback(
    async (purpose: 'initial' | 'open' | 'configuration_changed' = 'initial') => {
      setStatus('loading')
      if (purpose !== 'configuration_changed') setError(null)
      try {
        const nextSession = validateCaptureSession(
          await sessionProvider({
            platform: resolvedPlatform(),
            screen,
            display_locale: locale,
          }),
        )
        const nextEligible =
          purpose === 'configuration_changed'
            ? true
            : await recurrence.isEligible(
                nextSession.configuration.project_id,
                nextSession.configuration.definition,
                screen,
              )
        if (!mounted.current) return null
        feedbackID.current = createUUID()
        setSession(nextSession)
        sessionRef.current = nextSession
        setEligible(nextEligible)
        setStatus(nextEligible ? (purpose === 'configuration_changed' ? 'open' : 'ready') : 'ineligible')
        if (purpose === 'configuration_changed') setVisible(true)
        return { session: nextSession, eligible: nextEligible }
      } catch (caught) {
        const nextError =
          caught instanceof MobileClientError
            ? caught
            : new MobileClientError('network_error', 'Capture session request failed', {
                retryable: true,
                cause: caught,
              })
        if (mounted.current) {
          setError(nextError)
          setStatus('error')
          setEligible(false)
        }
        return null
      }
    },
    [locale, recurrence, screen, sessionProvider],
  )

  useEffect(() => {
    mounted.current = true
    void acquireSession()
    return () => {
      mounted.current = false
    }
  }, [acquireSession])

  useEffect(() => {
    const onAppStateChange = (nextState: AppStateStatus) => {
      if (nextState === 'background') {
        recurrence.onBackground()
      } else if (nextState === 'active' && recurrence.onForeground()) {
        void acquireSession()
      }
    }
    const subscription = AppState.addEventListener('change', onAppStateChange)
    return () => subscription.remove()
  }, [acquireSession, recurrence])

  const open = useCallback(async () => {
    if (visible) return
    if (sessionRef.current && eligible) {
      setVisible(true)
      setStatus('open')
      return
    }
    const acquired = await acquireSession('open')
    if (acquired?.eligible) {
      setVisible(true)
      setStatus('open')
    }
  }, [acquireSession, eligible, visible])

  const dismiss = useCallback(() => {
    const current = sessionRef.current
    setVisible(false)
    if (!current || !visible || status === 'submitted') return
    setEligible(false)
    setStatus('ineligible')
    void recurrence.record(
      current.configuration.project_id,
      current.configuration.definition,
      screen,
      'dismissed',
    )
  }, [recurrence, screen, status, visible])

  const submit = useCallback(
    async (
      state: FeedbackFormState,
      screenshot?: PreparedScreenshot,
    ): Promise<MobileSubmissionResult> => {
      const current = sessionRef.current
      if (!current) {
        throw new MobileClientError('access_denied', 'No capture session is available')
      }
      setStatus('submitting')
      setError(null)
      const comment = state.comment.trim()
      const reporterEmail = state.reporterEmail.trim()
      const consent = createConsentInput(current, state)
      const request: MobileFeedbackRequest = {
        client_feedback_id: feedbackID.current,
        responses: state.responses,
        ...(comment ? { comment } : {}),
        ...(reporterEmail ? { reporter_email: reporterEmail } : {}),
        ...(reporterEmail ? { notify_reporter: true as const } : {}),
        ...(consent ? { consent } : {}),
        ...(screenshot
          ? {
              screenshot: {
                client_attachment_id: screenshot.clientAttachmentId,
                content_type: screenshot.contentType,
                size_bytes: screenshot.size,
              },
            }
          : {}),
        app_version: appVersion,
        os_version: osVersion,
        ...(deviceLocale ? { device_locale: deviceLocale } : {}),
      }
      try {
        const result = await client.submit(current, request, {
          ...(screenshot ? { screenshot } : {}),
          onAccepted: async () => {
            await recurrence.record(
              current.configuration.project_id,
              current.configuration.definition,
              screen,
              'submitted',
            )
            if (mounted.current) {
              setStatus('submitted')
              setEligible(false)
            }
          },
        })
        return result
      } catch (caught) {
        const nextError =
          caught instanceof MobileClientError
            ? caught
            : new MobileClientError('network_error', 'Feedback request failed', {
                retryable: true,
                cause: caught,
              })
        if (nextError.code === 'configuration_changed') {
          setError(nextError)
          await acquireSession('configuration_changed')
        } else {
          setError(nextError)
          setStatus('open')
        }
        throw nextError
      }
    },
    [
      acquireSession,
      appVersion,
      client,
      deviceLocale,
      osVersion,
      recurrence,
      screen,
    ],
  )

  const messages = useMemo(() => systemMessages(session?.configuration.display_locale ?? locale), [
    locale,
    session?.configuration.display_locale,
  ])
  const contextValue = useMemo(
    () => ({
      session,
      visible,
      status,
      error,
      eligible,
      screen,
      theme,
      messages,
      ...(screenshotProvider ? { screenshotProvider } : {}),
      open,
      dismiss,
      submit,
      clearError: () => setError(null),
    }),
    [
      dismiss,
      eligible,
      error,
      messages,
      open,
      screen,
      screenshotProvider,
      session,
      status,
      submit,
      theme,
      visible,
    ],
  )

  return <FeedbackContext.Provider value={contextValue}>{children}</FeedbackContext.Provider>
}

export function useFidibekiFeedback(): UseFidibekiFeedbackResult {
  const context = useFeedbackContext()
  return useMemo(
    () => ({
      open: context.open,
      close: context.dismiss,
      status: context.status,
      error: context.error,
      eligible: context.eligible,
    }),
    [context.dismiss, context.eligible, context.error, context.open, context.status],
  )
}
