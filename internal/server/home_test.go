package server

import (
	"strings"
	"testing"
	"time"

	"github.com/gopkgreg/gopkgreg/internal/goindex"
)

// fakeIndex stands in for the index.golang.org feed.
type fakeIndex []goindex.Release

func (f fakeIndex) Latest(n int) []goindex.Release { return f[:min(n, len(f))] }

func ecosystem() fakeIndex {
	return fakeIndex{
		{Path: "github.com/spf13/cobra", Version: "v1.10.2", Time: time.Now().Add(-3 * time.Minute)},
		{Path: "golang.org/x/net", Version: "v0.40.0", Time: time.Now().Add(-time.Hour)},
	}
}

func TestHomeShowsEcosystemWhenNothingIsHosted(t *testing.T) {
	env := newTestEnvWith(t, func(c *Config) { c.GoIndex = ecosystem() })
	_, body := fetch(t, env.srv, "/")
	for _, want := range []string{
		"Just released across Go",
		`href="/?m=github.com%2Fspf13%2Fcobra"`, // the public module view
		"v1.10.2",
		"Public module · released",
		"doesn't host them",
		"Nothing is hosted on gopkgreg yet",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("home page is missing %q", want)
		}
	}
	for _, unwanted := range []string{"No modules yet.", `aria-label="Registry statistics"`, "ecosystem-below"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("home page with nothing hosted shows %q", unwanted)
		}
	}
}

func TestHomeShowsEcosystemBelowHostedModules(t *testing.T) {
	env := newTestEnvWith(t, func(c *Config) { c.GoIndex = ecosystem() })
	env.publish(t, "gopkgreg.test/alice/retry", "v1.0.0")
	_, body := fetch(t, env.srv, "/")
	for _, want := range []string{"Just updated", `href="/alice/retry"`, `aria-label="Registry statistics"`, "Just released across Go", "ecosystem-below"} {
		if !strings.Contains(body, want) {
			t.Errorf("home page is missing %q", want)
		}
	}
	if strings.Contains(body, "Nothing is hosted on gopkgreg yet") {
		t.Error("home page invites the first publish although a module is hosted")
	}
	// Hosted modules come first.
	if strings.Index(body, "Just updated") > strings.Index(body, "Just released across Go") {
		t.Error("public releases are listed above the registry's own modules")
	}
}

func TestHomeWithoutEcosystemKeepsEmptyState(t *testing.T) {
	_, body := fetch(t, newTestEnv(t).srv, "/")
	if !strings.Contains(body, "No modules yet.") || strings.Contains(body, "Just released across Go") {
		t.Error("home page without the index feed should show the empty registry message only")
	}
}
