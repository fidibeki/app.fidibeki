import 'package:flutter/foundation.dart';
import 'package:flutter/widgets.dart';
import 'package:uuid/uuid.dart';

import 'client.dart';
import 'errors.dart';
import 'models.dart';
import 'recurrence.dart';
import 'storage.dart';
import 'submission.dart';

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
    FidibekiMobileClient? mobileClient,
    String Function()? feedbackIdGenerator,
  }) : platform = platform ?? _runtimePlatform(),
       normalizedScreen = normalizeFidibekiScreen(screen),
       _now = now ?? DateTime.now,
       _mobileClient =
           mobileClient ??
           FidibekiMobileClient(apiBaseUrl: apiBaseUrl, now: now),
       _ownsMobileClient = mobileClient == null,
       _feedbackId = (feedbackIdGenerator ?? const Uuid().v4).call(),
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
  final FidibekiMobileClient _mobileClient;
  final bool _ownsMobileClient;
  final String _feedbackId;
  final FidibekiRecurrenceEngine _recurrence;
  final Map<String, Object> _responses = <String, Object>{};

  FidibekiFeedbackStatus _status = FidibekiFeedbackStatus.initializing;
  FidibekiCaptureSession? _session;
  FidibekiFeedbackAccepted? _accepted;
  FidibekiException? _error;
  DateTime? _nextEligibleAt;
  bool _eligible = false;
  bool _visible = false;
  bool _disposed = false;
  Future<void>? _initialization;
  Future<FidibekiFeedbackAccepted>? _submission;

  FidibekiFeedbackStatus get status => _status;
  FidibekiCaptureSession? get session => _session;
  FidibekiFeedbackAccepted? get accepted => _accepted;
  FidibekiException? get error => _error;
  Map<String, String> get fieldErrors =>
      _error?.fields ?? const <String, String>{};
  Map<String, Object> get responses => Map.unmodifiable(_responses);
  DateTime? get nextEligibleAt => _nextEligibleAt;
  bool get eligible => _eligible;
  bool get visible => _visible;

  Object? response(String key) => _responses[key];

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

  void setRating(String key, int rating) {
    if (rating < 1 || rating > 5) {
      throw RangeError.range(rating, 1, 5, 'rating');
    }
    _setResponse(key, rating);
  }

  void setText(String key, String value) {
    _setResponse(key, value);
  }

  void _setResponse(String key, Object value) {
    if (_disposed ||
        !_visible ||
        (_status != FidibekiFeedbackStatus.open &&
            _status != FidibekiFeedbackStatus.error)) {
      return;
    }
    _setState(() {
      _responses[key] = value;
      _error = null;
      _status = FidibekiFeedbackStatus.open;
    });
  }

  Future<FidibekiFeedbackAccepted> submit() {
    final active = _submission;
    if (active != null) return active;
    late final Future<FidibekiFeedbackAccepted> tracked;
    tracked = _submit().whenComplete(() {
      if (identical(_submission, tracked)) _submission = null;
    });
    _submission = tracked;
    return tracked;
  }

  Future<FidibekiFeedbackAccepted> _submit() async {
    final current = _session;
    if (_disposed || current == null || !_eligible || !_visible) {
      throw const FidibekiException(
        FidibekiErrorCode.accessDenied,
        'No capture session is available.',
      );
    }
    final displayLocale = current.configuration.displayLocale;
    final fields = _validateResponses(
      current.configuration.definition,
      displayLocale,
    );
    if (fields.isNotEmpty) {
      final error = FidibekiException(
        FidibekiErrorCode.validation,
        _usesPortuguese(displayLocale)
            ? 'Preencha os campos de feedback obrigatórios.'
            : 'Complete the required feedback fields.',
        fields: Map.unmodifiable(fields),
      );
      _setState(() {
        _error = error;
        _status = FidibekiFeedbackStatus.open;
      });
      throw error;
    }

    final allowedKeys = current.configuration.definition.responseBlocks
        .map((block) => block.key)
        .toSet();
    final request = FidibekiFeedbackRequest(
      clientFeedbackId: _feedbackId,
      responses: <String, Object>{
        for (final entry in _responses.entries)
          if (allowedKeys.contains(entry.key)) entry.key: entry.value,
      },
      appVersion: appVersion,
      osVersion: osVersion,
      deviceLocale: deviceLocale,
    );
    _setState(() {
      _error = null;
      _status = FidibekiFeedbackStatus.submitting;
    });
    try {
      final accepted = await _mobileClient.submitFeedback(current, request);
      await _recurrence.recordSubmitted(
        current.configuration.projectId,
        current.configuration.definition,
        normalizedScreen,
      );
      final recurrence = await _recurrence.eligibility(
        current.configuration.projectId,
        current.configuration.definition,
        normalizedScreen,
      );
      _setState(() {
        _accepted = accepted;
        _eligible = false;
        _nextEligibleAt = recurrence.nextEligibleAt;
        _status = FidibekiFeedbackStatus.submitted;
      });
      return accepted;
    } on FidibekiException catch (error) {
      _setState(() {
        _error = error;
        _status = FidibekiFeedbackStatus.open;
      });
      rethrow;
    } on Object {
      const error = FidibekiException(
        FidibekiErrorCode.network,
        'Feedback request failed.',
        retryable: true,
      );
      _setState(() {
        _error = error;
        _status = FidibekiFeedbackStatus.open;
      });
      throw error;
    }
  }

  bool _usesPortuguese(String displayLocale) {
    return displayLocale
            .trim()
            .toLowerCase()
            .split(RegExp('[-_]'))
            .firstOrNull ==
        'pt';
  }

  Map<String, String> _validateResponses(
    FidibekiWidgetDefinition definition,
    String displayLocale,
  ) {
    final portuguese = _usesPortuguese(displayLocale);
    final fields = <String, String>{};
    for (final block in definition.responseBlocks) {
      final value = _responses[block.key];
      switch (block.type) {
        case FidibekiResponseBlockType.rating:
          if (value != null && (value is! int || value < 1 || value > 5)) {
            fields['responses.${block.key}'] = portuguese
                ? 'Selecione uma classificação entre 1 e 5.'
                : 'Select a rating from 1 to 5.';
          } else if (block.required && value == null) {
            fields['responses.${block.key}'] = portuguese
                ? 'Selecione uma classificação.'
                : 'Select a rating.';
          }
        case FidibekiResponseBlockType.freeText:
          if (value != null && value is! String) {
            fields['responses.${block.key}'] = portuguese
                ? 'Escreva um feedback válido.'
                : 'Enter valid feedback.';
          } else if (value is String && value.length > 5000) {
            fields['responses.${block.key}'] = portuguese
                ? 'O feedback não pode exceder 5000 caracteres.'
                : 'Feedback must not exceed 5000 characters.';
          } else if (block.required &&
              (value is! String || value.trim().isEmpty)) {
            fields['responses.${block.key}'] = portuguese
                ? 'Escreva o seu feedback.'
                : 'Enter feedback.';
          }
        case FidibekiResponseBlockType.reaction:
        case FidibekiResponseBlockType.singleChoice:
          break;
      }
    }
    return fields;
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
    if (_ownsMobileClient) _mobileClient.close();
    super.dispose();
  }
}
