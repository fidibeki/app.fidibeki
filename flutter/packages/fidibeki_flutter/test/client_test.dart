import 'dart:convert';
import 'dart:io';
import 'dart:async';

import 'package:fidibeki_flutter/fidibeki_flutter.dart';
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

FidibekiCaptureSession captureSession() {
  return FidibekiCaptureSession.fromJson(fixtureJson('session-valid.json'));
}

void main() {
  test(
    'submits the shared strict request and parses accepted feedback',
    () async {
      final fixture = fixtureJson('feedback-submit-valid.json');
      final requestJson = fixture['request']! as Map<String, Object?>;
      late http.Request observed;
      final client = FidibekiMobileClient(
        apiBaseUrl: Uri.parse('https://api.example.test'),
        httpClient: MockClient((request) async {
          observed = request;
          return http.Response(jsonEncode(fixture['accepted']), 201);
        }),
      );

      final request = FidibekiFeedbackRequest.fromJson(requestJson);
      final accepted = await client.submitFeedback(captureSession(), request);

      expect(observed.method, 'POST');
      expect(observed.url.path, '/v1/mobile/feedback');
      expect(observed.headers['authorization'], startsWith('Bearer fmc_'));
      expect(jsonDecode(observed.body), requestJson);
      expect(accepted.feedbackPublicId, 'FDBK-FIXTURE');
      expect(accepted.attachments, isEmpty);
    },
  );

  test(
    'retains only an exact retry after an ambiguous transport result',
    () async {
      final fixture = fixtureJson('feedback-submit-valid.json');
      final requestJson = fixture['request']! as Map<String, Object?>;
      var now = DateTime.utc(2026, 7, 30, 12);
      var attempts = 0;
      final client = FidibekiMobileClient(
        apiBaseUrl: Uri.parse('https://api.example.test'),
        now: () => now,
        httpClient: MockClient((request) async {
          attempts += 1;
          if (attempts == 1) {
            throw http.ClientException('connection reset');
          }
          return http.Response(jsonEncode(fixture['accepted']), 200);
        }),
      );
      final sessionJson = fixtureJson('session-valid.json');
      sessionJson['submit_expires_at'] = now
          .add(const Duration(seconds: 1))
          .toIso8601String();
      final session = FidibekiCaptureSession.fromJson(sessionJson);
      final request = FidibekiFeedbackRequest.fromJson(requestJson);

      await expectLater(
        client.submitFeedback(session, request),
        throwsA(
          isA<FidibekiException>()
              .having((error) => error.code, 'code', FidibekiErrorCode.network)
              .having((error) => error.retryable, 'retryable', isTrue),
        ),
      );

      now = now.add(const Duration(seconds: 2));
      final changedJson = Map<String, Object?>.of(requestJson)
        ..['comment'] = 'Changed after ambiguity';
      await expectLater(
        client.submitFeedback(
          session,
          FidibekiFeedbackRequest.fromJson(changedJson),
        ),
        throwsA(
          isA<FidibekiException>().having(
            (error) => error.code,
            'code',
            FidibekiErrorCode.retryPayloadChanged,
          ),
        ),
      );
      await expectLater(client.submitFeedback(session, request), completes);
      expect(attempts, 2);
    },
  );

  test('retains only an exact retry after a malformed success', () async {
    final fixture = fixtureJson('feedback-submit-valid.json');
    final requestJson = fixture['request']! as Map<String, Object?>;
    var attempts = 0;
    final client = FidibekiMobileClient(
      apiBaseUrl: Uri.parse('https://api.example.test'),
      httpClient: MockClient((request) async {
        attempts += 1;
        return attempts == 1
            ? http.Response('{invalid-json', 201)
            : http.Response(jsonEncode(fixture['accepted']), 201);
      }),
    );
    final request = FidibekiFeedbackRequest.fromJson(requestJson);

    await expectLater(
      client.submitFeedback(captureSession(), request),
      throwsA(
        isA<FidibekiException>()
            .having(
              (error) => error.code,
              'code',
              FidibekiErrorCode.invalidResponse,
            )
            .having((error) => error.retryable, 'retryable', isTrue),
      ),
    );

    final changedJson = Map<String, Object?>.of(requestJson)
      ..['comment'] = 'Changed after malformed acceptance';
    await expectLater(
      client.submitFeedback(
        captureSession(),
        FidibekiFeedbackRequest.fromJson(changedJson),
      ),
      throwsA(
        isA<FidibekiException>().having(
          (error) => error.code,
          'code',
          FidibekiErrorCode.retryPayloadChanged,
        ),
      ),
    );
    await expectLater(
      client.submitFeedback(captureSession(), request),
      completes,
    );
    expect(attempts, 2);
  });

  test('coalesces concurrent attempts with the same ID and body', () async {
    final fixture = fixtureJson('feedback-submit-valid.json');
    final request = FidibekiFeedbackRequest.fromJson(
      fixture['request']! as Map<String, Object?>,
    );
    final response = Completer<http.Response>();
    var attempts = 0;
    final client = FidibekiMobileClient(
      apiBaseUrl: Uri.parse('https://api.example.test'),
      httpClient: MockClient((request) {
        attempts += 1;
        return response.future;
      }),
    );

    final first = client.submitFeedback(captureSession(), request);
    final second = client.submitFeedback(captureSession(), request);
    await Future<void>.delayed(Duration.zero);
    expect(attempts, 1);

    response.complete(http.Response(jsonEncode(fixture['accepted']), 201));
    await expectLater(
      Future.wait(<Future<FidibekiFeedbackAccepted>>[first, second]),
      completes,
    );
    expect(attempts, 1);
  });

  test('maps every shared backend error to a typed safe exception', () async {
    final fixture = fixtureJson('feedback-submit-valid.json');
    final request = FidibekiFeedbackRequest.fromJson(
      fixture['request']! as Map<String, Object?>,
    );
    final errorFixture = fixtureJson('feedback-submit-errors.json');
    final cases = errorFixture['cases']! as List<Object?>;

    for (final rawCase in cases) {
      final testCase = rawCase! as Map<String, Object?>;
      final client = FidibekiMobileClient(
        apiBaseUrl: Uri.parse('https://api.example.test'),
        httpClient: MockClient(
          (request) async => http.Response(
            jsonEncode(testCase['body']),
            testCase['status']! as int,
          ),
        ),
      );

      try {
        await client.submitFeedback(captureSession(), request);
        fail('Expected ${testCase['expected_code']}');
      } on FidibekiException catch (error) {
        expect(error.code.wireName, testCase['expected_code']);
        expect(error.retryable, testCase['retryable']);
        expect(error.toString(), isNot(contains('fmc_')));
        expect(
          error.toString(),
          isNot(contains('The chart labels are difficult to read')),
        );
        if (testCase['status'] == 422) {
          expect(error.fields['responses.feedback'], 'is required');
        }
      }
    }
  });
}
