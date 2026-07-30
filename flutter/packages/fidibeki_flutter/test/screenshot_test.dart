import 'dart:typed_data';

import 'package:fidibeki_flutter/fidibeki_flutter.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/image_bytes.dart';

void main() {
  test(
    'prepares bounded supported bytes and disposes the host asset once',
    () async {
      var reads = 0;
      var disposals = 0;
      final prepared = await prepareFidibekiScreenshot(
        FidibekiScreenshotAsset(
          contentType: 'image/png',
          sizeBytes: validPngBytes().length,
          read: (maximumBytes) async {
            reads += 1;
            expect(maximumBytes, maxFidibekiScreenshotBytes);
            return validPngBytes();
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
      expect(prepared.bytes, validPngBytes());
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

  test('accepts signatures for every supported screenshot type', () async {
    for (final testCase in <(String, Uint8List)>[
      ('image/png', validPngBytes()),
      ('image/jpeg', validJpegBytes()),
      ('image/webp', validWebpBytes()),
    ]) {
      final prepared = await prepareFidibekiScreenshot(
        FidibekiScreenshotAsset(
          contentType: testCase.$1,
          sizeBytes: testCase.$2.length,
          read: (_) async => testCase.$2,
        ),
      );
      expect(prepared.contentType, testCase.$1);
      expect(prepared.bytes, testCase.$2);
      await prepared.dispose();
    }
  });

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

  test(
    'rejects bytes whose signature does not match the declared type',
    () async {
      var disposals = 0;
      final cases = <(String, List<int>)>[
        ('image/png', <int>[0xff, 0xd8, 0xff, 0xd9]),
        ('image/jpeg', <int>[0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
        ('image/webp', <int>[0x52, 0x49, 0x46, 0x46]),
      ];

      for (final testCase in cases) {
        await expectLater(
          prepareFidibekiScreenshot(
            FidibekiScreenshotAsset(
              contentType: testCase.$1,
              sizeBytes: testCase.$2.length,
              read: (_) async => Uint8List.fromList(testCase.$2),
              dispose: () async {
                disposals += 1;
              },
            ),
          ),
          throwsA(
            isA<FidibekiException>().having(
              (error) => error.code,
              'code',
              FidibekiErrorCode.attachment,
            ),
          ),
        );
      }
      expect(disposals, cases.length);
    },
  );
}
