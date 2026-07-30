import 'package:flutter_test/flutter_test.dart';

import '../../tool/check_coverage.dart';

void main() {
  test('counts package library lines and ignores tests', () {
    const lcov = '''
SF:packages/fidibeki_flutter/lib/src/controller.dart
LF:10
LH:9
end_of_record
SF:packages/fidibeki_flutter/test/controller_test.dart
LF:20
LH:20
end_of_record
SF:packages/fidibeki_flutter_shared_preferences/lib/storage.dart
LF:2
LH:2
end_of_record
''';

    final summary = summarizePackageCoverage(lcov);

    expect(summary.foundLines, 12);
    expect(summary.hitLines, 11);
    expect(summary.percent, closeTo(91.67, 0.01));
  });

  test('fails closed when no package library records exist', () {
    expect(
      () => summarizePackageCoverage(
        'SF:example/lib/main.dart\nLF:2\nLH:2\nend_of_record\n',
      ),
      throwsA(isA<FormatException>()),
    );
  });
}
