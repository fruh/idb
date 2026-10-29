// Package version reports the build version of idb.
package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
)

// Set at build time with -ldflags "-X github.com/fruh/idb/internal/version.Version=...".
var (
	Version = "dev"
	Commit  = ""
	Date    = ""
)

// Short returns just the version, e.g. "v1.2.3" or "dev".
func Short() string {
	v, _, _ := resolve()
	return v
}

// String returns the one-line description printed by `idb version`.
func String() string {
	v, commit, date := resolve()
	var parts []string
	if commit != "" {
		parts = append(parts, "commit "+commit)
	}
	if date != "" {
		parts = append(parts, "built "+date)
	}
	parts = append(parts, runtime.Version()+" "+runtime.GOOS+"/"+runtime.GOARCH)
	return fmt.Sprintf("idb %s (%s)", v, strings.Join(parts, ", "))
}

// resolve prefers ldflags values and falls back to the module/VCS info that
// the Go toolchain embeds, so `go install ...@v1.2.3` reports v1.2.3.
func resolve() (v, commit, date string) {
	v, commit, date = Version, Commit, Date
	if v != "dev" {
		return
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	if mv := bi.Main.Version; mv != "" && mv != "(devel)" {
		v = mv
	}
	dirty := false
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			if commit == "" {
				commit = s.Value
				if len(commit) > 7 {
					commit = commit[:7]
				}
			}
		case "vcs.time":
			if date == "" {
				date = s.Value
			}
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if dirty && commit != "" {
		commit += "-dirty"
	}
	return
}
