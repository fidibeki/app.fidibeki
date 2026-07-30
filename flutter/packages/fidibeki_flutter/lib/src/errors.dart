enum FidibekiErrorCode {
  accessDenied('access_denied'),
  configurationChanged('configuration_changed'),
  invalidResponse('invalid_response'),
  network('network_error'),
  retryPayloadChanged('retry_payload_changed'),
  sessionExpired('session_expired'),
  validation('validation_error');

  const FidibekiErrorCode(this.wireName);

  final String wireName;
}

final class FidibekiException implements Exception {
  const FidibekiException(
    this.code,
    this.message, {
    this.retryable = false,
    this.fields = const <String, String>{},
  });

  final FidibekiErrorCode code;
  final String message;
  final bool retryable;
  final Map<String, String> fields;

  @override
  String toString() => 'FidibekiException(${code.name}): $message';
}

Never invalidCaptureSession(String reason) {
  throw FidibekiException(
    FidibekiErrorCode.invalidResponse,
    'Invalid capture session: $reason',
  );
}
