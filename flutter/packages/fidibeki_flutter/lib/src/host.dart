import 'package:flutter/widgets.dart';

import 'controller.dart';

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
  @override
  void initState() {
    super.initState();
    widget.controller.initialize();
  }

  @override
  void didUpdateWidget(FidibekiFeedbackHost oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.controller != widget.controller) {
      widget.controller.initialize();
    }
  }

  @override
  Widget build(BuildContext context) {
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
    final prompt =
        controller.session?.configuration.definition.localization.prompt ?? '';

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
                child: ColoredBox(
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
