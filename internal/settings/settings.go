// Package settings holds the user-editable settings, persisted in the store.
package settings

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/Wuhn14/MusicHub/internal/store"
)

// ErrInvalid wraps every validation failure; its message is French and safe
// to show to the user.
var ErrInvalid = errors.New("réglage invalide")

// Allowed values for Settings.Duplicates.
const (
	DuplicatesBest         = "best"
	DuplicatesKeepBoth     = "keep-both"
	DuplicatesKeepExisting = "keep-existing"
)

// Settings is the full set of user settings.
type Settings struct {
	AcoustIDKey       string
	ScanInterval      time.Duration
	OrganizeDropped   bool
	WriteTagsUploaded bool
	WriteTagsDropped  bool
	Duplicates        string
	MaxUploadBytes    int64
	MaxTranscodes     int
}

// Patch is a partial update: nil fields are left unchanged.
type Patch struct {
	AcoustIDKey       *string
	ScanInterval      *time.Duration
	OrganizeDropped   *bool
	WriteTagsUploaded *bool
	WriteTagsDropped  *bool
	Duplicates        *string
	MaxUploadBytes    *int64
	MaxTranscodes     *int
}

// Defaults returns the settings used until the user changes something.
func Defaults() Settings {
	return Settings{
		ScanInterval:      15 * time.Minute,
		WriteTagsUploaded: true,
		Duplicates:        DuplicatesBest,
		MaxUploadBytes:    2 << 30,
		MaxTranscodes:     3,
	}
}

const (
	minScanInterval = time.Minute
	maxScanInterval = 24 * time.Hour
	minUploadBytes  = 1 << 20
	maxUploadBytes  = 20 << 30
	minTranscodes   = 1
	maxTranscodes   = 16
)

// Storage keys.
const (
	kAcoustID     = "acoustid_key"
	kScan         = "scan_interval_seconds"
	kOrganize     = "organize_dropped"
	kTagsUploaded = "write_tags_uploaded"
	kTagsDropped  = "write_tags_dropped"
	kDuplicates   = "duplicates"
	kMaxUpload    = "max_upload_bytes"
	kTranscodes   = "max_transcodes"
)

// Service reads and writes settings.
type Service struct {
	st *store.Store
	mu sync.Mutex // serialises read-validate-write updates
}

// New returns a Service backed by st.
func New(st *store.Store) *Service { return &Service{st: st} }

// Get returns the current settings, defaults filling any missing key.
func (s *Service) Get(ctx context.Context) (Settings, error) {
	kv, err := s.st.GetSettings(ctx)
	if err != nil {
		return Settings{}, err
	}
	return fromMap(kv), nil
}

// Update applies p after validating the resulting settings as a whole.
func (s *Service) Update(ctx context.Context, p Patch) (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next, err := s.Get(ctx)
	if err != nil {
		return Settings{}, err
	}
	if p.AcoustIDKey != nil {
		next.AcoustIDKey = *p.AcoustIDKey
	}
	if p.ScanInterval != nil {
		next.ScanInterval = *p.ScanInterval
	}
	if p.OrganizeDropped != nil {
		next.OrganizeDropped = *p.OrganizeDropped
	}
	if p.WriteTagsUploaded != nil {
		next.WriteTagsUploaded = *p.WriteTagsUploaded
	}
	if p.WriteTagsDropped != nil {
		next.WriteTagsDropped = *p.WriteTagsDropped
	}
	if p.Duplicates != nil {
		next.Duplicates = *p.Duplicates
	}
	if p.MaxUploadBytes != nil {
		next.MaxUploadBytes = *p.MaxUploadBytes
	}
	if p.MaxTranscodes != nil {
		next.MaxTranscodes = *p.MaxTranscodes
	}
	if err := validate(next); err != nil {
		return Settings{}, err
	}
	if err := s.st.PutSettings(ctx, toMap(next)); err != nil {
		return Settings{}, err
	}
	return next, nil
}

func validate(s Settings) error {
	switch {
	case s.ScanInterval < minScanInterval || s.ScanInterval > maxScanInterval:
		return fmt.Errorf("%w : l'intervalle de scan doit être compris entre 1 minute et 24 heures", ErrInvalid)
	case s.MaxUploadBytes < minUploadBytes || s.MaxUploadBytes > maxUploadBytes:
		return fmt.Errorf("%w : la taille maximale d'envoi doit être comprise entre 1 Mo et 20 Go", ErrInvalid)
	case s.MaxTranscodes < minTranscodes || s.MaxTranscodes > maxTranscodes:
		return fmt.Errorf("%w : le nombre de conversions simultanées doit être compris entre 1 et 16", ErrInvalid)
	}
	switch s.Duplicates {
	case DuplicatesBest, DuplicatesKeepBoth, DuplicatesKeepExisting:
	default:
		return fmt.Errorf("%w : choix de gestion des doublons inconnu", ErrInvalid)
	}
	return nil
}

func toMap(s Settings) map[string]string {
	return map[string]string{
		kAcoustID:     s.AcoustIDKey,
		kScan:         strconv.FormatInt(int64(s.ScanInterval/time.Second), 10),
		kOrganize:     strconv.FormatBool(s.OrganizeDropped),
		kTagsUploaded: strconv.FormatBool(s.WriteTagsUploaded),
		kTagsDropped:  strconv.FormatBool(s.WriteTagsDropped),
		kDuplicates:   s.Duplicates,
		kMaxUpload:    strconv.FormatInt(s.MaxUploadBytes, 10),
		kTranscodes:   strconv.Itoa(s.MaxTranscodes),
	}
}

// fromMap decodes stored values; a missing or unparsable value falls back to
// its default so a damaged row never prevents startup.
func fromMap(kv map[string]string) Settings {
	s := Defaults()
	if v, ok := kv[kAcoustID]; ok {
		s.AcoustIDKey = v
	}
	if n, err := strconv.ParseInt(kv[kScan], 10, 64); err == nil {
		s.ScanInterval = time.Duration(n) * time.Second
	}
	setBool := func(key string, dst *bool) {
		if b, err := strconv.ParseBool(kv[key]); err == nil {
			*dst = b
		}
	}
	setBool(kOrganize, &s.OrganizeDropped)
	setBool(kTagsUploaded, &s.WriteTagsUploaded)
	setBool(kTagsDropped, &s.WriteTagsDropped)
	if v := kv[kDuplicates]; v != "" {
		s.Duplicates = v
	}
	if n, err := strconv.ParseInt(kv[kMaxUpload], 10, 64); err == nil {
		s.MaxUploadBytes = n
	}
	if n, err := strconv.Atoi(kv[kTranscodes]); err == nil {
		s.MaxTranscodes = n
	}
	return s
}
