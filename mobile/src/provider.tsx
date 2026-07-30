import { AppState, Platform, type AppStateStatus } from 'react-native'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { MobileClient } from './client'
import { FeedbackContext } from './context'
import { useFeedbackContext } from './context'
import { createConsentInput } from './form-state'
import { systemMessages } from './localization'
import { normalizeScreen, RecurrenceEngine } from './recurrence'
import { validateCaptureSession } from './session'
import { FeedbackSheet } from './sheet'
import {
  MobileClientError,
  type FeedbackFormState,
  type FeedbackDraftPolicy,
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

type SessionAcquisitionPurpose =
  | 'initial'
  | 'open'
  | 'configuration_changed'
  | 'session_invalidated'
  | 'cooldown_elapsed'

const MAX_TIMER_DELAY_MS = 2_147_483_647

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
  const sessionProviderRef = useRef(sessionProvider)
  const storageRef = useRef(storage)
  sessionProviderRef.current = sessionProvider
  storageRef.current = storage
  const storageProxy = useMemo(
    () => ({
      getItem: (key: string) => storageRef.current.getItem(key),
      setItem: (key: string, value: string) => storageRef.current.setItem(key, value),
      removeItem: (key: string) => storageRef.current.removeItem(key),
    }),
    [],
  )
  const recurrence = useMemo(
    () => new RecurrenceEngine(storageProxy, appSessionId ? { appSessionId } : {}),
    [appSessionId, storageProxy],
  )
  const [session, setSession] = useState<MobileCaptureSession | null>(null)
  const sessionRef = useRef<MobileCaptureSession | null>(null)
  const [status, setStatus] = useState<FeedbackStatus>('loading')
  const [error, setError] = useState<MobileClientError | null>(null)
  const [eligible, setEligible] = useState(false)
  const [visible, setVisible] = useState(false)
  const [draftPolicy, setDraftPolicy] = useState<FeedbackDraftPolicy>('reset')
  const [draftRevision, setDraftRevision] = useState(0)
  const [nextEligibleAt, setNextEligibleAt] = useState<number | null>(null)
  const feedbackID = useRef(createUUID())
  const mounted = useRef(false)
  const acquisitionGeneration = useRef(0)
  const activeSessionGeneration = useRef(0)
  const eligibleRef = useRef(false)
  const pendingDraftRefresh = useRef(false)

  useEffect(() => {
    sessionRef.current = session
  }, [session])

  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
      acquisitionGeneration.current += 1
    }
  }, [])

  const acquireSession = useCallback(
    async (purpose: SessionAcquisitionPurpose = 'initial') => {
      const generation = ++acquisitionGeneration.current
      const preservesDraft = purpose === 'configuration_changed'
      if (!preservesDraft) pendingDraftRefresh.current = false
      setStatus('loading')
      eligibleRef.current = false
      setEligible(false)
      if (!preservesDraft) setError(null)
      if (!preservesDraft) {
        sessionRef.current = null
        setSession(null)
        setVisible(false)
      }
      try {
        const nextSession = validateCaptureSession(
          await sessionProviderRef.current({
            platform: resolvedPlatform(),
            screen,
            display_locale: locale,
          }),
        )
        const recurrenceEligibility = preservesDraft
          ? { eligible: true, nextEligibleAt: null }
          : await recurrence.eligibility(
              nextSession.configuration.project_id,
              nextSession.configuration.definition,
              screen,
            )
        if (!mounted.current || generation !== acquisitionGeneration.current) return null
        feedbackID.current = createUUID()
        activeSessionGeneration.current = generation
        pendingDraftRefresh.current = false
        setSession(nextSession)
        sessionRef.current = nextSession
        setDraftPolicy(preservesDraft ? 'reconcile' : 'reset')
        setDraftRevision((current) => current + 1)
        eligibleRef.current = recurrenceEligibility.eligible
        setEligible(recurrenceEligibility.eligible)
        setNextEligibleAt(recurrenceEligibility.nextEligibleAt)
        const shouldOpen = purpose === 'configuration_changed' || purpose === 'session_invalidated'
        setStatus(recurrenceEligibility.eligible ? (shouldOpen ? 'open' : 'ready') : 'ineligible')
        setVisible(shouldOpen && recurrenceEligibility.eligible)
        return {
          session: nextSession,
          eligible: recurrenceEligibility.eligible,
        }
      } catch (caught) {
        const nextError =
          caught instanceof MobileClientError
            ? caught
            : new MobileClientError('network_error', 'Capture session request failed', {
                retryable: true,
                cause: caught,
              })
        if (mounted.current && generation === acquisitionGeneration.current) {
          setError(nextError)
          setStatus('error')
          eligibleRef.current = false
          setEligible(false)
          setNextEligibleAt(null)
          setVisible(false)
        }
        return null
      }
    },
    [locale, recurrence, screen],
  )

  useEffect(() => {
    void acquireSession()
  }, [acquireSession])

  useEffect(() => {
    if (eligible || nextEligibleAt === null) return
    let timer: ReturnType<typeof setTimeout> | undefined
    let cancelled = false
    const schedule = () => {
      if (cancelled) return
      const remaining = nextEligibleAt - Date.now()
      if (remaining <= 0) {
        void acquireSession('cooldown_elapsed')
        return
      }
      timer = setTimeout(schedule, Math.min(remaining, MAX_TIMER_DELAY_MS))
    }
    schedule()
    return () => {
      cancelled = true
      if (timer !== undefined) clearTimeout(timer)
    }
  }, [acquireSession, eligible, nextEligibleAt])

  useEffect(() => {
    const onAppStateChange = (nextState: AppStateStatus) => {
      if (nextState === 'background') {
        recurrence.onBackground()
      } else if (nextState === 'active') {
        const sessionReset = recurrence.onForeground()
        if (sessionReset || (nextEligibleAt !== null && Date.now() >= nextEligibleAt)) {
          void acquireSession(sessionReset ? 'initial' : 'cooldown_elapsed')
        }
      }
    }
    const subscription = AppState.addEventListener('change', onAppStateChange)
    return () => subscription.remove()
  }, [acquireSession, nextEligibleAt, recurrence])

  const open = useCallback(async () => {
    if (visible) return
    if (sessionRef.current && eligibleRef.current) {
      setVisible(true)
      setStatus('open')
      return
    }
    const purpose = pendingDraftRefresh.current ? 'configuration_changed' : 'open'
    const acquired = await acquireSession(purpose)
    if (acquired?.eligible) {
      setVisible(true)
      setStatus('open')
    }
  }, [acquireSession, visible])

  const dismiss = useCallback(() => {
    if (!sessionRef.current || !visible || status === 'submitting') return
    setVisible(false)
    if (status === 'open') setStatus('ready')
  }, [status, visible])

  const submit = useCallback(
    async (state: FeedbackFormState, screenshot?: PreparedScreenshot): Promise<MobileSubmissionResult> => {
      const current = sessionRef.current
      const generation = acquisitionGeneration.current
      if (!current || !eligibleRef.current || activeSessionGeneration.current !== generation) {
        throw new MobileClientError('access_denied', 'No capture session is available')
      }
      setStatus('submitting')
      setError(null)
      const comment = state.comment.trim()
      const contactEnabled =
        current.configuration.definition.collect_contact && current.configuration.contact_notice !== undefined
      const reporterEmail = contactEnabled ? state.reporterEmail.trim() : ''
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
            const recurrenceEligibility = await recurrence.eligibility(
              current.configuration.project_id,
              current.configuration.definition,
              screen,
            )
            if (mounted.current && sessionRef.current === current) {
              setStatus('submitted')
              eligibleRef.current = false
              setEligible(false)
              setNextEligibleAt(recurrenceEligibility.nextEligibleAt)
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
          pendingDraftRefresh.current = true
          setError(nextError)
          await acquireSession('configuration_changed')
        } else if (nextError.code === 'session_expired' || nextError.code === 'access_denied') {
          setError(nextError)
          await acquireSession('session_invalidated')
        } else {
          setError(nextError)
          setStatus('open')
        }
        throw nextError
      }
    },
    [acquireSession, appVersion, client, deviceLocale, osVersion, recurrence, screen],
  )

  const messages = useMemo(
    () => systemMessages(session?.configuration.display_locale ?? locale),
    [locale, session?.configuration.display_locale],
  )
  const contextValue = useMemo(
    () => ({
      session,
      visible,
      status,
      error,
      eligible,
      draftPolicy,
      draftRevision,
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
      draftPolicy,
      draftRevision,
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

  return (
    <FeedbackContext.Provider value={contextValue}>
      {children}
      <FeedbackSheet />
    </FeedbackContext.Provider>
  )
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
