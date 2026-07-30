import 'package:flutter/material.dart';

import 'controller.dart';
import 'errors.dart';
import 'models.dart';

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
    final dark = brightness == Brightness.dark;
    final background = dark ? const Color(0xff141716) : const Color(0xffffffff);
    final text = dark ? const Color(0xfff4f7f6) : const Color(0xff17211f);
    final border = dark ? const Color(0xff3a4441) : const Color(0xffccd5d2);
    final definition = controller.session?.configuration.definition;
    final localization = definition?.localization;
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
                                label: 'Close',
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

Color _configuredColor(String? value, Color fallback) {
  if (value == null) return fallback;
  return Color(int.parse('ff${value.substring(1)}', radix: 16));
}

final class _FeedbackForm extends StatelessWidget {
  const _FeedbackForm({
    required this.controller,
    required this.definition,
    required this.background,
    required this.text,
    required this.border,
  });

  final FidibekiFeedbackController controller;
  final FidibekiWidgetDefinition definition;
  final Color background;
  final Color text;
  final Color border;

  @override
  Widget build(BuildContext context) {
    final accent = _configuredColor(
      definition.appearance.accentColor,
      const Color(0xff087f6b),
    );
    final accentForeground = _configuredColor(
      definition.appearance.accentForeground,
      Colors.white,
    );
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
                  )
                else if (block.type == FidibekiResponseBlockType.freeText)
                  _TextResponseField(
                    controller: controller,
                    block: block,
                    label: localization.blockLabels[block.key] ?? block.key,
                    text: text,
                    border: border,
                  ),
              if (controller.error != null &&
                  controller.error!.code != FidibekiErrorCode.validation)
                Padding(
                  padding: const EdgeInsets.only(top: 8),
                  child: Text(
                    controller.error!.message,
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
                        localization.submitLabel,
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
  });

  final FidibekiFeedbackController controller;
  final FidibekiResponseBlock block;
  final String label;
  final Color accent;
  final Color text;

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
                  label: '$rating of 5',
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
