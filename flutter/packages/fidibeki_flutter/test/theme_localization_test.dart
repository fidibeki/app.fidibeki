import 'package:fidibeki_flutter/fidibeki_flutter.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('system copy supports Portuguese and falls back to English', () {
    expect(fidibekiSystemMessages('pt-PT').close, 'Fechar');
    expect(fidibekiSystemMessages('fr-FR').close, 'Close');
    expect(
      fidibekiSystemMessages('es-ES').consentRequired,
      contains('consent'),
    );
    expect(fidibekiSystemMessages('en-GB').ratingValue(4), '4 out of 5');
  });

  test('host colors override definition appearance and keep contrast', () {
    const appearance = FidibekiAppearance(
      theme: 'dark',
      accentColor: '#087f6b',
      accentForeground: '#ffffff',
      surfaceColor: '#141716',
      textColor: '#f4f7f6',
    );
    const host = FidibekiTheme(
      mode: FidibekiThemeMode.light,
      accent: Color(0xffffcc00),
    );

    final resolved = resolveFidibekiTheme(
      hostTheme: host,
      appearance: appearance,
      platformBrightness: Brightness.dark,
    );

    expect(resolved.mode, FidibekiThemeMode.light);
    expect(resolved.accent, const Color(0xffffcc00));
    expect(resolved.background, const Color(0xff141716));
    expect(resolved.text, const Color(0xfff4f7f6));
    expect(resolved.onAccent, Colors.black);
  });

  test('platform brightness supplies the final theme fallback', () {
    final resolved = resolveFidibekiTheme(
      appearance: const FidibekiAppearance(),
      platformBrightness: Brightness.dark,
    );

    expect(resolved.mode, FidibekiThemeMode.dark);
    expect(resolved.background, const Color(0xff141716));
    expect(resolved.text, const Color(0xfff4f7f6));
  });
}
