import 'package:fidibeki_flutter/fidibeki_flutter.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('keeps the documented public error taxonomy stable', () {
    expect(
      FidibekiErrorCode.values
          .map((value) => (value.name, value.wireName))
          .toList(),
      <(String, String)>[
        ('accessDenied', 'access_denied'),
        ('attachment', 'attachment_error'),
        ('configurationChanged', 'configuration_changed'),
        ('invalidResponse', 'invalid_response'),
        ('network', 'network_error'),
        ('retryPayloadChanged', 'retry_payload_changed'),
        ('sessionExpired', 'session_expired'),
        ('validation', 'validation_error'),
      ],
    );
  });
}
