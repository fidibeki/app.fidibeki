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
		resolved.SurfaceColor == nil || *resolved.SurfaceColor != surface || resolved.LogoURL == nil || *resolved.LogoURL != logo {
		t.Fatalf("resolved = %#v", resolved)
	}
}

func TestAppearanceValidationRejectsUnsafeTokens(t *testing.T) {
	t.Parallel()
	position, theme, accent := "center", "custom", "#ffffff"
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

func TestAppearanceValidationRequiresSurfaceAndTextTogether(t *testing.T) {
	t.Parallel()
	surface := "#ffffff"
	fields := Validate(Normalize(AppearanceOverrides{SurfaceColor: &surface}))
	if fields["surface_color"] == "" || fields["text_color"] == "" {
		t.Fatalf("fields = %#v", fields)
	}
}
