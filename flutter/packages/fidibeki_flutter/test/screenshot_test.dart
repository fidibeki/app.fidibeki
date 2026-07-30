import 'dart:typed_data';

import 'package:fidibeki_flutter/fidibeki_flutter.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test(
    'prepares bounded supported bytes and disposes the host asset once',
    () async {
      var reads = 0;
      var disposals = 0;
      final prepared = await prepareFidibekiScreenshot(
        FidibekiScreenshotAsset(
          contentType: 'image/png',
          sizeBytes: 3,
          read: (maximumBytes) async {
            reads += 1;
            expect(maximumBytes, maxFidibekiScreenshotBytes);
            return Uint8List.fromList(<int>[1, 2, 3]);
          },
          dispose: () async {
            disposals += 1;
          },
        ),
        clientAttachmentIdGenerator: () =>
            '88888888-8888-4888-8888-888888888888',
      );

      expect(
        prepared.clientAttachmentId,
        '88888888-8888-4888-8888-888888888888',
      );
      expect(prepared.contentType, 'image/png');
      expect(prepared.bytes, Uint8List.fromList(<int>[1, 2, 3]));
      expect(() => prepared.bytes[0] = 9, throwsUnsupportedError);
      expect(reads, 1);
      expect(prepared.toString(), isNot(contains('file:')));

      await prepared.dispose();
      await prepared.dispose();
      expect(disposals, 1);
    },
  );

  test(
    'rejects invalid declarations before reading and disposes them',
    () async {
      for (final declaration in <(String, int)>[
        ('image/gif', 3),
        ('image/png', 0),
        ('image/webp', maxFidibekiScreenshotBytes + 1),
      ]) {
        var reads = 0;
        var disposals = 0;
        final asset = FidibekiScreenshotAsset(
          contentType: declaration.$1,
          sizeBytes: declaration.$2,
          read: (_) async {
            reads += 1;
            return Uint8List.fromList(<int>[1, 2, 3]);
          },
          dispose: () async {
            disposals += 1;
          },
        );

        await expectLater(
          prepareFidibekiScreenshot(asset),
          throwsA(
            isA<FidibekiException>().having(
              (error) => error.code,
              'code',
              FidibekiErrorCode.attachment,
            ),
          ),
        );
        expect(reads, 0);
        expect(disposals, 1);
      }
    },
  );

  test('rejects byte mismatches and reader failures with cleanup', () async {
    var mismatchDisposals = 0;
    await expectLater(
      prepareFidibekiScreenshot(
        FidibekiScreenshotAsset(
          contentType: 'image/jpeg',
          sizeBytes: 4,
          read: (_) async => Uint8List.fromList(<int>[1, 2, 3]),
          dispose: () async {
            mismatchDisposals += 1;
          },
        ),
      ),
      throwsA(isA<FidibekiException>()),
    );
    expect(mismatchDisposals, 1);

    var failureDisposals = 0;
    await expectLater(
      prepareFidibekiScreenshot(
        FidibekiScreenshotAsset(
          contentType: 'image/webp',
          sizeBytes: 3,
          read: (_) async => throw StateError('private host failure'),
          dispose: () async {
            failureDisposals += 1;
          },
        ),
      ),
      throwsA(
        isA<FidibekiException>()
            .having((error) => error.code, 'code', FidibekiErrorCode.attachment)
            .having(
              (error) => error.toString(),
              'safe error',
              isNot(contains('private host failure')),
            ),
      ),
    );
    expect(failureDisposals, 1);
  });
}
