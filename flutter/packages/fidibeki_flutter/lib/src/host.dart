import 'package:flutter/material.dart';

import 'controller.dart';
import 'errors.dart';
import 'localization.dart';
import 'models.dart';
import 'theme.dart';

final class FidibekiFeedbackHost extends StatefulWidget {
  const FidibekiFeedbackHost({
    required this.controller,
    required this.child,
    super.key,
  });

  final FidibekiFeedbackController controller;
  final Widget child;

  @override
  State<FidibekiFeedbackHost> createState() => _FidibekiFeedbackHostState();
}

final class _FidibekiFeedbackHostState extends State<FidibekiFeedbackHost> {
  late final OverlayEntry _entry;
  bool _entryInserted = false;

  @override
  void initState() {
    super.initState();
    _entry = OverlayEntry(builder: _buildOverlay);
    widget.controller.initialize();
  }

  @override
  void didUpdateWidget(FidibekiFeedbackHost oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.controller != widget.controller) {
      widget.controller.initialize();
    }
    _entry.markNeedsBuild();
  }

  @override
  Widget build(BuildContext context) {
    _entryInserted = true;
    return Overlay(initialEntries: <OverlayEntry>[_entry]);
  }

  Widget _buildOverlay(BuildContext context) {
    return ListenableBuilder(
      listenable: widget.controller,
      builder: (context, _) {
        return Stack(
          fit: StackFit.expand,
          children: <Widget>[
            widget.child,
            if (widget.controller.visible)
              _FeedbackSheet(controller: widget.controller),
          ],
        );
      },
    );
  }

  @override
  void dispose() {
    if (_entryInserted) _entry.remove();
    _entry.dispose();
    super.dispose();
  }
}

final class _FeedbackSheet extends StatelessWidget {
  const _FeedbackSheet({required this.controller});

  final FidibekiFeedbackController controller;

  @override
  Widget build(BuildContext context) {
    final brightness = MediaQuery.platformBrightnessOf(context);
    final definition = controller.session?.configuration.definition;
    final colors = resolveFidibekiTheme(
      hostTheme: controller.theme,
      appearance: definition?.appearance ?? const FidibekiAppearance(),
      platformBrightness: brightness,
    );
    final background = colors.background;
    final text = colors.text;
    final border = colors.border;
    final localization = definition?.localization;
    final messages = fidibekiSystemMessages(
      controller.session?.configuration.displayLocale ??
          controller.locale.toLanguageTag(),
    );
    final prompt = localization?.prompt ?? '';

    return Semantics(
      scopesRoute: true,
      explicitChildNodes: true,
      namesRoute: true,
      label: prompt,
      child: Stack(
        fit: StackFit.expand,
        children: <Widget>[
          ModalBarrier(
            color: const Color(0x52000000),
            dismissible: true,
            onDismiss: controller.close,
          ),
          Align(
            alignment: Alignment.bottomCenter,
            child: FractionallySizedBox(
              heightFactor: 0.75,
              widthFactor: 1,
              child: ClipRRect(
                borderRadius: const BorderRadius.vertical(
                  top: Radius.circular(20),
                ),
                child: Material(
                  key: const ValueKey('fidibeki-sheet'),
                  color: background,
                  child: SafeArea(
                    top: false,
                    child: Column(
                      children: <Widget>[
                        const SizedBox(height: 7),
                        DecoratedBox(
                          decoration: BoxDecoration(
                            color: border,
                            borderRadius: BorderRadius.circular(2),
                          ),
                          child: const SizedBox(width: 36, height: 4),
                        ),
                        const SizedBox(height: 3),
                        Container(
                          constraints: const BoxConstraints(minHeight: 52),
                          padding: const EdgeInsets.only(left: 16, right: 8),
                          decoration: BoxDecoration(
                            border: Border(
                              bottom: BorderSide(color: border, width: 0.5),
                            ),
                          ),
                          child: Row(
                            children: <Widget>[
                              Expanded(
                                child: Text(
                                  prompt,
                                  style: TextStyle(
                                    color: text,
                                    fontSize: 18,
                                    fontWeight: FontWeight.w700,
                                    height: 1.3,
                                  ),
                                ),
                              ),
                              Semantics(
                                button: true,
                                label: messages.close,
                                child: GestureDetector(
                                  behavior: HitTestBehavior.opaque,
                                  onTap: controller.close,
                                  child: SizedBox(
                                    width: 44,
                                    height: 44,
                                    child: Center(
                                      child: ExcludeSemantics(
                                        child: Text(
                                          '×',
                                          style: TextStyle(
                                            color: text,
                                            fontSize: 30,
                                            height: 1,
                                          ),
                                        ),
                                      ),
                                    ),
                                  ),
                                ),
                              ),
                            ],
                          ),
                        ),
                        if (controller.status ==
                            FidibekiFeedbackStatus.submitted)
                          Expanded(
                            child: Center(
                              child: Padding(
                                padding: const EdgeInsets.all(24),
                                child: Semantics(
                                  liveRegion: true,
                                  child: Text(
                                    localization?.successMessage ?? '',
                                    textAlign: TextAlign.center,
                                    style: TextStyle(
                                      color: text,
                                      fontSize: 18,
                                      fontWeight: FontWeight.w600,
                                      height: 1.4,
                                    ),
                                  ),
                                ),
                              ),
                            ),
                          )
                        else if (definition != null && localization != null)
                          Expanded(
                            child: _FeedbackForm(
                              controller: controller,
                              definition: definition,
                              background: background,
                              text: text,
                              border: border,
                              messages: messages,
                              colors: colors,
                            ),
                          )
                        else
                          const Expanded(child: SizedBox.shrink()),
                      ],
                    ),
                  ),
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }
}

final class _FeedbackForm extends StatelessWidget {
  const _FeedbackForm({
    required this.controller,
    required this.definition,
    required this.background,
    required this.text,
    required this.border,
    required this.messages,
    required this.colors,
  });

  final FidibekiFeedbackController controller;
  final FidibekiWidgetDefinition definition;
  final Color background;
  final Color text;
  final Color border;
  final FidibekiSystemMessages messages;
  final FidibekiResolvedTheme colors;

  @override
  Widget build(BuildContext context) {
    final accent = colors.accent;
    final accentForeground = colors.onAccent;
    final localization = definition.localization;

    return Column(
      children: <Widget>[
        Expanded(
          child: ListView(
            key: const ValueKey('fidibeki-form-scroll'),
            padding: const EdgeInsets.fromLTRB(16, 18, 16, 20),
            children: <Widget>[
              for (final block in definition.responseBlocks)
                if (block.type == FidibekiResponseBlockType.rating)
                  _RatingField(
                    controller: controller,
                    block: block,
                    label: localization.blockLabels[block.key] ?? block.key,
                    accent: accent,
                    text: text,
                    messages: messages,
                  )
                else if (block.type == FidibekiResponseBlockType.freeText)
                  _TextResponseField(
                    controller: controller,
                    block: block,
                    label: localization.blockLabels[block.key] ?? block.key,
                    text: text,
                    border: border,
                  )
                else
                  _ChoiceField(
                    controller: controller,
                    block: block,
                    label:
                        localization.blockLabels[block.key] ??
                        _defaultBlockLabel(block.type, messages),
                    optionLabels:
                        localization.optionLabels[block.key] ??
                        const <String, String>{},
                    accent: accent,
                    text: text,
                    border: border,
                  ),
              if (definition.allowComment)
                _OptionalAction(
                  key: const ValueKey('fidibeki-comment-disclosure'),
                  label: controller.commentExpanded
                      ? messages.hideMoreContext
                      : messages.addMoreContext,
                  expanded: controller.commentExpanded,
                  onTap: controller.toggleCommentExpanded,
                  accent: accent,
                  text: text,
                  border: border,
                ),
              if (definition.allowComment && controller.commentExpanded)
                Padding(
                  padding: const EdgeInsets.only(top: 10, bottom: 18),
                  child: TextFormField(
                    key: const ValueKey('fidibeki-comment'),
                    initialValue: controller.comment,
                    minLines: 3,
                    maxLines: 6,
                    maxLength: 10000,
                    onChanged: controller.setComment,
                    style: TextStyle(color: text, fontSize: 16),
                    decoration: InputDecoration(
                      hintText: localization.commentPlaceholder,
                      errorText: controller.fieldErrors['comment'],
                      border: OutlineInputBorder(
                        borderRadius: BorderRadius.circular(7),
                        borderSide: BorderSide(color: border),
                      ),
                    ),
                  ),
                ),
              if (definition.collectContact &&
                  controller.session?.configuration.contactNotice != null &&
                  !controller.contactExpanded)
                _OptionalAction(
                  key: const ValueKey('fidibeki-contact-disclosure'),
                  label: messages.getUpdates,
                  expanded: false,
                  onTap: controller.expandContact,
                  accent: accent,
                  text: text,
                  border: border,
                ),
              if (definition.collectContact &&
                  controller.session?.configuration.contactNotice != null &&
                  controller.contactExpanded)
                _ContactFields(
                  controller: controller,
                  definition: definition,
                  accent: accent,
                  text: text,
                  border: border,
                  messages: messages,
                ),
              if (_disclosesMobileAi(definition))
                _AiDisclosure(
                  controller: controller,
                  definition: definition,
                  accent: accent,
                  text: text,
                  messages: messages,
                ),
              if (controller.error != null &&
                  controller.error!.code != FidibekiErrorCode.validation)
                Padding(
                  padding: const EdgeInsets.only(top: 8),
                  child: Text(
                    controller.error!.code ==
                            FidibekiErrorCode.configurationChanged
                        ? messages.configurationChanged
                        : messages.submitFailed,
                    key: const ValueKey('fidibeki-submit-error'),
                    style: const TextStyle(
                      color: Color(0xffb42318),
                      fontSize: 14,
                    ),
                  ),
                ),
            ],
          ),
        ),
        DecoratedBox(
          decoration: BoxDecoration(
            color: background,
            border: Border(top: BorderSide(color: border, width: 0.5)),
          ),
          child: SafeArea(
            top: false,
            minimum: const EdgeInsets.fromLTRB(16, 12, 16, 12),
            child: SizedBox(
              width: double.infinity,
              height: 50,
              child: FilledButton(
                key: const ValueKey('fidibeki-submit'),
                onPressed:
                    controller.status == FidibekiFeedbackStatus.submitting
                    ? null
                    : () async {
                        try {
                          await controller.submit();
                        } on FidibekiException {
                          // The controller exposes the typed failure to the UI.
                        }
                      },
                style: FilledButton.styleFrom(
                  backgroundColor: accent,
                  foregroundColor: accentForeground,
                  disabledBackgroundColor: accent.withValues(alpha: 0.55),
                  shape: RoundedRectangleBorder(
                    borderRadius: BorderRadius.circular(7),
                  ),
                ),
                child: controller.status == FidibekiFeedbackStatus.submitting
                    ? SizedBox(
                        width: 22,
                        height: 22,
                        child: CircularProgressIndicator(
                          strokeWidth: 2.5,
                          color: accentForeground,
                        ),
                      )
                    : Text(
                        controller.status == FidibekiFeedbackStatus.submitting
                            ? messages.sending
                            : localization.submitLabel,
                        style: const TextStyle(
                          fontSize: 16,
                          fontWeight: FontWeight.w700,
                        ),
                      ),
              ),
            ),
          ),
        ),
      ],
    );
  }
}

final class _RatingField extends StatelessWidget {
  const _RatingField({
    required this.controller,
    required this.block,
    required this.label,
    required this.accent,
    required this.text,
    required this.messages,
  });

  final FidibekiFeedbackController controller;
  final FidibekiResponseBlock block;
  final String label;
  final Color accent;
  final Color text;
  final FidibekiSystemMessages messages;

  @override
  Widget build(BuildContext context) {
    final selected = controller.response(block.key) as int? ?? 0;
    final error = controller.fieldErrors['responses.${block.key}'];
    return Padding(
      padding: const EdgeInsets.only(bottom: 22),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Text(
            block.required ? '$label *' : label,
            style: TextStyle(
              color: text,
              fontSize: 16,
              fontWeight: FontWeight.w700,
            ),
          ),
          const SizedBox(height: 8),
          Row(
            children: <Widget>[
              for (var rating = 1; rating <= 5; rating += 1)
                Semantics(
                  button: true,
                  selected: selected == rating,
                  label: messages.ratingValue(rating),
                  child: InkResponse(
                    key: ValueKey('fidibeki-rating-${block.key}-$rating'),
                    onTap: () => controller.setRating(block.key, rating),
                    radius: 24,
                    child: SizedBox(
                      width: 44,
                      height: 44,
                      child: Center(
                        child: ExcludeSemantics(
                          child: Text(
                            rating <= selected ? '★' : '☆',
                            style: TextStyle(
                              color: rating <= selected ? accent : text,
                              fontSize: 30,
                              height: 1,
                            ),
                          ),
                        ),
                      ),
                    ),
                  ),
                ),
            ],
          ),
          if (error != null)
            Text(
              error,
              key: ValueKey('fidibeki-error-${block.key}'),
              style: const TextStyle(color: Color(0xffb42318), fontSize: 13),
            ),
        ],
      ),
    );
  }
}

final class _TextResponseField extends StatelessWidget {
  const _TextResponseField({
    required this.controller,
    required this.block,
    required this.label,
    required this.text,
    required this.border,
  });

  final FidibekiFeedbackController controller;
  final FidibekiResponseBlock block;
  final String label;
  final Color text;
  final Color border;

  @override
  Widget build(BuildContext context) {
    final error = controller.fieldErrors['responses.${block.key}'];
    return Padding(
      padding: const EdgeInsets.only(bottom: 18),
      child: TextFormField(
        key: ValueKey('fidibeki-text-${block.key}'),
        initialValue: controller.response(block.key) as String? ?? '',
        minLines: 3,
        maxLines: 6,
        keyboardType: TextInputType.multiline,
        textCapitalization: TextCapitalization.sentences,
        onChanged: (value) => controller.setText(block.key, value),
        style: TextStyle(color: text, fontSize: 16),
        decoration: InputDecoration(
          labelText: block.required ? '$label *' : label,
          alignLabelWithHint: true,
          errorText: error,
          border: OutlineInputBorder(
            borderRadius: BorderRadius.circular(7),
            borderSide: BorderSide(color: border),
          ),
        ),
      ),
    );
  }
}

String _defaultBlockLabel(
  FidibekiResponseBlockType type,
  FidibekiSystemMessages messages,
) {
  return switch (type) {
    FidibekiResponseBlockType.rating => messages.rating,
    FidibekiResponseBlockType.freeText => messages.feedback,
    FidibekiResponseBlockType.reaction => messages.reaction,
    FidibekiResponseBlockType.singleChoice => messages.feedback,
  };
}

String _humanizeOption(String value) {
  final normalized = value.replaceAll(RegExp('[-_]+'), ' ').trim();
  if (normalized.isEmpty) return value;
  return '${normalized[0].toUpperCase()}${normalized.substring(1)}';
}

final class _ChoiceField extends StatelessWidget {
  const _ChoiceField({
    required this.controller,
    required this.block,
    required this.label,
    required this.optionLabels,
    required this.accent,
    required this.text,
    required this.border,
  });

  final FidibekiFeedbackController controller;
  final FidibekiResponseBlock block;
  final String label;
  final Map<String, String> optionLabels;
  final Color accent;
  final Color text;
  final Color border;

  @override
  Widget build(BuildContext context) {
    final selected = controller.response(block.key) as String?;
    final error = controller.fieldErrors['responses.${block.key}'];
    return Padding(
      padding: const EdgeInsets.only(bottom: 22),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Text(
            block.required ? '$label *' : label,
            style: TextStyle(
              color: text,
              fontSize: 16,
              fontWeight: FontWeight.w700,
            ),
          ),
          const SizedBox(height: 10),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: <Widget>[
              for (final option in block.options)
                Semantics(
                  selected: selected == option.value,
                  button: true,
                  child: ChoiceChip(
                    key: ValueKey(
                      'fidibeki-choice-${block.key}-${option.value}',
                    ),
                    label: Text(
                      optionLabels[option.value] ??
                          _humanizeOption(option.value),
                    ),
                    selected: selected == option.value,
                    onSelected: (_) =>
                        controller.setChoice(block.key, option.value),
                    selectedColor: accent.withValues(alpha: 0.18),
                    backgroundColor: Colors.transparent,
                    side: BorderSide(
                      color: selected == option.value ? accent : border,
                    ),
                    labelStyle: TextStyle(
                      color: selected == option.value ? accent : text,
                      fontWeight: selected == option.value
                          ? FontWeight.w700
                          : FontWeight.w500,
                    ),
                    shape: RoundedRectangleBorder(
                      borderRadius: BorderRadius.circular(7),
                    ),
                    showCheckmark: false,
                  ),
                ),
            ],
          ),
          if (error != null) ...<Widget>[
            const SizedBox(height: 6),
            Text(
              error,
              key: ValueKey('fidibeki-error-${block.key}'),
              style: const TextStyle(color: Color(0xffb42318), fontSize: 13),
            ),
          ],
        ],
      ),
    );
  }
}

final class _OptionalAction extends StatelessWidget {
  const _OptionalAction({
    required this.label,
    required this.expanded,
    required this.onTap,
    required this.accent,
    required this.text,
    required this.border,
    super.key,
  });

  final String label;
  final bool expanded;
  final VoidCallback onTap;
  final Color accent;
  final Color text;
  final Color border;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 12),
      child: InkWell(
        onTap: onTap,
        borderRadius: BorderRadius.circular(7),
        child: Container(
          constraints: const BoxConstraints(minHeight: 48),
          padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
          decoration: BoxDecoration(
            border: Border.all(color: border),
            borderRadius: BorderRadius.circular(7),
          ),
          child: Row(
            children: <Widget>[
              Icon(
                expanded
                    ? Icons.remove_circle_outline
                    : Icons.add_circle_outline,
                color: accent,
                size: 20,
              ),
              const SizedBox(width: 10),
              Expanded(
                child: Text(
                  label,
                  style: TextStyle(
                    color: text,
                    fontSize: 15,
                    fontWeight: FontWeight.w600,
                  ),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

final class _ContactFields extends StatelessWidget {
  const _ContactFields({
    required this.controller,
    required this.definition,
    required this.accent,
    required this.text,
    required this.border,
    required this.messages,
  });

  final FidibekiFeedbackController controller;
  final FidibekiWidgetDefinition definition;
  final Color accent;
  final Color text;
  final Color border;
  final FidibekiSystemMessages messages;

  @override
  Widget build(BuildContext context) {
    final localization = definition.localization;
    final notice = controller.session!.configuration.contactNotice!;
    return Padding(
      key: const ValueKey('fidibeki-contact-section'),
      padding: const EdgeInsets.only(bottom: 18),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Text(
            messages.getUpdates,
            style: TextStyle(
              color: text,
              fontSize: 16,
              fontWeight: FontWeight.w700,
            ),
          ),
          const SizedBox(height: 10),
          TextFormField(
            key: const ValueKey('fidibeki-reporter-email'),
            initialValue: controller.reporterEmail,
            keyboardType: TextInputType.emailAddress,
            autofillHints: const <String>[AutofillHints.email],
            autocorrect: false,
            onChanged: controller.setReporterEmail,
            style: TextStyle(color: text, fontSize: 16),
            decoration: InputDecoration(
              labelText: localization.contactLabel,
              hintText: messages.emailPlaceholder,
              errorText: controller.fieldErrors['reporter_email'],
              border: OutlineInputBorder(
                borderRadius: BorderRadius.circular(7),
                borderSide: BorderSide(color: border),
              ),
            ),
          ),
          const SizedBox(height: 8),
          Row(
            crossAxisAlignment: CrossAxisAlignment.center,
            children: <Widget>[
              Switch(
                key: const ValueKey('fidibeki-consent'),
                value: controller.consentGranted,
                activeTrackColor: accent,
                onChanged: controller.setConsentGranted,
              ),
              const SizedBox(width: 8),
              Expanded(
                child: Text(
                  localization.consentLabel,
                  style: TextStyle(color: text, fontSize: 14, height: 1.35),
                ),
              ),
            ],
          ),
          if (controller.fieldErrors['consent'] case final error?)
            Padding(
              padding: const EdgeInsets.only(top: 2),
              child: Text(
                error,
                key: const ValueKey('fidibeki-error-consent'),
                style: const TextStyle(color: Color(0xffb42318), fontSize: 13),
              ),
            ),
          const SizedBox(height: 8),
          Text(
            '${notice.purposeSummary} ${notice.lawfulBasisSummary} '
            '${notice.retentionSummary}',
            style: TextStyle(color: text.withValues(alpha: 0.7), height: 1.4),
          ),
          TextButton(
            onPressed: () => controller.openLink(notice.privacyPolicyUrl),
            style: TextButton.styleFrom(
              foregroundColor: accent,
              padding: const EdgeInsets.symmetric(vertical: 6),
            ),
            child: Text(messages.privacyPolicy),
          ),
        ],
      ),
    );
  }
}

bool _disclosesMobileAi(FidibekiWidgetDefinition definition) {
  final ai = definition.aiProcessing;
  return ai?.enabled == true &&
      ai!.inputCategories.any(
        (category) => category == 'feedback_text' || category == 'screenshot',
      );
}

final class _AiDisclosure extends StatelessWidget {
  const _AiDisclosure({
    required this.controller,
    required this.definition,
    required this.accent,
    required this.text,
    required this.messages,
  });

  final FidibekiFeedbackController controller;
  final FidibekiWidgetDefinition definition;
  final Color accent;
  final Color text;
  final FidibekiSystemMessages messages;

  @override
  Widget build(BuildContext context) {
    final privacyPolicyUrl = definition.aiProcessing!.privacyPolicyUrl!;
    return Padding(
      key: const ValueKey('fidibeki-ai-disclosure'),
      padding: const EdgeInsets.only(bottom: 8),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Text(
            messages.aiProcessingDisclosure,
            style: TextStyle(color: text.withValues(alpha: 0.7), height: 1.4),
          ),
          TextButton(
            onPressed: () => controller.openLink(privacyPolicyUrl),
            style: TextButton.styleFrom(
              foregroundColor: accent,
              padding: const EdgeInsets.symmetric(vertical: 6),
            ),
            child: Text(messages.privacyPolicy),
          ),
        ],
      ),
    );
  }
}
