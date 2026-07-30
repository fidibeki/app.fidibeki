import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:fidibeki_flutter/fidibeki_flutter.dart';
import 'package:flutter/cupertino.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

Map<String, Object?> fixtureJson(String name) {
  var directory = Directory.current.absolute;
  while (!File('${directory.path}/pnpm-workspace.yaml').existsSync()) {
    directory = directory.parent;
  }
  return jsonDecode(
        File(
          '${directory.path}/contracts/mobile-capture/fixtures/$name',
        ).readAsStringSync(),
      )
      as Map<String, Object?>;
}

FidibekiCaptureSession sheetSession() {
  return FidibekiCaptureSession.fromJson(fixtureJson('session-valid.json'));
}

FidibekiCaptureSession longCopySession() {
  final json = fixtureJson('session-valid.json');
  final configuration = json['configuration']! as Map<String, Object?>;
  final definition = configuration['definition']! as Map<String, Object?>;
  final localization = definition['localization']! as Map<String, Object?>;
  localization['prompt'] =
      'Tell us what happened during this checkout experience';
  localization['submit_label'] = 'Send this detailed feedback';
  final optionLabels = localization['option_labels']! as Map<String, Object?>;
  final journey = optionLabels['journey']! as Map<String, Object?>;
  journey['blocked'] =
      'I could not complete this part of the checkout experience';
  return FidibekiCaptureSession.fromJson(json);
}

FidibekiFeedbackController sheetController({
  FidibekiCaptureSession Function()? session,
  FidibekiMobileClient? mobileClient,
}) {
  return FidibekiFeedbackController(
    apiBaseUrl: Uri.parse('https://api.example.test'),
    appVersion: '2.4.0',
    locale: const Locale('en'),
    osVersion: '18.5',
    platform: FidibekiPlatform.ios,
    screen: '/checkout',
    sessionProvider: (_) async => (session ?? sheetSession)(),
    storage: MemoryFidibekiStorage(),
    mobileClient: mobileClient,
  );
}

Future<void> pumpMaterialHost(
  WidgetTester tester,
  FidibekiFeedbackController controller, {
  TextScaler? textScaler,
  bool disableAnimations = false,
}) async {
  await tester.pumpWidget(
    MaterialApp(
      builder: (context, child) {
        final host = FidibekiFeedbackHost(
          controller: controller,
          child: child ?? const SizedBox.shrink(),
        );
        if (textScaler == null && !disableAnimations) return host;
        final media = MediaQuery.of(context);
        return MediaQuery(
          data: media.copyWith(
            disableAnimations: disableAnimations,
            textScaler: textScaler ?? media.textScaler,
          ),
          child: host,
        );
      },
      home: const ColoredBox(color: Color(0xffe8eeec)),
    ),
  );
  await tester.pump();
  await controller.open();
  await tester.pump();
}

void main() {
  setUp(resetFidibekiProcessAppSessionForTesting);

  testWidgets('expands above the keyboard to the remaining safe viewport', (
    tester,
  ) async {
    await tester.binding.setSurfaceSize(const Size(390, 844));
    addTearDown(() => tester.binding.setSurfaceSize(null));
    tester.view.devicePixelRatio = 1;
    addTearDown(() => tester.view.resetDevicePixelRatio());
    final controller = sheetController();
    await pumpMaterialHost(tester, controller);

    tester.view.viewInsets = const FakeViewPadding(bottom: 300);
    addTearDown(() => tester.view.resetViewInsets());
    await tester.pumpAndSettle();

    final sheet = find.byKey(const ValueKey('fidibeki-sheet'));
    expect(tester.getSize(sheet).height, closeTo(544, 1));
    expect(tester.getBottomRight(sheet).dy, closeTo(544, 1));
    expect(tester.takeException(), isNull);
    controller.dispose();
  });

  testWidgets('system back closes the sheet without popping host content', (
    tester,
  ) async {
    final controller = sheetController();
    await pumpMaterialHost(tester, controller);
    expect(controller.visible, isTrue);

    await tester.binding.handlePopRoute();
    await tester.pump();

    expect(controller.visible, isFalse);
    expect(find.byKey(const ValueKey('fidibeki-sheet')), findsNothing);
    controller.dispose();
  });

  testWidgets('focuses the first invalid text response after submit', (
    tester,
  ) async {
    await tester.binding.setSurfaceSize(const Size(320, 568));
    addTearDown(() => tester.binding.setSurfaceSize(null));
    final controller = sheetController();
    await pumpMaterialHost(tester, controller);
    controller.setRating('rating', 5);
    controller.setChoice('journey', 'easy');
    await tester.pump();

    await tester.tap(find.byKey(const ValueKey('fidibeki-submit')));
    await tester.pumpAndSettle();

    final editable = tester.widget<EditableText>(
      find.descendant(
        of: find.byKey(const ValueKey('fidibeki-text-feedback')),
        matching: find.byType(EditableText),
      ),
    );
    expect(editable.focusNode.hasFocus, isTrue);
    expect(controller.fieldErrors, contains('responses.feedback'));
    expect(tester.takeException(), isNull);
    controller.dispose();
  });

  testWidgets('renders and interacts under CupertinoApp', (tester) async {
    final controller = sheetController();
    await tester.pumpWidget(
      CupertinoApp(
        builder: (context, child) => FidibekiFeedbackHost(
          controller: controller,
          child: child ?? const SizedBox.shrink(),
        ),
        home: const ColoredBox(color: CupertinoColors.systemGroupedBackground),
      ),
    );
    await tester.pump();
    await controller.open();
    await tester.pump();

    expect(find.byKey(const ValueKey('fidibeki-sheet')), findsOneWidget);
    await tester.tap(find.byKey(const ValueKey('fidibeki-rating-rating-4')));
    await tester.pump();
    expect(controller.response('rating'), 4);
    expect(tester.takeException(), isNull);
    controller.dispose();
  });

  testWidgets('announces required fields with localized semantics', (
    tester,
  ) async {
    final semantics = tester.ensureSemantics();
    final controller = sheetController();
    await pumpMaterialHost(tester, controller);

    expect(find.bySemanticsLabel(RegExp(r'Rating, Required')), findsOneWidget);
    expect(
      find.bySemanticsLabel(RegExp(r'Checkout journey, Required')),
      findsOneWidget,
    );
    expect(
      find.bySemanticsLabel(RegExp(r'Feedback, Required')),
      findsOneWidget,
    );
    semantics.dispose();
    controller.dispose();
  });

  testWidgets('busy submission blocks every dismiss path and is announced', (
    tester,
  ) async {
    final response = Completer<http.Response>();
    final controller = sheetController(
      mobileClient: FidibekiMobileClient(
        apiBaseUrl: Uri.parse('https://api.example.test'),
        httpClient: MockClient((request) => response.future),
      ),
    );
    final semantics = tester.ensureSemantics();
    await pumpMaterialHost(tester, controller);
    controller.setRating('rating', 5);
    controller.setChoice('journey', 'easy');
    controller.setText('feedback', 'The checkout would not complete');
    await tester.pump();

    await tester.tap(find.byKey(const ValueKey('fidibeki-submit')));
    await tester.pump();

    expect(controller.status, FidibekiFeedbackStatus.submitting);
    expect(
      tester
          .widget<IconButton>(find.byKey(const ValueKey('fidibeki-close')))
          .onPressed,
      isNull,
    );
    expect(
      tester
          .widget<ModalBarrier>(find.byKey(const ValueKey('fidibeki-scrim')))
          .dismissible,
      isFalse,
    );
    expect(find.bySemanticsLabel('Sending...'), findsOneWidget);

    await tester.binding.handlePopRoute();
    await tester.pump();
    expect(controller.visible, isTrue);

    response.complete(
      http.Response(
        jsonEncode(<String, Object?>{
          'feedback_id': 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
          'feedback_public_id': 'FDBK-SHEET',
          'attachments': <Object?>[],
        }),
        201,
      ),
    );
    await tester.pumpAndSettle();
    expect(controller.status, FidibekiFeedbackStatus.submitted);
    semantics.dispose();
    controller.dispose();
  });

  testWidgets('long copy at 200 percent fits a compact phone', (tester) async {
    await tester.binding.setSurfaceSize(const Size(320, 568));
    addTearDown(() => tester.binding.setSurfaceSize(null));
    final controller = sheetController(session: longCopySession);
    await pumpMaterialHost(
      tester,
      controller,
      textScaler: const TextScaler.linear(2),
    );

    expect(find.byKey(const ValueKey('fidibeki-sheet')), findsOneWidget);
    expect(find.byKey(const ValueKey('fidibeki-submit')), findsOneWidget);
    expect(tester.takeException(), isNull);
    controller.dispose();
  });

  testWidgets('rotation preserves a stable responsive sheet', (tester) async {
    await tester.binding.setSurfaceSize(const Size(390, 844));
    addTearDown(() => tester.binding.setSurfaceSize(null));
    final controller = sheetController();
    await pumpMaterialHost(tester, controller);

    await tester.binding.setSurfaceSize(const Size(844, 390));
    await tester.pumpAndSettle();

    final sheet = find.byKey(const ValueKey('fidibeki-sheet'));
    expect(tester.getSize(sheet).height, closeTo(292.5, 1));
    expect(tester.getSize(sheet).width, 844);
    expect(tester.takeException(), isNull);
    controller.dispose();
  });

  testWidgets('reduced motion applies keyboard geometry in one frame', (
    tester,
  ) async {
    await tester.binding.setSurfaceSize(const Size(390, 844));
    addTearDown(() => tester.binding.setSurfaceSize(null));
    tester.view.devicePixelRatio = 1;
    addTearDown(() => tester.view.resetDevicePixelRatio());
    final controller = sheetController();
    await pumpMaterialHost(tester, controller, disableAnimations: true);

    tester.view.viewInsets = const FakeViewPadding(bottom: 300);
    addTearDown(() => tester.view.resetViewInsets());
    await tester.pump();

    final sheet = find.byKey(const ValueKey('fidibeki-sheet'));
    expect(tester.getSize(sheet).height, closeTo(544, 1));
    expect(tester.getBottomRight(sheet).dy, closeTo(544, 1));
    controller.dispose();
  });
}
