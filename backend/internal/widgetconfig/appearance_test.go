package widgetconfig

import "testing"

func TestAppearanceValidationAndResolution(t *testing.T) {
	t.Parallel()
	left, dark, accent := "bottom-left", "dark", "#005f56"
	surface, text, logo := "#101817", "#f7faf9", "https://cdn.example.com/brand.svg"
	workspace := Normalize(AppearanceOverrides{Position: &left, Theme: &dark, AccentColor: &accent})
	definition := Normalize(AppearanceOverrides{SurfaceColor: &surface, TextColor: &text, LogoURL: &logo})
	if fields := Validate(workspace); len(fields) != 0 {
		t.Fatalf("workspace fields = %#v", fields)
	}
	if fields := Validate(definition); len(fields) != 0 {
		t.Fatalf("definition fields = %#v", fields)
	}
	resolved := Resolve(workspace, definition)
	if resolved.Position != left || resolved.Theme != dark || resolved.AccentColor != accent ||
		resolved.AccentForeground != "#ffffff" ||
		resolved.SurfaceColor == nil || *resolved.SurfaceColor != surface || resolved.LogoURL == nil || *resolved.LogoURL != logo {
		t.Fatalf("resolved = %#v", resolved)
	}
}

func TestAppearanceValidationRejectsUnsafeTokens(t *testing.T) {
	t.Parallel()
	position, theme, accent := "center", "custom", "#fffff"
	surface, text, logo := "#ffffff", "#eeeeee", "http://user@example.com/logo.svg#x"
	fields := Validate(Normalize(AppearanceOverrides{
		Position: &position, Theme: &theme, AccentColor: &accent,
		SurfaceColor: &surface, TextColor: &text, LogoURL: &logo,
	}))
	for _, field := range []string{"position", "theme", "accent_color", "text_color", "logo_url"} {
		if fields[field] == "" {
			t.Errorf("missing %s error in %#v", field, fields)
		}
	}
}

func TestAppearanceDerivesAccessibleAccentForeground(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		accent     string
		foreground string
	}{
		{name: "reported light brand green", accent: "#46c754", foreground: "#171a19"},
		{name: "dark brand green", accent: "#005f56", foreground: "#ffffff"},
		{name: "normalized uppercase", accent: "#46C754", foreground: "#171a19"},
		{name: "token gap falls back to black", accent: "#777777", foreground: "#000000"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			accent := test.accent
			normalized := Normalize(AppearanceOverrides{AccentColor: &accent})
			if fields := Validate(normalized); len(fields) != 0 {
				t.Fatalf("fields = %#v", fields)
			}
			resolved := Resolve(normalized)
			if resolved.AccentForeground != test.foreground {
				t.Fatalf("foreground for %s = %s, want %s", test.accent, resolved.AccentForeground, test.foreground)
			}
			if ratio := contrastRatio(resolved.AccentColor, resolved.AccentForeground); ratio < 4.5 {
				t.Fatalf("contrast for %s/%s = %.2f", resolved.AccentColor, resolved.AccentForeground, ratio)
			}
		})
	}
}

func TestAppearanceValidationRequiresSurfaceAndTextTogether(t *testing.T) {
	t.Parallel()
	surface := "#ffffff"
	fields := Validate(Normalize(AppearanceOverrides{SurfaceColor: &surface}))
	if fields["surface_color"] == "" || fields["text_color"] == "" {
		t.Fatalf("fields = %#v", fields)
	}
}
