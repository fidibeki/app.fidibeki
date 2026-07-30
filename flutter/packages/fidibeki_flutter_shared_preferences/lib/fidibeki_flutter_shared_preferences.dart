library;

import 'package:fidibeki_flutter/fidibeki_flutter.dart';
import 'package:shared_preferences/shared_preferences.dart';

final class SharedPreferencesFidibekiStorage implements FidibekiStorage {
  SharedPreferencesFidibekiStorage({
    SharedPreferencesAsync? preferences,
    this.namespace = '@fidibeki/flutter',
  }) : _preferences = preferences ?? SharedPreferencesAsync();

  final SharedPreferencesAsync _preferences;
  final String namespace;

  String _key(String key) => '$namespace:$key';

  @override
  Future<void> delete(String key) => _preferences.remove(_key(key));

  @override
  Future<String?> read(String key) => _preferences.getString(_key(key));

  @override
  Future<void> write(String key, String value) {
    return _preferences.setString(_key(key), value);
  }
}
