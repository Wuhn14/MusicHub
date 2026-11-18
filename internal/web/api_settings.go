package web

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/Wuhn14/MusicHub/internal/media"
	"github.com/Wuhn14/MusicHub/internal/settings"
)

// maxSettingsBody bounds a PUT /api/settings body.
const maxSettingsBody = 64 << 10

// SettingsService reads and updates the persisted settings.
type SettingsService interface {
	Get(ctx context.Context) (settings.Settings, error)
	Update(ctx context.Context, p settings.Patch) (settings.Settings, error)
}

// ToolStatusReader reports the state of the external media tools.
type ToolStatusReader interface {
	Status(ctx context.Context) []media.ToolStatus
}

type settingsJSON struct {
	AcoustIDKey         string `json:"acoustidKey"`
	AcoustIDKeySet      bool   `json:"acoustidKeySet"`
	ScanIntervalMinutes int    `json:"scanIntervalMinutes"`
	OrganizeDropped     bool   `json:"organizeDropped"`
	WriteTagsUploaded   bool   `json:"writeTagsUploaded"`
	WriteTagsDropped    bool   `json:"writeTagsDropped"`
	Duplicates          string `json:"duplicates"`
	MaxUploadBytes      int64  `json:"maxUploadBytes"`
	MaxTranscodes       int    `json:"maxTranscodes"`
}

// settingsPatchJSON uses pointers so that absent fields stay unchanged.
type settingsPatchJSON struct {
	AcoustIDKey         *string `json:"acoustidKey"`
	ScanIntervalMinutes *int    `json:"scanIntervalMinutes"`
	OrganizeDropped     *bool   `json:"organizeDropped"`
	WriteTagsUploaded   *bool   `json:"writeTagsUploaded"`
	WriteTagsDropped    *bool   `json:"writeTagsDropped"`
	Duplicates          *string `json:"duplicates"`
	MaxUploadBytes      *int64  `json:"maxUploadBytes"`
	MaxTranscodes       *int    `json:"maxTranscodes"`
}

// maskKey shows only the last four characters of a secret.
func maskKey(k string) string {
	if k == "" {
		return ""
	}
	r := []rune(k)
	if len(r) > 4 {
		r = r[len(r)-4:]
	}
	return "••••" + string(r)
}

func toSettingsJSON(s settings.Settings) settingsJSON {
	return settingsJSON{
		AcoustIDKey:         maskKey(s.AcoustIDKey),
		AcoustIDKeySet:      s.AcoustIDKey != "",
		ScanIntervalMinutes: int(s.ScanInterval / time.Minute),
		OrganizeDropped:     s.OrganizeDropped,
		WriteTagsUploaded:   s.WriteTagsUploaded,
		WriteTagsDropped:    s.WriteTagsDropped,
		Duplicates:          s.Duplicates,
		MaxUploadBytes:      s.MaxUploadBytes,
		MaxTranscodes:       s.MaxTranscodes,
	}
}

func (a *api) getSettings(w http.ResponseWriter, r *http.Request) {
	s, err := a.settings.Get(r.Context())
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toSettingsJSON(s))
}

func (a *api) putSettings(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}
	dec :=json.NewDecoder(http.MaxBytesReader(w, r.Body, maxSettingsBody))
	dec.DisallowUnknownFields()
	var in settingsPatchJSON
	if err := dec.Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "Requête invalide.")
		return
	}
	// Exactly one JSON value is accepted.
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "Requête invalide.")
		return
	}
	if in.AcoustIDKey != nil && !utf8.ValidString(*in.AcoustIDKey) {
		writeError(w, http.StatusBadRequest, "Requête invalide.")
		return
	}
	p := settings.Patch{
		AcoustIDKey:       in.AcoustIDKey,
		OrganizeDropped:   in.OrganizeDropped,
		WriteTagsUploaded: in.WriteTagsUploaded,
		WriteTagsDropped:  in.WriteTagsDropped,
		Duplicates:        in.Duplicates,
		MaxUploadBytes:    in.MaxUploadBytes,
		MaxTranscodes:     in.MaxTranscodes,
	}
	if in.ScanIntervalMinutes != nil {
		// Clamp before multiplying so a huge value cannot overflow into range.
		m := *in.ScanIntervalMinutes
		if m < 0 || m > 24*60+1 {
			m = 24*60 + 1
		}
		d := time.Duration(m) * time.Minute
		p.ScanInterval = &d
	}
	s, err := a.settings.Update(r.Context(), p)
	if err != nil {
		if errors.Is(err, settings.ErrInvalid) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toSettingsJSON(s))
}

type toolJSON struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Version   string `json:"version"`
	Available bool   `json:"available"`
	Error     string `json:"error"`
}

func (a *api) system(w http.ResponseWriter, r *http.Request) {
	statuses := a.tools.Status(r.Context())
	out := struct {
		Tools []toolJSON `json:"tools"`
	}{Tools: make([]toolJSON, 0, len(statuses))}
	for _, s := range statuses {
		out.Tools = append(out.Tools, toolJSON{Name: s.Name, Path: s.Path, Version: s.Version, Available: s.Available, Error: s.Error})
	}
	writeJSON(w, http.StatusOK, out)
}
