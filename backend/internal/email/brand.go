package email

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// BrandLogoPath is served by the API itself, on the same host every email is
// addressed from, because no other host answers for it in every environment.
// Deriving the logo host from the base domain looks right and is not: the apex
// belongs to the marketing site in production and to the app in staging, so a
// path that resolves in one tier 404s in the other and the header collapses to
// alt text. The bytes ship inside the binary that sends the mail, so the logo
// cannot outlive or fall behind the deploy that references it.
const BrandLogoPath = "/brand/wordmark.png"

//go:embed assets/wordmark.png
var brandLogo []byte

// brandLogoETag lets clients and the caches in front of them revalidate cheaply
// after a deploy changes the artwork. The response is otherwise immutable.
var brandLogoETag = `"` + hex.EncodeToString(sha256Sum(brandLogo)[:16]) + `"`

func sha256Sum(payload []byte) []byte {
	sum := sha256.Sum256(payload)
	return sum[:]
}

// RegisterBrandLogoRoute mounts the wordmark on the router that answers for the
// API's own public base URL, which is the host BrandLogoURL addresses. Both sides
// of that agreement live here so a rename cannot break only one of them.
func RegisterBrandLogoRoute(router chi.Router) {
	router.Method(http.MethodGet, BrandLogoPath, BrandLogoHandler())
}

// BrandLogoHandler serves the wordmark embedded above. Email clients fetch it
// unauthenticated, often through a proxy that caches on the recipient's behalf,
// so the response carries no cookies and a long-lived public cache directive.
func BrandLogoHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("ETag", brandLogoETag)
		http.ServeContent(w, r, "wordmark.png", time.Time{}, bytes.NewReader(brandLogo))
	})
}
