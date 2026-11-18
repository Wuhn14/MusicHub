// Command musichub runs the MusicHub server.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Wuhn14/MusicHub/internal/catalog"
	"github.com/Wuhn14/MusicHub/internal/catalog/musicbrainz"
	"github.com/Wuhn14/MusicHub/internal/config"
	"github.com/Wuhn14/MusicHub/internal/server"
	"github.com/Wuhn14/MusicHub/internal/store"
	"github.com/Wuhn14/MusicHub/internal/web"
)

// version is reported in the MusicBrainz User-Agent. Release builds set it
// with -ldflags "-X main.version=1.2.3".
var version = "dev"

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return fmt.Errorf("création du dossier de données : %w", err)
	}

	st, err := store.Open(filepath.Join(cfg.DataDir, "musichub.db"))
	if err != nil {
		return fmt.Errorf("ouverture de la base de données : %w", err)
	}
	defer st.Close()

	userAgent := fmt.Sprintf("MusicHub/%s ( https://github.com/Wuhn14/MusicHub )", version)
	mb := musicbrainz.New(cfg.MusicBrainzURL, userAgent, time.Second)
	cat := catalog.New(st, mb, time.Now)
	covers := catalog.NewCovers(cfg.CoverArtURL, filepath.Join(cfg.DataDir, "covers"), time.Now)

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           server.New(web.NewAPI(cat, covers), web.NewUI()),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// SIGTERM is what `docker stop` sends.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	log.Printf("MusicHub à l'écoute sur %s", srv.Addr)

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
