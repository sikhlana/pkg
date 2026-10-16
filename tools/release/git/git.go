// Package git runs the git commands the release tool needs.
package git

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"golang.org/x/mod/semver"

	"github.com/sikhlana/pkg/tools/release/version"
)

// Repo runs git in a repository's top-level directory.
type Repo struct {
	// Root is the repository's top-level directory.
	Root string
}

// Open returns the repository containing dir, whose root git rev-parse
// --show-toplevel reports.
func Open(ctx context.Context, dir string) (Repo, error) {
	out, err := Repo{Root: dir}.run(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		return Repo{}, fmt.Errorf("finding repository root: %w", err)
	}

	return Repo{Root: strings.TrimSpace(out)}, nil
}

// Commit resolves ref to a full commit SHA.
func (r Repo) Commit(ctx context.Context, ref string) (string, error) {
	out, err := r.run(ctx, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("resolving %s: %w", ref, err)
	}

	return strings.TrimSpace(out), nil
}

// MergeBase returns the best common ancestor of a and b.
func (r Repo) MergeBase(ctx context.Context, a, b string) (string, error) {
	out, err := r.run(ctx, "merge-base", "--end-of-options", a, b)
	if err != nil {
		return "", fmt.Errorf("finding merge base of %s and %s: %w", a, b, err)
	}

	return strings.TrimSpace(out), nil
}

// ChangedFiles lists the files that differ between from and to, as
// slash-separated paths relative to the root. A rename lists both the old
// and the new path.
func (r Repo) ChangedFiles(ctx context.Context, from, to string) ([]string, error) {
	out, err := r.run(ctx, "diff", "--name-only", "--no-renames", "-z", "--end-of-options", from, to, "--")
	if err != nil {
		return nil, fmt.Errorf("listing files changed between %s and %s: %w", from, to, err)
	}

	return splitNonEmpty(out, "\x00"), nil
}

// FileAt returns the contents of path at ref, and false when ref has no such
// file.
func (r Repo) FileAt(ctx context.Context, ref, path string) ([]byte, bool, error) {
	out, err := r.run(ctx, "ls-tree", "--full-tree", "--name-only", "-z", "--end-of-options", ref, "--", path)
	if err != nil {
		return nil, false, fmt.Errorf("looking up %s at %s: %w", path, ref, err)
	}

	if out == "" {
		return nil, false, nil
	}

	content, err := r.run(ctx, "cat-file", "blob", ref+":"+path)
	if err != nil {
		return nil, false, fmt.Errorf("reading %s at %s: %w", path, ref, err)
	}

	return []byte(content), true, nil
}

// Versions returns the release versions tagged under prefix, a module's tag
// prefix such as http/server ("" for the root), in ascending order. A tag
// prefix/vMAJOR.MINOR.PATCH yields vMAJOR.MINOR.PATCH; tags with a
// pre-release or build suffix, or for a nested module, are ignored. Every
// major is returned: modules sharing a prefix, such as logger and logger/v2,
// tell theirs apart by the module path.
func (r Repo) Versions(ctx context.Context, prefix string) ([]string, error) {
	if prefix != "" {
		prefix += "/"
	}

	out, err := r.run(ctx, "tag", "--list", prefix+"v*")
	if err != nil {
		return nil, fmt.Errorf("listing tags under %q: %w", prefix, err)
	}

	var versions []string

	for _, tag := range splitNonEmpty(out, "\n") {
		if v := strings.TrimPrefix(tag, prefix); version.IsRelease(v) {
			versions = append(versions, v)
		}
	}

	semver.Sort(versions)

	return versions, nil
}

// Files lists every file in ref's tree, as slash-separated paths relative
// to the root.
func (r Repo) Files(ctx context.Context, ref string) ([]string, error) {
	out, err := r.run(ctx, "ls-tree", "-r", "--full-tree", "--name-only", "-z", "--end-of-options", ref)
	if err != nil {
		return nil, fmt.Errorf("listing files at %s: %w", ref, err)
	}

	return splitNonEmpty(out, "\x00"), nil
}

// Clean reports whether the working tree and index match HEAD for every
// tracked file. Untracked files don't count.
func (r Repo) Clean(ctx context.Context) (bool, error) {
	out, err := r.run(ctx, "status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return false, fmt.Errorf("checking the working tree: %w", err)
	}

	return out == "", nil
}

// TagExists reports whether the tag exists.
func (r Repo) TagExists(ctx context.Context, tag string) (bool, error) {
	out, err := r.run(ctx, "tag", "--list", tag)
	if err != nil {
		return false, fmt.Errorf("looking up tag %s: %w", tag, err)
	}

	return strings.TrimSpace(out) == tag, nil
}

// Subjects returns the subjects of the commits reachable from to but not
// from from that touch pathspecs, newest first. An empty from means every
// commit reachable from to.
func (r Repo) Subjects(ctx context.Context, from, to string, pathspecs ...string) ([]string, error) {
	return r.log(ctx, "%s", from, to, pathspecs)
}

// Commits returns the SHAs of the commits reachable from to but not from
// from that touch pathspecs, newest first. An empty from means every commit
// reachable from to.
func (r Repo) Commits(ctx context.Context, from, to string, pathspecs ...string) ([]string, error) {
	return r.log(ctx, "%H", from, to, pathspecs)
}

func (r Repo) log(ctx context.Context, format, from, to string, pathspecs []string) ([]string, error) {
	revs := to

	if from != "" {
		revs = from + ".." + to
	}

	args := append([]string{"log", "--format=" + format, "--end-of-options", revs, "--"}, pathspecs...)

	out, err := r.run(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("listing commits in %s: %w", revs, err)
	}

	return splitNonEmpty(out, "\n"), nil
}

func (r Repo) run(ctx context.Context, args ...string) (string, error) {
	var stdout, stderr bytes.Buffer

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = r.Root
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("running git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}

	return stdout.String(), nil
}

func splitNonEmpty(s, sep string) []string {
	var parts []string

	for part := range strings.SplitSeq(s, sep) {
		if part != "" {
			parts = append(parts, part)
		}
	}

	return parts
}
