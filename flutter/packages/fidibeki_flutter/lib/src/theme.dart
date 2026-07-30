import 'package:flutter/material.dart';

import 'models.dart';

enum FidibekiThemeMode { light, dark, system }

final class FidibekiTheme {
  const FidibekiTheme({
    this.mode,
    this.accent,
    this.onAccent,
    this.background,
    this.text,
    this.muted,
    this.border,
    this.danger,
  });

  final FidibekiThemeMode? mode;
  final Color? accent;
  final Color? onAccent;
  final Color? background;
  final Color? text;
  final Color? muted;
  final Color? border;
  final Color? danger;
}

final class FidibekiResolvedTheme {
  const FidibekiResolvedTheme({
    required this.mode,
    required this.accent,
    required this.onAccent,
    required this.background,
    required this.text,
    required this.muted,
    required this.border,
    required this.danger,
  });

  final FidibekiThemeMode mode;
  final Color accent;
  final Color onAccent;
  final Color background;
  final Color text;
  final Color muted;
  final Color border;
  final Color danger;
}

Color? _appearanceColor(String? value) {
  if (value == null) return null;
  return Color(int.parse('ff${value.substring(1)}', radix: 16));
}

double _contrast(Color first, Color second) {
  final lighter = first.computeLuminance() > second.computeLuminance()
      ? first.computeLuminance()
      : second.computeLuminance();
  final darker = first.computeLuminance() > second.computeLuminance()
      ? second.computeLuminance()
      : first.computeLuminance();
  return (lighter + 0.05) / (darker + 0.05);
}

Color _readableForeground(Color accent, Color? proposed) {
  if (proposed != null && _contrast(accent, proposed) >= 4.5) {
    return proposed;
  }
  return _contrast(accent, Colors.black) >= _contrast(accent, Colors.white)
      ? Colors.black
      : Colors.white;
}

FidibekiResolvedTheme resolveFidibekiTheme({
  FidibekiTheme? hostTheme,
  required FidibekiAppearance appearance,
  required Brightness platformBrightness,
}) {
  final appearanceMode = switch (appearance.theme) {
    'light' => FidibekiThemeMode.light,
    'dark' => FidibekiThemeMode.dark,
    _ => FidibekiThemeMode.system,
  };
  final requestedMode = hostTheme?.mode ?? appearanceMode;
  final mode = requestedMode == FidibekiThemeMode.system
      ? platformBrightness == Brightness.dark
            ? FidibekiThemeMode.dark
            : FidibekiThemeMode.light
      : requestedMode;
  final dark = mode == FidibekiThemeMode.dark;
  final defaultText = dark ? const Color(0xfff4f7f6) : const Color(0xff17211f);
  final text =
      hostTheme?.text ?? _appearanceColor(appearance.textColor) ?? defaultText;
  final accent =
      hostTheme?.accent ??
      _appearanceColor(appearance.accentColor) ??
      const Color(0xff087f6b);
  final appearanceForeground = hostTheme?.accent == null
      ? _appearanceColor(appearance.accentForeground)
      : null;

  return FidibekiResolvedTheme(
    mode: mode,
    accent: accent,
    onAccent: _readableForeground(
      accent,
      hostTheme?.onAccent ?? appearanceForeground,
    ),
    background:
        hostTheme?.background ??
        _appearanceColor(appearance.surfaceColor) ??
        (dark ? const Color(0xff141716) : const Color(0xffffffff)),
    text: text,
    muted: hostTheme?.muted ?? text.withValues(alpha: 0.7),
    border:
        hostTheme?.border ??
        (dark ? const Color(0xff3a4441) : const Color(0xffccd5d2)),
    danger: hostTheme?.danger ?? const Color(0xffb42318),
  );
}
