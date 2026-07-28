import {
  AccessibilityInfo,
  KeyboardAvoidingView,
  Linking,
  Modal,
  Platform,
  Pressable,
  SafeAreaView,
  ScrollView,
  StyleSheet,
  Switch,
  Text,
  TextInput,
  View,
} from 'react-native'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useFeedbackContext } from './context'
import {
  EMPTY_FORM_STATE,
  MAX_TEXT_LENGTH,
  reconcileResponses,
  validateForm,
} from './form-state'
import { prepareScreenshot } from './screenshot'
import type {
  FeedbackFormState,
  MobileResponseBlock,
  PreparedScreenshot,
} from './types'

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

function colorsFor(mode: 'light' | 'dark', theme: ReturnType<typeof useFeedbackContext>['theme']): Colors {
  const dark = mode === 'dark'
  return {
    accent: theme.accentColor ?? (dark ? '#50d0ba' : '#087f6b'),
    background: theme.backgroundColor ?? (dark ? '#141716' : '#ffffff'),
    surface: dark ? '#202523' : '#f5f7f6',
    text: theme.textColor ?? (dark ? '#f4f7f6' : '#17211f'),
    muted: theme.mutedTextColor ?? (dark ? '#b6c0bd' : '#53635f'),
    border: theme.borderColor ?? (dark ? '#3a4441' : '#ccd5d2'),
    danger: theme.dangerColor ?? (dark ? '#ff8f87' : '#b42318'),
    onAccent: dark ? '#071411' : '#ffffff',
  }
}

function useReducedMotion(): boolean {
  const [reducedMotion, setReducedMotion] = useState(false)
  useEffect(() => {
    let active = true
    void AccessibilityInfo.isReduceMotionEnabled().then((enabled) => {
      if (active) setReducedMotion(enabled)
    })
    const subscription = AccessibilityInfo.addEventListener(
      'reduceMotionChanged',
      setReducedMotion,
    )
    return () => {
      active = false
      subscription.remove()
    }
  }, [])
  return reducedMotion
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
  const displayedLabel = `${label}${block.required ? ` (${context.messages.required})` : ''}`
  if (block.type === 'free_text') {
    return (
      <View style={styles.field} testID={`fidibeki-field-${block.key}`}>
        <Text style={[styles.label, { color: colors.text }]}>{displayedLabel}</Text>
        <TextInput
          accessibilityLabel={label}
          multiline
          maxLength={MAX_TEXT_LENGTH}
          onChangeText={onChange}
          placeholder={label}
          placeholderTextColor={colors.muted}
          style={[
            styles.textArea,
            { borderColor: error ? colors.danger : colors.border, color: colors.text },
          ]}
          value={typeof value === 'string' ? value : ''}
        />
        <FieldError message={error} colors={colors} />
      </View>
    )
  }
  const choices =
    block.type === 'rating'
      ? [1, 2, 3, 4, 5].map((rating) => ({
          value: rating,
          label: String(rating),
          accessibilityLabel: context.messages.ratingValue(rating),
        }))
      : (block.options ?? []).map((option) => ({
          value: option.value,
          label: optionLabels[option.value] ?? option.value,
          accessibilityLabel: optionLabels[option.value] ?? option.value,
        }))
  return (
    <View
      accessibilityRole="radiogroup"
      accessibilityLabel={label}
      style={styles.field}
      testID={`fidibeki-field-${block.key}`}
    >
      <Text style={[styles.label, { color: colors.text }]}>{displayedLabel}</Text>
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
                styles.choice,
                {
                  backgroundColor: selected ? colors.accent : colors.surface,
                  borderColor: selected ? colors.accent : colors.border,
                },
              ]}
            >
              <Text style={{ color: selected ? colors.onAccent : colors.text }}>
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
  return value
    .replace(/[_-]+/g, ' ')
    .replace(/\b\w/g, (character) => character.toUpperCase())
}

export function FeedbackSheet() {
  const context = useFeedbackContext()
  const session = context.session
  const definition = session?.configuration.definition
  const contactNotice = session?.configuration.contact_notice
  const reducedMotion = useReducedMotion()
  const mode = context.theme.mode ?? 'light'
  const colors = useMemo(() => colorsFor(mode, context.theme), [context.theme, mode])
  const [form, setForm] = useState<FeedbackFormState>(EMPTY_FORM_STATE)
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({})
  const [screenshot, setScreenshot] = useState<PreparedScreenshot | undefined>()
  const screenshotRef = useRef<PreparedScreenshot | undefined>(undefined)
  const [screenshotMessage, setScreenshotMessage] = useState<string | null>(null)
  const [attachmentFailed, setAttachmentFailed] = useState(false)
  const [readingScreenshot, setReadingScreenshot] = useState(false)
  const previousSession = useRef(session)

  useEffect(() => {
    screenshotRef.current = screenshot
  }, [screenshot])

  useEffect(
    () => () => {
      void screenshotRef.current?.dispose()
    },
    [],
  )

  useEffect(() => {
    const previous = previousSession.current
    const previousDefinition = previous?.configuration.definition
    const previousNotice = previous?.configuration.contact_notice
    const definitionChanged =
      previousDefinition &&
      definition &&
      previousDefinition.definition_version !== definition.definition_version
    const consentContextChanged =
      previous &&
      session &&
      (previous.configuration.display_locale !== session.configuration.display_locale ||
        previousNotice?.notice_version !== contactNotice?.notice_version ||
        previousNotice?.controller_profile_version !== contactNotice?.controller_profile_version ||
        previousNotice?.widget_definition_version !== contactNotice?.widget_definition_version)
    if (previousDefinition && definition && (definitionChanged || consentContextChanged)) {
      setForm((current) => ({
        ...current,
        responses: definitionChanged
          ? reconcileResponses(
              previousDefinition.response_blocks,
              definition.response_blocks,
              current.responses,
            )
          : current.responses,
        consentGranted: false,
      }))
      if (!definition.can_attach_screenshot) {
        void screenshotRef.current?.dispose()
        setScreenshot(undefined)
      }
    }
    previousSession.current = session
  }, [contactNotice, definition, session])

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
    setReadingScreenshot(true)
    setScreenshotMessage(null)
    try {
      const asset = await context.screenshotProvider()
      if (!asset) {
        setScreenshotMessage(context.messages.screenshotCancelled)
        return
      }
      const prepared = await prepareScreenshot(asset)
      await screenshotRef.current?.dispose()
      setScreenshot(prepared)
      setScreenshotMessage(context.messages.screenshotAdded)
    } catch {
      setScreenshotMessage(context.messages.screenshotProviderFailed)
    } finally {
      setReadingScreenshot(false)
    }
  }, [context.messages, context.screenshotProvider, definition?.can_attach_screenshot])

  const removeScreenshot = useCallback(async () => {
    await screenshotRef.current?.dispose()
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
  const submitting = context.status === 'submitting'
  const submitted = context.status === 'submitted'
  const canAddScreenshot =
    definition.can_attach_screenshot && context.screenshotProvider !== undefined && !screenshot
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
      presentationStyle="pageSheet"
      supportedOrientations={['portrait', 'landscape']}
      testID="fidibeki-modal"
      visible={context.visible}
    >
      <SafeAreaView style={[styles.safeArea, { backgroundColor: colors.background }]}>
        <KeyboardAvoidingView
          behavior={Platform.OS === 'ios' ? 'padding' : 'height'}
          keyboardVerticalOffset={0}
          style={styles.keyboardLayout}
          testID={`fidibeki-keyboard-${Platform.OS}`}
        >
          <View style={[styles.header, { borderBottomColor: colors.border }]}>
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
            contentContainerStyle={styles.content}
            keyboardDismissMode="interactive"
            keyboardShouldPersistTaps="handled"
          >
            {submitted ? (
              <View accessibilityLiveRegion="polite" style={styles.success}>
                <Text style={[styles.successText, { color: colors.text }]}>
                  {customerCopy.success_message}
                </Text>
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
                    label={
                      customerCopy.block_labels?.[block.key] ??
                      defaultBlockLabel(block.type, context.messages)
                    }
                    onChange={(value) => updateResponse(block.key, value)}
                    optionLabels={Object.fromEntries(
                      (block.options ?? []).map((option) => [
                        option.value,
                        customerCopy.option_labels?.[block.key]?.[option.value] ??
                          humanizeOption(option.value),
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
                      message={validationMessage(
                        'comment',
                        fieldErrors.comment,
                        context.messages,
                      )}
                    />
                  </View>
                ) : null}
                {definition.collect_contact && contactNotice ? (
                  <View style={styles.contactSection}>
                    <Text style={[styles.label, { color: colors.text }]}>
                      {customerCopy.contact_label}
                    </Text>
                    <TextInput
                      accessibilityLabel={customerCopy.contact_label}
                      autoCapitalize="none"
                      autoComplete="email"
                      inputMode="email"
                      onChangeText={(reporterEmail) =>
                        setForm((current) => ({ ...current, reporterEmail }))
                      }
                      placeholder={context.messages.emailPlaceholder}
                      placeholderTextColor={colors.muted}
                      style={[
                        styles.input,
                        {
                          borderColor: fieldErrors.reporter_email
                            ? colors.danger
                            : colors.border,
                          color: colors.text,
                        },
                      ]}
                      value={form.reporterEmail}
                    />
                    <FieldError
                      colors={colors}
                      message={validationMessage(
                        'reporter_email',
                        fieldErrors.reporter_email,
                        context.messages,
                      )}
                    />
                    <View style={styles.consentRow}>
                      <Switch
                        accessibilityLabel={customerCopy.consent_label}
                        accessibilityRole="switch"
                        accessibilityState={{ checked: form.consentGranted }}
                        onValueChange={(consentGranted) =>
                          setForm((current) => ({ ...current, consentGranted }))
                        }
                        trackColor={{ false: colors.border, true: colors.accent }}
                        value={form.consentGranted}
                      />
                      <Text style={[styles.consentText, { color: colors.text }]}>
                        {customerCopy.consent_label}
                      </Text>
                    </View>
                    <FieldError
                      colors={colors}
                      message={validationMessage(
                        'consent',
                        fieldErrors.consent,
                        context.messages,
                      )}
                    />
                    <Text style={[styles.notice, { color: colors.muted }]}>
                      {contactNotice.purpose_summary}{' '}
                      {contactNotice.lawful_basis_summary}{' '}
                      {contactNotice.retention_summary}
                    </Text>
                    <Pressable
                      accessibilityRole="link"
                      onPress={() =>
                        void Linking.openURL(contactNotice.privacy_policy_url)
                      }
                    >
                      <Text style={[styles.link, { color: colors.accent }]}>
                        {context.messages.privacyPolicy}
                      </Text>
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
                      <Text style={[styles.link, { color: colors.accent }]}>
                        {context.messages.removeScreenshot}
                      </Text>
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
                    <Text style={{ color: colors.muted }}>
                      {context.messages.aiProcessingDisclosure}
                    </Text>
                    <Pressable
                      accessibilityRole="link"
                      onPress={() =>
                        void Linking.openURL(definition.ai_processing!.privacy_policy_url!)
                      }
                    >
                      <Text style={[styles.link, { color: colors.accent }]}>
                        {context.messages.privacyPolicy}
                      </Text>
                    </Pressable>
                  </View>
                ) : null}
                {context.error ? (
                  <Text
                    accessibilityLiveRegion="assertive"
                    style={[styles.errorText, { color: colors.danger }]}
                  >
                    {context.error.code === 'configuration_changed'
                      ? context.messages.configurationChanged
                      : context.messages.submitFailed}
                  </Text>
                ) : null}
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
              </>
            )}
          </ScrollView>
        </KeyboardAvoidingView>
      </SafeAreaView>
    </Modal>
  )
}

const styles = StyleSheet.create({
  safeArea: { flex: 1 },
  keyboardLayout: { flex: 1 },
  header: {
    minHeight: 60,
    borderBottomWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 20,
    paddingVertical: 12,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 16,
  },
  title: { flex: 1, fontSize: 20, fontWeight: '700', lineHeight: 26 },
  iconButton: {
    width: 44,
    height: 44,
    alignItems: 'center',
    justifyContent: 'center',
  },
  closeIcon: { fontSize: 30, lineHeight: 32 },
  content: { padding: 20, paddingBottom: 40, gap: 20 },
  field: { gap: 8 },
  label: { fontSize: 15, fontWeight: '600', lineHeight: 20 },
  textArea: {
    minHeight: 104,
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
  choiceRow: { flexDirection: 'row', flexWrap: 'wrap', gap: 8 },
  choice: {
    minWidth: 44,
    minHeight: 44,
    borderWidth: 1,
    borderRadius: 6,
    paddingHorizontal: 14,
    alignItems: 'center',
    justifyContent: 'center',
  },
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
  errorText: { fontSize: 13, lineHeight: 18 },
  success: { minHeight: 200, alignItems: 'center', justifyContent: 'center', gap: 12 },
  successText: { fontSize: 18, fontWeight: '600', lineHeight: 25, textAlign: 'center' },
})
