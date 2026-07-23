package identity

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	MaxSourceBytes      = 2 << 20
	maxSourceDimension  = 4096
	maxSourcePixelCount = 16_000_000
)

var (
	ErrImageTooLarge = errors.New("identity image is too large")
	ErrImageInvalid  = errors.New("identity image is invalid")
)

type ImageKind string

const (
	ImageKindLogo   ImageKind = "workspace_logo"
	ImageKindAvatar ImageKind = "member_avatar"
)

type ImageProcessor struct {
	MinDimension int
}

type ImageVariants struct {
	Small []byte
	Large []byte
}

func (processor ImageProcessor) Process(
	filename string,
	declaredContentType string,
	body []byte,
	kind ImageKind,
) (ImageVariants, error) {
	if len(body) == 0 {
		return ImageVariants{}, fmt.Errorf("%w: file is empty", ErrImageInvalid)
	}
	if len(body) > MaxSourceBytes {
		return ImageVariants{}, ErrImageTooLarge
	}
	expectedFormat, expectedContentType, ok := identityImageFormat(filename)
	if !ok {
		return ImageVariants{}, fmt.Errorf("%w: extension must be png, jpg, jpeg, or webp", ErrImageInvalid)
	}
	mediaType, _, err := mime.ParseMediaType(declaredContentType)
	if err != nil || mediaType != expectedContentType {
		return ImageVariants{}, fmt.Errorf("%w: declared content type does not match extension", ErrImageInvalid)
	}
	if detected := http.DetectContentType(body); detected != expectedContentType {
		return ImageVariants{}, fmt.Errorf("%w: detected content type does not match extension", ErrImageInvalid)
	}
	source, format, err := image.Decode(bytes.NewReader(body))
	if err != nil || format != expectedFormat {
		return ImageVariants{}, fmt.Errorf("%w: image cannot be decoded", ErrImageInvalid)
	}
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	minDimension := processor.MinDimension
	if minDimension <= 0 {
		minDimension = 32
	}
	if width < minDimension || height < minDimension ||
		width > maxSourceDimension || height > maxSourceDimension ||
		int64(width)*int64(height) > maxSourcePixelCount {
		return ImageVariants{}, fmt.Errorf(
			"%w: dimensions must be between %d and %d pixels",
			ErrImageInvalid, minDimension, maxSourceDimension,
		)
	}
	small, err := normalizedPNG(source, kind, 64)
	if err != nil {
		return ImageVariants{}, err
	}
	large, err := normalizedPNG(source, kind, 256)
	if err != nil {
		return ImageVariants{}, err
	}
	return ImageVariants{Small: small, Large: large}, nil
}

func identityImageFormat(filename string) (format string, contentType string, ok bool) {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(filename))) {
	case ".png":
		return "png", "image/png", true
	case ".jpg", ".jpeg":
		return "jpeg", "image/jpeg", true
	case ".webp":
		return "webp", "image/webp", true
	default:
		return "", "", false
	}
}

func normalizedPNG(source image.Image, kind ImageKind, size int) ([]byte, error) {
	target := image.NewNRGBA(image.Rect(0, 0, size, size))
	bounds := source.Bounds()
	switch kind {
	case ImageKindAvatar:
		side := min(bounds.Dx(), bounds.Dy())
		left := bounds.Min.X + (bounds.Dx()-side)/2
		top := bounds.Min.Y + (bounds.Dy()-side)/2
		sourceBounds := image.Rect(left, top, left+side, top+side)
		xdraw.CatmullRom.Scale(target, target.Bounds(), source, sourceBounds, draw.Src, nil)
	case ImageKindLogo:
		width, height := fitDimensions(bounds.Dx(), bounds.Dy(), size)
		targetBounds := image.Rect(
			(size-width)/2, (size-height)/2,
			(size-width)/2+width, (size-height)/2+height,
		)
		xdraw.CatmullRom.Scale(target, targetBounds, source, bounds, draw.Src, nil)
	default:
		return nil, fmt.Errorf("%w: unsupported identity image kind", ErrImageInvalid)
	}
	var encoded bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&encoded, target); err != nil {
		return nil, fmt.Errorf("encode normalized identity image: %w", err)
	}
	return encoded.Bytes(), nil
}

func fitDimensions(width, height, size int) (int, int) {
	if width >= height {
		return size, max(1, height*size/width)
	}
	return max(1, width*size/height), size
}
