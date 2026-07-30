import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

void main() {
  test('release host requests network but no sensitive Android permission', () {
    final manifest = File(
      'example/android/app/src/main/AndroidManifest.xml',
    ).readAsStringSync();

    expect(manifest, contains('android.permission.INTERNET'));
    for (final permission in <String>[
      'CAMERA',
      'READ_MEDIA_IMAGES',
      'READ_EXTERNAL_STORAGE',
      'WRITE_EXTERNAL_STORAGE',
      'RECORD_AUDIO',
      'ACCESS_COARSE_LOCATION',
      'ACCESS_FINE_LOCATION',
      'READ_CONTACTS',
    ]) {
      expect(manifest, isNot(contains('android.permission.$permission')));
    }
  });

  test('iOS host declares no sensitive resource usage', () {
    final plist = File(
      'example/ios/Runner/Info.plist',
    ).readAsStringSync();

    for (final key in <String>[
      'NSCameraUsageDescription',
      'NSPhotoLibraryUsageDescription',
      'NSMicrophoneUsageDescription',
      'NSLocationWhenInUseUsageDescription',
      'NSContactsUsageDescription',
    ]) {
      expect(plist, isNot(contains(key)));
    }
  });
}
