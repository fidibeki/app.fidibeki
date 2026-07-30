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

Map<String, Object?> acceptedJson() {
  return <String, Object?>{
    'feedback_id': '66666666-6666-4666-8666-666666666666',
    'feedback_public_id': 'FDBK-CONSENT',
    'attachments': <Object?>[],
  };
}

void main() {
  test(
    'submits every dynamic response with versioned reporter consent',
    () async {
      final session = FidibekiCaptureSession.fromJson(
        fixtureJson('session-contact-valid.json'),
      );
      final expected = fixtureJson('feedback-submit-consent.json');
      late Map<String, Object?> wireRequest;
      final controller = FidibekiFeedbackController(
        apiBaseUrl: Uri.parse('https://api.example.test'),
        appVersion: '2.4.0',
        deviceLocale: 'pt-PT',
        locale: const Locale('pt', 'PT'),
        osVersion: '18.5',
        platform: FidibekiPlatform.ios,
        screen: '/checkout',
        sessionProvider: (_) async => session,
        storage: MemoryFidibekiStorage(),
        feedbackIdGenerator: () => '55555555-5555-4555-8555-555555555555',
        mobileClient: FidibekiMobileClient(
          apiBaseUrl: Uri.parse('https://api.example.test'),
          httpClient: MockClient((request) async {
            wireRequest = jsonDecode(request.body) as Map<String, Object?>;
            return http.Response(jsonEncode(acceptedJson()), 201);
          }),
        ),
      );

      await controller.open();
      controller.setRating('rating', 5);
      controller.setChoice('reaction', 'happy');
      controller.setChoice('journey', 'easy');
      controller.setText('feedback', 'O pagamento foi simples');
      controller.setComment('Usei a aplicação em modo escuro');
      controller.setReporterEmail('reporter@example.test');
      controller.setConsentGranted(true);
      await controller.submit();

      expect(wireRequest, expected);
      expect(session.configuration.contactNotice!.purposeSummary, isNotEmpty);
      expect(session.configuration.definition.aiProcessing!.enabled, isTrue);
    },
  );

  test(
    'configuration refresh reconciles answers and clears versioned consent',
    () async {
      final first = fixtureJson('session-contact-valid.json');
      final second = fixtureJson('session-contact-valid.json');
      second['token'] = 'fmc_refreshed_contact_not_a_real_bearer_0000';
      final secondConfiguration =
          second['configuration']! as Map<String, Object?>;
      secondConfiguration['display_locale'] = 'en';
      final secondDefinition =
          secondConfiguration['definition']! as Map<String, Object?>;
      secondDefinition['definition_version'] = 3;
      final blocks = secondDefinition['response_blocks']! as List<Object?>;
      secondDefinition['response_blocks'] = blocks
          .where((raw) => (raw! as Map<String, Object?>)['key'] != 'reaction')
          .map((raw) {
            final block = raw! as Map<String, Object?>;
            if (block['key'] == 'journey') {
              return <String, Object?>{
                ...block,
                'options': <Object?>[
                  <String, Object?>{'value': 'blocked'},
                  <String, Object?>{'value': 'other'},
                ],
              };
            }
            return block;
          })
          .toList(growable: false);
      final notice =
          secondConfiguration['contact_notice']! as Map<String, Object?>;
      notice['notice_version'] = '2026-08-controller-v2';
      notice['widget_definition_version'] = 3;
      notice['locale'] = 'en';

      var sessionRequests = 0;
      var feedbackRequests = 0;
      final wireRequests = <Map<String, Object?>>[];
      var feedbackSequence = 0;
      final controller = FidibekiFeedbackController(
        apiBaseUrl: Uri.parse('https://api.example.test'),
        appVersion: '2.4.0',
        locale: const Locale('pt', 'PT'),
        osVersion: '18.5',
        platform: FidibekiPlatform.ios,
        screen: '/checkout',
        sessionProvider: (_) async {
          sessionRequests += 1;
          return FidibekiCaptureSession.fromJson(
            sessionRequests == 1 ? first : second,
          );
        },
        storage: MemoryFidibekiStorage(),
        feedbackIdGenerator: () {
          feedbackSequence += 1;
          return feedbackSequence == 1
              ? '55555555-5555-4555-8555-555555555555'
              : '77777777-7777-4777-8777-777777777777';
        },
        mobileClient: FidibekiMobileClient(
          apiBaseUrl: Uri.parse('https://api.example.test'),
          httpClient: MockClient((request) async {
            feedbackRequests += 1;
            wireRequests.add(jsonDecode(request.body) as Map<String, Object?>);
            if (feedbackRequests == 1) {
              return http.Response(
                jsonEncode(<String, Object?>{
                  'error': <String, Object?>{
                    'code': 'configuration_changed',
                    'message': 'definition or notice changed',
                  },
                }),
                409,
              );
            }
            return http.Response(jsonEncode(acceptedJson()), 201);
          }),
        ),
      );

      await controller.open();
      controller.setRating('rating', 5);
      controller.setChoice('reaction', 'happy');
      controller.setChoice('journey', 'easy');
      controller.setText('feedback', 'Keep this answer');
      controller.setComment('Keep this context');
      controller.setReporterEmail('reporter@example.test');
      controller.setConsentGranted(true);

      await expectLater(
        controller.submit(),
        throwsA(
          isA<FidibekiException>().having(
            (error) => error.code,
            'code',
            FidibekiErrorCode.configurationChanged,
          ),
        ),
      );

      expect(sessionRequests, 2);
      expect(controller.response('rating'), 5);
      expect(controller.response('feedback'), 'Keep this answer');
      expect(controller.response('reaction'), isNull);
      expect(controller.response('journey'), isNull);
      expect(controller.comment, 'Keep this context');
      expect(controller.reporterEmail, 'reporter@example.test');
      expect(controller.consentGranted, isFalse);
      expect(controller.visible, isTrue);
      expect(controller.status, FidibekiFeedbackStatus.open);

      controller.setChoice('journey', 'blocked');
      controller.setConsentGranted(true);
      await controller.submit();

      expect(wireRequests, hasLength(2));
      expect(
        wireRequests.last,
        containsPair(
          'client_feedback_id',
          '77777777-7777-4777-8777-777777777777',
        ),
      );
      final refreshedConsent =
          wireRequests.last['consent']! as Map<String, Object?>;
      expect(refreshedConsent['notice_version'], '2026-08-controller-v2');
      expect(refreshedConsent['widget_definition_version'], 3);
      expect(refreshedConsent['locale'], 'en');
    },
  );

  test('unavailable comment and contact never enter the request', () async {
    final json = fixtureJson('session-contact-valid.json');
    final configuration = json['configuration']! as Map<String, Object?>;
    final definition = configuration['definition']! as Map<String, Object?>;
    definition['allow_comment'] = false;
    definition['collect_contact'] = false;
    configuration.remove('contact_notice');
    late Map<String, Object?> wireRequest;
    final controller = FidibekiFeedbackController(
      apiBaseUrl: Uri.parse('https://api.example.test'),
      appVersion: '2.4.0',
      locale: const Locale('pt', 'PT'),
      osVersion: '18.5',
      platform: FidibekiPlatform.ios,
      screen: '/checkout',
      sessionProvider: (_) async => FidibekiCaptureSession.fromJson(json),
      storage: MemoryFidibekiStorage(),
      mobileClient: FidibekiMobileClient(
        apiBaseUrl: Uri.parse('https://api.example.test'),
        httpClient: MockClient((request) async {
          wireRequest = jsonDecode(request.body) as Map<String, Object?>;
          return http.Response(jsonEncode(acceptedJson()), 201);
        }),
      ),
    );

    await controller.open();
    controller.setRating('rating', 5);
    controller.setChoice('reaction', 'happy');
    controller.setChoice('journey', 'easy');
    controller.setText('feedback', 'Visible response');
    controller.setComment('Must stay hidden');
    controller.setReporterEmail('hidden@example.test');
    controller.setConsentGranted(true);
    await controller.submit();

    expect(wireRequest, isNot(contains('comment')));
    expect(wireRequest, isNot(contains('reporter_email')));
    expect(wireRequest, isNot(contains('notify_reporter')));
    expect(wireRequest, isNot(contains('consent')));
  });

  test('invalid reporter contact is rejected before network access', () async {
    var requests = 0;
    final controller = FidibekiFeedbackController(
      apiBaseUrl: Uri.parse('https://api.example.test'),
      appVersion: '2.4.0',
      locale: const Locale('en'),
      osVersion: '18.5',
      platform: FidibekiPlatform.android,
      screen: '/checkout',
      sessionProvider: (_) async => FidibekiCaptureSession.fromJson(
        fixtureJson('session-contact-valid.json'),
      ),
      storage: MemoryFidibekiStorage(),
      mobileClient: FidibekiMobileClient(
        apiBaseUrl: Uri.parse('https://api.example.test'),
        httpClient: MockClient((request) async {
          requests += 1;
          return http.Response(jsonEncode(acceptedJson()), 201);
        }),
      ),
    );

    await controller.open();
    controller.setRating('rating', 5);
    controller.setChoice('journey', 'easy');
    controller.setText('feedback', 'The visible fields are complete');
    controller.setReporterEmail('not-an-email');

    await expectLater(
      controller.submit(),
      throwsA(
        isA<FidibekiException>().having(
          (error) => error.fields.keys,
          'fields',
          containsAll(<String>['reporter_email', 'consent']),
        ),
      ),
    );
    expect(requests, 0);
  });
}
