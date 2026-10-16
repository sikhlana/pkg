// Package version validates and orders module release versions.
package version

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"golang.org/x/mod/semver"
)

// ErrInvalid reports a VERSION value that isn't exactly MAJOR.MINOR.PATCH.
var ErrInvalid = errors.New("invalid version")

// Parse validates the contents of a VERSION file and returns the version in
// Go's form, vMAJOR.MINOR.PATCH. Surrounding whitespace is ignored. The
// value must be exactly MAJOR.MINOR.PATCH, with no v prefix, pre-release or
// build suffix; anything else wraps ErrInvalid.
func Parse(s string) (string, error) {
	s = strings.TrimSpace(s)

	if strings.HasPrefix(s, "v") || !IsRelease("v"+s) {
		return "", fmt.Errorf("%w: %q, want MAJOR.MINOR.PATCH", ErrInvalid, s)
	}

	return "v" + s, nil
}

// IsRelease reports whether v is a release version in Go's form,
// vMAJOR.MINOR.PATCH, with no pre-release or build suffix.
func IsRelease(v string) bool {
	return semver.IsValid(v) && semver.Canonical(v) == v && semver.Prerelease(v) == ""
}

// Plain strips the v prefix, turning v0.4.0 into 0.4.0 as VERSION files
// hold it.
func Plain(v string) string {
	return strings.TrimPrefix(v, "v")
}

// Below returns the highest of sorted, which is in ascending order, that is
// lower than v, or "" when none is.
func Below(sorted []string, v string) string {
	for _, s := range slices.Backward(sorted) {
		if semver.Compare(s, v) < 0 {
			return s
		}
	}

	return ""
}
