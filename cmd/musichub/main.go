// Command musichub runs the MusicHub server.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Wuhn14/MusicHub/internal/acoustid"
	"github.com/Wuhn14/MusicHub/internal/catalog"
	"github.com/Wuhn14/MusicHub/internal/catalog/musicbrainz"
	"github.com/Wuhn14/MusicHub/internal/config"
	"github.com/Wuhn14/MusicHub/internal/identify"
	"github.com/Wuhn14/MusicHub/internal/library"
	"github.com/Wuhn14/MusicHub/internal/media"
	"github.com/Wuhn14/MusicHub/internal/server"
	"github.com/Wuhn14/MusicHub/internal/settings"
	"github.com/Wuhn14/MusicHub/internal/store"
	"github.com/Wuhn14/MusicHub/internal/stream"
	"github.com/Wuhn14/MusicHub/internal/upload"
	"github.com/Wuhn14/MusicHub/internal/web"
)

// version is reported in the MusicBrainz User-Agent. Release builds set it
// with -ldflags "-X main.version=1.2.3".
var version = "dev"

func main() {
	if err := run(); err != nil {
		slog.Error("MusicHub s'arrête", "err", err)
		os.Exit(1)
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

	// SIGTERM is what `docker stop` sends. Request contexts derive from ctx,
	// so in-flight MusicBrainz waits stop as soon as the signal arrives.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	tools := media.New(cfg.FFmpeg, cfg.FFprobe, cfg.FPcalc)
	settingsSvc := settings.New(st)
	scanner := library.NewScanner(st, tools, cfg.MusicDir, time.Now)
	// Uploaded files wait in the incoming folder until they are identified;
	// the scanner never walks it and never flags them as missing.
	incomingDir, err := filepath.Abs(filepath.Join(cfg.DataDir, "incoming"))
	if err != nil {
		return fmt.Errorf("dossier d'envoi : %w", err)
	}
	scanner.IncomingDir = incomingDir
	// Identified and local files are filed into the music folder.
	organizer := library.NewOrganizer(st, tools, cfg.MusicDir, settingsSvc)
	organizer.Recordings = mb
	// Moves interrupted by a stop are settled before anything reads the
	// files (the scanner, the identification worker, the API).
	if err := organizer.RepairPlacing(ctx); err != nil {
		slog.Warn("réparation des rangements interrompus incomplète", "err", err)
	}
	// Likewise for deletions of uploads interrupted by a stop.
	restoredDel, removedDel, err := upload.RecoverDeletes(ctx, st, incomingDir)
	if err != nil {
		slog.Warn("suppressions d'envois interrompues non vérifiées", "err", err)
	}
	if restoredDel > 0 || removedDel > 0 {
		slog.Info("suppressions d'envois interrompues réglées", "restaurés", restoredDel, "supprimés", removedDel)
	}

	// Identification runs in the background from a persistent job queue;
	// the scanner queues every new or modified pending file.
	identifier := identify.New(identify.Deps{
		Store: st, Tools: tools, AcoustID: acoustid.New(cfg.AcoustIDURL, 333*time.Millisecond),
		MB: mb, Catalog: cat, Settings: settingsSvc, Placer: organizer, Now: time.Now,
	})
	worker := identify.NewWorker(identifier, st)
	scanner.OnFileAdded = func(f store.File) { worker.Enqueue(f.ID) }
	workerDone := make(chan struct{})
	go func() { defer close(workerDone); worker.Run(ctx) }()
	// Like the runner below: stop before the database is closed.
	defer func() { stop(); <-workerDone }()

	runner := library.NewRunner(scanner, func() time.Duration {
		s, err := settingsSvc.Get(ctx)
		if err != nil || s.ScanInterval <= 0 {
			return 15 * time.Minute
		}
		return s.ScanInterval
	})
	runnerDone := make(chan struct{})
	go func() { defer close(runnerDone); runner.Run(ctx) }()
	// Deferred after st.Close, so it runs first: the scanner must stop using
	// the database before it is closed.
	defer func() { stop(); <-runnerDone }()

	streamer := stream.New(st, tools, func() int {
		s, err := settingsSvc.Get(ctx)
		if err != nil || s.MaxTranscodes <= 0 {
			return 3
		}
		return s.MaxTranscodes
	})

	removed, err := upload.CleanPartFiles(incomingDir)
	if err != nil {
		return fmt.Errorf("préparation du dossier d'envoi : %w", err)
	}
	if removed > 0 {
		slog.Info("envois interrompus nettoyés", "count", removed)
	}
	receiver := upload.New(st, tools, incomingDir, settingsSvc, func(f store.File) { worker.Enqueue(f.ID) })

	// Uploaded files identified (or made local) before a stop, but not moved
	// yet, are filed now.
	sweepDone := make(chan struct{})
	go func() {
		defer close(sweepDone)
		if n := organizer.PlaceStranded(ctx, incomingDir); n > 0 {
			slog.Info("fichiers envoyés rangés au démarrage", "count", n)
		}
	}()
	defer func() { stop(); <-sweepDone }()

	api := web.NewAPI(web.Deps{
		Catalog: cat, Covers: covers, Settings: settingsSvc, Tools: tools,
		Library: library.Service{Query: library.NewQuery(st), Runner: runner}, Stream: streamer,
		Availability: st, Identify: worker, Upload: receiver, Files: st,
		IdentifyActions: identifier, IdentifyStore: st, LocalCoversDir: filepath.Join(cfg.DataDir, "local-covers"),
		IncomingDir: incomingDir,
	})
	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           server.New(api, web.NewUI()),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	slog.Info("MusicHub à l'écoute", "addr", srv.Addr)

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
