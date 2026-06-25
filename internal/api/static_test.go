package api_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/rinfra/rinfra/internal/api"
)

// TestAllInOne_ServesStaticAndAPI verifies the RINFRA_WEB_DIR all-in-one mode:
// API paths still route to the chi handler, while everything else serves the
// built web console (index for /, 404.html fallback for unknown paths).
func TestAllInOne_ServesStaticAndAPI(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "index.html"), "<html>console</html>")
	mustWrite(t, filepath.Join(dir, "404.html"), "<html>not found</html>")
	t.Setenv("RINFRA_WEB_DIR", dir)

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := api.NewRouter(api.Services{}, log)

	// API path keeps precedence.
	if body, code := get(t, router, "/healthz"); code != http.StatusOK || body != "ok" {
		t.Errorf("/healthz = %d %q, want 200 ok", code, body)
	}
	// Root serves the console shell.
	if body, code := get(t, router, "/"); code != http.StatusOK || body != "<html>console</html>" {
		t.Errorf("/ = %d %q, want the console index", code, body)
	}
	// Unknown client path falls back to 404.html with a 404 status.
	if body, code := get(t, router, "/no-such-page"); code != http.StatusNotFound || body != "<html>not found</html>" {
		t.Errorf("/no-such-page = %d %q, want 404 + 404.html", code, body)
	}
}

// TestAllInOne_DisabledByDefault verifies that without RINFRA_WEB_DIR the router
// does not serve static files (API-only — unknown paths 404 from chi).
func TestAllInOne_DisabledByDefault(t *testing.T) {
	t.Setenv("RINFRA_WEB_DIR", "")
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := api.NewRouter(api.Services{}, log)
	if _, code := get(t, router, "/"); code != http.StatusNotFound {
		t.Errorf("/ = %d, want 404 (no static serving without RINFRA_WEB_DIR)", code)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func get(t *testing.T, h http.Handler, path string) (string, int) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr.Body.String(), rr.Code
}
