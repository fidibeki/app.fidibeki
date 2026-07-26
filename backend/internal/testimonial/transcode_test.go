package testimonial

import "testing"

func TestFitKeepsBothOrientations(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		source Dimensions
		want   Dimensions
	}{
		// The two shapes the product promises. Neither is the "real" one that the
		// other gets fitted into.
		{"landscape 1080p", Dimensions{1920, 1080}, Dimensions{1280, 720}},
		{"portrait 1080p", Dimensions{1080, 1920}, Dimensions{720, 1280}},
		{"landscape 4K", Dimensions{3840, 2160}, Dimensions{1280, 720}},
		{"portrait 4K", Dimensions{2160, 3840}, Dimensions{720, 1280}},
		// A square recording has no long edge to speak of; it must not be forced
		// into either shape.
		{"square", Dimensions{1440, 1440}, Dimensions{720, 720}},
		// Odd inputs exist. H.264 chroma subsampling cannot represent an odd
		// dimension, and encoders reject rather than round.
		{"odd source", Dimensions{1921, 1081}, Dimensions{1278, 720}},
		{"odd result", Dimensions{1000, 1401}, Dimensions{720, 1008}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			got := Fit(testCase.source)
			if got != testCase.want {
				t.Errorf("Fit(%v) = %v, want %v", testCase.source, got, testCase.want)
			}
			if got.Width%2 != 0 || got.Height%2 != 0 {
				t.Errorf("Fit(%v) = %v, which an H.264 encoder will refuse", testCase.source, got)
			}
		})
	}
}

// Stretching a small recording costs bytes and sharpness and gains nothing: it
// is the same picture with more pixels describing it.
func TestFitNeverUpscales(t *testing.T) {
	t.Parallel()

	for _, source := range []Dimensions{{640, 360}, {360, 640}, {480, 480}} {
		if got := Fit(source); got != source {
			t.Errorf("Fit(%v) = %v, want it left alone", source, got)
		}
	}
	// Odd and small still has to come back even.
	if got := Fit(Dimensions{641, 361}); got != (Dimensions{640, 360}) {
		t.Errorf("Fit(641x361) = %v, want 640x360", got)
	}
}

func TestFitRejectsNonsense(t *testing.T) {
	t.Parallel()

	for _, source := range []Dimensions{{0, 0}, {-1920, 1080}, {1920, 0}} {
		if got := Fit(source); got != (Dimensions{}) {
			t.Errorf("Fit(%v) = %v, want the zero value rather than a guess", source, got)
		}
	}
}

func TestPortraitIsWhatAPhoneProduces(t *testing.T) {
	t.Parallel()

	if !(Dimensions{1080, 1920}).Portrait() {
		t.Error("1080x1920 is portrait")
	}
	if (Dimensions{1920, 1080}).Portrait() {
		t.Error("1920x1080 is not portrait")
	}
	// A square is not portrait, and Fit must not treat it as one.
	if (Dimensions{720, 720}).Portrait() {
		t.Error("a square is not portrait")
	}
}
