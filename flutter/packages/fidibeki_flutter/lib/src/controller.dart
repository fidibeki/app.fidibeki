import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/widgets.dart';
import 'package:uuid/uuid.dart';

import 'client.dart';
import 'errors.dart';
import 'localization.dart';
import 'models.dart';
import 'recurrence.dart';
import 'screenshot.dart';
import 'storage.dart';
import 'submission.dart';
import 'theme.dart';

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
    this.linkOpener,
    this.theme,
    this.screenshotProvider,
    String Function()? clientAttachmentIdGenerator,
  }) : platform = platform ?? _runtimePlatform(),
       normalizedScreen = normalizeFidibekiScreen(screen),
       _now = now ?? DateTime.now,
       _mobileClient =
           mobileClient ??
           FidibekiMobileClient(apiBaseUrl: apiBaseUrl, now: now),
       _ownsMobileClient = mobileClient == null,
       _feedbackIdGenerator = feedbackIdGenerator ?? const Uuid().v4,
       _feedbackId = (feedbackIdGenerator ?? const Uuid().v4).call(),
       _clientAttachmentIdGenerator =
           clientAttachmentIdGenerator ?? const Uuid().v4,
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
  final Future<void> Function(Uri url)? linkOpener;
  final FidibekiTheme? theme;
  final FidibekiScreenshotProvider? screenshotProvider;
  final DateTime Function() _now;
  final FidibekiMobileClient _mobileClient;
  final bool _ownsMobileClient;
  final String Function() _feedbackIdGenerator;
  final String Function() _clientAttachmentIdGenerator;
  String _feedbackId;
  final FidibekiRecurrenceEngine _recurrence;
  final Map<String, Object> _responses = <String, Object>{};
  String _comment = '';
  String _reporterEmail = '';
  bool _consentGranted = false;
  bool _commentExpanded = false;
  bool _contactExpanded = false;
  FidibekiPreparedScreenshot? _screenshot;
  FidibekiScreenshotNotice? _screenshotNotice;
  FidibekiAttachmentOutcome? _attachmentOutcome;
  bool _readingScreenshot = false;
  int _screenshotGeneration = 0;

  FidibekiFeedbackStatus _status = FidibekiFeedbackStatus.initializing;
  FidibekiCaptureSession? _session;
  FidibekiFeedbackAccepted? _accepted;
  FidibekiException? _error;
  DateTime? _nextEligibleAt;
  bool _eligible = false;
  bool _visible = false;
  bool _disposed = false;
  Future<void>? _initialization;
  Future<void>? _configurationRefresh;
  Future<FidibekiFeedbackAccepted>? _submission;

  FidibekiFeedbackStatus get status => _status;
  FidibekiCaptureSession? get session => _session;
  FidibekiFeedbackAccepted? get accepted => _accepted;
  FidibekiException? get error => _error;
  Map<String, String> get fieldErrors =>
      _error?.fields ?? const <String, String>{};
  Map<String, Object> get responses => Map.unmodifiable(_responses);
  String get comment => _comment;
  String get reporterEmail => _reporterEmail;
  bool get consentGranted => _consentGranted;
  bool get commentExpanded => _commentExpanded;
  bool get contactExpanded => _contactExpanded;
  bool get hasScreenshot => _screenshot != null;
  bool get readingScreenshot => _readingScreenshot;
  FidibekiScreenshotNotice? get screenshotNotice => _screenshotNotice;
  FidibekiAttachmentOutcome? get attachmentOutcome => _attachmentOutcome;
  bool get canAddScreenshot {
    return screenshotProvider != null &&
        _session?.configuration.definition.canAttachScreenshot == true &&
        _screenshot == null;
  }

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
      final session = await _requestCaptureSession();
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

  Future<FidibekiCaptureSession> _requestCaptureSession() async {
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
    return session;
  }

  Future<void> refreshConfiguration() {
    return _refreshConfiguration();
  }

  Future<void> _refreshConfiguration({FidibekiException? reason}) {
    final active = _configurationRefresh;
    if (active != null) return active;
    late final Future<void> tracked;
    tracked = _performConfigurationRefresh(reason: reason).whenComplete(() {
      if (identical(_configurationRefresh, tracked)) {
        _configurationRefresh = null;
      }
    });
    _configurationRefresh = tracked;
    return tracked;
  }

  Future<void> _performConfigurationRefresh({FidibekiException? reason}) async {
    final previous = _session;
    if (_disposed || previous == null) {
      throw const FidibekiException(
        FidibekiErrorCode.accessDenied,
        'No capture session is available.',
      );
    }
    _setState(() {
      _status = FidibekiFeedbackStatus.initializing;
      _error = reason;
    });
    try {
      final next = await _requestCaptureSession();
      _reconcileDraft(
        previous.configuration.definition,
        next.configuration.definition,
        contactAvailable:
            next.configuration.definition.collectContact &&
            next.configuration.contactNotice != null,
      );
      _setState(() {
        _session = next;
        _feedbackId = _feedbackIdGenerator();
        _accepted = null;
        _attachmentOutcome = null;
        _eligible = true;
        _nextEligibleAt = null;
        _error = reason;
        _status = _visible
            ? FidibekiFeedbackStatus.open
            : FidibekiFeedbackStatus.ready;
      });
    } on FidibekiException catch (error) {
      _setState(() {
        _error = error;
        _status = _visible
            ? FidibekiFeedbackStatus.open
            : FidibekiFeedbackStatus.error;
      });
    } on Object {
      const error = FidibekiException(
        FidibekiErrorCode.network,
        'Capture session request failed.',
        retryable: true,
      );
      _setState(() {
        _error = error;
        _status = _visible
            ? FidibekiFeedbackStatus.open
            : FidibekiFeedbackStatus.error;
      });
    }
  }

  void _reconcileDraft(
    FidibekiWidgetDefinition previous,
    FidibekiWidgetDefinition next, {
    required bool contactAvailable,
  }) {
    final previousBlocks = <String, FidibekiResponseBlock>{
      for (final block in previous.responseBlocks) block.key: block,
    };
    final nextBlocks = <String, FidibekiResponseBlock>{
      for (final block in next.responseBlocks) block.key: block,
    };
    _responses.removeWhere((key, value) {
      final oldBlock = previousBlocks[key];
      final newBlock = nextBlocks[key];
      return oldBlock == null ||
          newBlock == null ||
          oldBlock.type != newBlock.type ||
          !_responseValueIsValid(newBlock, value);
    });
    if (!next.allowComment) {
      _comment = '';
      _commentExpanded = false;
    }
    if (!contactAvailable) {
      _reporterEmail = '';
      _contactExpanded = false;
    }
    if (!next.canAttachScreenshot) {
      _screenshotGeneration += 1;
      final screenshot = _screenshot;
      _screenshot = null;
      _screenshotNotice = null;
      _readingScreenshot = false;
      if (screenshot != null) unawaited(screenshot.dispose());
    }
    _consentGranted = false;
  }

  bool _responseValueIsValid(FidibekiResponseBlock block, Object value) {
    return switch (block.type) {
      FidibekiResponseBlockType.rating =>
        value is int && value >= 1 && value <= 5,
      FidibekiResponseBlockType.freeText =>
        value is String &&
            value.trim().isNotEmpty &&
            value.trim().length <= 10000,
      FidibekiResponseBlockType.reaction ||
      FidibekiResponseBlockType.singleChoice =>
        value is String && block.options.any((option) => option.value == value),
    };
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

  void setChoice(String key, String value) {
    _setResponse(key, value);
  }

  void setComment(String value) {
    if (_canEditDraft()) {
      _setState(() {
        _comment = value;
        _error = null;
        _status = FidibekiFeedbackStatus.open;
      });
    }
  }

  void setReporterEmail(String value) {
    if (_canEditDraft()) {
      _setState(() {
        _reporterEmail = value;
        _error = null;
        _status = FidibekiFeedbackStatus.open;
      });
    }
  }

  void setConsentGranted(bool value) {
    if (_canEditDraft()) {
      _setState(() {
        _consentGranted = value;
        _error = null;
        _status = FidibekiFeedbackStatus.open;
      });
    }
  }

  void toggleCommentExpanded() {
    final definition = _session?.configuration.definition;
    if (!_canEditDraft() || definition?.allowComment != true) return;
    _setState(() {
      _commentExpanded = !_commentExpanded;
    });
  }

  void expandContact() {
    final configuration = _session?.configuration;
    if (!_canEditDraft() ||
        configuration?.definition.collectContact != true ||
        configuration?.contactNotice == null) {
      return;
    }
    _setState(() {
      _contactExpanded = true;
    });
  }

  Future<void> openLink(Uri url) async {
    await linkOpener?.call(url);
  }

  Future<void> addScreenshot() async {
    final provider = screenshotProvider;
    final definition = _session?.configuration.definition;
    if (!_canEditDraft() ||
        provider == null ||
        definition?.canAttachScreenshot != true ||
        _screenshot != null ||
        _readingScreenshot) {
      return;
    }
    final generation = ++_screenshotGeneration;
    _setState(() {
      _readingScreenshot = true;
      _screenshotNotice = null;
    });

    FidibekiScreenshotAsset? asset;
    try {
      asset = await provider();
    } on Object {
      if (_disposed || generation != _screenshotGeneration) return;
      _setState(() {
        _readingScreenshot = false;
        _screenshotNotice = FidibekiScreenshotNotice.providerFailed;
      });
      return;
    }
    if (_disposed || generation != _screenshotGeneration) {
      await _disposeScreenshotAsset(asset);
      return;
    }
    if (asset == null) {
      _setState(() {
        _readingScreenshot = false;
        _screenshotNotice = FidibekiScreenshotNotice.cancelled;
      });
      return;
    }

    FidibekiPreparedScreenshot prepared;
    try {
      prepared = await prepareFidibekiScreenshot(
        asset,
        clientAttachmentIdGenerator: _clientAttachmentIdGenerator,
      );
    } on FidibekiException {
      if (_disposed || generation != _screenshotGeneration) return;
      _setState(() {
        _readingScreenshot = false;
        _screenshotNotice = FidibekiScreenshotNotice.invalid;
      });
      return;
    }
    if (_disposed || generation != _screenshotGeneration) {
      await prepared.dispose();
      return;
    }
    _setState(() {
      _screenshot = prepared;
      _readingScreenshot = false;
      _screenshotNotice = null;
    });
  }

  Future<void> _disposeScreenshotAsset(FidibekiScreenshotAsset? asset) async {
    try {
      await asset?.dispose?.call();
    } on Object {
      // Host cleanup failures remain private.
    }
  }

  Future<void> removeScreenshot() async {
    if (!_canEditDraft()) return;
    _screenshotGeneration += 1;
    final screenshot = _screenshot;
    _setState(() {
      _screenshot = null;
      _screenshotNotice = null;
      _readingScreenshot = false;
    });
    await screenshot?.dispose();
  }

  bool _canEditDraft() {
    return !_disposed &&
        _visible &&
        (_status == FidibekiFeedbackStatus.open ||
            _status == FidibekiFeedbackStatus.error);
  }

  void _setResponse(String key, Object value) {
    if (!_canEditDraft()) return;
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
    final messages = fidibekiSystemMessages(displayLocale);
    final fields = _validateResponses(
      current.configuration.definition,
      displayLocale,
    );
    if (fields.isNotEmpty) {
      final error = FidibekiException(
        FidibekiErrorCode.validation,
        messages.requiredResponse,
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
    final definition = current.configuration.definition;
    final comment = definition.allowComment ? _comment.trim() : '';
    final contactNotice = current.configuration.contactNotice;
    final reporterEmail = definition.collectContact && contactNotice != null
        ? _reporterEmail.trim()
        : '';
    final consent = reporterEmail.isEmpty || !_consentGranted
        ? null
        : FidibekiReporterConsent(
            noticeVersion: contactNotice!.noticeVersion,
            controllerProfileVersion: contactNotice.controllerProfileVersion,
            widgetDefinitionVersion: definition.definitionVersion,
            locale: contactNotice.locale,
          );
    final screenshot = definition.canAttachScreenshot ? _screenshot : null;
    final request = FidibekiFeedbackRequest(
      clientFeedbackId: _feedbackId,
      responses: <String, Object>{
        for (final entry in _responses.entries)
          if (allowedKeys.contains(entry.key)) entry.key: entry.value,
      },
      appVersion: appVersion,
      osVersion: osVersion,
      deviceLocale: deviceLocale,
      comment: comment.isEmpty ? null : comment,
      reporterEmail: reporterEmail.isEmpty ? null : reporterEmail,
      notifyReporter: reporterEmail.isNotEmpty,
      consent: consent,
      screenshot: screenshot == null
          ? null
          : FidibekiScreenshotDeclaration(
              clientAttachmentId: screenshot.clientAttachmentId,
              contentType: screenshot.contentType,
              sizeBytes: screenshot.sizeBytes,
            ),
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
        _attachmentOutcome = null;
        _eligible = false;
        _nextEligibleAt = recurrence.nextEligibleAt;
        _status = FidibekiFeedbackStatus.submitted;
      });
      if (screenshot != null) {
        final outcome = await _mobileClient.uploadScreenshot(
          current,
          accepted,
          screenshot,
        );
        await screenshot.dispose();
        _setState(() {
          if (identical(_screenshot, screenshot)) {
            _screenshot = null;
          }
          _attachmentOutcome = outcome;
        });
      }
      return accepted;
    } on FidibekiException catch (error) {
      _setState(() {
        _error = error;
        _status = FidibekiFeedbackStatus.open;
      });
      if (error.code == FidibekiErrorCode.configurationChanged) {
        await _refreshConfiguration(reason: error);
      }
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

  Map<String, String> _validateResponses(
    FidibekiWidgetDefinition definition,
    String displayLocale,
  ) {
    final messages = fidibekiSystemMessages(displayLocale);
    final portuguese =
        displayLocale.trim().toLowerCase().split(RegExp('[-_]')).firstOrNull ==
        'pt';
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
          } else if (value is String && value.length > 10000) {
            fields['responses.${block.key}'] = portuguese
                ? 'O feedback não pode exceder 10000 caracteres.'
                : 'Feedback must not exceed 10000 characters.';
          } else if (block.required &&
              (value is! String || value.trim().isEmpty)) {
            fields['responses.${block.key}'] = portuguese
                ? 'Escreva o seu feedback.'
                : 'Enter feedback.';
          }
        case FidibekiResponseBlockType.reaction:
        case FidibekiResponseBlockType.singleChoice:
          final valid =
              value is String &&
              block.options.any((option) => option.value == value);
          if (value != null && !valid) {
            fields['responses.${block.key}'] = portuguese
                ? 'Selecione uma resposta válida.'
                : 'Select a valid response.';
          } else if (block.required && !valid) {
            fields['responses.${block.key}'] = messages.requiredResponse;
          }
      }
    }
    if (_comment.trim().length > 10000) {
      fields['comment'] = portuguese
          ? 'O comentário não pode exceder 10000 caracteres.'
          : 'Comment must not exceed 10000 characters.';
    }
    final contactAvailable =
        definition.collectContact &&
        _session?.configuration.contactNotice != null;
    final reporter = contactAvailable ? _reporterEmail.trim() : '';
    if (reporter.isNotEmpty) {
      final validEmail =
          reporter.length <= 320 &&
          RegExp(r'^[^\s@]+@[^\s@]+\.[^\s@]+$').hasMatch(reporter);
      if (!validEmail) {
        fields['reporter_email'] = messages.invalidEmail;
      }
      if (!_consentGranted) {
        fields['consent'] = messages.consentRequired;
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
    _screenshotGeneration += 1;
    final screenshot = _screenshot;
    _screenshot = null;
    if (screenshot != null) unawaited(screenshot.dispose());
    if (_ownsMobileClient) _mobileClient.close();
    super.dispose();
  }
}
