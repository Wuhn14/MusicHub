// Package identify links library files to MusicBrainz recordings: from the
// MusicBrainz id in their tags, from their AcoustID fingerprint, or from a
// fielded search on their tags. Files that cannot be matched confidently stay
// pending with a short list of candidates. A background Worker runs the
// identifications from a persistent job queue.
package identify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"regexp"
	"sort"
	"sync"
	"time"

	"github.com/Wuhn14/MusicHub/internal/acoustid"
	"github.com/Wuhn14/MusicHub/internal/catalog/musicbrainz"
	"github.com/Wuhn14/MusicHub/internal/settings"
	"github.com/Wuhn14/MusicHub/internal/store"
	"github.com/Wuhn14/MusicHub/internal/textfold"
)

// Thresholds of the automatic match (see docs/conception/etape-2.md, 5.3).
const (
	// acoustIDMinScore is the lowest AcoustID score (0 to 1) accepted.
	acoustIDMinScore = 0.90
	// acoustIDMaxDelta is the largest gap between the recording and the file
	// durations for an AcoustID match.
	acoustIDMaxDelta = 5 * time.Second
	// searchMinScore is the lowest MusicBrainz search score (0 to 100) of
	// the top hit for a search match.
	searchMinScore = 95
	// searchMinLead is how far ahead of the second hit the top hit must be.
	searchMinLead = 10
	// searchMaxDelta is the largest duration gap for a search match.
	searchMaxDelta = 3 * time.Second
	// maxCandidates is how many candidates a pending file keeps.
	maxCandidates = 5
	// searchLimit is how many hits the tag search asks for.
	searchLimit = maxCandidates
)

var mbidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// ErrSkipped means the file is not waiting for identification (already
// identified, local, hidden, missing or unreadable).
var ErrSkipped = errors.New("identify: file is not pending")

// Fingerprinter computes the Chromaprint fingerprint of an audio file and its
// duration in seconds (media.Tools in production).
type Fingerprinter interface {
	Fingerprint(ctx context.Context, path string) (string, int, error)
}

// AcoustIDLookup identifies a fingerprint (acoustid.Client in production).
type AcoustIDLookup interface {
	Lookup(ctx context.Context, key, fp string, durationSec int) ([]acoustid.Match, error)
}

// RecordingFinder looks recordings up on MusicBrainz (musicbrainz.Client in
// production).
type RecordingFinder interface {
	FindRecordings(ctx context.Context, q musicbrainz.RecordingQuery, limit int) ([]musicbrainz.RecordingHit, error)
	GetRecording(ctx context.Context, id string) (musicbrainz.RecordingHit, error)
}

// AlbumLoader loads an album into the catalog cache (catalog.Catalog in
// production).
type AlbumLoader interface {
	Album(ctx context.Context, id string) (store.Album, error)
}

// SettingsGetter reads the user settings (settings.Service in production).
type SettingsGetter interface {
	Get(ctx context.Context) (settings.Settings, error)
}

// Deps are the services the identifier uses.
type Deps struct {
	Store    *store.Store
	Tools    Fingerprinter
	AcoustID AcoustIDLookup
	MB       RecordingFinder
	Catalog  AlbumLoader
	Settings SettingsGetter
	// Now is the clock (time.Now in production).
	Now func() time.Time
}

// Result describes the outcome of one identification.
type Result struct {
	// Status is store.FileIdentified or store.FilePending.
	Status string
	// RecordingID and AlbumID are set when the file was identified.
	RecordingID, AlbumID string
	// Source is how the match was found: store.SourceTag, SourceAcoustID or
	// SourceSearch; empty when the file stays pending.
	Source string
	// Candidates is how many candidates a pending file keeps.
	Candidates int
	// Duplicate tells what the duplicates rule did once the file was linked:
	// "" (nothing), "replaced-existing", "discarded-new", "hidden-new" or
	// "hidden-existing".
	Duplicate string
}

// Identifier identifies one file at a time.
type Identifier struct {
	d          Deps
	badKeyOnce sync.Once
}

// New returns an identifier. A nil Now defaults to time.Now.
func New(deps Deps) *Identifier {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	return &Identifier{d: deps}
}

// transientError marks a failure worth retrying later (a remote service is
// unreachable or overloaded).
type transientError struct{ err error }

func (e transientError) Error() string { return e.err.Error() }
func (e transientError) Unwrap() error { return e.err }

// IsTransient reports whether err is a temporary failure: the identification
// should be tried again later.
func IsTransient(err error) bool {
	var t transientError
	return errors.As(err, &t)
}

// wrapRemote classifies an error from a remote service.
func wrapRemote(err error) error {
	if errors.Is(err, musicbrainz.ErrUnavailable) || errors.Is(err, acoustid.ErrUnavailable) ||
		errors.Is(err, context.DeadlineExceeded) {
		return transientError{err}
	}
	return err
}

// match is the recording chosen for a file, with what is known of the
// release groups it appears on.
type match struct {
	hit    musicbrainz.RecordingHit
	source string
	// acoustRGs are the release groups AcoustID gave for the recording.
	acoustRGs []acoustid.RGRef
}

// Identify tries to identify the file. On success the file is linked
// (status identified) and the duplicates rule is applied; otherwise it stays
// pending with its best candidates. It returns ErrSkipped for a file that is
// not pending (also when its status changes while it is being identified:
// nothing is written then), store.ErrNotFound for an unknown file, and an
// error for which IsTransient is true when a remote service failed (the
// candidates gathered so far are kept).
func (id *Identifier) Identify(ctx context.Context, fileID string) (Result, error) {
	f, err := id.d.Store.GetFile(ctx, fileID)
	if err != nil {
		return Result{}, err
	}
	if f.Status != store.FilePending {
		return Result{Status: f.Status}, ErrSkipped
	}
	set, err := id.d.Settings.Get(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("reading settings: %w", err)
	}

	var cands []store.Candidate
	// keepPartial stores the candidates gathered before a failure.
	keepPartial := func(err error) (Result, error) {
		if len(cands) > 0 && ctx.Err() == nil {
			ranked := rankCandidates(cands)
			if serr := id.d.Store.SetCandidates(ctx, f.ID, ranked); serr != nil && !errors.Is(serr, store.ErrNotPending) {
				slog.Warn("identification : candidats non enregistrés", "file", f.ID, "err", serr)
			}
		}
		return Result{}, err
	}

	m, cands, err := id.find(ctx, f, set.AcoustIDKey)
	if err != nil {
		return keepPartial(err)
	}
	if m == nil {
		ranked := rankCandidates(cands)
		if err := id.d.Store.FinishUnmatched(ctx, f.ID, ranked, id.d.Now()); err != nil {
			return Result{}, notPending(err)
		}
		return Result{Status: store.FilePending, Candidates: len(ranked)}, nil
	}

	choice := chooseAlbum(m.hit.Releases, m.acoustRGs, f.Tags.MBReleaseGroupID, f.Tags.Album)
	albumID := choice.id
	if albumID != "" {
		if _, err := id.d.Catalog.Album(ctx, albumID); err != nil {
			if errors.Is(err, musicbrainz.ErrNotFound) {
				// The release group vanished from MusicBrainz: link the
				// recording alone.
				slog.Warn("identification : album introuvable, titre relié sans album", "file", f.ID, "album", albumID)
				albumID = ""
			} else {
				return keepPartial(wrapRemote(err))
			}
		}
	}
	disc, pos := choice.disc, choice.position
	if disc <= 0 || pos <= 0 {
		disc, pos = f.Tags.Disc, f.Tags.Track
	}
	if err := id.d.Store.LinkFile(ctx, f.ID, m.hit.RecordingID, albumID, disc, pos); err != nil {
		return Result{}, notPending(err)
	}
	res := Result{Status: store.FileIdentified, RecordingID: m.hit.RecordingID, AlbumID: albumID, Source: m.source}
	// The file is linked: finish the duplicates step even if a shutdown starts
	// now, so no half-resolved state is left behind.
	dup, err := resolveDuplicates(context.WithoutCancel(ctx), id.d.Store, f.ID, set.Duplicates)
	if err != nil {
		return res, fmt.Errorf("duplicates: %w", err)
	}
	res.Duplicate = dup
	return res, nil
}

// notPending maps the store's guard error to ErrSkipped.
func notPending(err error) error {
	if errors.Is(err, store.ErrNotPending) {
		return fmt.Errorf("%w: status changed during identification", ErrSkipped)
	}
	return err
}

// find runs the three identification steps in order. It returns the match,
// or nil and the candidates gathered.
func (id *Identifier) find(ctx context.Context, f store.File, key string) (*match, []store.Candidate, error) {
	var cands []store.Candidate

	// 1. MusicBrainz recording id in the tags.
	if rid := f.Tags.MBRecordingID; mbidPattern.MatchString(rid) {
		hit, err := id.d.MB.GetRecording(ctx, rid)
		switch {
		case err == nil:
			return &match{hit: hit, source: store.SourceTag}, nil, nil
		case errors.Is(err, musicbrainz.ErrNotFound):
			slog.Info("identification : identifiant MusicBrainz des tags inconnu", "file", f.ID)
		default:
			return nil, cands, wrapRemote(err)
		}
	}

	// 2. AcoustID fingerprint.
	if key != "" && id.d.AcoustID != nil && id.d.Tools != nil {
		matches, err := id.acoustIDLookup(ctx, f, key)
		if err != nil {
			return nil, cands, err
		}
		for _, am := range matches {
			if am.RecordingID == "" {
				continue
			}
			al := chooseAlbum(nil, am.ReleaseGroups, f.Tags.MBReleaseGroupID, f.Tags.Album)
			cands = append(cands, store.Candidate{
				RecordingID: am.RecordingID, Title: am.Title, LengthMs: am.DurationSec * 1000,
				Score: int(math.Round(am.Score * 100)), Source: store.SourceAcoustID,
				AlbumID: al.id, Album: al.title,
			})
		}
		if am, ok := bestAcoustID(matches, f.DurationMs); ok {
			hit, err := id.d.MB.GetRecording(ctx, am.RecordingID)
			switch {
			case err == nil:
				return &match{hit: hit, source: store.SourceAcoustID, acoustRGs: am.ReleaseGroups}, cands, nil
			case errors.Is(err, musicbrainz.ErrNotFound):
				// AcoustID knows a recording MusicBrainz no longer has
				// (merged or deleted): fall back to the search.
				slog.Info("identification : enregistrement AcoustID inconnu de MusicBrainz", "file", f.ID, "recording", am.RecordingID)
			default:
				return nil, cands, wrapRemote(err)
			}
		}
	}

	// 3. Fielded search on the tags.
	artist := f.Tags.Artist
	if artist == "" {
		artist = f.Tags.AlbumArtist
	}
	if artist != "" && f.Tags.Title != "" {
		hits, err := id.d.MB.FindRecordings(ctx, musicbrainz.RecordingQuery{Artist: artist, Title: f.Tags.Title, Album: f.Tags.Album}, searchLimit)
		if err != nil {
			return nil, cands, wrapRemote(err)
		}
		for _, h := range hits {
			al := chooseAlbum(h.Releases, nil, f.Tags.MBReleaseGroupID, f.Tags.Album)
			cands = append(cands, store.Candidate{
				RecordingID: h.RecordingID, Title: h.Title, Artist: h.ArtistName, LengthMs: h.LengthMs,
				Score: h.Score, Source: store.SourceSearch, AlbumID: al.id, Album: al.title,
			})
		}
		if unambiguous(hits, f.DurationMs) {
			return &match{hit: hits[0], source: store.SourceSearch}, cands, nil
		}
	}
	return nil, cands, nil
}

// acoustIDLookup fingerprints the file and asks AcoustID. A refused key or a
// fingerprint failure skips the step (nil matches, nil error); an unreachable
// service is a transient error.
func (id *Identifier) acoustIDLookup(ctx context.Context, f store.File, key string) ([]acoustid.Match, error) {
	fp, dur, err := id.d.Tools.Fingerprint(ctx, f.Path)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		slog.Warn("identification : empreinte impossible, étape AcoustID ignorée", "file", f.ID, "err", err)
		return nil, nil
	}
	matches, err := id.d.AcoustID.Lookup(ctx, key, fp, dur)
	switch {
	case err == nil:
		return matches, nil
	case errors.Is(err, acoustid.ErrBadKey):
		id.badKeyOnce.Do(func() {
			slog.Warn("AcoustID refuse la clé configurée : identification par empreinte désactivée jusqu'à correction dans les réglages")
		})
		return nil, nil
	case errors.Is(err, acoustid.ErrUnavailable), errors.Is(err, context.DeadlineExceeded):
		return nil, transientError{err}
	case ctx.Err() != nil:
		return nil, ctx.Err()
	default:
		slog.Warn("identification : AcoustID en erreur, étape ignorée", "file", f.ID, "err", err)
		return nil, nil
	}
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// durationClose reports whether both durations are known and at most max
// apart.
func durationClose(aMs, bMs int, max time.Duration) bool {
	if aMs <= 0 || bMs <= 0 {
		return false
	}
	return absDuration(time.Duration(aMs-bMs)*time.Millisecond) <= max
}

// bestAcoustID returns the best-scored match whose score reaches the
// threshold and whose duration agrees with the file. Matches are sorted best
// first; several recordings can share one fingerprint result, so the scan
// continues while the score stays above the threshold.
func bestAcoustID(matches []acoustid.Match, fileMs int) (acoustid.Match, bool) {
	for _, m := range matches {
		if m.Score < acoustIDMinScore {
			break
		}
		if m.RecordingID != "" && durationClose(m.DurationSec*1000, fileMs, acoustIDMaxDelta) {
			return m, true
		}
	}
	return acoustid.Match{}, false
}

// unambiguous reports whether the top search hit is a confident match: high
// score, clearly ahead of the second hit, and a duration within the tolerance.
func unambiguous(hits []musicbrainz.RecordingHit, fileMs int) bool {
	if len(hits) == 0 || hits[0].Score < searchMinScore {
		return false
	}
	if len(hits) > 1 && hits[1].Score > hits[0].Score-searchMinLead {
		return false
	}
	return durationClose(hits[0].LengthMs, fileMs, searchMaxDelta)
}

// rankCandidates merges candidates by recording (keeping the best score and
// filling missing details from the others), sorts them best first and keeps
// maxCandidates.
func rankCandidates(in []store.Candidate) []store.Candidate {
	byRec := map[string]int{}
	var out []store.Candidate
	for _, c := range in {
		if c.RecordingID == "" {
			continue
		}
		i, ok := byRec[c.RecordingID]
		if !ok {
			byRec[c.RecordingID] = len(out)
			out = append(out, c)
			continue
		}
		cur := out[i]
		best, other := cur, c
		if c.Score > cur.Score {
			best, other = c, cur
		}
		fill(&best.Title, other.Title)
		fill(&best.Artist, other.Artist)
		if best.AlbumID == "" {
			best.AlbumID, best.Album = other.AlbumID, other.Album
		}
		if best.LengthMs <= 0 {
			best.LengthMs = other.LengthMs
		}
		out[i] = best
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > maxCandidates {
		out = out[:maxCandidates]
	}
	for i := range out {
		out[i].Rank = i + 1
	}
	return out
}

func fill(dst *string, v string) {
	if *dst == "" {
		*dst = v
	}
}

// albumChoice is the release group retained for a recording.
type albumChoice struct {
	id, title      string
	disc, position int // 0 when unknown
}

// rgInfo summarises one release group a recording appears on.
type rgInfo struct {
	id, title      string
	secondary      []string
	date           string // earliest known release date, "" when unknown
	disc, position int
	order          int // first-seen order, to keep ties stable
}

// chooseAlbum picks the album of a recording. releases come from MusicBrainz
// (with dates, discs and positions); acoustRGs come from AcoustID (without
// dates). The rule, in order:
//  1. the release group of the tags, when the recording appears on it;
//  2. the release group given by AcoustID: when it gives exactly one that
//     contains the recording, that one; when it gives several, the next
//     rules only consider those;
//  3. the release group whose title equals the album tag (ignoring case and
//     accents);
//  4. the oldest release group without secondary type (studio album);
//  5. the oldest release group.
//
// Disc and position are those of the earliest release of the chosen group
// that gives them.
func chooseAlbum(releases []musicbrainz.ReleaseRef, acoustRGs []acoustid.RGRef, tagRG, tagAlbum string) albumChoice {
	var pool []*rgInfo
	byID := map[string]*rgInfo{}
	for _, r := range releases {
		if r.ReleaseGroupID == "" {
			continue
		}
		g, ok := byID[r.ReleaseGroupID]
		if !ok {
			g = &rgInfo{id: r.ReleaseGroupID, title: r.Title, secondary: r.SecondaryTypes, order: len(pool)}
			byID[g.id] = g
			pool = append(pool, g)
		}
		if r.Date != "" && (g.date == "" || r.Date < g.date) {
			g.date = r.Date
			if r.Disc > 0 && r.Position > 0 {
				g.disc, g.position = r.Disc, r.Position
			}
		}
		if g.disc <= 0 && r.Disc > 0 && r.Position > 0 {
			g.disc, g.position = r.Disc, r.Position
		}
	}
	// Without MusicBrainz releases, AcoustID's release groups are the pool.
	if len(pool) == 0 {
		for _, g := range acoustRGs {
			if g.ID == "" || byID[g.ID] != nil {
				continue
			}
			info := &rgInfo{id: g.ID, title: g.Title, secondary: g.SecondaryTypes, order: len(pool)}
			byID[g.ID] = info
			pool = append(pool, info)
		}
	}
	choice := func(g *rgInfo) albumChoice {
		return albumChoice{id: g.id, title: g.title, disc: g.disc, position: g.position}
	}

	// 1. Tags.
	if g := byID[tagRG]; tagRG != "" && g != nil {
		return choice(g)
	}
	if tagRG != "" && len(pool) == 0 {
		// Nothing known about the recording's releases: trust the tags.
		return albumChoice{id: tagRG, title: tagAlbum}
	}

	// 2. AcoustID.
	var fromAcoust []*rgInfo
	seen := map[string]bool{}
	for _, g := range acoustRGs {
		if info := byID[g.ID]; info != nil && !seen[g.ID] {
			seen[g.ID] = true
			fromAcoust = append(fromAcoust, info)
		}
	}
	if len(fromAcoust) == 1 {
		return choice(fromAcoust[0])
	}
	if len(fromAcoust) > 1 {
		pool = fromAcoust
	}
	if len(pool) == 0 {
		return albumChoice{}
	}

	// 3. Album title from the tags.
	if want := textfold.Fold(tagAlbum); want != "" {
		for _, g := range pool {
			if textfold.Fold(g.title) == want {
				return choice(g)
			}
		}
	}

	// 4 and 5. Oldest, studio albums first.
	oldest := func(keep func(*rgInfo) bool) *rgInfo {
		var best *rgInfo
		for _, g := range pool {
			if !keep(g) {
				continue
			}
			if best == nil || dateBefore(g, best) {
				best = g
			}
		}
		return best
	}
	if g := oldest(func(g *rgInfo) bool { return len(g.secondary) == 0 }); g != nil {
		return choice(g)
	}
	return choice(oldest(func(*rgInfo) bool { return true }))
}

// dateBefore orders release groups by date, unknown dates last, then by
// first-seen order.
func dateBefore(a, b *rgInfo) bool {
	switch {
	case a.date == b.date:
		return a.order < b.order
	case a.date == "":
		return false
	case b.date == "":
		return true
	}
	return a.date < b.date
}
