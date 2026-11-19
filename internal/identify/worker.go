package identify

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/Wuhn14/MusicHub/internal/store"
)

// JobKind is the kind of the identification jobs in the store.
const JobKind = "identify"

// retryDelays are the waits before the next attempt after the first, second
// and third transient failure. After one more failure the job is dropped and
// the file stays pending.
var retryDelays = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute}

// pollInterval is how often the worker looks for delayed jobs that became
// due.
const pollInterval = 30 * time.Second

// WorkerStatus is a snapshot of the identification queue.
type WorkerStatus struct {
	// Queued is how many jobs wait in the queue (due or delayed).
	Queued int
	// Done and Failed count the jobs finished since the process started.
	Done, Failed int
}

// Worker runs identification jobs from the persistent queue, one at a time.
// A single worker per database is assumed: jobs are not locked against
// another process or another Worker on the same store.
type Worker struct {
	id   *Identifier
	st   *store.Store
	wake chan struct{}
	poll time.Duration

	mu           sync.Mutex
	done, failed int
	running      string // id of the file being identified, "" when idle
	rerun        bool   // Enqueue hit the running file: queue it again after
}

// NewWorker returns a worker running id over the jobs stored in st.
func NewWorker(id *Identifier, st *store.Store) *Worker {
	return &Worker{id: id, st: st, wake: make(chan struct{}, 1), poll: pollInterval}
}

func (w *Worker) now() time.Time { return w.id.d.Now() }

// Enqueue adds an identification job for the file (nothing happens when it
// already has one) and wakes the worker. It never blocks on the worker.
func (w *Worker) Enqueue(fileID string) {
	w.mu.Lock()
	if fileID != "" && fileID == w.running {
		// Its job row is about to be deleted or rescheduled: remember the
		// request and queue the file again once the run ends.
		w.rerun = true
		w.mu.Unlock()
		return
	}
	w.mu.Unlock()
	if _, err := w.st.EnqueueJob(context.Background(), JobKind, fileID, w.now()); err != nil {
		slog.Error("identification : mise en file impossible", "file", fileID, "err", err)
		return
	}
	w.signal()
}

func (w *Worker) signal() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// Status returns the queue size and the counters.
func (w *Worker) Status() WorkerStatus {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	n, err := w.st.CountJobs(ctx, JobKind)
	if err != nil {
		slog.Warn("identification : file illisible", "err", err)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return WorkerStatus{Queued: n, Done: w.done, Failed: w.failed}
}

// Run queues the pending files that were never looked at, then processes
// the due jobs, waking on Enqueue and every 30 seconds for delayed jobs,
// until ctx is done.
func (w *Worker) Run(ctx context.Context) {
	w.enqueueBacklog(ctx)
	ticker := time.NewTicker(w.poll)
	defer ticker.Stop()
	for {
		w.processDue(ctx)
		select {
		case <-ctx.Done():
			return
		case <-w.wake:
		case <-ticker.C:
		}
	}
}

// enqueueBacklog queues every pending file without job nor candidates, so a
// library scanned before identification existed gets identified.
func (w *Worker) enqueueBacklog(ctx context.Context) {
	ids, err := w.st.PendingFilesWithoutJob(ctx, JobKind)
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("identification : fichiers en attente illisibles", "err", err)
		}
		return
	}
	now := w.now()
	for _, id := range ids {
		if _, err := w.st.EnqueueJob(ctx, JobKind, id, now); err != nil {
			if ctx.Err() == nil {
				slog.Error("identification : mise en file impossible", "file", id, "err", err)
			}
			return
		}
	}
	if len(ids) > 0 {
		slog.Info("identification : fichiers en attente mis en file", "nombre", len(ids))
	}
}

// processDue runs jobs until none is due or ctx is done.
func (w *Worker) processDue(ctx context.Context) {
	var last store.Job
	for ctx.Err() == nil {
		job, err := w.st.NextJob(ctx, JobKind, w.now())
		if errors.Is(err, store.ErrNotFound) {
			return
		}
		if err != nil {
			if ctx.Err() == nil {
				slog.Error("identification : lecture de la file impossible", "err", err)
			}
			return
		}
		if job.ID == last.ID && job.Attempts == last.Attempts {
			// The queue could not be updated after the previous run of this
			// job: wait for the next wake-up rather than spin.
			return
		}
		last = job
		w.process(ctx, job)
	}
}

// process runs one job and updates the queue according to the outcome. An
// Enqueue of the same file during the run queues it again afterwards.
func (w *Worker) process(ctx context.Context, job store.Job) {
	w.mu.Lock()
	w.running, w.rerun = job.FileID, false
	w.mu.Unlock()
	defer func() {
		w.mu.Lock()
		rerun := w.rerun
		w.running, w.rerun = "", false
		w.mu.Unlock()
		if rerun {
			w.Enqueue(job.FileID)
		}
	}()
	w.runJob(ctx, job)
}

func (w *Worker) runJob(ctx context.Context, job store.Job) {
	res, err := w.id.Identify(ctx, job.FileID)
	if err != nil && ctx.Err() != nil {
		// Interrupted: the job stays queued for the next start.
		return
	}
	// The outcome is settled: recording it must not be cut short by a stop,
	// or a finished job would stay queued.
	ctx = context.WithoutCancel(ctx)
	switch {
	case err == nil:
		slog.Info("identification terminée", "file", job.FileID, "status", res.Status,
			"source", res.Source, "candidats", res.Candidates, "doublon", res.Duplicate)
		w.finish(ctx, job, false)
	case errors.Is(err, ErrSkipped), errors.Is(err, store.ErrNotFound):
		w.drop(ctx, job)
	case IsTransient(err):
		attempts := job.Attempts + 1
		if attempts > len(retryDelays) {
			slog.Warn("identification abandonnée après plusieurs échecs, le fichier reste à identifier",
				"file", job.FileID, "tentatives", attempts, "err", err)
			w.finish(ctx, job, true)
			return
		}
		next := w.now().Add(retryDelays[attempts-1])
		slog.Info("identification reportée", "file", job.FileID, "tentative", attempts, "après", next, "err", err)
		if err := w.st.RescheduleJob(ctx, job.ID, attempts, next); err != nil {
			slog.Error("identification : report impossible", "file", job.FileID, "err", err)
			w.finish(ctx, job, true)
		}
	default:
		slog.Error("identification en échec", "file", job.FileID, "err", err)
		w.finish(ctx, job, true)
	}
}

// finish deletes the job and counts it.
func (w *Worker) finish(ctx context.Context, job store.Job, failed bool) {
	w.drop(ctx, job)
	w.mu.Lock()
	defer w.mu.Unlock()
	if failed {
		w.failed++
	} else {
		w.done++
	}
}

func (w *Worker) drop(ctx context.Context, job store.Job) {
	if err := w.st.DeleteJob(ctx, job.ID); err != nil {
		slog.Error("identification : suppression de la tâche impossible", "job", job.ID, "err", err)
	}
}
