package email

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/go-chi/chi/v5"
)

// The header of every transactional email is a link to this route, so the URL the
// mail carries and the route the API answers on have to be the same string.
func TestRegisteredBrandLogoRouteAnswersTheURLTheEmailCarries(t *testing.T) {
	t.Parallel()
	router := chi.NewRouter()
	RegisterBrandLogoRoute(router)
	logo, err := BrandLogoURL("https://api.fidibeki.com")
	if err != nil {
		t.Fatalf("BrandLogoURL() error = %v", err)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, logo, nil))
	if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("GET %s = %d %q", logo, recorder.Code, recorder.Header().Get("Content-Type"))
	}
}

func TestBrandLogoHandlerServesCacheableEmbeddedPNG(t *testing.T) {
	t.Parallel()
	handler := BrandLogoHandler()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, BrandLogoPath, nil))
	response := recorder.Result()
	defer func() {
		if err := response.Body.Close(); err != nil {
			t.Errorf("close response: %v", err)
		}
	}()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	if got := response.Header.Get("Content-Type"); got != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", got)
	}
	// Mail clients and the proxies in front of them refetch the header image on
	// every open, so an uncacheable response would be paid for once per read.
	if got := response.Header.Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("Cache-Control = %q", got)
	}
	etag := response.Header.Get("ETag")
	if etag == "" {
		t.Fatal("ETag is empty")
	}
	if !bytes.Equal(recorder.Body.Bytes(), brandLogo) {
		t.Errorf("body = %d bytes, want the %d embedded bytes", recorder.Body.Len(), len(brandLogo))
	}
	if !bytes.HasPrefix(brandLogo, []byte("\x89PNG\r\n\x1a\n")) {
		t.Error("embedded logo is not a PNG")
	}

	revalidation := httptest.NewRequest(http.MethodGet, BrandLogoPath, nil)
	revalidation.Header.Set("If-None-Match", etag)
	cached := httptest.NewRecorder()
	handler.ServeHTTP(cached, revalidation)
	if cached.Code != http.StatusNotModified {
		t.Errorf("revalidated status = %d, want %d", cached.Code, http.StatusNotModified)
	}
}

// The API ships its own copy of the wordmark so that mail never depends on a web
// tier's deploy, which leaves two copies to drift apart. Rebranding has to touch
// both, and this is where that is said out loud.
func TestEmbeddedLogoMatchesTheMarketingWordmark(t *testing.T) {
	t.Parallel()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test file path")
	}
	path := filepath.Clean(filepath.Join(
		filepath.Dir(filename), "../../../marketing/public/brand/wordmark-color.png",
	))
	published, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !bytes.Equal(brandLogo, published) {
		t.Errorf(
			"assets/wordmark.png (%d bytes) differs from %s (%d bytes); copy the new artwork into both",
			len(brandLogo), path, len(published),
		)
	}
}
