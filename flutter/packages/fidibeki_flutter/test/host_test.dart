import 'dart:convert';
import 'dart:io';

import 'package:fidibeki_flutter/fidibeki_flutter.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

FidibekiCaptureSession fixtureSession() {
  var directory = Directory.current.absolute;
  while (!File('${directory.path}/pnpm-workspace.yaml').existsSync()) {
    directory = directory.parent;
  }
  final json =
      jsonDecode(
            File(
              '${directory.path}/contracts/mobile-capture/fixtures/session-valid.json',
            ).readAsStringSync(),
          )
          as Map<String, Object?>;
  return FidibekiCaptureSession.fromJson(json);
}

void main() {
  testWidgets('opens, closes, and reopens with one capture session', (
    tester,
  ) async {
    var sessionRequests = 0;
    final storage = MemoryFidibekiStorage();
    final controller = FidibekiFeedbackController(
      apiBaseUrl: Uri.parse('https://widget.example.test'),
      appVersion: '1.0.0',
      deviceLocale: 'en-PT',
      locale: const Locale('en'),
      osVersion: 'test',
      platform: FidibekiPlatform.ios,
      screen: '/chart?asset=BAI',
      sessionProvider: (request) async {
        sessionRequests += 1;
        expect(request.screen, '/chart');
        return fixtureSession();
      },
      storage: storage,
    );

    await tester.pumpWidget(
      MaterialApp(
        builder: (context, child) => FidibekiFeedbackHost(
          controller: controller,
          child: child ?? const SizedBox.shrink(),
        ),
        home: const SizedBox.expand(),
      ),
    );
    await tester.pump();

    expect(controller.status, FidibekiFeedbackStatus.ready);
    expect(controller.eligible, isTrue);
    expect(sessionRequests, 1);

    await controller.open();
    await tester.pump();
    expect(find.byKey(const ValueKey('fidibeki-sheet')), findsOneWidget);
    expect(controller.status, FidibekiFeedbackStatus.open);

    controller.close();
    await tester.pump();
    expect(find.byKey(const ValueKey('fidibeki-sheet')), findsNothing);
    expect(controller.status, FidibekiFeedbackStatus.ready);
    expect(controller.eligible, isTrue);
    expect(storage.values, isEmpty);

    await controller.open();
    await tester.pump();
    expect(find.byKey(const ValueKey('fidibeki-sheet')), findsOneWidget);
    expect(sessionRequests, 1);

    controller.dispose();
  });
}
