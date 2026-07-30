import 'dart:convert';
import 'dart:io';

import 'package:fidibeki_flutter/fidibeki_flutter.dart';
import 'package:fidibeki_flutter_shared_preferences/fidibeki_flutter_shared_preferences.dart';
import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;

String get localHost => Platform.isAndroid ? '10.0.2.2' : '127.0.0.1';

String get widgetBaseUrl {
  const configured = String.fromEnvironment('FIDIBEKI_WIDGET_BASE_URL');
  return configured.isEmpty ? 'http://$localHost:43136' : configured;
}

String get sessionUrl {
  const configured = String.fromEnvironment('FIDIBEKI_SESSION_URL');
  return configured.isEmpty ? 'http://$localHost:43136/session' : configured;
}

Future<FidibekiCaptureSession> createCaptureSession(
  FidibekiSessionRequest request,
) async {
  final response = await http.post(
    Uri.parse(sessionUrl),
    headers: const <String, String>{'content-type': 'application/json'},
    body: jsonEncode(<String, Object?>{
      'platform': request.platform.wireName,
      'screen': request.screen,
      'display_locale': request.displayLocale,
      'application_id': 'com.fidibeki.fidibeki_flutter_example',
      'widget_definition_key': 'mobile-example',
    }),
  );
  if (response.statusCode < 200 || response.statusCode >= 300) {
    throw StateError('Session endpoint returned ${response.statusCode}');
  }
  return FidibekiCaptureSession.fromJson(
    jsonDecode(response.body) as Map<String, Object?>,
  );
}

Future<FidibekiScreenshotAsset?> captureExampleScreenshot() async {
  final response = await http.get(Uri.parse('$widgetBaseUrl/fixture.png'));
  if (response.statusCode < 200 || response.statusCode >= 300) {
    throw StateError('Screenshot endpoint returned ${response.statusCode}');
  }
  final bytes = response.bodyBytes;
  return FidibekiScreenshotAsset(
    contentType: 'image/png',
    sizeBytes: bytes.length,
    read: (_) async => bytes,
  );
}

void main() {
  WidgetsFlutterBinding.ensureInitialized();
  runApp(FidibekiExampleApp(storage: SharedPreferencesFidibekiStorage()));
}

final class FidibekiExampleApp extends StatefulWidget {
  const FidibekiExampleApp({required this.storage, super.key});

  final FidibekiStorage storage;

  @override
  State<FidibekiExampleApp> createState() => _FidibekiExampleAppState();
}

final class _FidibekiExampleAppState extends State<FidibekiExampleApp> {
  late final FidibekiFeedbackController feedback = FidibekiFeedbackController(
    apiBaseUrl: Uri.parse(widgetBaseUrl),
    appVersion: '0.1.0',
    deviceLocale: Platform.localeName,
    locale: const Locale('en'),
    osVersion: Platform.operatingSystemVersion,
    screen: '/checkout/review',
    screenshotProvider: captureExampleScreenshot,
    sessionProvider: createCaptureSession,
    storage: widget.storage,
  );

  @override
  void dispose() {
    feedback.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      debugShowCheckedModeBanner: false,
      theme: ThemeData(
        colorScheme: ColorScheme.fromSeed(seedColor: const Color(0xff087f6b)),
      ),
      darkTheme: ThemeData(
        brightness: Brightness.dark,
        colorScheme: ColorScheme.fromSeed(
          seedColor: const Color(0xff087f6b),
          brightness: Brightness.dark,
        ),
      ),
      builder: (context, child) => FidibekiFeedbackHost(
        controller: feedback,
        child: child ?? const SizedBox.shrink(),
      ),
      home: _CheckoutScreen(feedback: feedback),
    );
  }
}

final class _CheckoutScreen extends StatelessWidget {
  const _CheckoutScreen({required this.feedback});

  final FidibekiFeedbackController feedback;

  @override
  Widget build(BuildContext context) {
    return ListenableBuilder(
      listenable: feedback,
      builder: (context, _) {
        return Scaffold(
          appBar: AppBar(
            title: const Text('Checkout review'),
            actions: <Widget>[
              Padding(
                padding: const EdgeInsets.only(right: 4),
                child: Center(
                  child: Text(
                    feedback.status.name,
                    key: const ValueKey('fidibeki-status'),
                    style: Theme.of(context).textTheme.labelMedium,
                  ),
                ),
              ),
              IconButton(
                key: const ValueKey('host-feedback-trigger'),
                tooltip: 'Give feedback',
                onPressed: feedback.eligible ? feedback.open : null,
                icon: const Text('💬', style: TextStyle(fontSize: 24)),
              ),
            ],
          ),
          body: ListView(
            padding: const EdgeInsets.all(20),
            children: <Widget>[
              Text(
                'Order #1048',
                style: Theme.of(context).textTheme.headlineSmall,
              ),
              const SizedBox(height: 20),
              const ListTile(
                contentPadding: EdgeInsets.zero,
                title: Text('Workspace subscription'),
                subtitle: Text('Annual plan'),
                trailing: Text('€228.00'),
              ),
              const Divider(),
              const ListTile(
                contentPadding: EdgeInsets.zero,
                title: Text('Payment method'),
                trailing: Text('Visa •••• 4242'),
              ),
            ],
          ),
        );
      },
    );
  }
}
