import 'dart:convert';
import 'dart:io';

import 'package:fidibeki_flutter/fidibeki_flutter.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

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

  testWidgets('submits a numeric star rating and required feedback text', (
    tester,
  ) async {
    late Map<String, Object?> wireRequest;
    final controller = FidibekiFeedbackController(
      apiBaseUrl: Uri.parse('https://api.example.test'),
      appVersion: '2.4.0',
      deviceLocale: 'pt-PT',
      locale: const Locale('en'),
      osVersion: '18.5',
      platform: FidibekiPlatform.android,
      screen: '/checkout/review',
      sessionProvider: (_) async => fixtureSession(),
      storage: MemoryFidibekiStorage(),
      mobileClient: FidibekiMobileClient(
        apiBaseUrl: Uri.parse('https://api.example.test'),
        httpClient: MockClient((request) async {
          wireRequest = jsonDecode(request.body) as Map<String, Object?>;
          return http.Response(
            jsonEncode(<String, Object?>{
              'feedback_id': '44444444-4444-4444-8444-444444444444',
              'feedback_public_id': 'FDBK-WIDGET',
              'attachments': <Object?>[],
            }),
            201,
          );
        }),
      ),
      feedbackIdGenerator: () => '33333333-3333-4333-8333-333333333333',
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
    await controller.open();
    await tester.pump();

    await tester.tap(find.byKey(const ValueKey('fidibeki-rating-rating-4')));
    await tester.enterText(
      find.byKey(const ValueKey('fidibeki-text-feedback')),
      'The chart labels are difficult to read',
    );
    await tester.tap(find.byKey(const ValueKey('fidibeki-submit')));
    await tester.pumpAndSettle();

    expect(wireRequest['responses'], containsPair('rating', 4));
    expect(controller.status, FidibekiFeedbackStatus.submitted);
    expect(find.text('Thanks for the feedback.'), findsOneWidget);
  });
}
