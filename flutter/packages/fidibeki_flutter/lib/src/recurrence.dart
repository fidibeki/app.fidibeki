import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:uuid/uuid.dart';

import 'models.dart';
import 'storage.dart';

const _storagePrefix = '@fidibeki/flutter/v1';
const backgroundSessionResetDuration = Duration(minutes: 30);

String? _processAppSessionId;

String _currentProcessAppSessionId(String Function() generator) {
  return _processAppSessionId ??= generator();
}

@visibleForTesting
void resetFidibekiProcessAppSessionForTesting() {
  _processAppSessionId = null;
}

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
    String? appSessionId,
    DateTime Function()? now,
    String Function()? appSessionIdGenerator,
  }) : _appSessionId =
           appSessionId ??
           _currentProcessAppSessionId(
             appSessionIdGenerator ?? const Uuid().v4,
           ),
       _hostOwnsSession = appSessionId != null,
       _appSessionIdGenerator = appSessionIdGenerator ?? const Uuid().v4,
       _now = now ?? DateTime.now;

  final FidibekiStorage storage;
  String _appSessionId;
  final bool _hostOwnsSession;
  final String Function() _appSessionIdGenerator;
  final DateTime Function() _now;
  DateTime? _backgroundedAt;

  String get currentAppSessionId => _appSessionId;

  void onBackground() {
    if (!_hostOwnsSession && _backgroundedAt == null) {
      _backgroundedAt = _now().toUtc();
    }
  }

  bool onForeground() {
    final backgroundedAt = _backgroundedAt;
    if (_hostOwnsSession || backgroundedAt == null) return false;
    _backgroundedAt = null;
    if (_now().toUtc().difference(backgroundedAt) <
        backgroundSessionResetDuration) {
      return false;
    }
    _appSessionId = _appSessionIdGenerator();
    _processAppSessionId = _appSessionId;
    return true;
  }

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
    if (state == 'dismissed') {
      await storage.delete(key);
      return const FidibekiRecurrenceEligibility(eligible: true);
    }
    if (state != 'submitted') {
      await storage.delete(key);
      return const FidibekiRecurrenceEligibility(eligible: true);
    }
    if (record.keys.any(
      (key) => !const <String>{
        'state',
        'recorded_at',
        'app_session_id',
      }.contains(key),
    )) {
      await storage.delete(key);
      return const FidibekiRecurrenceEligibility(eligible: true);
    }
    final rawRecordedAt = record['recorded_at'];
    final recordedAt = rawRecordedAt is String
        ? DateTime.tryParse(rawRecordedAt)?.toUtc()
        : null;
    if (recordedAt == null) {
      await storage.delete(key);
      return const FidibekiRecurrenceEligibility(eligible: true);
    }
    if (definition.recurrenceMode == FidibekiRecurrenceMode.session) {
      final recordedAppSessionId = record['app_session_id'];
      if (recordedAppSessionId is! String || recordedAppSessionId.isEmpty) {
        await storage.delete(key);
        return const FidibekiRecurrenceEligibility(eligible: true);
      }
      return FidibekiRecurrenceEligibility(
        eligible: recordedAppSessionId != _appSessionId,
      );
    }
    if (definition.recurrenceMode == FidibekiRecurrenceMode.never) {
      return const FidibekiRecurrenceEligibility(eligible: false);
    }
    final nextEligibleAt = recordedAt.add(
      Duration(days: definition.recurrenceDays ?? 30),
    );
    final now = _now().toUtc();
    return FidibekiRecurrenceEligibility(
      eligible: !now.isBefore(nextEligibleAt),
      nextEligibleAt: now.isBefore(nextEligibleAt) ? nextEligibleAt : null,
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
          'app_session_id': _appSessionId,
      }),
    );
  }
}
