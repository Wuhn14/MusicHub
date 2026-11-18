// Package acoustid is a small client for the AcoustID web service (API v2).
// It identifies a recording from its Chromaprint fingerprint. Requests are
// spaced to respect the public limit and retried when the service is
// overloaded. The API key is never logged nor put in an error message.
package acoustid

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	// ErrBadKey means AcoustID refused the API key.
	ErrBadKey = errors.New("acoustid: invalid API key")
	// ErrUnavailable means AcoustID is unreachable or still overloaded after
	// the retries.
	ErrUnavailable = errors.New("acoustid: service unavailable")
)

const (
	maxRetries = 3
	// maxBody caps the decoded response size.
	maxBody = 5 << 20
)

// codeInvalidKey is the AcoustID error code for a refused API key. Other
// codes are not interpreted: retries depend on the HTTP status only.
const (
	codeInvalidKey = 4
)

// RGRef is a release group a recording appears on.
type RGRef struct {
	ID, Title, Type string
	SecondaryTypes  []string
}

// Match is a recording proposed for a fingerprint, with the score (0 to 1) of
// the fingerprint result it comes from.
type Match struct {
	Score         float64
	RecordingID   string
	Title         string
	DurationSec   int
	ReleaseGroups []RGRef
}

// Client talks to one AcoustID server. It is safe for concurrent use; all
// callers share one rate limiter.
type Client struct {
	baseURL     string
	minInterval time.Duration
	http        *http.Client
	retryBase   time.Duration // first retry wait, doubled each time (1s in production)

	mu   sync.Mutex
	next time.Time // earliest start of the next request
}

// New returns a client for the web service at baseURL (for example
// https://api.acoustid.org/v2). Request starts are at least minInterval
// apart (333ms in production, three requests per second).
func New(baseURL string, minInterval time.Duration) *Client {
	return &Client{
		baseURL:     strings.TrimRight(baseURL, "/"),
		minInterval: minInterval,
		http:        &http.Client{Timeout: 30 * time.Second},
		retryBase:   time.Second,
	}
}

func (c *Client) wait(ctx context.Context) error {
	c.mu.Lock()
	now := time.Now()
	start := c.next
	if start.Before(now) {
		start = now
	}
	c.next = start.Add(c.minInterval)
	c.mu.Unlock()
	return sleep(ctx, time.Until(start))
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

type jsonResponse struct {
	Status string `json:"status"`
	Error  struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Results []struct {
		Score      float64 `json:"score"`
		Recordings []struct {
			ID            string `json:"id"`
			Title         string `json:"title"`
			Duration      int    `json:"duration"`
			ReleaseGroups []struct {
				ID             string   `json:"id"`
				Title          string   `json:"title"`
				Type           string   `json:"type"`
				SecondaryTypes []string `json:"secondarytypes"`
			} `json:"releasegroups"`
		} `json:"recordings"`
	} `json:"results"`
}

// Lookup identifies the recording behind fingerprint fp of a file lasting
// durationSec seconds. The matches are flattened to one per (result,
// recording), best score first. An unknown fingerprint gives no match and no
// error.
func (c *Client) Lookup(ctx context.Context, key, fp string, durationSec int) ([]Match, error) {
	form := url.Values{
		"client":      {key},
		"duration":    {strconv.Itoa(durationSec)},
		"fingerprint": {fp},
		"meta":        {"recordings releasegroups compress"},
		"format":      {"json"},
	}
	body := form.Encode()

	for attempt := 0; ; attempt++ {
		if err := c.wait(ctx); err != nil {
			return nil, err
		}
		res, retry, err := c.do(ctx, body, key)
		if err == nil {
			return flatten(res), nil
		}
		if !retry {
			return nil, err
		}
		if attempt >= maxRetries {
			return nil, fmt.Errorf("%w: still overloaded after %d retries", ErrUnavailable, maxRetries)
		}
		slog.Debug("acoustid: retrying", "attempt", attempt+1)
		if err := sleep(ctx, c.retryBase<<attempt); err != nil {
			return nil, err
		}
	}
}

// do performs one request. The bool tells whether the failure is worth a
// retry (5xx, 429, or an AcoustID service error).
func (c *Client) do(ctx context.Context, body, key string) (jsonResponse, bool, error) {
	var out jsonResponse
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/lookup", strings.NewReader(body))
	if err != nil {
		return out, false, redact(err, key)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return out, false, ctx.Err()
		}
		return out, false, fmt.Errorf("%w: %s", ErrUnavailable, redact(err, key))
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
		_, _ = io.Copy(io.Discard, resp.Body)
		return out, true, ErrUnavailable
	}
	// AcoustID answers errors with a 4xx status and a JSON body: decode before
	// judging the status.
	decErr := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&out)
	if decErr != nil && ctx.Err() != nil {
		return out, false, ctx.Err()
	}
	if out.Status == "error" {
		switch out.Error.Code {
		case codeInvalidKey:
			return out, false, ErrBadKey
		}
		return out, false, fmt.Errorf("acoustid: error %d: %s", out.Error.Code, redactString(out.Error.Message, key))
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return out, false, fmt.Errorf("acoustid: unexpected status %s", resp.Status)
	}
	if decErr != nil {
		return out, false, fmt.Errorf("acoustid: decoding response: %w", decErr)
	}
	return out, false, nil
}

func flatten(res jsonResponse) []Match {
	out := []Match{}
	for _, r := range res.Results {
		for _, rec := range r.Recordings {
			m := Match{Score: r.Score, RecordingID: rec.ID, Title: rec.Title, DurationSec: rec.Duration, ReleaseGroups: []RGRef{}}
			for _, g := range rec.ReleaseGroups {
				m.ReleaseGroups = append(m.ReleaseGroups, RGRef{ID: g.ID, Title: g.Title, Type: g.Type, SecondaryTypes: g.SecondaryTypes})
			}
			out = append(out, m)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}

// redact returns the error text with the key masked.
func redact(err error, key string) error {
	return errors.New(redactString(err.Error(), key))
}

func redactString(s, key string) string {
	if key == "" {
		return s
	}
	return strings.ReplaceAll(s, key, "***")
}
