// Command fakemb serves invented MusicBrainz data for development and
// end-to-end tests. Point MUSICHUB_MB_URL and MUSICHUB_CAA_URL at it.
package main

import (
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"syscall"

	"github.com/Wuhn14/MusicHub/internal/catalog/mbtest"
)

var coverPath = regexp.MustCompile(`^/release-group/[^/]+/front(-\d+)?$`)

func main() {
	addr := flag.String("addr", "127.0.0.1:18081", "address to listen on")
	flag.Parse()

	fake := mbtest.New()
	defer fake.Close()

	// Cover Art Archive has no covers for invented albums: answer 404 so the
	// same server can be used for MUSICHUB_CAA_URL.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if coverPath.MatchString(r.URL.Path) {
			http.NotFound(w, r)
			return
		}
		fake.Config.Handler.ServeHTTP(w, r)
	})

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	srv := &http.Server{Handler: handler}
	go func() { _ = srv.Serve(ln) }()

	log.Printf("fake MusicBrainz and Cover Art Archive listening on http://%s", ln.Addr())
	log.Printf("search for: \"Lumen Harbor\" (artist %s), \"Paper Lanterns\" (album %s)", mbtest.ArtistLumen, mbtest.AlbumTwoDisc)
	log.Printf("artist with %d generated albums: %s", mbtest.ManyAlbums, mbtest.ArtistMany)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	_ = srv.Close()
}
