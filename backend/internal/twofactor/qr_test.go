package twofactor

import (
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
)

// The golden vectors in testdata/qr_vectors.json were produced by the `qrcode`
// Python library, an independent implementation, at error-correction level M in
// byte mode. Matching them module for module proves the version selection,
// Reed-Solomon blocks, function patterns, mask choice, and format and version
// information all follow ISO/IEC 18004 rather than merely being self-consistent.
type qrVector struct {
	Payload string   `json:"payload"`
	Version int      `json:"version"`
	Rows    []string `json:"rows"`
}

func TestEncodeQRMatchesReferenceVectors(t *testing.T) {
	raw, err := os.ReadFile("testdata/qr_vectors.json")
	if err != nil {
		t.Fatalf("read QR vectors: %v", err)
	}
	var vectors []qrVector
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatalf("decode QR vectors: %v", err)
	}
	if len(vectors) == 0 {
		t.Fatal("expected QR reference vectors")
	}
	for _, vector := range vectors {
		label := vector.Payload
		if len(label) > 24 {
			label = label[:24] + "..."
		}
		t.Run(label, func(t *testing.T) {
			matrix, err := EncodeQR(vector.Payload)
			if err != nil {
				t.Fatalf("encode QR: %v", err)
			}
			if len(matrix) != len(vector.Rows) {
				t.Fatalf("symbol size = %d, want %d", len(matrix), len(vector.Rows))
			}
			for row, expected := range vector.Rows {
				if actual := renderRow(matrix[row]); actual != expected {
					t.Fatalf("row %d\n got %s\nwant %s", row, actual, expected)
				}
			}
		})
	}
}

func renderRow(row []bool) string {
	var builder strings.Builder
	for _, dark := range row {
		if dark {
			builder.WriteByte('1')
			continue
		}
		builder.WriteByte('0')
	}
	return builder.String()
}

func TestEncodeQRRejectsOversizedPayload(t *testing.T) {
	if _, err := EncodeQR(strings.Repeat("z", 1000)); !errors.Is(err, ErrQRPayloadTooLong) {
		t.Fatalf("error = %v, want %v", err, ErrQRPayloadTooLong)
	}
}

func TestQRCodeSVGRendersEveryDarkModule(t *testing.T) {
	payload := ProvisioningURI("Fidibeki", "member@example.com", "JBSWY3DPEHPK3PXP")
	matrix, err := EncodeQR(payload)
	if err != nil {
		t.Fatalf("encode QR: %v", err)
	}
	dark := 0
	for _, row := range matrix {
		for _, module := range row {
			if module {
				dark++
			}
		}
	}
	document, err := QRCodeSVG(payload)
	if err != nil {
		t.Fatalf("render SVG: %v", err)
	}
	if count := strings.Count(document, "h1v1h-1z"); count != dark {
		t.Fatalf("rendered modules = %d, want %d", count, dark)
	}
	if !strings.HasPrefix(document, "<svg ") || !strings.HasSuffix(document, "</svg>") {
		t.Fatal("expected a standalone SVG document")
	}
	// A four-module quiet zone on each side is required for reliable scanning.
	side := strconv.Itoa(len(matrix) + 8)
	if !strings.Contains(document, `viewBox="0 0 `+side+" "+side+`"`) {
		t.Fatalf("unexpected viewBox in %s", document[:80])
	}
}
