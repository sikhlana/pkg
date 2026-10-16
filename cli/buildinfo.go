package cli

import (
	"cmp"
	"runtime/debug"
	"strings"
)

// Link-time build values. Each is empty unless the build sets it with -ldflags -X, for example:
//
//	go build -ldflags "-X github.com/sikhlana/pkg/cli.Version=v1.2.3"
//
// ReadBuildInfo prefers a non-empty value here to what the go command stamped into the binary.
var (
	// Version is the program's version.
	Version string

	// Revision is the version-control revision the program was built from.
	Revision string

	// Time is the time of Revision, in whatever format the build passes.
	Time string
)

const (
	develVersion   = "(devel)"
	defaultVersion = "dev"
)

// BuildInfo describes how the running program was built.
type BuildInfo struct {
	// Version is the program's version, never empty in a BuildInfo from ReadBuildInfo.
	Version string

	// Revision is the version-control revision, or empty when unknown.
	Revision string

	// Time is the time of Revision, or empty when unknown. When stamped by the go command, it's the commit time in
	// RFC 3339 format.
	Time string

	// Modified reports whether the go command recorded uncommitted changes in the source tree at build time.
	Modified bool
}

// ReadBuildInfo returns the running program's BuildInfo, resolving each field on its own. Version, Revision and Time
// come from the link-time variable of the same name when it's non-empty. Otherwise they come from what the go command
// stamped into the binary: the main module's version unless it's (devel), vcs.revision and vcs.time. Failing both,
// Version is "dev" and the others are empty. Modified comes only from vcs.modified.
func ReadBuildInfo() BuildInfo {
	stamped, _ := debug.ReadBuildInfo()

	return resolveBuildInfo(BuildInfo{Version: Version, Revision: Revision, Time: Time}, stamped)
}

// String returns the version followed by a parenthesised, comma-separated list of the revision, the time and the
// word modified, each included only when known or true, as in "v1.2.3 (abc123, 2026-10-16T09:00:00Z, modified)".
// The parentheses are left out when the list is empty.
func (b BuildInfo) String() string {
	var details []string

	if b.Revision != "" {
		details = append(details, b.Revision)
	}

	if b.Time != "" {
		details = append(details, b.Time)
	}

	if b.Modified {
		details = append(details, "modified")
	}

	if len(details) == 0 {
		return b.Version
	}

	return b.Version + " (" + strings.Join(details, ", ") + ")"
}

// resolveBuildInfo fills each of linked's Version, Revision and Time that's empty from stamped, which may be nil, and
// takes Modified from stamped alone.
func resolveBuildInfo(linked BuildInfo, stamped *debug.BuildInfo) BuildInfo {
	if stamped == nil {
		stamped = &debug.BuildInfo{}
	}

	settings := make(map[string]string, len(stamped.Settings))

	for _, s := range stamped.Settings {
		settings[s.Key] = s.Value
	}

	mainVersion := stamped.Main.Version

	if mainVersion == develVersion {
		mainVersion = ""
	}

	return BuildInfo{
		Version:  cmp.Or(linked.Version, mainVersion, defaultVersion),
		Revision: cmp.Or(linked.Revision, settings["vcs.revision"]),
		Time:     cmp.Or(linked.Time, settings["vcs.time"]),
		Modified: settings["vcs.modified"] == "true",
	}
}
