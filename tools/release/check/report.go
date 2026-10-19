package check

import (
	"fmt"
	"strings"

	"github.com/sikhlana/pkg/tools/release/report"
)

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

func orNone(s string) string {
	if s == "" {
		return "none"
	}

	return s
}
