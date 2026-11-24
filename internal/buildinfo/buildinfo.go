// Package buildinfo reports which Foliarz build is running.
package buildinfo

import (
	"runtime/debug"
	"strings"
)

// Version and Commit can be injected at build time, e.g.:
//
//	go build -ldflags "-X github.com/mdukat/foliarz/internal/buildinfo.Version=v1.0.0 -X github.com/mdukat/foliarz/internal/buildinfo.Commit=abc1234"
var (
	Version string
	Commit  string
)

// String returns a short human-readable identifier of the running build, such
// as "v1.0.0+abc1234", "abc1234" or "unknown".
func String() string {
	version := Version
	commit := Commit
	dirty := false

	if bi, ok := debug.ReadBuildInfo(); ok {
		if version == "" {
			if v := bi.Main.Version; v != "" && v != "(devel)" && !isPseudoVersion(v) {
				version = v
			}
		}
		if commit == "" {
			for _, s := range bi.Settings {
				switch s.Key {
				case "vcs.revision":
					commit = shortCommit(s.Value)
				case "vcs.modified":
					dirty = s.Value == "true"
				}
			}
		}
	}

	if commit != "" && dirty {
		commit += "-dirty"
	}

	switch {
	case version != "" && commit != "":
		return version + "+" + commit
	case version != "":
		return version
	case commit != "":
		return commit
	default:
		return "unknown"
	}
}

// isPseudoVersion reports whether v is a Go pseudo-version derived from a VCS
// revision, which carries no release information of its own.
func isPseudoVersion(v string) bool {
	return strings.HasPrefix(v, "v0.0.0-")
}

func shortCommit(rev string) string {
	const max = 7
	if len(rev) > max {
		return rev[:max]
	}
	return rev
}
