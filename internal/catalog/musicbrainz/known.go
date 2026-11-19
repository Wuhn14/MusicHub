package musicbrainz

import "context"

type knownRecordingKey struct{}

// WithRecording returns a context carrying a recording already fetched, so
// that code further down (the organizer) does not look it up again.
func WithRecording(ctx context.Context, hit RecordingHit) context.Context {
	return context.WithValue(ctx, knownRecordingKey{}, hit)
}

// RecordingFrom returns the recording carried by ctx when it has the given
// id.
func RecordingFrom(ctx context.Context, id string) (RecordingHit, bool) {
	hit, ok := ctx.Value(knownRecordingKey{}).(RecordingHit)
	if !ok || id == "" || hit.RecordingID != id {
		return RecordingHit{}, false
	}
	return hit, true
}
