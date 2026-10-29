// Package version reports the twig version. Release builds inject Version
// and Commit with -ldflags -X; builds from a checkout or go install fall
// back to the build information the Go toolchain stamps into every binary.
package version

import (
	"fmt"
	"runtime/debug"
	"strings"
)

// Set by -ldflags -X at release time; empty otherwise.
var (
	// Version is the release version, without a leading "v".
	Version = ""
	// Commit is the full commit hash the binary was built from.
	Commit = ""
)

// resolved reads the build information once: the module version when the
// binary was installed with go install, the VCS stamp otherwise.
func resolved() (v, commit string) {
	v, commit = Version, Commit

	if info, ok := debug.ReadBuildInfo(); ok {
		if v == "" && info.Main.Version != "" && info.Main.Version != "(devel)" && !isPseudo(info.Main.Version) {
			v = strings.TrimPrefix(info.Main.Version, "v")
		}
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				if commit == "" {
					commit = s.Value
				}
			case "vcs.modified":
				if s.Value == "true" && commit != "" && !strings.HasSuffix(commit, "+dirty") {
					commit += "+dirty"
				}
			}
		}
	}

	if v == "" {
		v = "dev"
	}
	return v, commit
}

// Number returns the bare version twig reports: the release version when the
// linker injected one, otherwise "dev".
func Number() string {
	v, _ := resolved()
	return v
}

// CommitShort returns the resolved commit, trimmed to 12 characters, or
// "none" when nothing is known. A dirty marker is kept: it says more about
// the build than two extra hash digits.
func CommitShort() string {
	_, commit := resolved()
	if commit == "" || commit == "none" {
		return "none"
	}
	dirty := strings.HasSuffix(commit, "+dirty")
	commit = strings.TrimSuffix(commit, "+dirty")
	if len(commit) > 12 {
		commit = commit[:12]
	}
	if dirty {
		commit += "+dirty"
	}
	return commit
}

// String is the full version line: name, version and commit.
func String() string {
	v, commit := resolved()
	if commit == "" || commit == "none" {
		return "twig " + v
	}
	if len(commit) > 12 {
		commit = commit[:12]
	}
	return fmt.Sprintf("twig %s (%s)", v, commit)
}

// isPseudo reports whether a module version is the pseudo-version the
// toolchain derives from an untagged commit: those read as noise in
// --version, where the commit hash says the same thing better.
func isPseudo(v string) bool {
	return strings.HasPrefix(v, "v0.0.0-") || strings.Contains(v, "-00010101000000-")
}
