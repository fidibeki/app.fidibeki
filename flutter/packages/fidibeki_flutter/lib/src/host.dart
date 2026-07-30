import 'dart:async';

import 'package:flutter/material.dart';

import 'controller.dart';
import 'errors.dart';
import 'localization.dart';
import 'models.dart';
import 'screenshot.dart';
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

final class _FidibekiFeedbackHostState extends State<FidibekiFeedbackHost>
    with WidgetsBindingObserver {
  late final OverlayEntry _entry;
  bool _entryInserted = false;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
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
  void didChangeAppLifecycleState(AppLifecycleState state) {
    unawaited(widget.controller.handleAppLifecycleState(state));
  }

  @override
  Future<bool> didPopRoute() async {
    if (!widget.controller.visible) return false;
    widget.controller.close();
    return true;
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
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
    final media = MediaQuery.of(context);
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
    final submitting = controller.status == FidibekiFeedbackStatus.submitting;
    final sheet = Material(
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
                border: Border(bottom: BorderSide(color: border, width: 0.5)),
              ),
              child: Row(
                children: <Widget>[
                  Expanded(
                    child: Semantics(
                      header: true,
                      label: prompt,
                      child: ExcludeSemantics(
                        child: Text(
                          prompt,
                          maxLines: 2,
                          overflow: TextOverflow.ellipsis,
                          style: TextStyle(
                            color: text,
                            fontSize: 18,
                            fontWeight: FontWeight.w700,
                            height: 1.3,
                          ),
                        ),
                      ),
                    ),
                  ),
                  IconButton(
                    key: const ValueKey('fidibeki-close'),
                    tooltip: messages.close,
                    onPressed: submitting ? null : controller.close,
                    icon: Icon(Icons.close, color: text),
                  ),
                ],
              ),
            ),
            if (controller.status == FidibekiFeedbackStatus.submitted)
              Expanded(
                child: Center(
                  child: Padding(
                    padding: const EdgeInsets.all(24),
                    child: Semantics(
                      liveRegion: true,
                      child: Column(
                        mainAxisSize: MainAxisSize.min,
                        children: <Widget>[
                          Text(
                            localization?.successMessage ?? '',
                            textAlign: TextAlign.center,
                            style: TextStyle(
                              color: text,
                              fontSize: 18,
                              fontWeight: FontWeight.w600,
                              height: 1.4,
                            ),
                          ),
                          if (controller.attachmentOutcome?.status ==
                              FidibekiAttachmentStatus.failed)
                            Padding(
                              padding: const EdgeInsets.only(top: 10),
                              child: Text(
                                messages.attachmentFailed,
                                textAlign: TextAlign.center,
                                style: TextStyle(
                                  color: colors.danger,
                                  fontSize: 14,
                                  height: 1.4,
                                ),
                              ),
                            ),
                        ],
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
    );
    final localizedSheet =
        Localizations.of<MaterialLocalizations>(
              context,
              MaterialLocalizations,
            ) ==
            null
        ? Localizations.override(
            context: context,
            locale: const Locale('en'),
            delegates: const <LocalizationsDelegate<dynamic>>[
              DefaultMaterialLocalizations.delegate,
            ],
            child: sheet,
          )
        : sheet;

    return Semantics(
      scopesRoute: true,
      explicitChildNodes: true,
      namesRoute: true,
      label: prompt,
      child: LayoutBuilder(
        builder: (context, constraints) {
          final keyboardInset = media.viewInsets.bottom;
          final keyboardVisible = keyboardInset > 0;
          final topInset = keyboardVisible ? media.padding.top : 0.0;
          final availableHeight =
              (constraints.maxHeight - keyboardInset - topInset).clamp(
                0.0,
                constraints.maxHeight,
              );
          final sheetHeight = keyboardVisible
              ? availableHeight
              : constraints.maxHeight * 0.75;
          final duration = media.disableAnimations
              ? Duration.zero
              : const Duration(milliseconds: 180);
          return Stack(
            fit: StackFit.expand,
            children: <Widget>[
              ModalBarrier(
                key: const ValueKey('fidibeki-scrim'),
                color: const Color(0x52000000),
                dismissible: !submitting,
                onDismiss: submitting ? null : controller.close,
              ),
              AnimatedPositioned(
                curve: Curves.easeOutCubic,
                duration: duration,
                left: 0,
                right: 0,
                bottom: keyboardInset,
                height: sheetHeight,
                child: ClipRRect(
                  borderRadius: const BorderRadius.vertical(
                    top: Radius.circular(20),
                  ),
                  child: localizedSheet,
                ),
              ),
            ],
          );
        },
      ),
    );
  }
}

final class _FeedbackForm extends StatefulWidget {
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
  State<_FeedbackForm> createState() => _FeedbackFormState();
}

final class _FeedbackFormState extends State<_FeedbackForm> {
  final ScrollController _scrollController = ScrollController();
  final Map<String, GlobalKey> _fieldKeys = <String, GlobalKey>{};
  final Map<String, FocusNode> _focusNodes = <String, FocusNode>{};

  GlobalKey _fieldKey(String key) {
    return _fieldKeys.putIfAbsent(key, GlobalKey.new);
  }

  FocusNode _focusNode(String key) {
    return _focusNodes.putIfAbsent(
      key,
      () => FocusNode(debugLabel: 'fidibeki-$key'),
    );
  }

  Future<void> _submit() async {
    try {
      await widget.controller.submit();
    } on FidibekiException catch (error) {
      if (error.code == FidibekiErrorCode.validation && mounted) {
        WidgetsBinding.instance.addPostFrameCallback((_) {
          if (mounted) _focusFirstInvalidField();
        });
      }
    }
  }

  void _focusFirstInvalidField() {
    final fields = widget.controller.fieldErrors;
    final orderedKeys = <String>[
      for (final block in widget.definition.responseBlocks) block.key,
      'comment',
      'reporter_email',
      'consent',
    ];
    final first = orderedKeys.where((key) {
      final errorKey =
          key == 'comment' || key == 'reporter_email' || key == 'consent'
          ? key
          : 'responses.$key';
      return fields.containsKey(errorKey);
    }).firstOrNull;
    if (first == null) return;
    final context = _fieldKeys[first]?.currentContext;
    if (context != null) {
      unawaited(
        Scrollable.ensureVisible(
          context,
          alignment: 0.12,
          curve: Curves.easeOutCubic,
          duration: MediaQuery.disableAnimationsOf(context)
              ? Duration.zero
              : const Duration(milliseconds: 180),
        ),
      );
    }
    _focusNodes[first]?.requestFocus();
  }

  Widget _responseField(
    FidibekiResponseBlock block,
    FidibekiLocalization localization,
  ) {
    final controller = widget.controller;
    final messages = widget.messages;
    final colors = widget.colors;
    final field = switch (block.type) {
      FidibekiResponseBlockType.rating => _RatingField(
        controller: controller,
        block: block,
        label: localization.blockLabels[block.key] ?? block.key,
        accent: colors.accent,
        text: widget.text,
        messages: messages,
      ),
      FidibekiResponseBlockType.freeText => _TextResponseField(
        controller: controller,
        block: block,
        label: localization.blockLabels[block.key] ?? block.key,
        text: widget.text,
        border: widget.border,
        messages: messages,
        focusNode: _focusNode(block.key),
      ),
      FidibekiResponseBlockType.reaction ||
      FidibekiResponseBlockType.singleChoice => _ChoiceField(
        controller: controller,
        block: block,
        label:
            localization.blockLabels[block.key] ??
            _defaultBlockLabel(block.type, messages),
        optionLabels:
            localization.optionLabels[block.key] ?? const <String, String>{},
        accent: colors.accent,
        text: widget.text,
        border: widget.border,
        messages: messages,
      ),
    };
    return KeyedSubtree(key: _fieldKey(block.key), child: field);
  }

  @override
  void dispose() {
    _scrollController.dispose();
    for (final focusNode in _focusNodes.values) {
      focusNode.dispose();
    }
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final controller = widget.controller;
    final definition = widget.definition;
    final background = widget.background;
    final text = widget.text;
    final border = widget.border;
    final messages = widget.messages;
    final colors = widget.colors;
    final accent = colors.accent;
    final accentForeground = colors.onAccent;
    final localization = definition.localization;

    return Column(
      children: <Widget>[
        Expanded(
          child: SingleChildScrollView(
            key: const ValueKey('fidibeki-form-scroll'),
            controller: _scrollController,
            padding: const EdgeInsets.fromLTRB(16, 18, 16, 20),
            keyboardDismissBehavior: ScrollViewKeyboardDismissBehavior.onDrag,
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                for (final block in definition.responseBlocks)
                  _responseField(block, localization),
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
                    key: _fieldKey('comment'),
                    padding: const EdgeInsets.only(top: 10, bottom: 18),
                    child: TextFormField(
                      key: const ValueKey('fidibeki-comment'),
                      focusNode: _focusNode('comment'),
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
                  KeyedSubtree(
                    key: _fieldKey('reporter_email'),
                    child: _ContactFields(
                      controller: controller,
                      definition: definition,
                      accent: accent,
                      text: text,
                      border: border,
                      messages: messages,
                      focusNode: _focusNode('reporter_email'),
                      consentKey: _fieldKey('consent'),
                    ),
                  ),
                if (controller.canAddScreenshot || controller.readingScreenshot)
                  _ScreenshotAction(
                    controller: controller,
                    accent: accent,
                    text: text,
                    border: border,
                    messages: messages,
                  ),
                if (controller.hasScreenshot)
                  _ScreenshotAttached(
                    controller: controller,
                    accent: accent,
                    text: text,
                    border: border,
                    messages: messages,
                  ),
                if (controller.screenshotNotice case final notice?)
                  Padding(
                    padding: const EdgeInsets.only(bottom: 12),
                    child: Text(
                      _screenshotNoticeText(notice, messages),
                      key: const ValueKey('fidibeki-screenshot-message'),
                      style: TextStyle(
                        color: text.withValues(alpha: 0.7),
                        fontSize: 14,
                      ),
                    ),
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
                    child: Semantics(
                      liveRegion: true,
                      child: Text(
                        controller.error!.code ==
                                FidibekiErrorCode.configurationChanged
                            ? messages.configurationChanged
                            : messages.submitFailed,
                        key: const ValueKey('fidibeki-submit-error'),
                        style: TextStyle(color: colors.danger, fontSize: 14),
                      ),
                    ),
                  ),
              ],
            ),
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
              child: ConstrainedBox(
                constraints: const BoxConstraints(minHeight: 50),
                child: Semantics(
                  button: true,
                  enabled:
                      controller.status != FidibekiFeedbackStatus.submitting,
                  liveRegion:
                      controller.status == FidibekiFeedbackStatus.submitting,
                  label: controller.status == FidibekiFeedbackStatus.submitting
                      ? messages.sending
                      : localization.submitLabel,
                  child: ExcludeSemantics(
                    child: FilledButton(
                      key: const ValueKey('fidibeki-submit'),
                      onPressed:
                          controller.status == FidibekiFeedbackStatus.submitting
                          ? null
                          : _submit,
                      style: FilledButton.styleFrom(
                        minimumSize: const Size.fromHeight(50),
                        padding: const EdgeInsets.symmetric(
                          horizontal: 18,
                          vertical: 12,
                        ),
                        backgroundColor: accent,
                        foregroundColor: accentForeground,
                        disabledBackgroundColor: accent.withValues(alpha: 0.55),
                        shape: RoundedRectangleBorder(
                          borderRadius: BorderRadius.circular(7),
                        ),
                      ),
                      child:
                          controller.status == FidibekiFeedbackStatus.submitting
                          ? SizedBox(
                              width: 22,
                              height: 22,
                              child: CircularProgressIndicator(
                                strokeWidth: 2.5,
                                color: accentForeground,
                              ),
                            )
                          : Text(
                              localization.submitLabel,
                              maxLines: 2,
                              overflow: TextOverflow.ellipsis,
                              textAlign: TextAlign.center,
                              style: const TextStyle(
                                fontSize: 16,
                                fontWeight: FontWeight.w700,
                              ),
                            ),
                    ),
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
          Semantics(
            label: block.required ? '$label, ${messages.required}' : label,
            header: true,
            child: ExcludeSemantics(
              child: Text(
                block.required ? '$label *' : label,
                style: TextStyle(
                  color: text,
                  fontSize: 16,
                  fontWeight: FontWeight.w700,
                ),
              ),
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
                          child: Icon(
                            rating <= selected
                                ? Icons.star_rounded
                                : Icons.star_border_rounded,
                            color: rating <= selected ? accent : text,
                            size: 30,
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
    required this.messages,
    required this.focusNode,
  });

  final FidibekiFeedbackController controller;
  final FidibekiResponseBlock block;
  final String label;
  final Color text;
  final Color border;
  final FidibekiSystemMessages messages;
  final FocusNode focusNode;

  @override
  Widget build(BuildContext context) {
    final error = controller.fieldErrors['responses.${block.key}'];
    return Padding(
      padding: const EdgeInsets.only(bottom: 18),
      child: Semantics(
        label: block.required ? '$label, ${messages.required}' : label,
        textField: true,
        child: TextFormField(
          key: ValueKey('fidibeki-text-${block.key}'),
          focusNode: focusNode,
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
    required this.messages,
  });

  final FidibekiFeedbackController controller;
  final FidibekiResponseBlock block;
  final String label;
  final Map<String, String> optionLabels;
  final Color accent;
  final Color text;
  final Color border;
  final FidibekiSystemMessages messages;

  @override
  Widget build(BuildContext context) {
    final selected = controller.response(block.key) as String?;
    final error = controller.fieldErrors['responses.${block.key}'];
    return Padding(
      padding: const EdgeInsets.only(bottom: 22),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Semantics(
            label: block.required ? '$label, ${messages.required}' : label,
            header: true,
            child: ExcludeSemantics(
              child: Text(
                block.required ? '$label *' : label,
                style: TextStyle(
                  color: text,
                  fontSize: 16,
                  fontWeight: FontWeight.w700,
                ),
              ),
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
    required this.focusNode,
    required this.consentKey,
  });

  final FidibekiFeedbackController controller;
  final FidibekiWidgetDefinition definition;
  final Color accent;
  final Color text;
  final Color border;
  final FidibekiSystemMessages messages;
  final FocusNode focusNode;
  final Key consentKey;

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
            focusNode: focusNode,
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
            key: consentKey,
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

String _screenshotNoticeText(
  FidibekiScreenshotNotice notice,
  FidibekiSystemMessages messages,
) {
  return switch (notice) {
    FidibekiScreenshotNotice.cancelled => messages.screenshotCancelled,
    FidibekiScreenshotNotice.invalid => messages.screenshotInvalid,
    FidibekiScreenshotNotice.providerFailed =>
      messages.screenshotProviderFailed,
  };
}

final class _ScreenshotAction extends StatelessWidget {
  const _ScreenshotAction({
    required this.controller,
    required this.accent,
    required this.text,
    required this.border,
    required this.messages,
  });

  final FidibekiFeedbackController controller;
  final Color accent;
  final Color text;
  final Color border;
  final FidibekiSystemMessages messages;

  @override
  Widget build(BuildContext context) {
    return Padding(
      key: const ValueKey('fidibeki-screenshot-action'),
      padding: const EdgeInsets.only(bottom: 12),
      child: InkWell(
        onTap: controller.readingScreenshot ? null : controller.addScreenshot,
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
              if (controller.readingScreenshot)
                SizedBox(
                  width: 20,
                  height: 20,
                  child: CircularProgressIndicator(
                    strokeWidth: 2,
                    color: accent,
                  ),
                )
              else
                Icon(
                  Icons.add_photo_alternate_outlined,
                  color: accent,
                  size: 20,
                ),
              const SizedBox(width: 10),
              Expanded(
                child: Text(
                  controller.readingScreenshot
                      ? messages.addingScreenshot
                      : messages.addScreenshot,
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

final class _ScreenshotAttached extends StatelessWidget {
  const _ScreenshotAttached({
    required this.controller,
    required this.accent,
    required this.text,
    required this.border,
    required this.messages,
  });

  final FidibekiFeedbackController controller;
  final Color accent;
  final Color text;
  final Color border;
  final FidibekiSystemMessages messages;

  @override
  Widget build(BuildContext context) {
    return Container(
      key: const ValueKey('fidibeki-screenshot-attached'),
      constraints: const BoxConstraints(minHeight: 48),
      margin: const EdgeInsets.only(bottom: 12),
      padding: const EdgeInsets.only(left: 12, right: 2),
      decoration: BoxDecoration(
        border: Border.all(color: border),
        borderRadius: BorderRadius.circular(7),
      ),
      child: Row(
        children: <Widget>[
          Icon(Icons.check_circle_outline, color: accent, size: 20),
          const SizedBox(width: 10),
          Expanded(
            child: Text(
              messages.screenshotAdded,
              style: TextStyle(
                color: text,
                fontSize: 15,
                fontWeight: FontWeight.w600,
              ),
            ),
          ),
          IconButton(
            key: const ValueKey('fidibeki-screenshot-remove'),
            tooltip: messages.removeScreenshot,
            onPressed: controller.removeScreenshot,
            icon: Icon(Icons.close, color: text),
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
