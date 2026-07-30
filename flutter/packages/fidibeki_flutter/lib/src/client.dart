import 'dart:convert';

import 'package:crypto/crypto.dart';
import 'package:http/http.dart' as http;

import 'errors.dart';
import 'localization.dart';
import 'models.dart';
import 'screenshot.dart';
import 'submission.dart';

bool _isLocalDevelopmentHost(String host) {
  return const <String>{
    'localhost',
    '127.0.0.1',
    '::1',
    '10.0.2.2',
  }.contains(host);
}

Uri _normalizeApiBaseUrl(Uri value) {
  if (!value.hasScheme ||
      value.host.isEmpty ||
      value.userInfo.isNotEmpty ||
      (value.scheme != 'https' &&
          !(value.scheme == 'http' && _isLocalDevelopmentHost(value.host)))) {
    throw ArgumentError.value(
      value,
      'apiBaseUrl',
      'must use HTTPS outside a local development runtime',
    );
  }
  final normalizedPath = value.path.replaceFirst(RegExp(r'/+$'), '');
  return value.replace(path: normalizedPath, query: null, fragment: null);
}

bool _isRetryableStatus(int status) {
  return status == 408 || status == 429 || status >= 500;
}

FidibekiException? _terminalStatusError(int status) {
  if (status == 401 || status == 403 || status == 404) {
    return const FidibekiException(
      FidibekiErrorCode.accessDenied,
      'The capture session is not available.',
    );
  }
  if (status == 410) {
    return const FidibekiException(
      FidibekiErrorCode.sessionExpired,
      'The capture session has expired.',
    );
  }
  return null;
}

Map<String, String> _validationFields(
  Object? details,
  FidibekiCaptureSession session,
) {
  final fields = <String, String>{};
  final messages = fidibekiSystemMessages(session.configuration.displayLocale);
  final responseFields = <String>{
    for (final block in session.configuration.definition.responseBlocks)
      'responses.${block.key}',
  };

  String? safeMessage(String field) {
    if (responseFields.contains(field)) return messages.invalidResponse;
    return switch (field) {
      'comment' =>
        session.configuration.displayLocale.toLowerCase().startsWith('pt')
            ? 'Introduza um comentário válido.'
            : 'Enter a valid comment.',
      'reporter_email' => messages.invalidEmail,
      'consent' => messages.consentRequired,
      _ => null,
    };
  }

  void addField(Object? field, Object? message) {
    if (field is! String || message is! String) return;
    final safe = safeMessage(field);
    if (safe != null) fields[field] = safe;
  }

  if (details is List) {
    for (final detail in details) {
      if (detail is Map) addField(detail['field'], detail['message']);
    }
  } else if (details is Map && details['fields'] is Map) {
    for (final entry in (details['fields']! as Map).entries) {
      addField(entry.key, entry.value);
    }
  }
  return Map.unmodifiable(fields);
}

FidibekiException _responseError(
  int status,
  Object? body,
  FidibekiCaptureSession session,
) {
  if (body is! Map || body['error'] is! Map) {
    return FidibekiException(
      FidibekiErrorCode.network,
      'The server returned an invalid error response.',
      retryable: _isRetryableStatus(status),
    );
  }
  final error = body['error']! as Map;
  final code = error['code'];
  if (status == 409 && code == 'configuration_changed') {
    return const FidibekiException(
      FidibekiErrorCode.configurationChanged,
      'The feedback configuration has changed.',
    );
  }
  if (status == 422) {
    return FidibekiException(
      FidibekiErrorCode.validation,
      'Feedback is invalid.',
      fields: _validationFields(error['details'], session),
    );
  }
  return FidibekiException(
    FidibekiErrorCode.network,
    'Feedback request failed.',
    retryable: _isRetryableStatus(status),
  );
}

final class FidibekiMobileClient {
  FidibekiMobileClient({
    required Uri apiBaseUrl,
    http.Client? httpClient,
    DateTime Function()? now,
  }) : _apiBaseUrl = _normalizeApiBaseUrl(apiBaseUrl),
       _httpClient = httpClient ?? http.Client(),
       _ownsHttpClient = httpClient == null,
       _now = now ?? DateTime.now;

  static const _maximumRetainedAttempts = 32;
  static const _boundRetryDuration = Duration(minutes: 10);

  final Uri _apiBaseUrl;
  final http.Client _httpClient;
  final bool _ownsHttpClient;
  final DateTime Function() _now;
  final Map<String, _FidibekiSubmissionAttempt> _attempts =
      <String, _FidibekiSubmissionAttempt>{};

  Future<FidibekiFeedbackAccepted> submitFeedback(
    FidibekiCaptureSession session,
    FidibekiFeedbackRequest request,
  ) {
    final serialized = jsonEncode(request.toWireJson());
    final digest = sha256.convert(utf8.encode(serialized)).toString();
    final now = _now().toUtc();
    _attempts.removeWhere((_, attempt) {
      return attempt.inFlight == null &&
          (!attempt.retain || !now.isBefore(attempt.expiresAt));
    });

    var attempt = _attempts[request.clientFeedbackId];
    if (attempt != null && attempt.digest != digest) {
      return Future<FidibekiFeedbackAccepted>.error(
        const FidibekiException(
          FidibekiErrorCode.retryPayloadChanged,
          'An idempotent retry must use the exact original request.',
        ),
      );
    }
    if (attempt == null) {
      if (!now.isBefore(session.submitExpiresAt)) {
        return Future<FidibekiFeedbackAccepted>.error(
          const FidibekiException(
            FidibekiErrorCode.sessionExpired,
            'The capture session has expired.',
          ),
        );
      }
      if (_attempts.length >= _maximumRetainedAttempts) {
        return Future<FidibekiFeedbackAccepted>.error(
          const FidibekiException(
            FidibekiErrorCode.network,
            'Too many feedback requests are awaiting a result.',
            retryable: true,
          ),
        );
      }
      attempt = _FidibekiSubmissionAttempt(
        digest: digest,
        expiresAt: session.submitExpiresAt,
      );
      _attempts[request.clientFeedbackId] = attempt;
    }
    final inFlight = attempt.inFlight;
    if (inFlight != null) return inFlight;

    late final Future<FidibekiFeedbackAccepted> tracked;
    tracked = _performSubmission(session, serialized, attempt).whenComplete(() {
      if (identical(attempt!.inFlight, tracked)) {
        attempt.inFlight = null;
      }
      if (!attempt.retain) {
        _attempts.remove(request.clientFeedbackId);
      }
    });
    attempt.inFlight = tracked;
    return tracked;
  }

  Future<FidibekiFeedbackAccepted> _performSubmission(
    FidibekiCaptureSession session,
    String serialized,
    _FidibekiSubmissionAttempt attempt,
  ) async {
    http.Response response;
    try {
      response = await _httpClient.post(
        Uri.parse('$_apiBaseUrl/v1/mobile/feedback'),
        headers: <String, String>{
          'authorization': 'Bearer ${session.token}',
          'content-type': 'application/json',
        },
        body: serialized,
      );
    } on Object {
      _bindAttempt(attempt);
      throw const FidibekiException(
        FidibekiErrorCode.network,
        'Feedback request failed.',
        retryable: true,
      );
    }
    if (response.statusCode < 200 || response.statusCode >= 300) {
      final terminal = _terminalStatusError(response.statusCode);
      if (terminal != null) {
        attempt.retain = false;
        throw terminal;
      }
      Object? errorBody;
      try {
        errorBody = jsonDecode(response.body);
      } on Object {
        final retryable = _isRetryableStatus(response.statusCode);
        if (retryable) {
          _bindAttempt(attempt);
        } else {
          attempt.retain = attempt.bound;
        }
        throw FidibekiException(
          FidibekiErrorCode.network,
          'The server returned an invalid error response.',
          retryable: retryable,
        );
      }
      final error = _responseError(response.statusCode, errorBody, session);
      if (error.retryable) {
        _bindAttempt(attempt);
      } else {
        attempt.retain = false;
      }
      throw error;
    }
    Object? body;
    try {
      body = jsonDecode(response.body);
    } on Object {
      _bindAttempt(attempt);
      throw const FidibekiException(
        FidibekiErrorCode.invalidResponse,
        'The server returned an invalid response.',
        retryable: true,
      );
    }
    try {
      final accepted = FidibekiFeedbackAccepted.fromJson(body);
      _bindAttempt(attempt);
      return accepted;
    } on FidibekiException {
      _bindAttempt(attempt);
      throw const FidibekiException(
        FidibekiErrorCode.invalidResponse,
        'The server returned an invalid response.',
        retryable: true,
      );
    }
  }

  Future<FidibekiAttachmentOutcome> uploadScreenshot(
    FidibekiCaptureSession session,
    FidibekiFeedbackAccepted accepted,
    FidibekiPreparedScreenshot screenshot,
  ) async {
    final grant = accepted.attachments
        .where(
          (candidate) =>
              candidate.clientAttachmentId == screenshot.clientAttachmentId,
        )
        .firstOrNull;
    if (grant == null) {
      return FidibekiAttachmentOutcome(
        attachmentId: screenshot.clientAttachmentId,
        status: FidibekiAttachmentStatus.failed,
        stage: FidibekiAttachmentStage.upload,
        error: 'The server did not grant this attachment.',
      );
    }
    final uploadUrl = grant.presignedUrl;
    final localDevelopment =
        uploadUrl.scheme == 'http' && _isLocalDevelopmentHost(uploadUrl.host);
    if ((uploadUrl.scheme != 'https' && !localDevelopment) ||
        uploadUrl.host.isEmpty ||
        uploadUrl.userInfo.isNotEmpty ||
        uploadUrl.fragment.isNotEmpty ||
        !grant.expiresAt.isAfter(_now().toUtc())) {
      return FidibekiAttachmentOutcome(
        attachmentId: grant.attachmentId,
        status: FidibekiAttachmentStatus.failed,
        stage: FidibekiAttachmentStage.upload,
        error: 'The attachment upload grant is invalid.',
      );
    }

    int uploadStatus;
    try {
      final upload = http.Request(grant.method, uploadUrl)
        ..followRedirects = false
        ..headers.addAll(grant.headers)
        ..headers['content-type'] = screenshot.contentType
        ..bodyBytes = screenshot.bytes;
      final response = await _httpClient.send(upload);
      uploadStatus = response.statusCode;
      await response.stream.drain<void>();
    } on Object {
      return FidibekiAttachmentOutcome(
        attachmentId: grant.attachmentId,
        status: FidibekiAttachmentStatus.failed,
        stage: FidibekiAttachmentStage.upload,
        error: 'The screenshot upload failed.',
      );
    }
    if (uploadStatus < 200 || uploadStatus >= 300) {
      return FidibekiAttachmentOutcome(
        attachmentId: grant.attachmentId,
        status: FidibekiAttachmentStatus.failed,
        stage: FidibekiAttachmentStage.upload,
        error: 'The screenshot upload failed.',
      );
    }

    int completionStatus;
    try {
      final request =
          http.Request(
              'POST',
              Uri.parse(
                '$_apiBaseUrl/v1/mobile/attachments/'
                '${Uri.encodeComponent(grant.attachmentId)}/complete',
              ),
            )
            ..followRedirects = false
            ..headers.addAll(<String, String>{
              'authorization': 'Bearer ${session.token}',
              'content-type': 'application/json',
            })
            ..body = jsonEncode(<String, String>{
              'complete_token': grant.completeToken,
            });
      final response = await _httpClient.send(request);
      completionStatus = response.statusCode;
      await response.stream.drain<void>();
    } on Object {
      return FidibekiAttachmentOutcome(
        attachmentId: grant.attachmentId,
        status: FidibekiAttachmentStatus.failed,
        stage: FidibekiAttachmentStage.complete,
        error: 'The screenshot completion request failed.',
      );
    }
    if (completionStatus < 200 || completionStatus >= 300) {
      return FidibekiAttachmentOutcome(
        attachmentId: grant.attachmentId,
        status: FidibekiAttachmentStatus.failed,
        stage: FidibekiAttachmentStage.complete,
        error: 'The screenshot completion request failed.',
      );
    }
    return FidibekiAttachmentOutcome(
      attachmentId: grant.attachmentId,
      status: FidibekiAttachmentStatus.uploaded,
    );
  }

  void _bindAttempt(_FidibekiSubmissionAttempt attempt) {
    attempt.retain = true;
    if (!attempt.bound) {
      attempt.bound = true;
      attempt.expiresAt = _now().toUtc().add(_boundRetryDuration);
    }
  }

  void close() {
    if (_ownsHttpClient) _httpClient.close();
  }
}

final class _FidibekiSubmissionAttempt {
  _FidibekiSubmissionAttempt({required this.digest, required this.expiresAt});

  final String digest;
  DateTime expiresAt;
  Future<FidibekiFeedbackAccepted>? inFlight;
  bool retain = true;
  bool bound = false;
}
