package widgetconfig

import (
	"math"
	"net/url"
	"strconv"
	"strings"
)

const (
	DefaultPosition         = "bottom-right"
	// DefaultTheme is "auto": the widget resolves the active theme via the
	// priority order — custom element attribute > host data-theme > system
	// (prefers-color-scheme). An explicit "light" or "dark" here wins over
	// the system only when the custom element and host data-theme are absent.
	DefaultTheme            = "auto"
	DefaultAccent           = "#087f73"
	DefaultAccentForeground = "#ffffff"
	AccentInk               = "#171a19"
	accentBlack             = "#000000"
	minimumTextContrast     = 4.5
)

type AppearanceOverrides struct {
	Position     *string `json:"position,omitempty"`
	Theme        *string `json:"theme,omitempty"`
	AccentColor  *string `json:"accent_color,omitempty"`
	SurfaceColor *string `json:"surface_color,omitempty"`
	TextColor    *string `json:"text_color,omitempty"`
	LogoURL      *string `json:"logo_url,omitempty"`
}

type Appearance struct {
	Position         string  `json:"position"`
	Theme            string  `json:"theme"`
	AccentColor      string  `json:"accent_color"`
	AccentForeground string  `json:"accent_foreground"`
	SurfaceColor     *string `json:"surface_color,omitempty"`
	TextColor        *string `json:"text_color,omitempty"`
	LogoURL          *string `json:"logo_url,omitempty"`
}

func Normalize(input AppearanceOverrides) AppearanceOverrides {
	return AppearanceOverrides{
		Position: normalizeLower(input.Position), Theme: normalizeLower(input.Theme),
		AccentColor: normalizeLower(input.AccentColor), SurfaceColor: normalizeLower(input.SurfaceColor),
		TextColor: normalizeLower(input.TextColor), LogoURL: normalizeTrim(input.LogoURL),
	}
}

func Validate(input AppearanceOverrides) map[string]string {
	fields := map[string]string{}
	if input.Position != nil && *input.Position != "bottom-left" && *input.Position != "bottom-right" {
		fields["position"] = "must be bottom-left or bottom-right"
	}
	if input.Theme != nil && *input.Theme != "light" && *input.Theme != "dark" && *input.Theme != "auto" {
		fields["theme"] = "must be light, dark, or auto"
	}
	for name, value := range map[string]*string{
		"accent_color": input.AccentColor, "surface_color": input.SurfaceColor, "text_color": input.TextColor,
	} {
		if value != nil && !validHex(*value) {
			fields[name] = "must be a six-digit hex color"
		}
	}
	if (input.SurfaceColor == nil) != (input.TextColor == nil) {
		fields["surface_color"] = "surface_color and text_color must be configured together"
		fields["text_color"] = "surface_color and text_color must be configured together"
	}
	if input.SurfaceColor != nil && input.TextColor != nil && validHex(*input.SurfaceColor) && validHex(*input.TextColor) &&
		contrastRatio(*input.SurfaceColor, *input.TextColor) < minimumTextContrast {
		fields["text_color"] = "must have at least 4.5:1 contrast against surface_color"
	}
	if input.LogoURL != nil && *input.LogoURL != "" && !validLogoURL(*input.LogoURL) {
		fields["logo_url"] = "must be an HTTPS URL without credentials or fragments"
	}
	return fields
}

func Resolve(layers ...AppearanceOverrides) Appearance {
	appearance := Appearance{Position: DefaultPosition, Theme: DefaultTheme, AccentColor: DefaultAccent}
	for _, layer := range layers {
		if layer.Position != nil {
			appearance.Position = *layer.Position
		}
		if layer.Theme != nil {
			appearance.Theme = *layer.Theme
		}
		if layer.AccentColor != nil {
			appearance.AccentColor = *layer.AccentColor
		}
		if layer.SurfaceColor != nil {
			appearance.SurfaceColor = copyString(layer.SurfaceColor)
		}
		if layer.TextColor != nil {
			appearance.TextColor = copyString(layer.TextColor)
		}
		if layer.LogoURL != nil {
			appearance.LogoURL = copyString(layer.LogoURL)
		}
	}
	if appearance.LogoURL != nil && *appearance.LogoURL == "" {
		appearance.LogoURL = nil
	}
	appearance.AccentForeground = ResolveAccentForeground(appearance.AccentColor)
	return appearance
}

func ResolveAccentForeground(accent string) string {
	accent = strings.ToLower(strings.TrimSpace(accent))
	if !validHex(accent) {
		return DefaultAccentForeground
	}
	for _, foreground := range []string{AccentInk, DefaultAccentForeground, accentBlack} {
		if contrastRatio(accent, foreground) >= minimumTextContrast {
			return foreground
		}
	}
	return accentBlack
}

func normalizeLower(value *string) *string {
	if value == nil {
		return nil
	}
	normalized := strings.ToLower(strings.TrimSpace(*value))
	return &normalized
}

func normalizeTrim(value *string) *string {
	if value == nil {
		return nil
	}
	normalized := strings.TrimSpace(*value)
	return &normalized
}

func validHex(value string) bool {
	if len(value) != 7 || value[0] != '#' {
		return false
	}
	_, err := strconv.ParseUint(value[1:], 16, 24)
	return err == nil
}

func validLogoURL(value string) bool {
	if len(value) > 2048 {
		return false
	}
	parsed, err := url.ParseRequestURI(value)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil && parsed.Fragment == ""
}

func contrastRatio(left, right string) float64 {
	leftLuminance := luminance(left)
	rightLuminance := luminance(right)
	lightest := math.Max(leftLuminance, rightLuminance)
	darkest := math.Min(leftLuminance, rightLuminance)
	return (lightest + 0.05) / (darkest + 0.05)
}

func luminance(value string) float64 {
	components := make([]float64, 3)
	for index := range components {
		parsed, _ := strconv.ParseUint(value[1+index*2:3+index*2], 16, 8)
		component := float64(parsed) / 255
		if component <= 0.04045 {
			components[index] = component / 12.92
		} else {
			components[index] = math.Pow((component+0.055)/1.055, 2.4)
		}
	}
	return 0.2126*components[0] + 0.7152*components[1] + 0.0722*components[2]
}

func copyString(value *string) *string {
	copied := *value
	return &copied
}
