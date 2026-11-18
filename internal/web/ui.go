package web

import (
	"bytes"
	"embed"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"
)

// dist holds the compiled Svelte interface (see the web/ directory). Only
// .gitkeep is committed, so a fresh clone embeds an empty directory until
// `npm run build` has been run.
//
//go:embed all:dist
var dist embed.FS

// notBuiltPage is served for every interface route when dist has no
// index.html, so a fresh `go run` explains what is missing instead of 404ing.
const notBuiltPage = `<!doctype html>
<html lang="fr">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>MusicHub : interface non compilée</title>
<style>
body{margin:0;min-height:100vh;display:grid;place-items:center;background:#121212;color:#e8e8e8;font-family:system-ui,sans-serif}
main{max-width:36rem;padding:2rem}
code{background:#2a2a2a;padding:.15rem .4rem;border-radius:4px}
</style>
</head>
<body>
<main>
<h1>Interface non compilée</h1>
<p>Le serveur MusicHub fonctionne, mais l'interface web n'a pas encore été compilée.</p>
<p>Depuis la racine du dépôt, lance <code>cd web &amp;&amp; npm ci &amp;&amp; npm run build</code>, puis recompile et relance le serveur.</p>
<p>L'API reste disponible sous <code>/api/</code>.</p>
</main>
</body>
</html>
`

// NewUI returns the handler serving the embedded web interface.
func NewUI() http.Handler {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		// fs.Sub only fails on an invalid path, which "dist" is not.
		panic(err)
	}
	return newUI(sub)
}

type ui struct {
	fsys  fs.FS
	index []byte // nil when the interface has not been built
}

// newUI serves files from fsys. Paths without an extension are interface
// routes and get index.html; a missing file with an extension is a 404.
func newUI(fsys fs.FS) http.Handler {
	index, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			slog.Error("ui: reading index.html failed", "err", err)
		}
		index = nil
	}
	return &ui{fsys: fsys, index: index}
}

func (u *ui) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "Méthode non autorisée.", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if path.Ext(name) == "" {
		u.serveIndex(w, r)
		return
	}
	if hidden(name) {
		http.NotFound(w, r)
		return
	}
	data, err := fs.ReadFile(u.fsys, name)
	if err != nil {
		// Covers missing files and directories alike.
		http.NotFound(w, r)
		return
	}
	if strings.HasPrefix(name, "assets/") {
		// Vite puts a content hash in every file name under assets/.
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}

func (u *ui) serveIndex(w http.ResponseWriter, r *http.Request) {
	page := u.index
	if page == nil {
		page = []byte(notBuiltPage)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(page))
}

// hidden reports whether any path segment is a dotfile (such as .gitkeep).
func hidden(name string) bool {
	for _, seg := range strings.Split(name, "/") {
		if strings.HasPrefix(seg, ".") {
			return true
		}
	}
	return false
}
