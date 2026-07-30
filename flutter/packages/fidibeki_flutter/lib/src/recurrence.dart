import 'dart:convert';

import 'models.dart';
import 'storage.dart';

const _storagePrefix = '@fidibeki/flutter/v1';

String normalizeFidibekiScreen(String screen) {
  final trimmed = screen.trim();
  final query = trimmed.indexOf('?');
  final fragment = trimmed.indexOf('#');
  final boundaries = <int>[if (query >= 0) query, if (fragment >= 0) fragment]
    ..sort();
  final path = boundaries.isEmpty
      ? trimmed
      : trimmed.substring(0, boundaries.first);
  final normalized = '/$path'
      .replaceAll(RegExp(r'/+'), '/')
      .replaceAll(RegExp(r'/+$'), '');
  return normalized.isEmpty ? '/' : normalized;
}

String fidibekiRecurrenceStorageKey(
  String projectId,
  String definitionKey,
  String screen,
) {
  return <String>[
    _storagePrefix,
    Uri.encodeComponent(projectId),
    Uri.encodeComponent(definitionKey),
    Uri.encodeComponent(normalizeFidibekiScreen(screen)),
  ].join(':');
}

final class FidibekiRecurrenceEligibility {
  const FidibekiRecurrenceEligibility({
    required this.eligible,
    this.nextEligibleAt,
  });

  final bool eligible;
  final DateTime? nextEligibleAt;
}

final class FidibekiRecurrenceEngine {
  FidibekiRecurrenceEngine(
    this.storage, {
    required this.appSessionId,
    DateTime Function()? now,
  }) : _now = now ?? DateTime.now;

  final FidibekiStorage storage;
  final String appSessionId;
  final DateTime Function() _now;

  Future<FidibekiRecurrenceEligibility> eligibility(
    String projectId,
    FidibekiWidgetDefinition definition,
    String screen,
  ) async {
    final key = fidibekiRecurrenceStorageKey(
      projectId,
      definition.definitionKey,
      screen,
    );
    final raw = await storage.read(key);
    if (raw == null) {
      return const FidibekiRecurrenceEligibility(eligible: true);
    }
    Map<String, Object?> record;
    try {
      record = (jsonDecode(raw) as Map).cast<String, Object?>();
    } on Object {
      await storage.delete(key);
      return const FidibekiRecurrenceEligibility(eligible: true);
    }
    final state = record['state'];
    if (state == 'dismissed' || state is! String) {
      await storage.delete(key);
      return const FidibekiRecurrenceEligibility(eligible: true);
    }
    if (state != 'submitted') {
      return const FidibekiRecurrenceEligibility(eligible: true);
    }
    if (definition.recurrenceMode == FidibekiRecurrenceMode.session) {
      return FidibekiRecurrenceEligibility(
        eligible: record['app_session_id'] != appSessionId,
      );
    }
    if (definition.recurrenceMode == FidibekiRecurrenceMode.never) {
      return const FidibekiRecurrenceEligibility(eligible: false);
    }
    final recordedAt = DateTime.tryParse(
      record['recorded_at'] as String? ?? '',
    );
    if (recordedAt == null) {
      await storage.delete(key);
      return const FidibekiRecurrenceEligibility(eligible: true);
    }
    final nextEligibleAt = recordedAt.toUtc().add(
      Duration(days: definition.recurrenceDays ?? 30),
    );
    return FidibekiRecurrenceEligibility(
      eligible: !_now().toUtc().isBefore(nextEligibleAt),
      nextEligibleAt: _now().toUtc().isBefore(nextEligibleAt)
          ? nextEligibleAt
          : null,
    );
  }

  Future<void> recordSubmitted(
    String projectId,
    FidibekiWidgetDefinition definition,
    String screen,
  ) {
    return storage.write(
      fidibekiRecurrenceStorageKey(projectId, definition.definitionKey, screen),
      jsonEncode(<String, Object?>{
        'state': 'submitted',
        'recorded_at': _now().toUtc().toIso8601String(),
        if (definition.recurrenceMode == FidibekiRecurrenceMode.session)
          'app_session_id': appSessionId,
      }),
    );
  }
}
