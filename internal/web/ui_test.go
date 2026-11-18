package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

const indexHTML = `<!doctype html><html><body><div id="app"></div></body></html>`

func builtFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":          {Data: []byte(indexHTML)},
		"assets/app-1a2b.js":  {Data: []byte(`console.log("app")`)},
		"assets/app-1a2b.css": {Data: []byte(`body{}`)},
		".gitkeep":            {Data: []byte{}},
	}
}

func get(t *testing.T, h http.Handler, path string) (*http.Response, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	res := rec.Result()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res, string(body)
}

func TestUIServesIndexForAppRoutes(t *testing.T) {
	h := newUI(builtFS())
	for _, path := range []string{
		"/",
		"/artist/11111111-2222-3333-4444-555555555555",
		"/album/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		"/search",
	} {
		res, body := get(t, h, path)
		if res.StatusCode != http.StatusOK {
			t.Errorf("%s: status %d, want 200", path, res.StatusCode)
		}
		if body != indexHTML {
			t.Errorf("%s: body %q, want index.html", path, body)
		}
		if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Errorf("%s: Content-Type %q, want text/html", path, ct)
		}
		if cc := res.Header.Get("Cache-Control"); cc != "no-cache" {
			t.Errorf("%s: Cache-Control %q, want no-cache", path, cc)
		}
	}
}

func TestUIServesAssets(t *testing.T) {
	h := newUI(builtFS())
	res, body := get(t, h, "/assets/app-1a2b.js")
	if res.StatusCode != http.StatusOK || body != `console.log("app")` {
		t.Fatalf("status %d body %q", res.StatusCode, body)
	}
	if cc := res.Header.Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("hashed asset Cache-Control %q, want immutable", cc)
	}
}

func TestUIMissingAssetIs404(t *testing.T) {
	h := newUI(builtFS())
	for _, path := range []string{"/assets/missing.js", "/favicon.ico", "/artist/x/cover.png"} {
		res, _ := get(t, h, path)
		if res.StatusCode != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", path, res.StatusCode)
		}
	}
}

func TestUIRejectsTraversal(t *testing.T) {
	h := newUI(builtFS())
	res, body := get(t, h, "/../../etc/passwd.txt")
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("status %d, want 404 (body %q)", res.StatusCode, body)
	}
}

func TestUINotBuiltExplainsHowToBuild(t *testing.T) {
	h := newUI(fstest.MapFS{".gitkeep": {Data: []byte{}}})
	res, body := get(t, h, "/artist/11111111-2222-3333-4444-555555555555")
	if res.StatusCode != http.StatusOK {
		t.Errorf("status %d, want 200", res.StatusCode)
	}
	if !strings.Contains(body, "npm run build") {
		t.Errorf("body %q should explain how to build the UI", body)
	}
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type %q, want text/html", ct)
	}
}

func TestNewUIUsesEmbeddedDist(t *testing.T) {
	// Whether or not the UI is built, the root must answer with HTML.
	res, _ := get(t, NewUI(), "/")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200", res.StatusCode)
	}
}
