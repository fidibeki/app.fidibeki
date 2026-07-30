import 'dart:convert';
import 'dart:io';

import 'package:fidibeki_flutter/fidibeki_flutter.dart';
import 'package:flutter_test/flutter_test.dart';

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

FidibekiWidgetDefinition definition({
  FidibekiRecurrenceMode mode = FidibekiRecurrenceMode.session,
  int? days,
}) {
  final json = fixtureJson('session-valid.json');
  final configuration = json['configuration']! as Map<String, Object?>;
  final rawDefinition = configuration['definition']! as Map<String, Object?>;
  rawDefinition['recurrence_mode'] = mode.wireName;
  rawDefinition['recurrence_days'] = mode == FidibekiRecurrenceMode.cooldown
      ? days ?? 30
      : null;
  return FidibekiWidgetDefinition.fromJson(rawDefinition);
}

void main() {
  setUp(resetFidibekiProcessAppSessionForTesting);

  test('shares an implicit app session across controller remounts', () async {
    final storage = MemoryFidibekiStorage();
    var sequence = 0;
    String identifier() {
      sequence += 1;
      return 'session-$sequence';
    }

    final first = FidibekiRecurrenceEngine(
      storage,
      appSessionIdGenerator: identifier,
    );
    await first.recordSubmitted('project', definition(), '/checkout');
    final remount = FidibekiRecurrenceEngine(
      storage,
      appSessionIdGenerator: identifier,
    );

    expect(remount.currentAppSessionId, first.currentAppSessionId);
    expect(
      (await remount.eligibility(
        'project',
        definition(),
        '/checkout',
      )).eligible,
      isFalse,
    );
  });

  test(
    'rotates an implicit session after 30 continuous background minutes',
    () async {
      final storage = MemoryFidibekiStorage();
      var now = DateTime.utc(2026, 7, 30, 12);
      var sequence = 0;
      final engine = FidibekiRecurrenceEngine(
        storage,
        now: () => now,
        appSessionIdGenerator: () => 'session-${++sequence}',
      );
      await engine.recordSubmitted('project', definition(), '/checkout');

      engine.onBackground();
      now = now.add(
        backgroundSessionResetDuration - const Duration(seconds: 1),
      );
      expect(engine.onForeground(), isFalse);
      expect(
        (await engine.eligibility(
          'project',
          definition(),
          '/checkout',
        )).eligible,
        isFalse,
      );

      engine.onBackground();
      now = now.add(backgroundSessionResetDuration);
      expect(engine.onForeground(), isTrue);
      expect(
        (await engine.eligibility(
          'project',
          definition(),
          '/checkout',
        )).eligible,
        isTrue,
      );
    },
  );

  test('never rotates a host-owned app session', () async {
    final storage = MemoryFidibekiStorage();
    var now = DateTime.utc(2026, 7, 30, 12);
    final engine = FidibekiRecurrenceEngine(
      storage,
      appSessionId: 'host-session',
      now: () => now,
    );
    await engine.recordSubmitted('project', definition(), '/checkout');

    engine.onBackground();
    now = now.add(const Duration(days: 1));

    expect(engine.onForeground(), isFalse);
    expect(engine.currentAppSessionId, 'host-session');
    expect(
      (await engine.eligibility('project', definition(), '/checkout')).eligible,
      isFalse,
    );
  });

  test('treats a cold process launch as a new implicit app session', () async {
    final storage = MemoryFidibekiStorage();
    var sequence = 0;
    final first = FidibekiRecurrenceEngine(
      storage,
      appSessionIdGenerator: () => 'session-${++sequence}',
    );
    await first.recordSubmitted('project', definition(), '/checkout');

    resetFidibekiProcessAppSessionForTesting();
    final cold = FidibekiRecurrenceEngine(
      storage,
      appSessionIdGenerator: () => 'session-${++sequence}',
    );

    expect(cold.currentAppSessionId, isNot(first.currentAppSessionId));
    expect(
      (await cold.eligibility('project', definition(), '/checkout')).eligible,
      isTrue,
    );
  });

  test('applies cooldown timestamps and persistent never recurrence', () async {
    final storage = MemoryFidibekiStorage();
    var now = DateTime.utc(2026, 7, 30);
    final engine = FidibekiRecurrenceEngine(
      storage,
      appSessionId: 'host-session',
      now: () => now,
    );
    final cooldown = definition(mode: FidibekiRecurrenceMode.cooldown, days: 2);
    await engine.recordSubmitted('project', cooldown, '/checkout');

    now = now.add(const Duration(days: 2) - const Duration(seconds: 1));
    final pending = await engine.eligibility('project', cooldown, '/checkout');
    expect(pending.eligible, isFalse);
    expect(pending.nextEligibleAt, DateTime.utc(2026, 8, 1));

    now = now.add(const Duration(seconds: 1));
    expect(
      (await engine.eligibility('project', cooldown, '/checkout')).eligible,
      isTrue,
    );

    final never = definition(mode: FidibekiRecurrenceMode.never);
    await engine.recordSubmitted('project', never, '/never');
    now = DateTime.utc(2099);
    expect(
      (await engine.eligibility('project', never, '/never')).eligible,
      isFalse,
    );
  });

  test('deletes legacy and malformed recurrence records', () async {
    final storage = MemoryFidibekiStorage();
    final current = definition();
    final key = fidibekiRecurrenceStorageKey(
      'project',
      current.definitionKey,
      '/checkout',
    );
    final engine = FidibekiRecurrenceEngine(
      storage,
      appSessionId: 'host-session',
    );

    for (final record in <String>[
      '{not-json',
      jsonEncode(<String, Object?>{
        'state': 'dismissed',
        'recorded_at': DateTime.now().toUtc().toIso8601String(),
      }),
      jsonEncode(<String, Object?>{
        'state': 'unknown',
        'recorded_at': DateTime.now().toUtc().toIso8601String(),
      }),
      jsonEncode(<String, Object?>{'state': 'submitted', 'recorded_at': 123}),
    ]) {
      storage.values[key] = record;
      expect(
        (await engine.eligibility('project', current, '/checkout')).eligible,
        isTrue,
      );
      expect(storage.values, isNot(contains(key)));
    }
  });

  test('persists recurrence metadata only', () async {
    final storage = MemoryFidibekiStorage();
    final engine = FidibekiRecurrenceEngine(
      storage,
      appSessionId: 'anonymous-session',
      now: () => DateTime.utc(2026, 7, 30),
    );

    await engine.recordSubmitted('project', definition(), '/checkout');

    final stored =
        jsonDecode(storage.values.values.single) as Map<String, Object?>;
    expect(
      stored.keys,
      unorderedEquals(<String>['state', 'recorded_at', 'app_session_id']),
    );
    expect(jsonEncode(stored), isNot(contains('fmc_')));
    expect(jsonEncode(stored), isNot(contains('@example')));
  });
}
