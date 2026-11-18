package web

import (
	"net/http"
	"testing"
)

type stubStream struct{ gotID string }

func (s *stubStream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.gotID = r.PathValue("fileId")
	w.WriteHeader(http.StatusNoContent)
}

func TestAPIStreamRoute(t *testing.T) {
	st := &stubStream{}
	h := NewAPI(Deps{Stream: st})
	for _, m := range []string{"GET", "HEAD"} {
		if rec := send(t, h, m, "/api/stream/abc", ""); rec.Code != http.StatusNoContent || st.gotID != "abc" {
			t.Errorf("%s: code %d id %q", m, rec.Code, st.gotID)
		}
	}
	if rec := send(t, NewAPI(Deps{}), "GET", "/api/stream/abc", ""); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("unwired: %d", rec.Code)
	}
}
