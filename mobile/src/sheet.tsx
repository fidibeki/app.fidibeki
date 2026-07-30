import {
  AccessibilityInfo,
  Keyboard,
  KeyboardAvoidingView,
  Linking,
  Modal,
  Platform,
  Pressable,
  ScrollView,
  StyleSheet,
  Switch,
  Text,
  TextInput,
  useColorScheme,
  View,
} from 'react-native'
import { SafeAreaView, useSafeAreaInsets } from 'react-native-safe-area-context'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useFeedbackContext } from './context'
import { EMPTY_FORM_STATE, MAX_TEXT_LENGTH, reconcileResponses, validateForm } from './form-state'
import { prepareScreenshot } from './screenshot'
import type { FeedbackFormState, MobileAppearance, MobileResponseBlock, PreparedScreenshot } from './types'

interface Colors {
  accent: string
  background: string
  surface: string
  text: string
  muted: string
  border: string
  danger: string
  onAccent: string
}

function colorsFor(
  mode: 'light' | 'dark',
  theme: ReturnType<typeof useFeedbackContext>['theme'],
  appearance: MobileAppearance,
): Colors {
  const dark = mode === 'dark'
  return {
    accent: theme.accentColor ?? appearance.accent_color ?? (dark ? '#50d0ba' : '#087f6b'),
    background: theme.backgroundColor ?? appearance.surface_color ?? (dark ? '#141716' : '#ffffff'),
    surface: appearance.surface_color ?? (dark ? '#202523' : '#f5f7f6'),
    text: theme.textColor ?? appearance.text_color ?? (dark ? '#f4f7f6' : '#17211f'),
    muted: theme.mutedTextColor ?? (dark ? '#b6c0bd' : '#53635f'),
    border: theme.borderColor ?? (dark ? '#3a4441' : '#ccd5d2'),
    danger: theme.dangerColor ?? (dark ? '#ff8f87' : '#b42318'),
    onAccent: appearance.accent_foreground ?? (dark ? '#071411' : '#ffffff'),
  }
}

function emptyFormState(): FeedbackFormState {
  return { ...EMPTY_FORM_STATE, responses: {} }
}

function useReducedMotion(): boolean {
  const [reducedMotion, setReducedMotion] = useState(false)
  useEffect(() => {
    let active = true
    void AccessibilityInfo.isReduceMotionEnabled().then((enabled) => {
      if (active) setReducedMotion(enabled)
    })
    const subscription = AccessibilityInfo.addEventListener('reduceMotionChanged', setReducedMotion)
    return () => {
      active = false
      subscription.remove()
    }
  }, [])
  return reducedMotion
}

function useKeyboardVisible(sheetVisible: boolean): boolean {
  const [keyboardVisible, setKeyboardVisible] = useState(false)
  useEffect(() => {
    const showEvent = Platform.OS === 'ios' ? 'keyboardWillShow' : 'keyboardDidShow'
    const hideEvent = Platform.OS === 'ios' ? 'keyboardWillHide' : 'keyboardDidHide'
    const show = Keyboard.addListener(showEvent, () => setKeyboardVisible(true))
    const hide = Keyboard.addListener(hideEvent, () => setKeyboardVisible(false))
    return () => {
      show.remove()
      hide.remove()
    }
  }, [])
  useEffect(() => {
    if (!sheetVisible && keyboardVisible) setKeyboardVisible(false)
  }, [keyboardVisible, sheetVisible])
  return keyboardVisible
}

function FieldError({ message, colors }: { message: string | undefined; colors: Colors }) {
  if (!message) return null
  return (
    <Text accessibilityLiveRegion="polite" style={[styles.errorText, { color: colors.danger }]}>
      {message}
    </Text>
  )
}

function ResponseField({
  block,
  label,
  optionLabels,
  value,
  error,
  colors,
  onChange,
}: {
  block: MobileResponseBlock
  label: string
  optionLabels: Record<string, string>
  value: string | number | undefined
  error: string | undefined
  colors: Colors
  onChange(value: string | number): void
}) {
  const context = useFeedbackContext()
  const fieldLabel = (
    <Text style={[styles.label, { color: colors.text }]}>
      {label}
      {block.required ? <Text style={{ color: colors.danger }}> *</Text> : null}
    </Text>
  )
  if (block.type === 'free_text') {
    return (
      <View style={styles.field} testID={`fidibeki-field-${block.key}`}>
        {fieldLabel}
        <TextInput
          accessibilityHint={block.required ? context.messages.required : undefined}
          accessibilityLabel={label}
          multiline
          maxLength={MAX_TEXT_LENGTH}
          onChangeText={onChange}
          placeholder={label}
          placeholderTextColor={colors.muted}
          style={[
            styles.textArea,
            {
              borderColor: error ? colors.danger : colors.border,
              color: colors.text,
            },
          ]}
          value={typeof value === 'string' ? value : ''}
        />
        <FieldError message={error} colors={colors} />
      </View>
    )
  }
  if (block.type === 'rating') {
    const selectedRating = typeof value === 'number' ? value : 0
    return (
      <View
        accessibilityHint={block.required ? context.messages.required : undefined}
        accessibilityRole="radiogroup"
        accessibilityLabel={label}
        style={styles.field}
        testID={`fidibeki-field-${block.key}`}
      >
        {fieldLabel}
        <View style={styles.ratingRow}>
          {[1, 2, 3, 4, 5].map((rating) => {
            const selected = rating === selectedRating
            const filled = rating <= selectedRating
            return (
              <Pressable
                accessibilityLabel={context.messages.ratingValue(rating)}
                accessibilityRole="radio"
                accessibilityState={{ checked: selected }}
                key={rating}
                onPress={() => onChange(rating)}
                style={[
                  styles.starButton,
                  {
                    backgroundColor: selected ? colors.surface : 'transparent',
                    borderColor: selected ? colors.accent : 'transparent',
                  },
                ]}
                testID={`fidibeki-rating-${block.key}-${rating}`}
              >
                <Text
                  style={[styles.starIcon, { color: filled ? colors.accent : colors.muted }]}
                  testID={`fidibeki-star-icon-${block.key}-${rating}`}
                >
                  ★
                </Text>
              </Pressable>
            )
          })}
        </View>
        <FieldError message={error} colors={colors} />
      </View>
    )
  }
  const choices = (block.options ?? []).map((option) => ({
    value: option.value,
    label: optionLabels[option.value] ?? option.value,
    accessibilityLabel: optionLabels[option.value] ?? option.value,
  }))
  return (
    <View
      accessibilityHint={block.required ? context.messages.required : undefined}
      accessibilityRole="radiogroup"
      accessibilityLabel={label}
      style={styles.field}
      testID={`fidibeki-field-${block.key}`}
    >
      {fieldLabel}
      <View style={styles.choiceRow}>
        {choices.map((choice) => {
          const selected = choice.value === value
          return (
            <Pressable
              accessibilityLabel={choice.accessibilityLabel}
              accessibilityRole="radio"
              accessibilityState={{ checked: selected }}
              key={String(choice.value)}
              onPress={() => onChange(choice.value)}
              style={[
                styles.choiceChip,
                {
                  backgroundColor: selected ? colors.accent : colors.surface,
                  borderColor: selected ? colors.accent : colors.border,
                },
              ]}
              testID={`fidibeki-choice-${block.key}-${choice.value}`}
            >
              <Text style={[styles.choiceText, { color: selected ? colors.onAccent : colors.text }]}>
                {choice.label}
              </Text>
            </Pressable>
          )
        })}
      </View>
      <FieldError message={error} colors={colors} />
    </View>
  )
}

function validationMessage(
  key: string,
  reason: string | undefined,
  messages: ReturnType<typeof useFeedbackContext>['messages'],
): string | undefined {
  if (!reason) return undefined
  if (key === 'reporter_email') return messages.invalidEmail
  if (key === 'consent') return messages.consentRequired
  return reason === 'required' ? messages.requiredResponse : messages.invalidResponse
}

function defaultBlockLabel(
  type: MobileResponseBlock['type'],
  messages: ReturnType<typeof useFeedbackContext>['messages'],
): string {
  if (type === 'rating') return messages.rating
  if (type === 'reaction') return messages.reaction
  return messages.feedback
}

function humanizeOption(value: string): string {
  return value.replace(/[_-]+/g, ' ').replace(/\b\w/g, (character) => character.toUpperCase())
}

export function FeedbackSheet() {
  const context = useFeedbackContext()
  const session = context.session
  const definition = session?.configuration.definition
  const contactNotice = session?.configuration.contact_notice
  const reducedMotion = useReducedMotion()
  const keyboardVisible = useKeyboardVisible(context.visible)
  const safeAreaInsets = useSafeAreaInsets()
  const appearance = definition?.appearance ?? {}
  const systemMode = useColorScheme()
  const configuredMode =
    appearance.theme === 'light' || appearance.theme === 'dark'
      ? appearance.theme
      : systemMode === 'dark'
        ? 'dark'
        : 'light'
  const mode = context.theme.mode ?? configuredMode
  const colors = useMemo(() => colorsFor(mode, context.theme, appearance), [appearance, context.theme, mode])
  const [form, setForm] = useState<FeedbackFormState>(emptyFormState)
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({})
  const [screenshot, setScreenshot] = useState<PreparedScreenshot | undefined>()
  const screenshotRef = useRef<PreparedScreenshot | undefined>(undefined)
  const [screenshotMessage, setScreenshotMessage] = useState<string | null>(null)
  const [attachmentFailed, setAttachmentFailed] = useState(false)
  const [readingScreenshot, setReadingScreenshot] = useState(false)
  const previousSession = useRef<typeof session>(null)
  const appliedDraftRevision = useRef(0)
  const screenshotGeneration = useRef(0)

  useEffect(() => {
    screenshotRef.current = screenshot
  }, [screenshot])

  useEffect(
    () => () => {
      screenshotGeneration.current += 1
      void screenshotRef.current?.dispose()
    },
    [],
  )

  useEffect(() => {
    if (appliedDraftRevision.current === context.draftRevision || !session || !definition) {
      return
    }
    appliedDraftRevision.current = context.draftRevision
    screenshotGeneration.current += 1
    setReadingScreenshot(false)
    const previous = previousSession.current
    const previousDefinition = previous?.configuration.definition
    const definitionChanged =
      previousDefinition && definition && previousDefinition.definition_version !== definition.definition_version
    const resetDraft = context.draftPolicy === 'reset' || !previousDefinition
    if (resetDraft) {
      setForm(emptyFormState())
    } else {
      setForm((current) => ({
        ...current,
        responses: definitionChanged
          ? reconcileResponses(previousDefinition.response_blocks, definition.response_blocks, current.responses)
          : current.responses,
        comment: definition.allow_comment ? current.comment : '',
        reporterEmail: definition.collect_contact && contactNotice ? current.reporterEmail : '',
        consentGranted: false,
      }))
    }
    setFieldErrors({})
    setScreenshotMessage(null)
    setAttachmentFailed(false)
    if (resetDraft || !definition.can_attach_screenshot) {
      const previousScreenshot = screenshotRef.current
      screenshotRef.current = undefined
      void previousScreenshot?.dispose()
      setScreenshot(undefined)
    }
    previousSession.current = session
  }, [contactNotice, context.draftPolicy, context.draftRevision, definition, session])

  const updateResponse = useCallback((key: string, value: string | number) => {
    setForm((current) => ({
      ...current,
      responses: { ...current.responses, [key]: value },
    }))
    setFieldErrors((current) => {
      const next = { ...current }
      delete next[`responses.${key}`]
      return next
    })
  }, [])

  const addScreenshot = useCallback(async () => {
    if (!definition?.can_attach_screenshot || !context.screenshotProvider) return
    const generation = ++screenshotGeneration.current
    setReadingScreenshot(true)
    setScreenshotMessage(null)
    try {
      const asset = await context.screenshotProvider()
      if (generation !== screenshotGeneration.current) {
        await asset?.dispose?.()
        return
      }
      if (!asset) {
        setScreenshotMessage(context.messages.screenshotCancelled)
        return
      }
      const prepared = await prepareScreenshot(asset)
      if (generation !== screenshotGeneration.current) {
        await prepared.dispose()
        return
      }
      const previousScreenshot = screenshotRef.current
      screenshotRef.current = prepared
      await previousScreenshot?.dispose()
      if (generation !== screenshotGeneration.current) {
        await prepared.dispose()
        return
      }
      setScreenshot(prepared)
      setScreenshotMessage(context.messages.screenshotAdded)
    } catch {
      if (generation === screenshotGeneration.current) {
        setScreenshotMessage(context.messages.screenshotProviderFailed)
      }
    } finally {
      if (generation === screenshotGeneration.current) setReadingScreenshot(false)
    }
  }, [context.messages, context.screenshotProvider, definition?.can_attach_screenshot])

  const removeScreenshot = useCallback(async () => {
    screenshotGeneration.current += 1
    const previousScreenshot = screenshotRef.current
    screenshotRef.current = undefined
    await previousScreenshot?.dispose()
    setScreenshot(undefined)
    setScreenshotMessage(null)
  }, [])

  const submit = useCallback(async () => {
    if (!definition) return
    const validation = validateForm(definition, form)
    if (!validation.valid) {
      setFieldErrors(validation.fields)
      return
    }
    setFieldErrors({})
    setAttachmentFailed(false)
    try {
      const result = await context.submit(form, screenshot)
      if (screenshot) {
        await screenshot.dispose()
        setScreenshot(undefined)
      }
      setAttachmentFailed(result.attachments.some((outcome) => outcome.status === 'failed'))
    } catch {
      // The provider exposes a typed error and retains the draft for an exact retry.
    }
  }, [context, definition, form, screenshot])

  if (!session || !definition) return null

  const customerCopy = definition.localization
  const submitting = context.status === 'submitting' || context.status === 'loading' || !context.eligible
  const submitted = context.status === 'submitted'
  const canAddScreenshot = definition.can_attach_screenshot && context.screenshotProvider !== undefined && !screenshot
  const discloseMobileAI =
    definition.ai_processing?.enabled === true &&
    definition.ai_processing.input_categories?.some(
      (category) => category === 'feedback_text' || category === 'screenshot',
    ) === true

  return (
    <Modal
      accessibilityViewIsModal
      animationType={reducedMotion ? 'none' : 'slide'}
      onRequestClose={context.dismiss}
      presentationStyle="overFullScreen"
      statusBarTranslucent
      supportedOrientations={['portrait', 'landscape']}
      testID="fidibeki-modal"
      transparent
      visible={context.visible}
    >
      <SafeAreaView edges={['top']} style={styles.modalRoot}>
        <Pressable
          accessibilityElementsHidden
          importantForAccessibility="no-hide-descendants"
          onPress={context.dismiss}
          style={[
            styles.scrim,
            {
              backgroundColor: mode === 'dark' ? 'rgba(0, 0, 0, 0.68)' : 'rgba(8, 18, 16, 0.38)',
            },
          ]}
          testID="fidibeki-sheet-scrim"
        />
        <KeyboardAvoidingView
          behavior={Platform.OS === 'ios' ? 'padding' : 'height'}
          keyboardVerticalOffset={0}
          pointerEvents="box-none"
          style={styles.keyboardLayout}
          testID={`fidibeki-keyboard-${Platform.OS}`}
        >
          <View
            style={[
              styles.sheet,
              {
                backgroundColor: colors.background,
                borderColor: colors.border,
                height: keyboardVisible ? '100%' : '75%',
              },
            ]}
            testID="fidibeki-sheet-frame"
          >
            <View
              style={[styles.grabber, { backgroundColor: colors.border }]}
              testID="fidibeki-sheet-grabber"
            />
            <View
              style={[styles.header, { borderBottomColor: colors.border }]}
              testID="fidibeki-sheet-header"
            >
              <Text accessibilityRole="header" style={[styles.title, { color: colors.text }]}>
                {customerCopy.prompt}
              </Text>
              <Pressable
                accessibilityLabel={context.messages.close}
                accessibilityRole="button"
                hitSlop={12}
                onPress={context.dismiss}
                style={styles.iconButton}
                testID="fidibeki-close"
              >
                <Text style={[styles.closeIcon, { color: colors.text }]}>×</Text>
              </Pressable>
            </View>
            <ScrollView
              contentContainerStyle={[styles.content, submitted ? styles.successContent : null]}
              keyboardDismissMode="interactive"
              keyboardShouldPersistTaps="handled"
              style={styles.body}
              testID="fidibeki-sheet-body"
            >
              {submitted ? (
                <View accessibilityLiveRegion="polite" style={styles.success}>
                  <Text style={[styles.successText, { color: colors.text }]}>{customerCopy.success_message}</Text>
                  {attachmentFailed ? (
                    <Text style={[styles.errorText, { color: colors.danger }]}>
                      {context.messages.attachmentFailed}
                    </Text>
                  ) : null}
                </View>
              ) : (
                <>
                  {definition.response_blocks.map((block) => (
                    <ResponseField
                      block={block}
                      colors={colors}
                      error={validationMessage(
                        `responses.${block.key}`,
                        fieldErrors[`responses.${block.key}`],
                        context.messages,
                      )}
                      key={block.key}
                      label={customerCopy.block_labels?.[block.key] ?? defaultBlockLabel(block.type, context.messages)}
                      onChange={(value) => updateResponse(block.key, value)}
                      optionLabels={Object.fromEntries(
                        (block.options ?? []).map((option) => [
                          option.value,
                          customerCopy.option_labels?.[block.key]?.[option.value] ?? humanizeOption(option.value),
                        ]),
                      )}
                      value={form.responses[block.key]}
                    />
                  ))}
                  {definition.allow_comment ? (
                    <View style={styles.field}>
                      <TextInput
                        accessibilityLabel={customerCopy.comment_placeholder}
                        maxLength={MAX_TEXT_LENGTH}
                        multiline
                        onChangeText={(comment) => setForm((current) => ({ ...current, comment }))}
                        placeholder={customerCopy.comment_placeholder}
                        placeholderTextColor={colors.muted}
                        style={[
                          styles.textArea,
                          {
                            borderColor: fieldErrors.comment ? colors.danger : colors.border,
                            color: colors.text,
                          },
                        ]}
                        value={form.comment}
                      />
                      <FieldError
                        colors={colors}
                        message={validationMessage('comment', fieldErrors.comment, context.messages)}
                      />
                    </View>
                  ) : null}
                  {definition.collect_contact && contactNotice ? (
                    <View style={styles.contactSection}>
                      <Text style={[styles.label, { color: colors.text }]}>{customerCopy.contact_label}</Text>
                      <TextInput
                        accessibilityLabel={customerCopy.contact_label}
                        autoCapitalize="none"
                        autoComplete="email"
                        inputMode="email"
                        onChangeText={(reporterEmail) => setForm((current) => ({ ...current, reporterEmail }))}
                        placeholder={context.messages.emailPlaceholder}
                        placeholderTextColor={colors.muted}
                        style={[
                          styles.input,
                          {
                            borderColor: fieldErrors.reporter_email ? colors.danger : colors.border,
                            color: colors.text,
                          },
                        ]}
                        value={form.reporterEmail}
                      />
                      <FieldError
                        colors={colors}
                        message={validationMessage('reporter_email', fieldErrors.reporter_email, context.messages)}
                      />
                      <View style={styles.consentRow}>
                        <Switch
                          accessibilityLabel={customerCopy.consent_label}
                          accessibilityRole="switch"
                          accessibilityState={{ checked: form.consentGranted }}
                          onValueChange={(consentGranted) =>
                            setForm((current) => ({
                              ...current,
                              consentGranted,
                            }))
                          }
                          trackColor={{
                            false: colors.border,
                            true: colors.accent,
                          }}
                          value={form.consentGranted}
                        />
                        <Text style={[styles.consentText, { color: colors.text }]}>{customerCopy.consent_label}</Text>
                      </View>
                      <FieldError
                        colors={colors}
                        message={validationMessage('consent', fieldErrors.consent, context.messages)}
                      />
                      <Text style={[styles.notice, { color: colors.muted }]}>
                        {contactNotice.purpose_summary} {contactNotice.lawful_basis_summary}{' '}
                        {contactNotice.retention_summary}
                      </Text>
                      <Pressable
                        accessibilityRole="link"
                        onPress={() => void Linking.openURL(contactNotice.privacy_policy_url)}
                      >
                        <Text style={[styles.link, { color: colors.accent }]}>{context.messages.privacyPolicy}</Text>
                      </Pressable>
                    </View>
                  ) : null}
                  {canAddScreenshot ? (
                    <Pressable
                      accessibilityLabel={context.messages.addScreenshot}
                      accessibilityRole="button"
                      disabled={readingScreenshot}
                      onPress={() => void addScreenshot()}
                      style={[styles.secondaryButton, { borderColor: colors.border }]}
                    >
                      <Text style={{ color: colors.text }}>{context.messages.addScreenshot}</Text>
                    </Pressable>
                  ) : null}
                  {screenshot ? (
                    <View style={styles.screenshotRow}>
                      <Text style={{ color: colors.text }}>{context.messages.screenshotAdded}</Text>
                      <Pressable
                        accessibilityLabel={context.messages.removeScreenshot}
                        accessibilityRole="button"
                        onPress={() => void removeScreenshot()}
                      >
                        <Text style={[styles.link, { color: colors.accent }]}>{context.messages.removeScreenshot}</Text>
                      </Pressable>
                    </View>
                  ) : null}
                  {screenshotMessage ? (
                    <Text accessibilityLiveRegion="polite" style={{ color: colors.muted }}>
                      {screenshotMessage}
                    </Text>
                  ) : null}
                  {discloseMobileAI ? (
                    <View style={styles.aiDisclosure}>
                      <Text style={{ color: colors.muted }}>{context.messages.aiProcessingDisclosure}</Text>
                      <Pressable
                        accessibilityRole="link"
                        onPress={() => void Linking.openURL(definition.ai_processing!.privacy_policy_url!)}
                      >
                        <Text style={[styles.link, { color: colors.accent }]}>{context.messages.privacyPolicy}</Text>
                      </Pressable>
                    </View>
                  ) : null}
                  {context.error ? (
                    <Text accessibilityLiveRegion="assertive" style={[styles.errorText, { color: colors.danger }]}>
                      {context.error.code === 'configuration_changed'
                        ? context.messages.configurationChanged
                        : context.messages.submitFailed}
                    </Text>
                  ) : null}
                </>
              )}
            </ScrollView>
            {!submitted ? (
              <View
                style={[
                  styles.footer,
                  {
                    borderTopColor: colors.border,
                    paddingBottom: keyboardVisible ? 12 : Math.max(12, safeAreaInsets.bottom),
                  },
                ]}
                testID="fidibeki-sheet-footer"
              >
                <Pressable
                  accessibilityLabel={customerCopy.submit_label}
                  accessibilityRole="button"
                  accessibilityState={{ disabled: submitting }}
                  disabled={submitting}
                  onPress={() => void submit()}
                  style={[styles.primaryButton, { backgroundColor: colors.accent }]}
                  testID="fidibeki-submit"
                >
                  <Text style={[styles.primaryButtonText, { color: colors.onAccent }]}>
                    {customerCopy.submit_label}
                  </Text>
                </Pressable>
              </View>
            ) : null}
          </View>
        </KeyboardAvoidingView>
      </SafeAreaView>
    </Modal>
  )
}

const styles = StyleSheet.create({
  modalRoot: { flex: 1 },
  scrim: {
    position: 'absolute',
    top: 0,
    right: 0,
    bottom: 0,
    left: 0,
  },
  keyboardLayout: {
    flex: 1,
    justifyContent: 'flex-end',
  },
  sheet: {
    width: '100%',
    maxHeight: '100%',
    borderTopWidth: StyleSheet.hairlineWidth,
    borderLeftWidth: StyleSheet.hairlineWidth,
    borderRightWidth: StyleSheet.hairlineWidth,
    borderTopLeftRadius: 20,
    borderTopRightRadius: 20,
    overflow: 'hidden',
  },
  grabber: {
    alignSelf: 'center',
    width: 36,
    height: 4,
    borderRadius: 2,
    marginTop: 7,
    marginBottom: 3,
  },
  header: {
    minHeight: 52,
    borderBottomWidth: StyleSheet.hairlineWidth,
    paddingLeft: 16,
    paddingRight: 8,
    paddingVertical: 6,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 12,
  },
  title: { flex: 1, fontSize: 18, fontWeight: '700', lineHeight: 24 },
  iconButton: {
    width: 44,
    height: 44,
    alignItems: 'center',
    justifyContent: 'center',
  },
  closeIcon: { fontSize: 30, lineHeight: 32 },
  body: { flex: 1 },
  content: { paddingHorizontal: 16, paddingTop: 16, paddingBottom: 20, gap: 16 },
  successContent: { flexGrow: 1 },
  field: { gap: 8 },
  label: { fontSize: 15, fontWeight: '600', lineHeight: 20 },
  textArea: {
    minHeight: 88,
    borderWidth: 1,
    borderRadius: 6,
    paddingHorizontal: 12,
    paddingVertical: 10,
    textAlignVertical: 'top',
    fontSize: 16,
    lineHeight: 22,
  },
  input: {
    minHeight: 48,
    borderWidth: 1,
    borderRadius: 6,
    paddingHorizontal: 12,
    fontSize: 16,
  },
  ratingRow: {
    width: '100%',
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
  },
  starButton: {
    flex: 1,
    minWidth: 44,
    maxWidth: 56,
    height: 44,
    borderWidth: 1,
    borderRadius: 6,
    alignItems: 'center',
    justifyContent: 'center',
  },
  starIcon: {
    fontSize: 29,
    lineHeight: 34,
  },
  choiceRow: { flexDirection: 'row', flexWrap: 'wrap', gap: 8 },
  choiceChip: {
    minWidth: 44,
    minHeight: 44,
    borderWidth: 1,
    borderRadius: 6,
    paddingHorizontal: 14,
    alignItems: 'center',
    justifyContent: 'center',
  },
  choiceText: { fontSize: 15, lineHeight: 20 },
  contactSection: { gap: 10 },
  consentRow: { flexDirection: 'row', alignItems: 'center', gap: 10 },
  consentText: { flex: 1, fontSize: 14, lineHeight: 20 },
  notice: { fontSize: 13, lineHeight: 19 },
  link: { fontSize: 14, fontWeight: '600', lineHeight: 20 },
  secondaryButton: {
    minHeight: 48,
    borderWidth: 1,
    borderRadius: 6,
    paddingHorizontal: 16,
    alignItems: 'center',
    justifyContent: 'center',
  },
  screenshotRow: {
    minHeight: 44,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 12,
  },
  aiDisclosure: { gap: 8 },
  primaryButton: {
    minHeight: 50,
    borderRadius: 6,
    alignItems: 'center',
    justifyContent: 'center',
    paddingHorizontal: 18,
  },
  primaryButtonText: { fontSize: 16, fontWeight: '700' },
  footer: {
    borderTopWidth: StyleSheet.hairlineWidth,
    paddingTop: 12,
    paddingHorizontal: 16,
  },
  errorText: { fontSize: 13, lineHeight: 18 },
  success: {
    minHeight: 200,
    alignItems: 'center',
    justifyContent: 'center',
    gap: 12,
  },
  successText: {
    fontSize: 18,
    fontWeight: '600',
    lineHeight: 25,
    textAlign: 'center',
  },
})
