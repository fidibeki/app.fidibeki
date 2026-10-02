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

Map<String, Object?> unknownFieldTarget(
  Map<String, Object?> session,
  String target,
) {
  final configuration = session['configuration']! as Map<String, Object?>;
  final definition = configuration['definition']! as Map<String, Object?>;
  final blocks = definition['response_blocks']! as List<Object?>;
  return switch (target) {
    'session' => session,
    'configuration' => configuration,
    'definition' => definition,
    'response_block' => blocks.first! as Map<String, Object?>,
    'response_option' =>
      ((blocks[1]! as Map<String, Object?>)['options']! as List<Object?>).first!
          as Map<String, Object?>,
    'localization' => definition['localization']! as Map<String, Object?>,
    'appearance' => definition['appearance']! as Map<String, Object?>,
    'ai_processing' => definition['ai_processing']! as Map<String, Object?>,
    'controller_profile' =>
      configuration['controller_profile']! as Map<String, Object?>,
    'contact_notice' =>
      configuration['contact_notice']! as Map<String, Object?>,
    _ => throw StateError('Unknown fixture target: $target'),
  };
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

    test('accepts the customer provider label', () {
      final candidate = fixtureJson('session-valid.json');
      unknownFieldTarget(candidate, 'ai_processing')['provider_label'] = 'OpenAI';
      final session = FidibekiCaptureSession.fromJson(candidate);
      expect(
        session.configuration.definition.aiProcessing!.providerLabel,
        'OpenAI',
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

    test('rejects shared unknown fields at every contract object', () {
      final contract = fixtureJson('session-unknown-fields.json');
      final unknownField = contract['unknown_field']! as String;
      final cases = contract['cases']! as List<Object?>;

      for (final rawCase in cases) {
        final testCase = rawCase! as Map<String, Object?>;
        final session = fixtureJson(testCase['fixture']! as String);
        unknownFieldTarget(
          session,
          testCase['target']! as String,
        )[unknownField] = 'must be rejected';

        expect(
          () => FidibekiCaptureSession.fromJson(session),
          throwsA(
            isA<FidibekiException>().having(
              (error) => error.code,
              'code',
              FidibekiErrorCode.invalidResponse,
            ),
          ),
          reason: 'target ${testCase['target']}',
        );
      }
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
