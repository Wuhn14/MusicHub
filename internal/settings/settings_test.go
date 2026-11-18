package settings

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wuhn14/MusicHub/internal/store"
)

func openStore(t *testing.T, path string) *store.Store {
	t.Helper()
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestDefaults(t *testing.T) {
	d := Defaults()
	want := Settings{
		AcoustIDKey:       "",
		ScanInterval:      15 * time.Minute,
		OrganizeDropped:   false,
		WriteTagsUploaded: true,
		WriteTagsDropped:  false,
		Duplicates:        "best",
		MaxUploadBytes:    2 << 30,
		MaxTranscodes:     3,
	}
	if d != want {
		t.Fatalf("Defaults() = %+v, want %+v", d, want)
	}
	got, err := New(openStore(t, filepath.Join(t.TempDir(), "d.db"))).Get(context.Background())
	if err != nil || got != want {
		t.Fatalf("Get on empty store = %+v, %v", got, err)
	}
}

func TestUpdateValidates(t *testing.T) {
	svc := New(openStore(t, filepath.Join(t.TempDir(), "v.db")))
	ctx := context.Background()
	dur := func(d time.Duration) *time.Duration { return &d }
	i64 := func(n int64) *int64 { return &n }
	num := func(n int) *int { return &n }
	str := func(s string) *string { return &s }
	bad := []Patch{
		{ScanInterval: dur(30 * time.Second)},
		{ScanInterval: dur(25 * time.Hour)},
		{MaxUploadBytes: i64(1<<20 - 1)},
		{MaxUploadBytes: i64(20<<30 + 1)},
		{MaxTranscodes: num(0)},
		{MaxTranscodes: num(17)},
		{Duplicates: str("nope")},
	}
	for i, p := range bad {
		if _, err := svc.Update(ctx, p); !errors.Is(err, ErrInvalid) {
			t.Errorf("patch %d: err = %v, want ErrInvalid", i, err)
		}
	}
	// Nothing was stored by the rejected patches.
	if got, _ := svc.Get(ctx); got != Defaults() {
		t.Fatalf("rejected patches changed settings: %+v", got)
	}
	ok := Patch{ScanInterval: dur(time.Minute), MaxUploadBytes: i64(20 << 30), MaxTranscodes: num(16), Duplicates: str("keep-both")}
	if _, err := svc.Update(ctx, ok); err != nil {
		t.Fatalf("boundary values rejected: %v", err)
	}
}

func TestSettingsPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.db")
	st := openStore(t, path)
	ctx := context.Background()
	tr := true
	key := "secretkey1234"
	d := 5 * time.Minute
	n := 7
	if _, err := New(st).Update(ctx, Patch{AcoustIDKey: &key, OrganizeDropped: &tr, ScanInterval: &d, MaxTranscodes: &n}); err != nil {
		t.Fatal(err)
	}
	st.Close()
	got, err := New(openStore(t, path)).Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := Defaults()
	want.AcoustIDKey, want.OrganizeDropped, want.ScanInterval, want.MaxTranscodes = key, true, d, 7
	if got != want {
		t.Fatalf("after reopen = %+v, want %+v", got, want)
	}
	// An absent field keeps its value; an empty key deletes it.
	empty := ""
	got, err = New(openStore(t, path)).Update(ctx, Patch{AcoustIDKey: &empty})
	if err != nil || got.AcoustIDKey != "" || !got.OrganizeDropped {
		t.Fatalf("delete key: %+v, %v", got, err)
	}
}
