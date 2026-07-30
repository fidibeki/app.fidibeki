import 'package:fidibeki_flutter_shared_preferences/fidibeki_flutter_shared_preferences.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences_platform_interface/in_memory_shared_preferences_async.dart';
import 'package:shared_preferences_platform_interface/shared_preferences_async_platform_interface.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    SharedPreferencesAsyncPlatform.instance =
        InMemorySharedPreferencesAsync.empty();
  });

  tearDown(() {
    SharedPreferencesAsyncPlatform.instance = null;
  });

  test(
    'reads, writes, and deletes only namespaced recurrence values',
    () async {
      final storage = SharedPreferencesFidibekiStorage();

      await storage.write('recurrence', '{"state":"submitted"}');
      expect(await storage.read('recurrence'), '{"state":"submitted"}');

      await storage.delete('recurrence');
      expect(await storage.read('recurrence'), isNull);
    },
  );
}
