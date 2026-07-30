import 'package:flutter/foundation.dart';
import 'package:flutter/widgets.dart';
import 'package:uuid/uuid.dart';

import 'errors.dart';
import 'models.dart';
import 'recurrence.dart';
import 'storage.dart';

enum FidibekiFeedbackStatus {
  initializing,
  ready,
  open,
  submitting,
  submitted,
  ineligible,
  error,
}

FidibekiPlatform _runtimePlatform() {
  return switch (defaultTargetPlatform) {
    TargetPlatform.iOS => FidibekiPlatform.ios,
    TargetPlatform.android => FidibekiPlatform.android,
    _ => throw const FidibekiException(
      FidibekiErrorCode.accessDenied,
      'Fidibeki Flutter supports iOS and Android only.',
    ),
  };
}

final class FidibekiFeedbackController extends ChangeNotifier {
  FidibekiFeedbackController({
    required this.apiBaseUrl,
    required this.appVersion,
    required this.locale,
    required this.osVersion,
    required this.screen,
    required this.sessionProvider,
    required FidibekiStorage storage,
    this.deviceLocale,
    FidibekiPlatform? platform,
    String? appSessionId,
    DateTime Function()? now,
  }) : platform = platform ?? _runtimePlatform(),
       normalizedScreen = normalizeFidibekiScreen(screen),
       _now = now ?? DateTime.now,
       _recurrence = FidibekiRecurrenceEngine(
         storage,
         appSessionId: appSessionId ?? const Uuid().v4(),
         now: now,
       );

  final Uri apiBaseUrl;
  final String appVersion;
  final String? deviceLocale;
  final Locale locale;
  final String osVersion;
  final FidibekiPlatform platform;
  final String screen;
  final String normalizedScreen;
  final FidibekiSessionProvider sessionProvider;
  final DateTime Function() _now;
  final FidibekiRecurrenceEngine _recurrence;

  FidibekiFeedbackStatus _status = FidibekiFeedbackStatus.initializing;
  FidibekiCaptureSession? _session;
  FidibekiException? _error;
  DateTime? _nextEligibleAt;
  bool _eligible = false;
  bool _visible = false;
  bool _disposed = false;
  Future<void>? _initialization;

  FidibekiFeedbackStatus get status => _status;
  FidibekiCaptureSession? get session => _session;
  FidibekiException? get error => _error;
  DateTime? get nextEligibleAt => _nextEligibleAt;
  bool get eligible => _eligible;
  bool get visible => _visible;

  Future<void> initialize() {
    return _initialization ??= _initialize();
  }

  Future<void> _initialize() async {
    _setState(() {
      _status = FidibekiFeedbackStatus.initializing;
      _error = null;
    });
    try {
      final session = await sessionProvider(
        FidibekiSessionRequest(
          platform: platform,
          screen: normalizedScreen,
          displayLocale: locale.toLanguageTag(),
        ),
      );
      if (!session.submitExpiresAt.isAfter(_now().toUtc())) {
        throw const FidibekiException(
          FidibekiErrorCode.sessionExpired,
          'The capture session has expired.',
        );
      }
      final recurrence = await _recurrence.eligibility(
        session.configuration.projectId,
        session.configuration.definition,
        normalizedScreen,
      );
      _setState(() {
        _session = session;
        _eligible = recurrence.eligible;
        _nextEligibleAt = recurrence.nextEligibleAt;
        _status = recurrence.eligible
            ? FidibekiFeedbackStatus.ready
            : FidibekiFeedbackStatus.ineligible;
      });
    } on FidibekiException catch (error) {
      _fail(error);
    } on Object {
      _fail(
        const FidibekiException(
          FidibekiErrorCode.network,
          'Capture session request failed.',
          retryable: true,
        ),
      );
    }
  }

  Future<void> open() async {
    if (_disposed || _visible) return;
    await initialize();
    if (_session == null || !_eligible || _disposed) return;
    _setState(() {
      _visible = true;
      _status = FidibekiFeedbackStatus.open;
    });
  }

  void close() {
    if (_disposed ||
        !_visible ||
        _status == FidibekiFeedbackStatus.submitting) {
      return;
    }
    _setState(() {
      _visible = false;
      if (_status == FidibekiFeedbackStatus.open) {
        _status = FidibekiFeedbackStatus.ready;
      }
    });
  }

  void _fail(FidibekiException error) {
    _setState(() {
      _session = null;
      _eligible = false;
      _visible = false;
      _error = error;
      _status = FidibekiFeedbackStatus.error;
    });
  }

  void _setState(VoidCallback update) {
    if (_disposed) return;
    update();
    notifyListeners();
  }

  @override
  void dispose() {
    _disposed = true;
    super.dispose();
  }
}
