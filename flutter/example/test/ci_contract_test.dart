import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

void main() {
  test('Flutter CI is isolated, pinned, and complete', () {
    final workflow = File(
      '../.github/workflows/flutter-ci.yml',
    ).readAsStringSync();

    expect(workflow, contains('"flutter/**"'));
    expect(workflow, contains('"contracts/mobile-capture/**"'));
    expect(workflow, contains("flutter-version: '3.44.7'"));
    expect(workflow, contains('dart format --output=none'));
    expect(
      workflow,
      contains('flutter analyze --fatal-infos --fatal-warnings'),
    );
    expect(workflow, contains('--coverage-path=coverage/lcov.info'));
    expect(
      workflow,
      contains('tool/check_coverage.dart coverage/lcov.info 85'),
    );
    expect(workflow, contains('dart pub downgrade'));
    expect(workflow, contains('dart doc'));
    expect(workflow, contains('dart pub publish --dry-run'));
    expect(workflow, contains('flutter build apk --release'));
    expect(workflow, contains('flutter build ios --simulator --debug'));
    expect(RegExp(r'--exclude-tags=golden').allMatches(workflow), hasLength(2));
    expect(
      workflow,
      matches(
        RegExp(
          r'flutter test\s+'
          r'packages/fidibeki_flutter/test/sheet_golden_test\.dart',
        ),
      ),
    );
    expect(
      RegExp(r'runs-on: warp-ubuntu-latest-x64-4x').allMatches(workflow),
      hasLength(3),
    );
    expect(workflow, isNot(contains('runs-on: warp-ubuntu-2604-arm64-4x')));
    expect(workflow, contains('runs-on: warp-macos-15-arm64-6x'));
    expect(workflow, isNot(contains('railway')));
    expect(workflow, isNot(contains('environment: production')));
  });

  test('visual regression tests declare macOS-owned golden scope', () {
    final goldenTest = File(
      'packages/fidibeki_flutter/test/sheet_golden_test.dart',
    ).readAsStringSync();
    final testConfiguration = File('dart_test.yaml').readAsStringSync();

    expect(goldenTest, contains("tags: 'golden'"));
    expect(testConfiguration, contains('golden:'));
  });

  test('React Native CI follows shared mobile fixture changes', () {
    final workflow = File(
      '../.github/workflows/mobile-ci.yml',
    ).readAsStringSync();

    expect(
      RegExp(
        r'"contracts/mobile-capture/\*\*"',
        multiLine: true,
      ).allMatches(workflow),
      hasLength(2),
    );
  });
}
