package store

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

var fetched = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func TestArtistRoundTrip(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	a := Artist{ID: "a1", Name: "Test Band", SortName: "Band, Test", Disambiguation: "fictional", Country: "FR", FetchedAt: fetched}
	albums := []Album{
		{ID: "g1", Title: "First", ArtistID: "a1", ArtistName: "Test Band", PrimaryType: "Album", SecondaryTypes: []string{"Live"}, FirstReleaseDate: "2001-05-01", FetchedAt: fetched},
		{ID: "g2", Title: "Second", ArtistID: "a1", ArtistName: "Test Band", PrimaryType: "EP", FirstReleaseDate: "2003", FetchedAt: fetched},
	}
	if err := s.UpsertArtist(ctx, a, albums); err != nil {
		t.Fatalf("UpsertArtist: %v", err)
	}
	gotA, gotAlbums, err := s.GetArtist(ctx, "a1")
	if err != nil {
		t.Fatalf("GetArtist: %v", err)
	}
	if !reflect.DeepEqual(gotA, a) {
		t.Errorf("artist = %+v, want %+v", gotA, a)
	}
	if len(gotAlbums) != 2 {
		t.Fatalf("albums = %d, want 2", len(gotAlbums))
	}
	byID := map[string]Album{}
	for _, al := range gotAlbums {
		byID[al.ID] = al
	}
	if g := byID["g1"]; g.Title != "First" || !reflect.DeepEqual(g.SecondaryTypes, []string{"Live"}) || g.FirstReleaseDate != "2001-05-01" || g.ArtistName != "Test Band" {
		t.Errorf("album g1 = %+v", g)
	}
	if g := byID["g2"]; len(g.SecondaryTypes) != 0 || g.PrimaryType != "EP" {
		t.Errorf("album g2 = %+v", g)
	}
}

func sampleAlbum() Album {
	return Album{
		ID: "g1", Title: "Double", ArtistID: "a1", ArtistName: "Test Band", PrimaryType: "Album",
		FirstReleaseDate: "2001-05-01", FetchedAt: fetched,
		Tracks: []Track{
			{RecordingID: "r2", Title: "Disc two opener", Disc: 2, Position: 1, LengthMs: 1000},
			{RecordingID: "r12", Title: "Disc one closer", Disc: 1, Position: 12, LengthMs: 2000},
			{RecordingID: "r1", Title: "Disc one opener", Disc: 1, Position: 1, LengthMs: 3000},
		},
	}
}

func TestAlbumRoundTripKeepsTrackOrder(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	if err := s.UpsertAlbum(ctx, sampleAlbum()); err != nil {
		t.Fatalf("UpsertAlbum: %v", err)
	}
	got, err := s.GetAlbum(ctx, "g1")
	if err != nil {
		t.Fatalf("GetAlbum: %v", err)
	}
	want := []Track{
		{RecordingID: "r1", Title: "Disc one opener", Disc: 1, Position: 1, LengthMs: 3000},
		{RecordingID: "r12", Title: "Disc one closer", Disc: 1, Position: 12, LengthMs: 2000},
		{RecordingID: "r2", Title: "Disc two opener", Disc: 2, Position: 1, LengthMs: 1000},
	}
	if !reflect.DeepEqual(got.Tracks, want) {
		t.Errorf("tracks = %+v, want %+v", got.Tracks, want)
	}
	if got.Title != "Double" || !got.FetchedAt.Equal(fetched) {
		t.Errorf("album = %+v", got)
	}
}

func TestUpsertReplacesTracks(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	if err := s.UpsertAlbum(ctx, sampleAlbum()); err != nil {
		t.Fatal(err)
	}
	al := sampleAlbum()
	al.Title = "Double (remaster)"
	al.Tracks = []Track{{RecordingID: "r9", Title: "Only one", Disc: 1, Position: 1, LengthMs: 500}}
	if err := s.UpsertAlbum(ctx, al); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetAlbum(ctx, "g1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Double (remaster)" || len(got.Tracks) != 1 || got.Tracks[0].RecordingID != "r9" {
		t.Errorf("album = %+v", got)
	}
}

func TestGetMissingReturnsErrNotFound(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	if _, _, err := s.GetArtist(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetArtist err = %v, want ErrNotFound", err)
	}
	if _, err := s.GetAlbum(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetAlbum err = %v, want ErrNotFound", err)
	}
}
