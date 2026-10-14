package goindex

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

// index is a fake index.golang.org: it serves pages from entries, in order,
// one page per request, and records what was asked.
type index struct {
	mu       sync.Mutex
	pages    [][]Release
	status   int
	requests []*http.Request
}

func (x *index) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.requests = append(x.requests, r)
	if x.status != 0 {
		w.WriteHeader(x.status)
		return
	}
	var page []Release
	if len(x.pages) > 0 {
		page, x.pages = x.pages[0], x.pages[1:]
	}
	enc := json.NewEncoder(w)
	for _, e := range page {
		_ = enc.Encode(map[string]any{"Path": e.Path, "Version": e.Version, "Timestamp": e.Time})
	}
}

func newFeed(t *testing.T, x *index) *Feed {
	t.Helper()
	srv := httptest.NewServer(x)
	t.Cleanup(srv.Close)
	return &Feed{
		URL:       srv.URL + "/index",
		UserAgent: "gopkgreg-test",
		Skip:      func(p string) bool { return strings.HasPrefix(p, "gopkgreg.test/") },
		Now:       func() time.Time { return t0 },
	}
}

func rel(path, version string, minute int) Release {
	return Release{Path: path, Version: version, Time: t0.Add(time.Duration(minute) * time.Minute)}
}

func paths(rs []Release) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Path + "@" + r.Version
	}
	return out
}

func TestRefreshKeepsTaggedReleases(t *testing.T) {
	x := &index{pages: [][]Release{{
		rel("github.com/a/stable", "v1.2.0", 1),
		rel("github.com/a/pseudo", "v0.0.0-20261014120000-abcdefabcdef", 2),
		rel("github.com/a/pre", "v2.0.0-rc.1", 3),
		rel("github.com/a/stable", "v1.3.0", 4), // newer release of the same module
		rel("not a module path", "v1.0.0", 5),
		rel("github.com/a/badversion", "1.0.0", 6),
		rel("gopkgreg.test/alice/retry", "v1.0.0", 7), // hosted here: skipped
		rel("github.com/b/incompatible", "v28.0.0+incompatible", 8),
		rel("github.com/c/new", "v0.1.0", 9),
	}}}
	f := newFeed(t, x)
	if err := f.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(paths(f.Latest(10)), " ")
	want := "github.com/c/new@v0.1.0 github.com/b/incompatible@v28.0.0+incompatible github.com/a/stable@v1.3.0"
	if got != want {
		t.Errorf("Latest =\n  %s\nwant\n  %s", got, want)
	}
	if n := len(f.Latest(2)); n != 2 {
		t.Errorf("Latest(2) returned %d", n)
	}

	r := x.requests[0]
	if ua := r.Header.Get("User-Agent"); ua != "gopkgreg-test" {
		t.Errorf("User-Agent = %q", ua)
	}
	if since := r.URL.Query().Get("since"); since != t0.Add(-startWindow).Format(time.RFC3339Nano) {
		t.Errorf("first since = %s, want %s before now", since, startWindow)
	}
	if limit := r.URL.Query().Get("limit"); limit != fmt.Sprint(pageLimit) {
		t.Errorf("limit = %s", limit)
	}
}

func TestRefreshContinuesFromLastEntry(t *testing.T) {
	x := &index{pages: [][]Release{
		{rel("github.com/a/one", "v1.0.0", 1), rel("github.com/a/two", "v1.0.0", 2)},
		{rel("github.com/a/two", "v1.0.0", 2), rel("github.com/a/three", "v1.0.0", 3)},
	}}
	f := newFeed(t, x)
	for range 2 {
		if err := f.Refresh(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if since := x.requests[1].URL.Query().Get("since"); since != t0.Add(2*time.Minute).Format(time.RFC3339Nano) {
		t.Errorf("second since = %s, want the last entry's time", since)
	}
	// The entry repeated at the page boundary isn't listed twice.
	if got := strings.Join(paths(f.Latest(10)), " "); got != "github.com/a/three@v1.0.0 github.com/a/two@v1.0.0 github.com/a/one@v1.0.0" {
		t.Errorf("Latest = %s", got)
	}
}

func TestRefreshCatchesUpOnFullPages(t *testing.T) {
	full := make([]Release, pageLimit)
	for i := range full {
		full[i] = Release{Path: fmt.Sprintf("github.com/x/m%d", i), Version: "v1.0.0", Time: t0.Add(time.Duration(i) * time.Millisecond)}
	}
	x := &index{pages: [][]Release{full, {rel("github.com/x/last", "v1.0.0", 5)}}}
	f := newFeed(t, x)
	if err := f.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(x.requests) != 2 {
		t.Fatalf("made %d requests after a full page, want 2", len(x.requests))
	}
	got := f.Latest(keep + 10)
	if len(got) != keep {
		t.Errorf("kept %d releases, want %d", len(got), keep)
	}
	if got[0].Path != "github.com/x/last" {
		t.Errorf("newest = %s", got[0].Path)
	}
}

func TestRefreshStopsAfterMaxPages(t *testing.T) {
	pages := make([][]Release, maxPages+3)
	for p := range pages {
		pages[p] = make([]Release, pageLimit)
		for i := range pages[p] {
			pages[p][i] = Release{Path: fmt.Sprintf("github.com/x/p%dm%d", p, i), Version: "v1.0.0", Time: t0.Add(time.Duration(p*pageLimit+i) * time.Millisecond)}
		}
	}
	x := &index{pages: pages}
	f := newFeed(t, x)
	if err := f.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(x.requests) != maxPages {
		t.Errorf("made %d requests, want at most %d", len(x.requests), maxPages)
	}
}

func TestRefreshErrorKeepsReleases(t *testing.T) {
	x := &index{pages: [][]Release{{rel("github.com/a/one", "v1.0.0", 1)}}}
	f := newFeed(t, x)
	if err := f.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	x.mu.Lock()
	x.status = http.StatusServiceUnavailable
	x.mu.Unlock()
	if err := f.Refresh(context.Background()); err == nil || !strings.Contains(err.Error(), "503") {
		t.Errorf("Refresh during an outage = %v, want the 503", err)
	}
	if got := paths(f.Latest(10)); len(got) != 1 || got[0] != "github.com/a/one@v1.0.0" {
		t.Errorf("Latest after an outage = %v, want the earlier release kept", got)
	}
}

func TestRefreshRejectsBadEntry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintln(w, `{"Path":"github.com/a/one","Version":"v1.0.0","Timestamp":"2026-10-14T12:00:00Z"}`)
		_, _ = fmt.Fprintln(w, `{"Path":`)
	}))
	defer srv.Close()
	f := &Feed{URL: srv.URL, Now: func() time.Time { return t0 }}
	if err := f.Refresh(context.Background()); err == nil {
		t.Error("Refresh accepted a malformed entry")
	}
	if n := len(f.Latest(10)); n != 0 {
		t.Errorf("kept %d releases from a malformed page", n)
	}
}
