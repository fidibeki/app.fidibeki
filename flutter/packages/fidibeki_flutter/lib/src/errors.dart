enum FidibekiErrorCode {
  accessDenied,
  configurationChanged,
  invalidResponse,
  network,
  sessionExpired,
  validation,
}

final class FidibekiException implements Exception {
  const FidibekiException(this.code, this.message, {this.retryable = false});

  final FidibekiErrorCode code;
  final String message;
  final bool retryable;

  @override
  String toString() => 'FidibekiException(${code.name}): $message';
}

Never invalidCaptureSession(String reason) {
  throw FidibekiException(
    FidibekiErrorCode.invalidResponse,
    'Invalid capture session: $reason',
  );
}
