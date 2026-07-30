import 'dart:convert';
import 'dart:io';

import 'package:fidibeki_flutter/fidibeki_flutter.dart';
import 'package:flutter_test/flutter_test.dart';

File fixture(String name) {
  var directory = Directory.current.absolute;
  while (!File('${directory.path}/pnpm-workspace.yaml').existsSync()) {
    final parent = directory.parent;
    if (parent.path == directory.path) {
      throw StateError('Could not locate repository root');
    }
    directory = parent;
  }
  return File('${directory.path}/contracts/mobile-capture/fixtures/$name');
}

Map<String, Object?> fixtureJson(String name) {
  return jsonDecode(fixture(name).readAsStringSync()) as Map<String, Object?>;
}

void main() {
  group('shared mobile capture contracts', () {
    test('parses the versioned valid session fixture', () {
      final session = FidibekiCaptureSession.fromJson(
        fixtureJson('session-valid.json'),
      );

      expect(session.configuration.schemaVersion, 1);
      expect(session.configuration.definition.responseBlocks, hasLength(4));
      expect(
        session.configuration.definition.localization.prompt,
        'How did checkout go?',
      );
    });

    test('rejects an unsupported session schema', () {
      expect(
        () => FidibekiCaptureSession.fromJson(
          fixtureJson('session-invalid-schema.json'),
        ),
        throwsA(
          isA<FidibekiException>().having(
            (error) => error.code,
            'code',
            FidibekiErrorCode.invalidResponse,
          ),
        ),
      );
    });

    test('normalizes every shared screen case', () {
      final contract = fixtureJson('screen-normalization.json');
      final cases = contract['cases']! as List<Object?>;

      for (final rawCase in cases) {
        final testCase = rawCase! as Map<String, Object?>;
        final input = testCase['input']! as String;
        expect(normalizeFidibekiScreen(input), testCase['expected']);
        expect(
          fidibekiRecurrenceStorageKey('project', 'definition', input),
          contains(testCase['encoded_component']),
        );
      }
    });
  });
}
