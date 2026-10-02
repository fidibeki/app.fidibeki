package websitechat

import "testing"

func TestVisitorLocale(t *testing.T) {
	for _, c := range []struct {
		input, expected string
		valid           bool
	}{
		{"", "", true}, {"pt-pt", "pt-PT", true}, {"pt-AO", "pt-AO", true},
		{"ZH-hant-tw", "zh-Hant-TW", true}, {"es-419", "es-419", true},
		{"en-US-x-tracking", "", false}, {"<script>", "", false}, {"en_US", "", false},
	} {
		value, valid := normalizeVisitorLocale(c.input)
		if value != c.expected || valid != c.valid {
			t.Errorf("%q: got %q, %v", c.input, value, valid)
		}
	}
}
