import 'dart:convert';
import 'dart:io';

import 'package:fidibeki_flutter/fidibeki_flutter.dart';
import 'package:flutter/widgets.dart';
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

FidibekiCaptureSession ratingAndTextSession({String displayLocale = 'en'}) {
  final session = fixtureJson('session-valid.json');
  final configuration = session['configuration']! as Map<String, Object?>;
  configuration['display_locale'] = displayLocale;
  final definition = configuration['definition']! as Map<String, Object?>;
  final blocks = definition['response_blocks']! as List<Object?>;
  definition['response_blocks'] = blocks
      .where((raw) {
        final block = raw! as Map<String, Object?>;
        return block['type'] == 'rating' || block['type'] == 'free_text';
      })
      .toList(growable: false);
  return FidibekiCaptureSession.fromJson(session);
}

void main() {
  test(
    'accepted rating and text suppress recurrence only after backend acceptance',
    () async {
      final acceptedFixture = fixtureJson('feedback-submit-valid.json');
      final storage = MemoryFidibekiStorage();
      late Map<String, Object?> wireRequest;
      final mobileClient = FidibekiMobileClient(
        apiBaseUrl: Uri.parse('https://api.example.test'),
        httpClient: MockClient((request) async {
          wireRequest = jsonDecode(request.body) as Map<String, Object?>;
          expect(storage.values, isEmpty);
          return http.Response(jsonEncode(acceptedFixture['accepted']), 201);
        }),
      );
      final controller = FidibekiFeedbackController(
        apiBaseUrl: Uri.parse('https://api.example.test'),
        appVersion: '2.4.0',
        deviceLocale: 'pt-PT',
        locale: const Locale('en'),
        osVersion: '18.5',
        platform: FidibekiPlatform.ios,
        screen: '/chart?asset=BAI',
        sessionProvider: (_) async => ratingAndTextSession(),
        storage: storage,
        appSessionId: 'test-app-session',
        mobileClient: mobileClient,
        feedbackIdGenerator: () => '33333333-3333-4333-8333-333333333333',
      );

      await controller.open();
      controller.setRating('rating', 4);
      controller.setText('feedback', 'The chart labels are difficult to read');
      final accepted = await controller.submit();

      expect(accepted.feedbackPublicId, 'FDBK-FIXTURE');
      expect(wireRequest['responses'], <String, Object>{
        'rating': 4,
        'feedback': 'The chart labels are difficult to read',
      });
      expect(controller.status, FidibekiFeedbackStatus.submitted);
      expect(controller.eligible, isFalse);
      expect(controller.visible, isTrue);
      expect(storage.values.values.single, contains('"state":"submitted"'));
    },
  );

  test('required validation never reaches the backend or recurrence', () async {
    final storage = MemoryFidibekiStorage();
    var requests = 0;
    final controller = FidibekiFeedbackController(
      apiBaseUrl: Uri.parse('https://api.example.test'),
      appVersion: '2.4.0',
      locale: const Locale('en'),
      osVersion: '18.5',
      platform: FidibekiPlatform.android,
      screen: '/chart/',
      sessionProvider: (_) async => ratingAndTextSession(),
      storage: storage,
      mobileClient: FidibekiMobileClient(
        apiBaseUrl: Uri.parse('https://api.example.test'),
        httpClient: MockClient((request) async {
          requests += 1;
          return http.Response('{}', 500);
        }),
      ),
    );

    await controller.open();
    await expectLater(
      controller.submit(),
      throwsA(
        isA<FidibekiException>()
            .having((error) => error.code, 'code', FidibekiErrorCode.validation)
            .having(
              (error) => error.fields.keys,
              'fields',
              containsAll(<String>['responses.rating', 'responses.feedback']),
            ),
      ),
    );

    expect(requests, 0);
    expect(storage.values, isEmpty);
    expect(controller.status, FidibekiFeedbackStatus.open);
    expect(controller.eligible, isTrue);
  });

  test('required validation follows the resolved Portuguese locale', () async {
    final controller = FidibekiFeedbackController(
      apiBaseUrl: Uri.parse('https://api.example.test'),
      appVersion: '2.4.0',
      locale: const Locale('pt', 'PT'),
      osVersion: '18.5',
      platform: FidibekiPlatform.ios,
      screen: '/chart',
      sessionProvider: (_) async =>
          ratingAndTextSession(displayLocale: 'pt-PT'),
      storage: MemoryFidibekiStorage(),
    );

    await controller.open();
    await expectLater(controller.submit(), throwsA(isA<FidibekiException>()));

    expect(
      controller.fieldErrors,
      containsPair('responses.rating', 'Selecione uma classificação.'),
    );
    expect(
      controller.fieldErrors,
      containsPair('responses.feedback', 'Escreva o seu feedback.'),
    );
  });
}
