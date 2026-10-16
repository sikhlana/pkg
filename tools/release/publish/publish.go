// Package publish creates the GitHub release, and with it the tag, for every
// library module version that has none yet, and announces each new version
// to the Go module proxy.
package publish

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"golang.org/x/mod/semver"

	"github.com/sikhlana/pkg/tools/release/git"
	"github.com/sikhlana/pkg/tools/release/report"
	"github.com/sikhlana/pkg/tools/release/version"
	"github.com/sikhlana/pkg/tools/release/workspace"
)

// ErrPathMajor reports a VERSION whose major the module path doesn't take,
// which the go command would never resolve.
var ErrPathMajor = errors.New("VERSION's major doesn't match the module path")

//go:generate mockgen -typed -destination=mock_releases_test.go -package=publish_test . Releases

// Releases looks up and creates GitHub releases.
type Releases interface {
	// Exists reports whether a release exists for tag.
	Exists(ctx context.Context, tag string) (bool, error)
	// Create creates a release, and its tag when the tag doesn't exist.
	Create(ctx context.Context, release Release) error
}

// Release is a GitHub release to create. Its title is its tag.
type Release struct {
	// Tag is the release's tag, such as logger/v0.4.0.
	Tag string
	// Target is the commit to create the tag on. It's empty when the tag
	// already exists, which leaves the tag where it is.
	Target string
	// Notes is the release body.
	Notes string
}

// Status is what Run did for a module.
type Status string

// Statuses Run reports.
const (
	// StatusReleased means the module's version already had a release.
	StatusReleased Status = "already released"
	// StatusCreated means Run created the release.
	StatusCreated Status = "created"
	// StatusFailed means Run couldn't create the release; Outcome.Err says
	// why.
	StatusFailed Status = "failed"
)

// Outcome is what Run did for one library module.
type Outcome struct {
	// Dir is the module's directory.
	Dir string
	// Tag is the tag for the module's VERSION, empty when VERSION is
	// invalid.
	Tag string
	// Status is what happened.
	Status Status
	// Err is why the release failed, for StatusFailed.
	Err error
	// Skipped lists versions, such as v0.4.0, that VERSION held after the
	// previous release but that were never released, because a publish
	// failed and a later merge bumped VERSION again, for StatusCreated.
	// The new release includes their changes.
	Skipped []string
	// ProxyErr is why the module proxy announcement failed, for
	// StatusCreated. It doesn't fail the release.
	ProxyErr error
}

// Options configures Run.
type Options struct {
	// Repo is the repository, checked out at SHA with every tag fetched.
	Repo git.Repo
	// Workspace is the workspace at SHA, read from a clean working tree.
	Workspace workspace.Workspace
	// SHA is the checked-out commit new tags point at.
	SHA string
	// Releases looks up and creates releases.
	Releases Releases
	// Proxy announces new versions.
	Proxy Proxy
}

// Run creates a release, tagged as workspace.Module.Tag names it, for every
// library module whose VERSION has no release yet, and leaves the others
// untouched, so it's safe to re-run. A new tag points at opts.SHA; an existing tag without a release
// keeps its commit. One module failing doesn't stop the others; the returned
// error joins every failure.
func Run(ctx context.Context, opts Options) ([]Outcome, error) {
	outcomes := make([]Outcome, 0, len(opts.Workspace.Modules))

	var errs []error

	for _, m := range opts.Workspace.Modules {
		outcome := publishModule(ctx, opts, m)

		if outcome.Err != nil {
			errs = append(errs, fmt.Errorf("publishing %s: %w", m.Dir, outcome.Err))
		}

		outcomes = append(outcomes, outcome)
	}

	return outcomes, errors.Join(errs...)
}

func publishModule(ctx context.Context, opts Options, m workspace.Module) Outcome {
	outcome := Outcome{Dir: m.Dir, Status: StatusFailed}

	v, err := version.Parse(m.Version)
	if err != nil {
		outcome.Err = fmt.Errorf("reading %s: %w", m.File(workspace.VersionFile), err)
		return outcome
	}

	if !m.Owns(v) {
		outcome.Err = fmt.Errorf("%w: %s is %s, but %s takes only %s versions",
			ErrPathMajor, m.File(workspace.VersionFile), version.Plain(v), m.Path, pathTakes(m))

		return outcome
	}

	outcome.Tag = m.Tag(v)

	released, err := opts.Releases.Exists(ctx, outcome.Tag)
	if err != nil {
		outcome.Err = err
		return outcome
	}

	if released {
		outcome.Status = StatusReleased
		return outcome
	}

	release, skipped, err := newRelease(ctx, opts, m, v)
	if err != nil {
		outcome.Err = err
		return outcome
	}

	if err = opts.Releases.Create(ctx, release); err != nil {
		outcome.Err = err
		return outcome
	}

	outcome.Status = StatusCreated
	outcome.Skipped = skipped
	outcome.ProxyErr = opts.Proxy.Announce(ctx, m.Path, v)

	return outcome
}

// newRelease builds m's release for v, and lists the versions skipped since
// m's previous release.
func newRelease(ctx context.Context, opts Options, m workspace.Module, v string) (Release, []string, error) {
	tag := m.Tag(v)

	tagged, err := opts.Repo.TagExists(ctx, tag)
	if err != nil {
		return Release{}, nil, err
	}

	release := Release{Tag: tag, Target: opts.SHA}
	end := opts.SHA

	if tagged {
		release.Target, end = "", tag
	}

	versions, err := opts.Repo.Versions(ctx, m.TagPrefix())
	if err != nil {
		return Release{}, nil, err
	}

	from, previous := "", version.Below(m.Releases(versions), v)

	if previous != "" {
		from = m.Tag(previous)
	}

	if release.Notes, err = notes(ctx, opts, m, from, end); err != nil {
		return Release{}, nil, err
	}

	skipped, err := skippedVersions(ctx, opts, m, v, previous, from, end)
	if err != nil {
		return Release{}, nil, err
	}

	return release, skipped, nil
}

// skippedVersions returns, in ascending order, the versions m's VERSION held
// in commits from from to end that sit between previous (exclusive) and v
// and have no tag.
func skippedVersions(
	ctx context.Context,
	opts Options,
	m workspace.Module,
	v, previous, from, end string,
) ([]string, error) {
	file := m.File(workspace.VersionFile)

	commits, err := opts.Repo.Commits(ctx, from, end, file)
	if err != nil {
		return nil, err
	}

	var skipped []string

	for _, commit := range commits {
		var (
			held   string
			tagged bool
		)

		if held, err = heldVersion(ctx, opts.Repo, commit, file); err != nil {
			return nil, err
		}

		if held == "" || !m.Owns(held) || slices.Contains(skipped, held) ||
			semver.Compare(held, v) >= 0 || (previous != "" && semver.Compare(held, previous) <= 0) {
			continue
		}

		if tagged, err = opts.Repo.TagExists(ctx, m.Tag(held)); err != nil {
			return nil, err
		}

		if !tagged {
			skipped = append(skipped, held)
		}
	}

	semver.Sort(skipped)

	return skipped, nil
}

// heldVersion returns the version file held at commit, or "" when it was
// missing or invalid there.
func heldVersion(ctx context.Context, repo git.Repo, commit, file string) (string, error) {
	data, found, err := repo.FileAt(ctx, commit, file)

	if err != nil || !found {
		return "", err
	}

	held, err := version.Parse(string(data))
	if err != nil {
		return "", nil //nolint:nilerr // An invalid VERSION in history was never released either.
	}

	return held, nil
}

// notes lists the subjects of the commits from from to end that touch m's
// files, excluding nested modules; every such commit when from is empty, for
// a first release.
func notes(ctx context.Context, opts Options, m workspace.Module, from, end string) (string, error) {
	pathspecs := []string{m.Dir}

	for _, nested := range opts.Workspace.Nested(m) {
		pathspecs = append(pathspecs, ":(exclude)"+nested)
	}

	subjects, err := opts.Repo.Subjects(ctx, from, end, pathspecs...)
	if err != nil {
		return "", err
	}

	var b strings.Builder

	for _, subject := range subjects {
		fmt.Fprintf(&b, "- %s\n", subject)
	}

	return b.String(), nil
}

// Markdown renders outcomes for the job summary.
func Markdown(outcomes []Outcome) string {
	var b strings.Builder

	b.WriteString("## Publish\n\n")

	if len(outcomes) == 0 {
		b.WriteString("No library modules.\n")
		return b.String()
	}

	b.WriteString("| Module | Tag | Result |\n")
	b.WriteString("|--------|-----|--------|\n")

	for _, o := range outcomes {
		result := string(o.Status)

		if o.Err != nil {
			result += ": " + o.Err.Error()
		}

		if len(o.Skipped) > 0 {
			result += "; never released: " + strings.Join(o.Skipped, ", ")
		}

		if o.ProxyErr != nil {
			result += "; module proxy not updated: " + o.ProxyErr.Error()
		}

		fmt.Fprintf(&b, "| %s | %s | %s |\n", o.Dir, o.Tag, report.Cell(result))
	}

	return b.String()
}

// SkippedWarning explains o's skipped versions, or returns "" when there are
// none.
func (o Outcome) SkippedWarning() string {
	if len(o.Skipped) == 0 {
		return ""
	}

	return fmt.Sprintf(
		"%s: VERSION held %s after its previous release, but they were never released; %s includes their changes",
		o.Dir,
		strings.Join(o.Skipped, ", "),
		o.Tag,
	)
}

func pathTakes(m workspace.Module) string {
	if major := m.PathMajor(); major != "" {
		return strings.TrimPrefix(major, "/")
	}

	return "v0 and v1"
}
