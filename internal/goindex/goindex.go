// Package goindex follows index.golang.org, the public feed of every module
// version the Go module mirror has fetched, and keeps the newest tagged
// releases in memory so the home page can show what's happening across the
// Go ecosystem. It keeps module paths, versions and times only: no code is
// downloaded or stored.
package goindex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	xmodule "golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

// DefaultURL is the Go module index's feed endpoint.
const DefaultURL = "https://index.golang.org/index"

const (
	// pageLimit is the most entries the index returns per request.
	pageLimit = 2000
	// maxPages bounds one Refresh, so a long outage doesn't turn into a
	// burst of requests when the feed comes back.
	maxPages = 5
	// keep is how many releases are remembered, one per module.
	keep = 50
	// startWindow is how far back the first Refresh reads.
	startWindow = 10 * time.Minute
	// maxBody caps one page; 2,000 entries are about 300 KB.
	maxBody = 4 << 20
)

// Release is one tagged version of a public module.
type Release struct {
	Path    string
	Version string
	Time    time.Time // when the mirror first fetched it
}

// Feed follows the index. The zero value is not usable: set URL.
type Feed struct {
	URL       string
	HTTP      *http.Client
	UserAgent string
	Log       *slog.Logger
	// Skip reports modules to leave out, such as the registry's own, which
	// the home page lists separately.
	Skip func(modPath string) bool
	// Now returns the current time; tests replace it.
	Now func() time.Time

	mu     sync.Mutex
	since  time.Time // read the index from here next time
	latest []Release // newest first, one per module
}

// Run refreshes now and then every interval until ctx ends.
func (f *Feed) Run(ctx context.Context, interval time.Duration) {
	for {
		if err := f.Refresh(ctx); err != nil && ctx.Err() == nil && f.Log != nil {
			f.Log.Warn("refresh Go module index", "url", f.URL, "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

// Refresh reads the entries added since the last call. A full page means
// more are waiting, so it keeps reading, up to maxPages pages.
func (f *Feed) Refresh(ctx context.Context) error {
	f.mu.Lock()
	since := f.since
	f.mu.Unlock()
	if since.IsZero() {
		since = f.now().Add(-startWindow)
	}
	for range maxPages {
		entries, err := f.fetch(ctx, since)
		if err != nil {
			return err
		}
		f.mu.Lock()
		f.latest = merge(f.latest, entries, f.Skip)
		if n := len(entries); n > 0 && entries[n-1].Time.After(since) {
			since = entries[n-1].Time
		}
		f.since = since
		f.mu.Unlock()
		if len(entries) < pageLimit {
			break
		}
	}
	return nil
}

// Latest returns up to n of the newest releases, newest first.
func (f *Feed) Latest(n int) []Release {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Release(nil), f.latest[:min(n, len(f.latest))]...)
}

func (f *Feed) now() time.Time {
	if f.Now != nil {
		return f.Now()
	}
	return time.Now()
}

func (f *Feed) client() *http.Client {
	if f.HTTP != nil {
		return f.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// fetch reads one page of the index, oldest first.
func (f *Feed) fetch(ctx context.Context, since time.Time) ([]Release, error) {
	u := f.URL + "?" + url.Values{
		"since": {since.UTC().Format(time.RFC3339Nano)},
		"limit": {fmt.Sprint(pageLimit)},
	}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build index request: %w", err)
	}
	if f.UserAgent != "" {
		req.Header.Set("User-Agent", f.UserAgent)
	}
	resp, err := f.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("read index: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("read index: %s", resp.Status)
	}
	var entries []Release
	sc := bufio.NewScanner(io.LimitReader(resp.Body, maxBody))
	for sc.Scan() {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var e struct {
			Path, Version string
			Timestamp     time.Time
		}
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			return nil, fmt.Errorf("read index entry: %w", err)
		}
		entries = append(entries, Release{Path: e.Path, Version: e.Version, Time: e.Timestamp})
	}
	// A page over maxBody is cut mid-line, which fails as a bad entry.
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read index: %w", err)
	}
	return entries, nil
}

// merge puts the releases worth showing from entries (oldest first) ahead
// of latest (newest first), keeps one release per module, and trims to
// keep.
func merge(latest, entries []Release, skip func(string) bool) []Release {
	out := make([]Release, 0, keep)
	seen := make(map[string]bool, keep)
	add := func(r Release) {
		if len(out) < keep && !seen[r.Path] {
			seen[r.Path] = true
			out = append(out, r)
		}
	}
	for i := len(entries) - 1; i >= 0; i-- {
		if r := entries[i]; showable(r) && (skip == nil || !skip(r.Path)) {
			add(r)
		}
	}
	for _, r := range latest {
		add(r)
	}
	return out
}

// showable reports whether r is a valid module's tagged, stable release.
// Pseudo-versions (untagged commits) always carry a prerelease part, so
// rejecting prereleases rejects them too.
func showable(r Release) bool {
	return xmodule.CheckPath(r.Path) == nil &&
		semver.IsValid(r.Version) &&
		semver.Prerelease(r.Version) == "" &&
		!xmodule.IsPseudoVersion(r.Version)
}
