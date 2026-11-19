package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestHandlerServesTheApp(t *testing.T) {
	h := handler(fstest.MapFS{
		"index.html":       {Data: []byte(`<div id="root"></div>`)},
		"assets/app-1.js":  {Data: []byte(`console.log(1)`)},
		"assets/style.css": {Data: []byte(`body{}`)},
	})

	if rec := get(h, "/"); rec.Code != http.StatusOK || rec.Body.String() != `<div id="root"></div>` {
		t.Fatalf("GET / = %d %q, want index.html", rec.Code, rec.Body.String())
	}
	if rec := get(h, "/contacts/42"); rec.Code != http.StatusOK || rec.Body.String() != `<div id="root"></div>` {
		t.Fatalf("client-side route = %d %q, want index.html", rec.Code, rec.Body.String())
	}
	rec := get(h, "/assets/app-1.js")
	if rec.Body.String() != `console.log(1)` || !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("asset = %q, Cache-Control %q", rec.Body.String(), rec.Header().Get("Cache-Control"))
	}
}

func TestHandlerExplainsAMissingBuild(t *testing.T) {
	rec := get(handler(fstest.MapFS{".gitkeep": {}}), "/")
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "not built") {
		t.Fatalf("GET / without a build = %d %q", rec.Code, rec.Body.String())
	}
}
