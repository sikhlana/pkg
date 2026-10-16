// Package check verifies a pull request against the release rules: each
// changed library module's VERSION, its tag and the minimum its API change
// allows, plus the repository invariants.
package check

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"

	"github.com/sikhlana/pkg/tools/release/git"
	"github.com/sikhlana/pkg/tools/release/gorelease"
	"github.com/sikhlana/pkg/tools/release/plan"
	"github.com/sikhlana/pkg/tools/release/report"
	"github.com/sikhlana/pkg/tools/release/version"
	"github.com/sikhlana/pkg/tools/release/workspace"
)

// Options configures Run.
type Options struct {
	// Repo is the repository, checked out at the pull request head.
	Repo git.Repo
	// Workspace is the workspace at the pull request head.
	Workspace workspace.Workspace
	// Base is the pull request's base branch, such as origin/main.
	Base string
	// Gorelease returns the gorelease runner. Run calls it at most once,
	// when the first module needs an API comparison. Nil skips the
	// comparison.
	Gorelease func(context.Context) (gorelease.Runner, error)
}

// Module is the outcome for one changed library module.
type Module struct {
	// Dir is the module's directory.
	Dir string
	// New reports that the module doesn't exist on the base branch and has
	// never been released.
	New bool
	// Restored reports that the module doesn't exist on the base branch but
	// was released before it was removed; BaseVersion is its last release.
	Restored bool
	// BaseVersion is the VERSION on the base branch, or the last release
	// for a restored module. It's empty when new or missing.
	BaseVersion string
	// Version is the VERSION at the pull request head, empty when missing.
	Version string
	// APIBase is the version gorelease compared against, or
	// gorelease.BaseNone. It's empty when gorelease didn't run.
	APIBase string
	// APIReport is gorelease's output.
	APIReport string
}

// Report is the outcome of a check.
type Report struct {
	// Base is the base branch checked against.
	Base string
	// Modules are the changed library modules, sorted by directory.
	Modules []Module
	// Problems are every broken rule, repository-wide ones first.
	Problems []report.Problem
}

// Run checks the pull request whose head opts.Repo has checked out against
// opts.Base. A broken rule is a Problem in the Report, not an error; an
// error means the check couldn't run.
func Run(ctx context.Context, opts Options) (Report, error) {
	change, err := plan.Diff(ctx, opts.Repo, opts.Base)
	if err != nil {
		return Report{}, err
	}

	c := checker{opts: opts}
	r := Report{Base: opts.Base, Problems: opts.Workspace.Check()}

	for _, m := range plan.Changed(opts.Workspace, change) {
		if err = c.add(ctx, &r, m); err != nil {
			return Report{}, fmt.Errorf("checking %s: %w", m.Dir, err)
		}
	}

	return r, nil
}

// Failed reports whether any rule is broken.
func (r Report) Failed() bool {
	return len(r.Problems) > 0
}

// Markdown renders the report for the job summary: a row per changed module
// with its base version, new version and outcome, the problems not tied to
// a changed module, and each module's gorelease report.
func (r Report) Markdown() string {
	var b strings.Builder

	fmt.Fprintf(&b, "## Release check against %s\n\n", r.Base)

	if len(r.Modules) == 0 {
		b.WriteString("No library module changed.\n")
	} else {
		b.WriteString("| Module | Base | Version | Result |\n")
		b.WriteString("|--------|------|---------|--------|\n")

		for _, m := range r.Modules {
			fmt.Fprintf(
				&b,
				"| %s | %s | %s | %s |\n",
				m.Dir,
				m.base(),
				orNone(m.Version),
				report.Cell(r.outcome(m.Dir)),
			)
		}
	}

	if other := r.unattached(); len(other) > 0 {
		b.WriteString("\n### Repository\n\n")

		for _, p := range other {
			fmt.Fprintf(&b, "- %s\n", p.Message)
		}
	}

	for _, m := range r.Modules {
		if m.APIReport == "" {
			continue
		}

		fmt.Fprintf(&b, "\n<details><summary>%s: API comparison against %s</summary>\n\n", m.Dir, m.APIBase)
		fmt.Fprintf(&b, "```text\n%s\n```\n\n</details>\n", m.APIReport)
	}

	return b.String()
}

func (r Report) outcome(dir string) string {
	var reasons []string

	for _, p := range r.Problems {
		if p.Module == dir {
			reasons = append(reasons, p.Message)
		}
	}

	if len(reasons) == 0 {
		return "pass"
	}

	return "fail: " + strings.Join(reasons, "; ")
}

func (r Report) unattached() []report.Problem {
	var problems []report.Problem

	for _, p := range r.Problems {
		if !r.listed(p.Module) {
			problems = append(problems, p)
		}
	}

	return problems
}

func (r Report) listed(dir string) bool {
	for _, m := range r.Modules {
		if dir != "" && m.Dir == dir {
			return true
		}
	}

	return false
}

func (m Module) base() string {
	switch {
	case m.New:
		return "new"
	case m.Restored:
		return m.BaseVersion + " (last release)"
	default:
		return orNone(m.BaseVersion)
	}
}

type checker struct {
	opts   Options
	runner *gorelease.Runner
}

// add checks m and adds its outcome and problems to r.
func (c *checker) add(ctx context.Context, r *Report, m workspace.Module) error {
	result, problems, err := c.module(ctx, m)
	if err != nil {
		return err
	}

	r.Modules = append(r.Modules, result)
	r.Problems = append(r.Problems, problems...)

	return nil
}

func (c *checker) module(ctx context.Context, m workspace.Module) (Module, []report.Problem, error) {
	result := Module{Dir: m.Dir, Version: strings.TrimSpace(m.Version)}

	released, err := c.lastRelease(ctx, m)
	if err != nil {
		return Module{}, nil, err
	}

	baseVersion, exists, err := c.baseVersion(ctx, m)
	if err != nil {
		return Module{}, nil, err
	}

	switch {
	case exists:
		result.BaseVersion = baseVersion
	case released != "":
		result.Restored, result.BaseVersion = true, version.Plain(released)
	default:
		result.New = true
	}

	if !m.HasVersion {
		// workspace.Check reports the missing file.
		return result, nil, nil
	}

	v, err := version.Parse(m.Version)
	if err != nil {
		return result, []report.Problem{
			problem(m, workspace.VersionFile, fmt.Sprintf("%s: %v", m.File(workspace.VersionFile), err)),
		}, nil
	}

	// Once VERSION itself is wrong, its tag and API minimum add nothing.
	if problems := c.versionRules(m, v, result); len(problems) > 0 {
		return result, problems, nil
	}

	tag := m.Tag(v)

	taken, err := c.opts.Repo.TagExists(ctx, tag)
	if err != nil {
		return Module{}, nil, err
	}

	if taken {
		return result, []report.Problem{
			problem(m, workspace.VersionFile, fmt.Sprintf("the tag %s already exists", tag)),
		}, nil
	}

	problems, err := c.apiMinimum(ctx, m, v, released, &result)
	if err != nil {
		return Module{}, nil, err
	}

	return result, problems, nil
}

// lastRelease returns m's highest released version, such as v0.3.0: the
// highest tag under its prefix whose major matches its module path. It's
// empty when m has none.
func (c *checker) lastRelease(ctx context.Context, m workspace.Module) (string, error) {
	versions, err := c.opts.Repo.Versions(ctx, m.TagPrefix())
	if err != nil {
		return "", err
	}

	releases := m.Releases(versions)

	if len(releases) == 0 {
		return "", nil
	}

	return releases[len(releases)-1], nil
}

// baseVersion reads m's VERSION on the base branch. It reports whether m
// exists there at all; the version is empty when the file is missing or
// invalid, so the bump can't be compared.
func (c *checker) baseVersion(ctx context.Context, m workspace.Module) (string, bool, error) {
	_, exists, err := c.opts.Repo.FileAt(ctx, c.opts.Base, m.File(workspace.GoModFile))

	if err != nil || !exists {
		return "", false, err
	}

	data, found, err := c.opts.Repo.FileAt(ctx, c.opts.Base, m.File(workspace.VersionFile))

	if err != nil || !found {
		return "", true, err
	}

	return strings.TrimSpace(string(data)), true, nil
}

func (c *checker) versionRules(m workspace.Module, v string, result Module) []report.Problem {
	if !m.Owns(v) {
		return []report.Problem{problem(m, workspace.VersionFile, majorMismatch(m, v))}
	}

	switch {
	case result.New:
		if first := m.FirstVersion(); v != first {
			return []report.Problem{problem(m, workspace.VersionFile, fmt.Sprintf(
				"%s is new, and a new module starts at %s, not %s", m.Dir, version.Plain(first), version.Plain(v)))}
		}

		return nil
	case result.Restored:
		if semver.Compare(v, "v"+result.BaseVersion) <= 0 {
			return []report.Problem{problem(m, workspace.VersionFile, fmt.Sprintf(
				"%s was released up to %s before it was removed, so VERSION must be newer, not %s",
				m.Dir, result.BaseVersion, version.Plain(v)))}
		}

		return nil
	}

	baseV, err := version.Parse(result.BaseVersion)
	if err != nil {
		return nil
	}

	switch semver.Compare(v, baseV) {
	case 0:
		return []report.Problem{problem(m, workspace.VersionFile,
			fmt.Sprintf("%s changed without a VERSION bump: still %s, as on %s", m.Dir, version.Plain(v), c.opts.Base))}
	case -1:
		return []report.Problem{
			problem(
				m,
				workspace.VersionFile,
				fmt.Sprintf(
					"%s VERSION %s is not newer than %s on %s",
					m.Dir,
					version.Plain(v),
					result.BaseVersion,
					c.opts.Base,
				),
			),
		}
	default:
		return nil
	}
}

// apiMinimum runs gorelease against released, m's highest release, or
// against nothing when it's empty, and requires v to be at least its
// suggestion.
func (c *checker) apiMinimum(
	ctx context.Context,
	m workspace.Module,
	v, released string,
	result *Module,
) ([]report.Problem, error) {
	if c.opts.Gorelease == nil {
		return nil, nil
	}

	base, since := gorelease.BaseNone, ""

	if released != "" {
		base, since = released, " since "+released
	}

	runner, err := c.gorelease(ctx)
	if err != nil {
		return nil, err
	}

	out, err := runner.Run(ctx, filepath.Join(c.opts.Repo.Root, filepath.FromSlash(m.Dir)), base)
	if err != nil {
		return nil, err
	}

	result.APIBase, result.APIReport = base, out.Output

	var p report.Problem

	switch {
	case out.Incompatible && released != "" && semver.Major(released) != "v0":
		p = problem(m, workspace.GoModFile, fmt.Sprintf(
			"%s has incompatible API changes%s, which need a new major module path (%s)",
			m.Dir,
			since,
			nextMajorDir(m.Dir, released),
		))
	case out.Failed:
		p = problem(m, workspace.GoModFile, fmt.Sprintf("the API comparison against %s failed", base))
		p.Detail = out.Output
	case out.Suggested == "":
		p = problem(m, workspace.GoModFile, fmt.Sprintf("the API comparison against %s suggested no version", base))
		p.Detail = out.Output
	case semver.Compare(v, out.Suggested) < 0:
		p = problem(m, workspace.VersionFile, fmt.Sprintf("%s's API changes%s need at least %s, but VERSION is %s",
			m.Dir, since, version.Plain(out.Suggested), version.Plain(v)))
	default:
		return nil, nil
	}

	return []report.Problem{p}, nil
}

func (c *checker) gorelease(ctx context.Context) (gorelease.Runner, error) {
	if c.runner == nil {
		runner, err := c.opts.Gorelease(ctx)
		if err != nil {
			return gorelease.Runner{}, err
		}

		c.runner = &runner
	}

	return *c.runner, nil
}

func problem(m workspace.Module, file, message string) report.Problem {
	return report.Problem{File: m.File(file), Module: m.Dir, Message: message}
}

// majorMismatch explains a VERSION whose major the module path doesn't take,
// and where a v2 or later belongs instead.
func majorMismatch(m workspace.Module, v string) string {
	takes := "v0 and v1"

	if major := module.PathMajorPrefix(m.PathMajor()); major != "" {
		takes = major
	}

	message := fmt.Sprintf("%s VERSION %s is a %s, but module path %s only takes %s versions",
		m.Dir, version.Plain(v), semver.Major(v), m.Path, takes)

	if major := semver.Major(v); major != "v0" && major != "v1" {
		message += fmt.Sprintf("; %s lives in %s", major, path.Join(m.TagPrefix(), major))
	}

	return message
}

// nextMajorDir returns the directory a module moves to for the major after
// released: logger at v1.2.0 moves to logger/v2, and logger/v2 at v2.1.0 to
// logger/v3.
func nextMajorDir(dir, released string) string {
	major, err := strconv.Atoi(strings.TrimPrefix(semver.Major(released), "v"))
	if err != nil {
		return dir + "/vN"
	}

	prefix, _, ok := module.SplitPathVersion(dir)

	if !ok {
		prefix = dir
	}

	return fmt.Sprintf("%s/v%d", prefix, major+1)
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}

	return s
}
