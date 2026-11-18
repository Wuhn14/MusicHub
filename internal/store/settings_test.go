package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSettingsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	got, err := s.GetSettings(ctx)
	if err != nil || len(got) != 0 {
		t.Fatalf("fresh store: %v, %v", got, err)
	}
	if err := s.PutSettings(ctx, map[string]string{"a": "1", "b": "2"}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutSettings(ctx, map[string]string{"a": "3"}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err = s.GetSettings(ctx)
	if err != nil || got["a"] != "3" || got["b"] != "2" || len(got) != 2 {
		t.Fatalf("got %v, %v", got, err)
	}
}
