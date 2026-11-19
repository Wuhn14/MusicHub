package musicbrainz

import (
	"net/http"
	"sync"
	"testing"
	"time"
)

// sendTimes is a RoundTripper that records when each request leaves the
// client: rate-limit tests measure the client's own spacing, not the
// arrival times at the fake server (which scheduling under -race skews).
type sendTimes struct {
	mu    sync.Mutex
	times []time.Time
	next  http.RoundTripper
}

func (s *sendTimes) RoundTrip(r *http.Request) (*http.Response, error) {
	s.mu.Lock()
	s.times = append(s.times, time.Now())
	s.mu.Unlock()
	return s.next.RoundTrip(r)
}

func (s *sendTimes) all() []time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Time(nil), s.times...)
}

// recordSends makes c record its send times.
func recordSends(c *Client) *sendTimes {
	s := &sendTimes{next: http.DefaultTransport}
	c.http.Transport = s
	return s
}

// checkSpacing fails unless the k-th request (in send order) left at least
// k intervals after start, the moment before the first call. This holds for
// a working limiter whatever the scheduling delays: the k-th earliest send
// cannot precede the k-th time slot, which is k intervals after the first.
func checkSpacing(t *testing.T, start time.Time, times []time.Time, interval time.Duration) {
	t.Helper()
	for k, at := range times {
		if min := time.Duration(k) * interval; at.Sub(start) < min {
			t.Errorf("request %d sent %v after start, want >= %v", k, at.Sub(start), min)
		}
	}
}
