import 'dart:io';

final class CoverageSummary {
  const CoverageSummary({required this.foundLines, required this.hitLines});

  final int foundLines;
  final int hitLines;

  double get percent => hitLines * 100 / foundLines;
}

CoverageSummary summarizePackageCoverage(String lcov) {
  var packageLibrary = false;
  var foundLines = 0;
  var hitLines = 0;

  for (final line in lcov.split('\n')) {
    if (line.startsWith('SF:')) {
      final path = line.substring(3).replaceAll(r'\', '/');
      packageLibrary = RegExp(r'(?:^|/)packages/[^/]+/lib/').hasMatch(path);
    } else if (packageLibrary && line.startsWith('LF:')) {
      foundLines += int.parse(line.substring(3));
    } else if (packageLibrary && line.startsWith('LH:')) {
      hitLines += int.parse(line.substring(3));
    } else if (line == 'end_of_record') {
      packageLibrary = false;
    }
  }

  if (foundLines == 0) {
    throw const FormatException(
      'LCOV contains no package library coverage records.',
    );
  }
  if (hitLines > foundLines) {
    throw const FormatException('LCOV hit count exceeds its line count.');
  }
  return CoverageSummary(foundLines: foundLines, hitLines: hitLines);
}

void main(List<String> arguments) {
  final path = arguments.isEmpty ? 'coverage/lcov.info' : arguments.first;
  final minimum = arguments.length < 2 ? 85.0 : double.parse(arguments[1]);

  try {
    final summary = summarizePackageCoverage(File(path).readAsStringSync());
    final percent = summary.percent;
    stdout.writeln(
      'Package line coverage: ${percent.toStringAsFixed(2)}% '
      '(${summary.hitLines}/${summary.foundLines})',
    );
    if (percent < minimum) {
      stderr.writeln(
        'Coverage is below the ${minimum.toStringAsFixed(2)}% minimum.',
      );
      exitCode = 1;
    }
  } on Object catch (error) {
    stderr.writeln('Could not verify coverage: $error');
    exitCode = 1;
  }
}
