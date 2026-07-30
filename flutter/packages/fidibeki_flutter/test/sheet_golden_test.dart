import 'dart:convert';
import 'dart:io';

import 'package:fidibeki_flutter/fidibeki_flutter.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
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

FidibekiCaptureSession goldenSession({bool longCopy = false}) {
  final json = fixtureJson('session-valid.json');
  if (longCopy) {
    final configuration = json['configuration']! as Map<String, Object?>;
    final definition = configuration['definition']! as Map<String, Object?>;
    final localization = definition['localization']! as Map<String, Object?>;
    localization['prompt'] =
        'Tell us what happened during this checkout experience';
    localization['submit_label'] = 'Send this detailed feedback';
  }
  return FidibekiCaptureSession.fromJson(json);
}

Future<void> loadGoldenFont() async {
  var flutterRoot = File(Platform.resolvedExecutable).parent;
  while (!Directory('${flutterRoot.path}/packages/flutter').existsSync()) {
    if (flutterRoot.parent.path == flutterRoot.path) {
      throw StateError('Could not locate the Flutter SDK');
    }
    flutterRoot = flutterRoot.parent;
  }
  final fontDirectory =
      '${flutterRoot.path}/bin/cache/artifacts/material_fonts';

  Future<ByteData> fontData(String name) async {
    final font = File('$fontDirectory/$name');
    final bytes = await font.readAsBytes();
    return ByteData.sublistView(bytes);
  }

  final textLoader = FontLoader('FidibekiGolden')
    ..addFont(fontData('Roboto-Regular.ttf'));
  final iconLoader = FontLoader('MaterialIcons')
    ..addFont(fontData('MaterialIcons-Regular.otf'));
  await Future.wait(<Future<void>>[textLoader.load(), iconLoader.load()]);
}

Future<FidibekiFeedbackController> pumpGoldenSheet(
  WidgetTester tester, {
  required Size size,
  required Brightness brightness,
  required String goldenName,
  double textScale = 1,
  bool longCopy = false,
  bool selected = false,
}) async {
  await tester.binding.setSurfaceSize(size);
  addTearDown(() => tester.binding.setSurfaceSize(null));
  tester.view.devicePixelRatio = 1;
  addTearDown(() => tester.view.resetDevicePixelRatio());
  final controller = FidibekiFeedbackController(
    apiBaseUrl: Uri.parse('https://api.example.test'),
    appVersion: '2.4.0',
    locale: const Locale('en'),
    osVersion: '18.5',
    platform: FidibekiPlatform.ios,
    screen: '/checkout',
    sessionProvider: (_) async => goldenSession(longCopy: longCopy),
    storage: MemoryFidibekiStorage(),
  );
  await tester.pumpWidget(
    MaterialApp(
      debugShowCheckedModeBanner: false,
      theme: ThemeData(
        brightness: brightness,
        fontFamily: 'FidibekiGolden',
        useMaterial3: true,
      ),
      builder: (context, child) {
        return MediaQuery(
          data: MediaQuery.of(context).copyWith(
            platformBrightness: brightness,
            textScaler: TextScaler.linear(textScale),
          ),
          child: RepaintBoundary(
            key: ValueKey('fidibeki-golden-$goldenName'),
            child: FidibekiFeedbackHost(
              controller: controller,
              child: child ?? const SizedBox.shrink(),
            ),
          ),
        );
      },
      home: const ColoredBox(color: Color(0xffe7eeeb)),
    ),
  );
  await tester.pump();
  await controller.open();
  if (selected) {
    controller.setRating('rating', 4);
    controller.setChoice('reaction', 'happy');
    controller.setChoice('journey', 'easy');
  }
  await tester.pumpAndSettle();
  return controller;
}

void main() {
  setUpAll(loadGoldenFont);
  setUp(resetFidibekiProcessAppSessionForTesting);

  for (final scenario
      in <
        ({
          String name,
          Size size,
          Brightness brightness,
          double textScale,
          bool longCopy,
          bool selected,
        })
      >[
        (
          name: 'compact_light',
          size: const Size(320, 568),
          brightness: Brightness.light,
          textScale: 1,
          longCopy: false,
          selected: false,
        ),
        (
          name: 'standard_light',
          size: const Size(390, 844),
          brightness: Brightness.light,
          textScale: 1,
          longCopy: false,
          selected: true,
        ),
        (
          name: 'standard_dark',
          size: const Size(390, 844),
          brightness: Brightness.dark,
          textScale: 1,
          longCopy: false,
          selected: true,
        ),
        (
          name: 'compact_large_text',
          size: const Size(320, 568),
          brightness: Brightness.light,
          textScale: 1.6,
          longCopy: true,
          selected: false,
        ),
      ]) {
    testWidgets('matches ${scenario.name} Compact Progressive sheet', (
      tester,
    ) async {
      final controller = await pumpGoldenSheet(
        tester,
        size: scenario.size,
        brightness: scenario.brightness,
        textScale: scenario.textScale,
        longCopy: scenario.longCopy,
        selected: scenario.selected,
        goldenName: scenario.name,
      );

      expect(tester.takeException(), isNull);
      await expectLater(
        find.byKey(ValueKey('fidibeki-golden-${scenario.name}')),
        matchesGoldenFile('goldens/sheet_${scenario.name}.png'),
      );
      controller.dispose();
    }, tags: 'golden');
  }
}
