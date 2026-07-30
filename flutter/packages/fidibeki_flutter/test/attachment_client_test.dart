import 'dart:convert';
import 'dart:io';

import 'package:fidibeki_flutter/fidibeki_flutter.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'support/image_bytes.dart';

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
  return FidibekiCaptureSession.fromJson(
    fixtureJson('session-contact-valid.json'),
  );
}

Future<FidibekiPreparedScreenshot> preparedScreenshot() {
  return prepareFidibekiScreenshot(
    FidibekiScreenshotAsset(
      contentType: 'image/png',
      sizeBytes: validPngBytes().length,
      read: (_) async => validPngBytes(),
    ),
    clientAttachmentIdGenerator: () => '88888888-8888-4888-8888-888888888888',
  );
}

void main() {
  test('uses the shared upload grant and completion contract', () async {
    final fixture = fixtureJson('feedback-submit-screenshot.json');
    final observed = <http.Request>[];
    final client = FidibekiMobileClient(
      apiBaseUrl: Uri.parse('https://api.example.test'),
      httpClient: MockClient((request) async {
        observed.add(request);
        return switch (observed.length) {
          1 => http.Response(jsonEncode(fixture['accepted']), 201),
          2 => http.Response('', 200),
          _ => http.Response(jsonEncode(fixture['completion_accepted']), 200),
        };
      }),
    );
    final request = FidibekiFeedbackRequest.fromJson(
      fixture['request']! as Map<String, Object?>,
    );
    final screenshot = await preparedScreenshot();

    final accepted = await client.submitFeedback(captureSession(), request);
    final outcome = await client.uploadScreenshot(
      captureSession(),
      accepted,
      screenshot,
    );

    expect(outcome.status, FidibekiAttachmentStatus.uploaded);
    expect(outcome.attachmentId, 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb');
    expect(observed.map((request) => request.url.toString()), <String>[
      'https://api.example.test/v1/mobile/feedback',
      'https://objects.example.test/upload',
      'https://api.example.test/v1/mobile/attachments/'
          'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb/complete',
    ]);
    expect(observed[1].method, 'PUT');
    expect(observed[1].bodyBytes, validPngBytes());
    expect(observed[1].headers['authorization'], isNull);
    expect(observed[1].headers['content-type'], 'image/png');
    expect(observed[2].headers['authorization'], startsWith('Bearer fmc_'));
    expect(jsonDecode(observed[2].body), fixture['completion_request']);
    expect(jsonEncode(request.toWireJson()), isNot(contains('file:')));
  });

  test(
    'returns accepted feedback plus a failed upload without resubmitting',
    () async {
      final fixture = fixtureJson('feedback-submit-screenshot.json');
      var requests = 0;
      final client = FidibekiMobileClient(
        apiBaseUrl: Uri.parse('https://api.example.test'),
        httpClient: MockClient((request) async {
          requests += 1;
          if (requests == 1) {
            return http.Response(jsonEncode(fixture['accepted']), 201);
          }
          return http.Response('', 503);
        }),
      );
      final accepted = await client.submitFeedback(
        captureSession(),
        FidibekiFeedbackRequest.fromJson(
          fixture['request']! as Map<String, Object?>,
        ),
      );

      final outcome = await client.uploadScreenshot(
        captureSession(),
        accepted,
        await preparedScreenshot(),
      );

      expect(accepted.feedbackPublicId, 'FDBK-SCREENSHOT');
      expect(outcome.status, FidibekiAttachmentStatus.failed);
      expect(outcome.stage, FidibekiAttachmentStage.upload);
      expect(requests, 2);
    },
  );

  test(
    'reports completion failure after one accepted feedback and upload',
    () async {
      final fixture = fixtureJson('feedback-submit-screenshot.json');
      var requests = 0;
      final client = FidibekiMobileClient(
        apiBaseUrl: Uri.parse('https://api.example.test'),
        httpClient: MockClient((request) async {
          requests += 1;
          return switch (requests) {
            1 => http.Response(jsonEncode(fixture['accepted']), 201),
            2 => http.Response('', 200),
            _ => http.Response('', 503),
          };
        }),
      );
      final accepted = await client.submitFeedback(
        captureSession(),
        FidibekiFeedbackRequest.fromJson(
          fixture['request']! as Map<String, Object?>,
        ),
      );

      final outcome = await client.uploadScreenshot(
        captureSession(),
        accepted,
        await preparedScreenshot(),
      );

      expect(outcome.status, FidibekiAttachmentStatus.failed);
      expect(outcome.stage, FidibekiAttachmentStage.complete);
      expect(requests, 3);
    },
  );

  test(
    'rejects unsafe destinations, redirects, and private grant headers',
    () async {
      final fixture = fixtureJson('feedback-submit-screenshot.json');
      final unsafeAccepted = Map<String, Object?>.of(
        fixture['accepted']! as Map<String, Object?>,
      );
      final unsafeGrant = Map<String, Object?>.of(
        (unsafeAccepted['attachments']! as List<Object?>).single!
            as Map<String, Object?>,
      );
      unsafeGrant['presigned_url'] = 'http://objects.example.test/upload';
      unsafeAccepted['attachments'] = <Object?>[unsafeGrant];
      var unsafeRequests = 0;
      final unsafeClient = FidibekiMobileClient(
        apiBaseUrl: Uri.parse('https://api.example.test'),
        httpClient: MockClient((request) async {
          unsafeRequests += 1;
          return http.Response(jsonEncode(unsafeAccepted), 201);
        }),
      );
      final accepted = await unsafeClient.submitFeedback(
        captureSession(),
        FidibekiFeedbackRequest.fromJson(
          fixture['request']! as Map<String, Object?>,
        ),
      );
      final unsafeOutcome = await unsafeClient.uploadScreenshot(
        captureSession(),
        accepted,
        await preparedScreenshot(),
      );
      expect(unsafeOutcome.status, FidibekiAttachmentStatus.failed);
      expect(unsafeRequests, 1);

      var redirectRequests = 0;
      final redirectClient = FidibekiMobileClient(
        apiBaseUrl: Uri.parse('https://api.example.test'),
        httpClient: MockClient((request) async {
          redirectRequests += 1;
          if (redirectRequests == 1) {
            return http.Response(jsonEncode(fixture['accepted']), 201);
          }
          return http.Response(
            '',
            307,
            headers: <String, String>{
              'location': 'https://elsewhere.example.test/upload',
            },
          );
        }),
      );
      final redirectAccepted = await redirectClient.submitFeedback(
        captureSession(),
        FidibekiFeedbackRequest.fromJson(
          fixture['request']! as Map<String, Object?>,
        ),
      );
      final redirectOutcome = await redirectClient.uploadScreenshot(
        captureSession(),
        redirectAccepted,
        await preparedScreenshot(),
      );
      expect(redirectOutcome.status, FidibekiAttachmentStatus.failed);
      expect(redirectRequests, 2);

      final privateHeaderAccepted = Map<String, Object?>.of(
        fixture['accepted']! as Map<String, Object?>,
      );
      privateHeaderAccepted['attachments'] = <Object?>[
        <String, Object?>{
          ...((fixture['accepted']! as Map<String, Object?>)['attachments']!
                      as List<Object?>)
                  .single!
              as Map<String, Object?>,
          'headers': <String, Object?>{'authorization': 'must-not-forward'},
        },
      ];
      final privateHeaderClient = FidibekiMobileClient(
        apiBaseUrl: Uri.parse('https://api.example.test'),
        httpClient: MockClient(
          (_) async => http.Response(jsonEncode(privateHeaderAccepted), 201),
        ),
      );
      await expectLater(
        privateHeaderClient.submitFeedback(
          captureSession(),
          FidibekiFeedbackRequest.fromJson(
            fixture['request']! as Map<String, Object?>,
          ),
        ),
        throwsA(
          isA<FidibekiException>().having(
            (error) => error.code,
            'code',
            FidibekiErrorCode.invalidResponse,
          ),
        ),
      );
    },
  );
}
