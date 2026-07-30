import 'errors.dart';

String _requiredString(Object? value, String field) {
  if (value is! String || value.trim().isEmpty) {
    throw FidibekiException(
      FidibekiErrorCode.invalidResponse,
      'Invalid mobile feedback $field.',
    );
  }
  return value;
}

Map<String, Object?> _requiredObject(Object? value, String field) {
  if (value is! Map) {
    throw FidibekiException(
      FidibekiErrorCode.invalidResponse,
      'Invalid mobile feedback $field.',
    );
  }
  try {
    return value.cast<String, Object?>();
  } on TypeError {
    throw FidibekiException(
      FidibekiErrorCode.invalidResponse,
      'Invalid mobile feedback $field.',
    );
  }
}

final class FidibekiFeedbackRequest {
  FidibekiFeedbackRequest({
    required this.clientFeedbackId,
    required Map<String, Object> responses,
    required this.appVersion,
    required this.osVersion,
    this.comment,
    this.deviceLocale,
  }) : responses = Map.unmodifiable(responses);

  factory FidibekiFeedbackRequest.fromJson(Map<String, Object?> json) {
    final rawResponses = _requiredObject(json['responses'], 'responses');
    final responses = <String, Object>{};
    for (final entry in rawResponses.entries) {
      final value = entry.value;
      if (value is! String && value is! int) {
        throw const FidibekiException(
          FidibekiErrorCode.invalidResponse,
          'Invalid mobile feedback response value.',
        );
      }
      responses[entry.key] = value as Object;
    }
    String? optionalString(String field) {
      final value = json[field];
      return value == null ? null : _requiredString(value, field);
    }

    return FidibekiFeedbackRequest(
      clientFeedbackId: _requiredString(
        json['client_feedback_id'],
        'client feedback ID',
      ),
      responses: responses,
      comment: optionalString('comment'),
      appVersion: _requiredString(json['app_version'], 'app version'),
      osVersion: _requiredString(json['os_version'], 'OS version'),
      deviceLocale: optionalString('device_locale'),
    );
  }

  final String clientFeedbackId;
  final Map<String, Object> responses;
  final String? comment;
  final String appVersion;
  final String osVersion;
  final String? deviceLocale;

  Map<String, Object> toWireJson() {
    final json = <String, Object>{
      'client_feedback_id': clientFeedbackId,
      'responses': responses,
      'app_version': appVersion,
      'os_version': osVersion,
    };
    final comment = this.comment;
    if (comment != null) json['comment'] = comment;
    final deviceLocale = this.deviceLocale;
    if (deviceLocale != null) json['device_locale'] = deviceLocale;
    return json;
  }
}

final class FidibekiAttachmentGrant {
  FidibekiAttachmentGrant({
    required this.attachmentId,
    required this.clientAttachmentId,
    required this.presignedUrl,
    required this.method,
    required Map<String, String> headers,
    required this.completeToken,
    required this.expiresAt,
  }) : headers = Map.unmodifiable(headers);

  factory FidibekiAttachmentGrant.fromJson(Object? value) {
    final json = _requiredObject(value, 'attachment grant');
    final rawHeaders = _requiredObject(json['headers'], 'attachment headers');
    final headers = <String, String>{};
    for (final entry in rawHeaders.entries) {
      if (entry.value is! String) {
        throw const FidibekiException(
          FidibekiErrorCode.invalidResponse,
          'Invalid mobile feedback attachment headers.',
        );
      }
      headers[entry.key] = entry.value! as String;
    }
    final method = _requiredString(json['method'], 'attachment method');
    if (method != 'PUT') {
      throw const FidibekiException(
        FidibekiErrorCode.invalidResponse,
        'Invalid mobile feedback attachment method.',
      );
    }
    final expiresAt = DateTime.tryParse(
      _requiredString(json['expires_at'], 'attachment expiry'),
    );
    if (expiresAt == null) {
      throw const FidibekiException(
        FidibekiErrorCode.invalidResponse,
        'Invalid mobile feedback attachment expiry.',
      );
    }
    return FidibekiAttachmentGrant(
      attachmentId: _requiredString(json['attachment_id'], 'attachment ID'),
      clientAttachmentId: _requiredString(
        json['client_attachment_id'],
        'client attachment ID',
      ),
      presignedUrl: Uri.parse(
        _requiredString(json['presigned_url'], 'attachment URL'),
      ),
      method: method,
      headers: headers,
      completeToken: _requiredString(
        json['complete_token'],
        'attachment complete token',
      ),
      expiresAt: expiresAt.toUtc(),
    );
  }

  final String attachmentId;
  final String clientAttachmentId;
  final Uri presignedUrl;
  final String method;
  final Map<String, String> headers;
  final String completeToken;
  final DateTime expiresAt;
}

final class FidibekiFeedbackAccepted {
  FidibekiFeedbackAccepted({
    required this.feedbackId,
    required this.feedbackPublicId,
    required List<FidibekiAttachmentGrant> attachments,
  }) : attachments = List.unmodifiable(attachments);

  factory FidibekiFeedbackAccepted.fromJson(Object? value) {
    final json = _requiredObject(value, 'accepted response');
    final rawAttachments = json['attachments'];
    if (rawAttachments is! List) {
      throw const FidibekiException(
        FidibekiErrorCode.invalidResponse,
        'Invalid mobile feedback attachments.',
      );
    }
    return FidibekiFeedbackAccepted(
      feedbackId: _requiredString(json['feedback_id'], 'feedback ID'),
      feedbackPublicId: _requiredString(
        json['feedback_public_id'],
        'feedback public ID',
      ),
      attachments: rawAttachments
          .map(FidibekiAttachmentGrant.fromJson)
          .toList(growable: false),
    );
  }

  final String feedbackId;
  final String feedbackPublicId;
  final List<FidibekiAttachmentGrant> attachments;
}
