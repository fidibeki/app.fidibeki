import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:fidibeki_flutter/fidibeki_flutter.dart';
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

FidibekiCaptureSession screenshotSession() {
  final json = fixtureJson('session-valid.json');
  final configuration = json['configuration']! as Map<String, Object?>;
  final definition = configuration['definition']! as Map<String, Object?>;
  final blocks = definition['response_blocks']! as List<Object?>;
  definition['response_blocks'] = blocks
      .where((raw) {
        final type = (raw! as Map<String, Object?>)['type'];
        return type == 'rating' || type == 'free_text';
      })
      .toList(growable: false);
  definition['allow_comment'] = false;
  return FidibekiCaptureSession.fromJson(json);
}

Finder formScroll() {
  return find
      .descendant(
        of: find.byKey(const ValueKey('fidibeki-form-scroll')),
        matching: find.byType(Scrollable),
      )
      .first;
}

Future<void> renderController(
  WidgetTester tester,
  FidibekiFeedbackController controller,
) async {
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
}

void main() {
  testWidgets('invokes the host provider only after an explicit action', (
    tester,
  ) async {
    var providerCalls = 0;
    final controller = FidibekiFeedbackController(
      apiBaseUrl: Uri.parse('https://api.example.test'),
      appVersion: '2.4.0',
      locale: const Locale('en'),
      osVersion: '18.5',
      platform: FidibekiPlatform.ios,
      screen: '/checkout',
      sessionProvider: (_) async => screenshotSession(),
      screenshotProvider: () async {
        providerCalls += 1;
        return null;
      },
      storage: MemoryFidibekiStorage(),
    );

    await renderController(tester, controller);
    expect(providerCalls, 0);
    await tester.scrollUntilVisible(
      find.byKey(const ValueKey('fidibeki-screenshot-action')),
      120,
      scrollable: formScroll(),
    );
    await tester.tap(find.byKey(const ValueKey('fidibeki-screenshot-action')));
    await tester.pump();

    expect(providerCalls, 1);
    expect(find.text('Screenshot selection cancelled'), findsOneWidget);
  });

  testWidgets('keeps host provider failures private', (tester) async {
    final controller = FidibekiFeedbackController(
      apiBaseUrl: Uri.parse('https://api.example.test'),
      appVersion: '2.4.0',
      locale: const Locale('en'),
      osVersion: '18.5',
      platform: FidibekiPlatform.ios,
      screen: '/checkout',
      sessionProvider: (_) async => screenshotSession(),
      screenshotProvider: () async {
        throw StateError('private photo permission failure');
      },
      storage: MemoryFidibekiStorage(),
    );

    await renderController(tester, controller);
    await tester.scrollUntilVisible(
      find.byKey(const ValueKey('fidibeki-screenshot-action')),
      120,
      scrollable: formScroll(),
    );
    await tester.tap(find.byKey(const ValueKey('fidibeki-screenshot-action')));
    await tester.pump();

    expect(find.text('The app could not provide a screenshot'), findsOneWidget);
    expect(
      find.textContaining('private photo permission failure'),
      findsNothing,
    );
  });

  testWidgets('preserves a prepared screenshot across close and releases it', (
    tester,
  ) async {
    var disposals = 0;
    final controller = FidibekiFeedbackController(
      apiBaseUrl: Uri.parse('https://api.example.test'),
      appVersion: '2.4.0',
      locale: const Locale('en'),
      osVersion: '18.5',
      platform: FidibekiPlatform.android,
      screen: '/checkout',
      sessionProvider: (_) async => screenshotSession(),
      screenshotProvider: () async => FidibekiScreenshotAsset(
        contentType: 'image/png',
        sizeBytes: 3,
        read: (_) async => Uint8List.fromList(<int>[1, 2, 3]),
        dispose: () async {
          disposals += 1;
        },
      ),
      clientAttachmentIdGenerator: () => '88888888-8888-4888-8888-888888888888',
      storage: MemoryFidibekiStorage(),
    );

    await renderController(tester, controller);
    await tester.scrollUntilVisible(
      find.byKey(const ValueKey('fidibeki-screenshot-action')),
      120,
      scrollable: formScroll(),
    );
    await tester.tap(find.byKey(const ValueKey('fidibeki-screenshot-action')));
    await tester.pump();
    expect(
      find.byKey(const ValueKey('fidibeki-screenshot-attached')),
      findsOneWidget,
    );

    controller.close();
    await tester.pump();
    await controller.open();
    await tester.pump();
    await tester.scrollUntilVisible(
      find.byKey(const ValueKey('fidibeki-screenshot-attached')),
      120,
      scrollable: formScroll(),
    );
    expect(
      find.byKey(const ValueKey('fidibeki-screenshot-attached')),
      findsOneWidget,
    );

    await tester.tap(find.byKey(const ValueKey('fidibeki-screenshot-remove')));
    await tester.pump();
    expect(disposals, 1);
    expect(
      find.byKey(const ValueKey('fidibeki-screenshot-action')),
      findsOneWidget,
    );
  });

  testWidgets('keeps accepted success visible when screenshot upload fails', (
    tester,
  ) async {
    final fixture = fixtureJson('feedback-submit-screenshot.json');
    var requests = 0;
    final controller = FidibekiFeedbackController(
      apiBaseUrl: Uri.parse('https://api.example.test'),
      appVersion: '2.4.0',
      locale: const Locale('en'),
      osVersion: '18.5',
      platform: FidibekiPlatform.ios,
      screen: '/checkout',
      sessionProvider: (_) async => screenshotSession(),
      screenshotProvider: () async => FidibekiScreenshotAsset(
        contentType: 'image/png',
        sizeBytes: 3,
        read: (_) async => Uint8List.fromList(<int>[1, 2, 3]),
      ),
      clientAttachmentIdGenerator: () => '88888888-8888-4888-8888-888888888888',
      feedbackIdGenerator: () => '99999999-9999-4999-8999-999999999999',
      storage: MemoryFidibekiStorage(),
      mobileClient: FidibekiMobileClient(
        apiBaseUrl: Uri.parse('https://api.example.test'),
        httpClient: MockClient((request) async {
          requests += 1;
          if (requests == 1) {
            return http.Response(jsonEncode(fixture['accepted']), 201);
          }
          return http.Response('', 503);
        }),
      ),
    );

    await renderController(tester, controller);
    controller.setRating('rating', 5);
    controller.setText('feedback', 'The receipt is difficult to read');
    await tester.scrollUntilVisible(
      find.byKey(const ValueKey('fidibeki-screenshot-action')),
      120,
      scrollable: formScroll(),
    );
    await tester.tap(find.byKey(const ValueKey('fidibeki-screenshot-action')));
    await tester.pump();
    await tester.tap(find.byKey(const ValueKey('fidibeki-submit')));
    await tester.pumpAndSettle();

    expect(controller.status, FidibekiFeedbackStatus.submitted);
    expect(find.text('Thanks for the feedback.'), findsOneWidget);
    expect(
      find.text('Feedback was sent, but the screenshot could not be uploaded.'),
      findsOneWidget,
    );
    expect(requests, 2);
  });

  test(
    'ambiguous feedback retry preserves the exact screenshot declaration',
    () async {
      final fixture = fixtureJson('feedback-submit-screenshot.json');
      final feedbackBodies = <String>[];
      var requests = 0;
      var disposals = 0;
      final controller = FidibekiFeedbackController(
        apiBaseUrl: Uri.parse('https://api.example.test'),
        appVersion: '2.4.0',
        locale: const Locale('en'),
        osVersion: '18.5',
        platform: FidibekiPlatform.android,
        screen: '/checkout',
        sessionProvider: (_) async => screenshotSession(),
        screenshotProvider: () async => FidibekiScreenshotAsset(
          contentType: 'image/png',
          sizeBytes: 3,
          read: (_) async => Uint8List.fromList(<int>[1, 2, 3]),
          dispose: () async {
            disposals += 1;
          },
        ),
        clientAttachmentIdGenerator: () =>
            '88888888-8888-4888-8888-888888888888',
        feedbackIdGenerator: () => '99999999-9999-4999-8999-999999999999',
        storage: MemoryFidibekiStorage(),
        mobileClient: FidibekiMobileClient(
          apiBaseUrl: Uri.parse('https://api.example.test'),
          httpClient: MockClient((request) async {
            requests += 1;
            if (request.url.path == '/v1/mobile/feedback') {
              feedbackBodies.add(request.body);
              if (feedbackBodies.length == 1) {
                throw http.ClientException('ambiguous transport failure');
              }
              return http.Response(jsonEncode(fixture['accepted']), 201);
            }
            return http.Response('', 200);
          }),
        ),
      );

      await controller.open();
      controller.setRating('rating', 5);
      controller.setText('feedback', 'The receipt is difficult to read');
      await controller.addScreenshot();

      await expectLater(
        controller.submit(),
        throwsA(
          isA<FidibekiException>().having(
            (error) => error.code,
            'code',
            FidibekiErrorCode.network,
          ),
        ),
      );
      expect(controller.hasScreenshot, isTrue);

      await controller.submit();

      expect(feedbackBodies, hasLength(2));
      expect(feedbackBodies[1], feedbackBodies[0]);
      expect(
        (jsonDecode(feedbackBodies.first)
            as Map<String, Object?>)['screenshot'],
        (fixture['request']! as Map<String, Object?>)['screenshot'],
      );
      expect(controller.status, FidibekiFeedbackStatus.submitted);
      expect(disposals, 1);
      expect(requests, 4);
    },
  );
}
