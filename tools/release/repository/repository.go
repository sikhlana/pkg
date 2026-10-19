// Package repository parses GitHub repository names.
package repository

import (
	"errors"
	"fmt"
	"strings"
)

// ErrInvalid reports a repository name that isn't owner/name.
var ErrInvalid = errors.New("invalid repository")

// Parse splits a repository name given as owner/name, as in GITHUB_REPOSITORY, failing with ErrInvalid when either
// part is empty or the name has another slash.
func Parse(s string) (string, string, error) {
	owner, name, ok := strings.Cut(s, "/")

	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return "", "", fmt.Errorf("%w: %q, want owner/name", ErrInvalid, s)
	}

	return owner, name, nil
}
