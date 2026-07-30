import 'dart:async';
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

FidibekiCaptureSession lifecycleSession({
  String token = 'fmc_lifecycle_first_fake_token_000000000',
  FidibekiRecurrenceMode recurrenceMode = FidibekiRecurrenceMode.session,
  int? recurrenceDays,
}) {
  final json = fixtureJson('session-valid.json');
  json['token'] = token;
  final configuration = json['configuration']! as Map<String, Object?>;
  final definition = configuration['definition']! as Map<String, Object?>;
  final blocks = definition['response_blocks']! as List<Object?>;
  definition['response_blocks'] = blocks
      .where((raw) {
        final type = (raw! as Map<String, Object?>)['type'];
        return type == 'rating' || type == 'free_text';
      })
      .toList(growable: false);
  definition['recurrence_mode'] = recurrenceMode.wireName;
  definition['recurrence_days'] =
      recurrenceMode == FidibekiRecurrenceMode.cooldown
      ? recurrenceDays ?? 1
      : null;
  return FidibekiCaptureSession.fromJson(json);
}

Map<String, Object?> acceptedJson() {
  return <String, Object?>{
    'feedback_id': 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
    'feedback_public_id': 'FDBK-LIFECYCLE',
    'attachments': <Object?>[],
  };
}

final class WriteFailingStorage implements FidibekiStorage {
  @override
  Future<void> delete(String key) async {}

  @override
  Future<String?> read(String key) async => null;

  @override
  Future<void> write(String key, String value) async {
    throw StateError('private storage failure');
  }
}

void fillRequired(FidibekiFeedbackController controller) {
  controller.setRating('rating', 5);
  controller.setText('feedback', 'Lifecycle test feedback');
}

void main() {
  setUp(resetFidibekiProcessAppSessionForTesting);

  test(
    'storage failure after acceptance cannot reopen or duplicate feedback',
    () async {
      var requests = 0;
      final controller = FidibekiFeedbackController(
        apiBaseUrl: Uri.parse('https://api.example.test'),
        appVersion: '2.4.0',
        locale: const Locale('en'),
        osVersion: '18.5',
        platform: FidibekiPlatform.ios,
        screen: '/checkout',
        sessionProvider: (_) async => lifecycleSession(),
        storage: WriteFailingStorage(),
        mobileClient: FidibekiMobileClient(
          apiBaseUrl: Uri.parse('https://api.example.test'),
          httpClient: MockClient((request) async {
            requests += 1;
            return http.Response(jsonEncode(acceptedJson()), 201);
          }),
        ),
      );

      await controller.open();
      fillRequired(controller);
      await controller.submit();

      expect(controller.status, FidibekiFeedbackStatus.submitted);
      expect(controller.eligible, isFalse);
      expect(controller.accepted?.feedbackPublicId, 'FDBK-LIFECYCLE');
      await expectLater(
        controller.submit(),
        throwsA(
          isA<FidibekiException>().having(
            (error) => error.code,
            'code',
            FidibekiErrorCode.accessDenied,
          ),
        ),
      );
      expect(requests, 1);
    },
  );

  test(
    'rotates implicit session only after 30 continuous background minutes',
    () async {
      var now = DateTime.utc(2026, 7, 30, 12);
      var sessionRequests = 0;
      final controller = FidibekiFeedbackController(
        apiBaseUrl: Uri.parse('https://api.example.test'),
        appVersion: '2.4.0',
        locale: const Locale('en'),
        osVersion: '18.5',
        platform: FidibekiPlatform.android,
        screen: '/checkout',
        now: () => now,
        sessionProvider: (_) async {
          sessionRequests += 1;
          return lifecycleSession();
        },
        storage: MemoryFidibekiStorage(),
      );

      await controller.open();
      fillRequired(controller);
      await controller.handleAppLifecycleState(AppLifecycleState.paused);
      now = now.add(
        backgroundSessionResetDuration - const Duration(seconds: 1),
      );
      await controller.handleAppLifecycleState(AppLifecycleState.resumed);
      expect(sessionRequests, 1);
      expect(controller.visible, isTrue);

      await controller.handleAppLifecycleState(AppLifecycleState.paused);
      now = now.add(backgroundSessionResetDuration);
      await controller.handleAppLifecycleState(AppLifecycleState.resumed);

      expect(sessionRequests, 2);
      expect(controller.status, FidibekiFeedbackStatus.ready);
      expect(controller.visible, isFalse);
      expect(controller.responses, isEmpty);
    },
  );

  test(
    'rechecks a cooldown when foregrounded after eligibility time',
    () async {
      var now = DateTime.utc(2026, 7, 30);
      final storage = MemoryFidibekiStorage();
      final session = lifecycleSession(
        recurrenceMode: FidibekiRecurrenceMode.cooldown,
        recurrenceDays: 1,
      );
      final engine = FidibekiRecurrenceEngine(
        storage,
        appSessionId: 'host-session',
        now: () => now,
      );
      await engine.recordSubmitted(
        session.configuration.projectId,
        session.configuration.definition,
        '/checkout',
      );
      var sessionRequests = 0;
      final controller = FidibekiFeedbackController(
        apiBaseUrl: Uri.parse('https://api.example.test'),
        appVersion: '2.4.0',
        locale: const Locale('en'),
        osVersion: '18.5',
        platform: FidibekiPlatform.ios,
        screen: '/checkout',
        appSessionId: 'host-session',
        now: () => now,
        sessionProvider: (_) async {
          sessionRequests += 1;
          return lifecycleSession(
            recurrenceMode: FidibekiRecurrenceMode.cooldown,
            recurrenceDays: 1,
          );
        },
        storage: storage,
      );

      await controller.initialize();
      expect(controller.status, FidibekiFeedbackStatus.ineligible);
      expect(controller.nextEligibleAt, DateTime.utc(2026, 7, 31));

      now = now.add(const Duration(days: 1));
      await controller.handleAppLifecycleState(AppLifecycleState.resumed);

      expect(sessionRequests, 2);
      expect(controller.status, FidibekiFeedbackStatus.ready);
      expect(controller.eligible, isTrue);
    },
  );

  test(
    'replaces an access-denied session and never reuses its token',
    () async {
      var sessionRequests = 0;
      final authorizations = <String?>[];
      var feedbackRequests = 0;
      final controller = FidibekiFeedbackController(
        apiBaseUrl: Uri.parse('https://api.example.test'),
        appVersion: '2.4.0',
        locale: const Locale('en'),
        osVersion: '18.5',
        platform: FidibekiPlatform.android,
        screen: '/checkout',
        sessionProvider: (_) async {
          sessionRequests += 1;
          return lifecycleSession(
            token: sessionRequests == 1
                ? 'fmc_lifecycle_first_fake_token_000000000'
                : 'fmc_lifecycle_second_fake_token_00000000',
          );
        },
        storage: MemoryFidibekiStorage(),
        mobileClient: FidibekiMobileClient(
          apiBaseUrl: Uri.parse('https://api.example.test'),
          httpClient: MockClient((request) async {
            feedbackRequests += 1;
            authorizations.add(request.headers['authorization']);
            if (feedbackRequests == 1) {
              return http.Response(
                jsonEncode(<String, Object?>{
                  'error': <String, Object?>{
                    'code': 'access_denied',
                    'message': 'denied',
                  },
                }),
                403,
              );
            }
            return http.Response(jsonEncode(acceptedJson()), 201);
          }),
        ),
      );

      await controller.open();
      fillRequired(controller);
      await expectLater(
        controller.submit(),
        throwsA(
          isA<FidibekiException>().having(
            (error) => error.code,
            'code',
            FidibekiErrorCode.accessDenied,
          ),
        ),
      );

      expect(sessionRequests, 2);
      expect(controller.status, FidibekiFeedbackStatus.open);
      expect(controller.visible, isTrue);
      expect(controller.responses, isEmpty);

      fillRequired(controller);
      await controller.submit();

      expect(authorizations, hasLength(2));
      expect(authorizations[0], contains('first_fake_token'));
      expect(authorizations[1], contains('second_fake_token'));
    },
  );

  test(
    'failed configuration refresh cannot resubmit the stale session',
    () async {
      var sessionRequests = 0;
      var feedbackRequests = 0;
      final controller = FidibekiFeedbackController(
        apiBaseUrl: Uri.parse('https://api.example.test'),
        appVersion: '2.4.0',
        locale: const Locale('en'),
        osVersion: '18.5',
        platform: FidibekiPlatform.ios,
        screen: '/checkout',
        sessionProvider: (_) async {
          sessionRequests += 1;
          if (sessionRequests > 1) {
            throw StateError('session mint unavailable');
          }
          return lifecycleSession();
        },
        storage: MemoryFidibekiStorage(),
        mobileClient: FidibekiMobileClient(
          apiBaseUrl: Uri.parse('https://api.example.test'),
          httpClient: MockClient((request) async {
            feedbackRequests += 1;
            return http.Response(
              jsonEncode(<String, Object?>{
                'error': <String, Object?>{
                  'code': 'configuration_changed',
                  'message': 'changed',
                },
              }),
              409,
            );
          }),
        ),
      );

      await controller.open();
      fillRequired(controller);
      await expectLater(controller.submit(), throwsA(isA<FidibekiException>()));
      await expectLater(controller.submit(), throwsA(isA<FidibekiException>()));

      expect(sessionRequests, greaterThanOrEqualTo(2));
      expect(feedbackRequests, 1);
    },
  );

  test(
    'late session acquisition cannot mutate a disposed controller',
    () async {
      final session = Completer<FidibekiCaptureSession>();
      final controller = FidibekiFeedbackController(
        apiBaseUrl: Uri.parse('https://api.example.test'),
        appVersion: '2.4.0',
        locale: const Locale('en'),
        osVersion: '18.5',
        platform: FidibekiPlatform.ios,
        screen: '/checkout',
        sessionProvider: (_) => session.future,
        storage: MemoryFidibekiStorage(),
      );

      final initialization = controller.initialize();
      controller.dispose();
      session.complete(lifecycleSession());
      await initialization;

      expect(controller.session, isNull);
      expect(controller.status, FidibekiFeedbackStatus.initializing);
    },
  );

  test('initialization can retry after a transient session failure', () async {
    var sessionRequests = 0;
    final controller = FidibekiFeedbackController(
      apiBaseUrl: Uri.parse('https://api.example.test'),
      appVersion: '2.4.0',
      locale: const Locale('en'),
      osVersion: '18.5',
      platform: FidibekiPlatform.ios,
      screen: '/checkout',
      sessionProvider: (_) async {
        sessionRequests += 1;
        if (sessionRequests == 1) {
          throw StateError('session mint unavailable');
        }
        return lifecycleSession();
      },
      storage: MemoryFidibekiStorage(),
    );

    await controller.initialize();
    expect(controller.status, FidibekiFeedbackStatus.error);
    expect(controller.error?.code, FidibekiErrorCode.network);

    await controller.initialize();

    expect(sessionRequests, 2);
    expect(controller.status, FidibekiFeedbackStatus.ready);
    expect(controller.error, isNull);
  });

  test(
    'session provider errors cannot expose host secrets or reporter data',
    () async {
      final controller = FidibekiFeedbackController(
        apiBaseUrl: Uri.parse('https://api.example.test'),
        appVersion: '2.4.0',
        locale: const Locale('en'),
        osVersion: '18.5',
        platform: FidibekiPlatform.ios,
        screen: '/checkout',
        sessionProvider: (_) async {
          throw const FidibekiException(
            FidibekiErrorCode.network,
            'Bearer fmc_private_host_token belongs to alice@example.test',
            retryable: true,
            fields: <String, String>{'reporter_email': 'alice@example.test'},
          );
        },
        storage: MemoryFidibekiStorage(),
      );

      await controller.initialize();

      expect(controller.error?.code, FidibekiErrorCode.network);
      expect(controller.error?.retryable, isTrue);
      expect(controller.error?.fields, isEmpty);
      expect(controller.error.toString(), isNot(contains('fmc_')));
      expect(
        controller.error.toString(),
        isNot(contains('alice@example.test')),
      );
    },
  );

  test(
    'public configuration refresh waits for accepted submission boundary',
    () async {
      final response = Completer<http.Response>();
      var sessionRequests = 0;
      var feedbackRequests = 0;
      final controller = FidibekiFeedbackController(
        apiBaseUrl: Uri.parse('https://api.example.test'),
        appVersion: '2.4.0',
        locale: const Locale('en'),
        osVersion: '18.5',
        platform: FidibekiPlatform.android,
        screen: '/checkout',
        sessionProvider: (_) async {
          sessionRequests += 1;
          return lifecycleSession();
        },
        storage: MemoryFidibekiStorage(),
        mobileClient: FidibekiMobileClient(
          apiBaseUrl: Uri.parse('https://api.example.test'),
          httpClient: MockClient((request) {
            feedbackRequests += 1;
            return response.future;
          }),
        ),
      );
      await controller.open();
      fillRequired(controller);

      final submission = controller.submit();
      await Future<void>.delayed(Duration.zero);
      final refresh = controller.refreshConfiguration();
      await Future<void>.delayed(Duration.zero);

      expect(feedbackRequests, 1);
      expect(sessionRequests, 1);

      response.complete(http.Response(jsonEncode(acceptedJson()), 201));
      await submission;
      await refresh;

      expect(sessionRequests, 1);
      expect(controller.status, FidibekiFeedbackStatus.submitted);
      expect(controller.accepted?.feedbackPublicId, 'FDBK-LIFECYCLE');
    },
  );
}
