import 'dart:convert';

import 'package:fidibeki_flutter/fidibeki_flutter.dart';
import 'package:fidibeki_flutter_example/main.dart' as example;
import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;

const runLocalSmoke = bool.fromEnvironment('FIDIBEKI_LOCAL_SMOKE');

Future<Map<String, Object?>> evidence() async {
  final response = await http.get(
    Uri.parse('${example.widgetBaseUrl}/evidence'),
  );
  expect(response.statusCode, 200);
  return jsonDecode(response.body) as Map<String, Object?>;
}

void main() {
  test(
    'local host session submits exactly one accepted feedback',
    () async {
      final before = await evidence();
      final storage = MemoryFidibekiStorage();
      final controller = FidibekiFeedbackController(
        apiBaseUrl: Uri.parse(example.widgetBaseUrl),
        appVersion: '0.1.0',
        deviceLocale: 'en-PT',
        locale: const Locale('en'),
        osVersion: 'local-smoke',
        platform: FidibekiPlatform.ios,
        screen: '/checkout/review?step=payment#card',
        sessionProvider: example.createCaptureSession,
        storage: storage,
        appSessionId: 'flutter-local-smoke',
      );

      await controller.open();
      expect(
        controller.session!.configuration.definition.responseBlocks.map(
          (block) => block.type,
        ),
        <FidibekiResponseBlockType>[
          FidibekiResponseBlockType.rating,
          FidibekiResponseBlockType.freeText,
        ],
      );
      controller.setRating('rating', 4);
      controller.setText('feedback', 'Flutter local smoke feedback');
      final accepted = await controller.submit();
      final after = await evidence();

      expect(accepted.feedbackPublicId, 'FDBK-SMOKE');
      expect(after['session'], (before['session']! as int) + 1);
      expect(after['feedback'], (before['feedback']! as int) + 1);
      expect(
        (after['lastFeedback']! as Map<String, Object?>)['responses'],
        containsPair('rating', 4),
      );
      expect(controller.status, FidibekiFeedbackStatus.submitted);
      expect(controller.eligible, isFalse);
      expect(storage.values, hasLength(1));
      controller.dispose();
    },
    skip: !runLocalSmoke,
  );
}
