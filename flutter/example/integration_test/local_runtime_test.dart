import 'dart:convert';

import 'package:fidibeki_flutter/fidibeki_flutter.dart';
import 'package:fidibeki_flutter_example/main.dart' as example;
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';
import 'package:http/http.dart' as http;

const runNativeSmoke = bool.fromEnvironment('FIDIBEKI_NATIVE_SMOKE');

Future<Map<String, Object?>> _evidence() async {
  final response = await http.get(
    Uri.parse('${example.widgetBaseUrl}/evidence'),
  );
  expect(response.statusCode, 200);
  return jsonDecode(response.body) as Map<String, Object?>;
}

Finder _formScroll() {
  return find
      .descendant(
        of: find.byKey(const ValueKey('fidibeki-form-scroll')),
        matching: find.byType(Scrollable),
      )
      .first;
}

void main() {
  IntegrationTestWidgetsFlutterBinding.ensureInitialized();

  testWidgets(
    'native example preserves a draft and completes screenshot feedback',
    (tester) async {
      final before = await _evidence();
      final storage = MemoryFidibekiStorage();

      await tester.pumpWidget(example.FidibekiExampleApp(storage: storage));
      await tester.pumpAndSettle();

      final trigger = find.byKey(const ValueKey('host-feedback-trigger'));
      expect(trigger, findsOneWidget);
      await tester.tap(trigger);
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const ValueKey('fidibeki-rating-rating-4')));
      await tester.scrollUntilVisible(
        find.byKey(const ValueKey('fidibeki-text-feedback')),
        120,
        scrollable: _formScroll(),
      );
      await tester.enterText(
        find.byKey(const ValueKey('fidibeki-text-feedback')),
        'Native Flutter runtime feedback',
      );
      await tester.testTextInput.receiveAction(TextInputAction.done);
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const ValueKey('fidibeki-close')));
      await tester.pumpAndSettle();
      expect(find.byKey(const ValueKey('fidibeki-sheet')), findsNothing);

      await tester.tap(trigger);
      await tester.pumpAndSettle();
      expect(find.text('Native Flutter runtime feedback'), findsOneWidget);

      await tester.scrollUntilVisible(
        find.byKey(const ValueKey('fidibeki-screenshot-action')),
        120,
        scrollable: _formScroll(),
      );
      await tester.tap(
        find.byKey(const ValueKey('fidibeki-screenshot-action')),
      );
      await tester.pumpAndSettle();
      expect(
        find.byKey(const ValueKey('fidibeki-screenshot-attached')),
        findsOneWidget,
      );

      await tester.scrollUntilVisible(
        find.byKey(const ValueKey('fidibeki-submit')),
        120,
        scrollable: _formScroll(),
      );
      await tester.tap(find.byKey(const ValueKey('fidibeki-submit')));
      await tester.pumpAndSettle();

      expect(find.text('Runtime smoke completed.'), findsOneWidget);
      final after = await _evidence();
      expect(after['session'], (before['session']! as int) + 1);
      expect(after['feedback'], (before['feedback']! as int) + 1);
      expect(after['completed'], (before['completed']! as int) + 1);
      expect(after['uploadedBytes'], greaterThan(0));
      expect(
        (after['lastFeedback']! as Map<String, Object?>)['responses'],
        containsPair('rating', 4),
      );
    },
    skip: !runNativeSmoke,
  );
}
