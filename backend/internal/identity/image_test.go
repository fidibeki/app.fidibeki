package identity

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestImageProcessorNormalizesLogoAndAvatar(t *testing.T) {
	t.Parallel()

	source := image.NewNRGBA(image.Rect(0, 0, 120, 60))
	for y := 0; y < 60; y++ {
		for x := 0; x < 120; x++ {
			source.Set(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: 90, A: 255})
		}
	}
	pngBody := encodePNG(t, source)
	for _, test := range []struct {
		name string
		kind ImageKind
	}{
		{name: "logo", kind: ImageKindLogo},
		{name: "avatar", kind: ImageKindAvatar},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			variants, err := (ImageProcessor{}).Process("identity.png", "image/png", pngBody, test.kind)
			if err != nil {
				t.Fatalf("Process() error = %v", err)
			}
			small := decodePNG(t, variants.Small)
			large := decodePNG(t, variants.Large)
			if small.Bounds().Dx() != 64 || small.Bounds().Dy() != 64 ||
				large.Bounds().Dx() != 256 || large.Bounds().Dy() != 256 {
				t.Fatalf("variant dimensions = %v/%v", small.Bounds(), large.Bounds())
			}
			if test.kind == ImageKindLogo {
				if alpha := color.NRGBAModel.Convert(small.At(0, 0)).(color.NRGBA).A; alpha != 0 {
					t.Fatalf("logo corner alpha = %d, want transparent fit", alpha)
				}
			} else if alpha := color.NRGBAModel.Convert(small.At(0, 0)).(color.NRGBA).A; alpha == 0 {
				t.Fatal("avatar corner is transparent, want centered square crop")
			}
		})
	}
}

func TestImageProcessorAcceptsJPEGAndWebP(t *testing.T) {
	t.Parallel()

	source := image.NewRGBA(image.Rect(0, 0, 40, 40))
	var jpegBody bytes.Buffer
	if err := jpeg.Encode(&jpegBody, source, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatal(err)
	}
	if _, err := (ImageProcessor{}).Process(
		"avatar.jpeg", "image/jpeg; charset=binary", jpegBody.Bytes(), ImageKindAvatar,
	); err != nil {
		t.Fatalf("JPEG Process() error = %v", err)
	}

	webpBody, err := base64.StdEncoding.DecodeString(
		"UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEADsD+JaQAA3AAAAAA",
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (ImageProcessor{MinDimension: 1}).Process(
		"logo.webp", "image/webp", webpBody, ImageKindLogo,
	); err != nil {
		t.Fatalf("WebP Process() error = %v", err)
	}
}

func TestImageProcessorRejectsInvalidInputs(t *testing.T) {
	t.Parallel()

	valid := encodePNG(t, image.NewNRGBA(image.Rect(0, 0, 40, 40)))
	tests := []struct {
		name        string
		filename    string
		contentType string
		body        []byte
		kind        ImageKind
		want        error
	}{
		{name: "empty", filename: "a.png", contentType: "image/png", kind: ImageKindLogo, want: ErrImageInvalid},
		{name: "oversized", filename: "a.png", contentType: "image/png", body: make([]byte, MaxSourceBytes+1), kind: ImageKindLogo, want: ErrImageTooLarge},
		{name: "svg extension", filename: "a.svg", contentType: "image/svg+xml", body: []byte("<svg/>"), kind: ImageKindLogo, want: ErrImageInvalid},
		{name: "extension mismatch", filename: "a.jpg", contentType: "image/png", body: valid, kind: ImageKindLogo, want: ErrImageInvalid},
		{name: "mime mismatch", filename: "a.png", contentType: "image/jpeg", body: valid, kind: ImageKindLogo, want: ErrImageInvalid},
		{name: "truncated", filename: "a.png", contentType: "image/png", body: valid[:20], kind: ImageKindLogo, want: ErrImageInvalid},
		{name: "small", filename: "a.png", contentType: "image/png", body: encodePNG(t, image.NewNRGBA(image.Rect(0, 0, 31, 40))), kind: ImageKindLogo, want: ErrImageInvalid},
		{name: "unknown kind", filename: "a.png", contentType: "image/png", body: valid, kind: "other", want: ErrImageInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := (ImageProcessor{}).Process(
				test.filename, test.contentType, test.body, test.kind,
			)
			if !errors.Is(err, test.want) {
				t.Fatalf("Process() error = %v, want %v", err, test.want)
			}
		})
	}
}

func encodePNG(t *testing.T, source image.Image) []byte {
	t.Helper()
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

func decodePNG(t *testing.T, body []byte) image.Image {
	t.Helper()
	decoded, err := png.Decode(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}
