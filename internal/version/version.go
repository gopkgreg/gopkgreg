// Package version reports which build of Gopkgreg is running.
package version

import "runtime/debug"

// Module is Gopkgreg's module path; the CLI installs from Module + "/cmd/gopkgreg".
const Module = "github.com/gopkgreg/gopkgreg"

// CLIInstall is the command that installs the gopkgreg CLI.
const CLIInstall = "go install " + Module + "/cmd/gopkgreg@latest"

// Version is set for release builds:
//
//	go build -ldflags "-X github.com/gopkgreg/gopkgreg/internal/version.Version=v1.2.3"
//
// When it's empty, String falls back to what the go command recorded.
var Version = ""

// String returns the release version, the module version for
// "go install …@v1.2.3" builds, or "devel" plus the commit for local builds.
func String() string {
	if Version != "" {
		return Version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "devel"
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	var rev string
	var dirty bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if rev == "" {
		return "devel"
	}
	v := "devel+" + rev[:min(12, len(rev))]
	if dirty {
		v += "-dirty"
	}
	return v
}
