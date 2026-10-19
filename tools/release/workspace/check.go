package workspace

import (
	"fmt"
	goversion "go/version"
	"slices"

	"golang.org/x/mod/module"

	"github.com/sikhlana/pkg/tools/release/report"
)

// Check reports every broken repository invariant: go.work must list
// exactly the go.mod directories outside ToolsDir; every library module needs
// a README.md, a VERSION and the module path RepoPath/Dir; and go.work's and
// every module's go directive must be the Go language version in
// .go-version, at any patch release.
func (w Workspace) Check() []report.Problem {
	var problems []report.Problem

	for _, dir := range w.GoModDirs {
		if !slices.Contains(w.Uses, dir) {
			problems = append(problems, report.Problem{
				File:    GoWorkFile,
				Message: fmt.Sprintf("%s has a go.mod but isn't registered in go.work", dir),
			})
		}
	}

	for _, dir := range w.Uses {
		switch {
		case IsTool(dir):
			problems = append(problems, report.Problem{
				File: GoWorkFile,
				Message: fmt.Sprintf(
					"go.work lists %s, but %s/ holds repo tooling, not library modules",
					dir,
					ToolsDir,
				),
			})
		case !slices.Contains(w.GoModDirs, dir):
			problems = append(problems, report.Problem{
				File:    GoWorkFile,
				Message: fmt.Sprintf("go.work lists %s, which has no go.mod", dir),
			})
		}
	}

	if !sameLanguage(w.WorkGoVersion, w.GoVersion) {
		problems = append(problems, report.Problem{
			File: GoWorkFile,
			Message: fmt.Sprintf(
				"go.work declares go %s, want %s from %s",
				w.WorkGoVersion,
				w.GoVersion,
				GoVersionFile,
			),
		})
	}

	for _, m := range w.Modules {
		problems = append(problems, w.checkModule(m)...)
	}

	return problems
}

func (w Workspace) checkModule(m Module) []report.Problem {
	var problems []report.Problem

	if !m.HasReadme {
		problems = append(problems, report.Problem{
			File:    m.File(ReadmeFile),
			Module:  m.Dir,
			Message: fmt.Sprintf("%s has no %s", m.Dir, ReadmeFile),
		})
	}

	if !m.HasVersion {
		problems = append(problems, report.Problem{
			File:    m.File(VersionFile),
			Module:  m.Dir,
			Message: fmt.Sprintf("%s has no %s file", m.Dir, VersionFile),
		})
	}

	if p, ok := w.checkPath(m); !ok {
		problems = append(problems, p)
	}

	if !sameLanguage(m.GoVersion, w.GoVersion) {
		problems = append(problems, report.Problem{
			File:   m.File(GoModFile),
			Module: m.Dir,
			Message: fmt.Sprintf(
				"%s declares go %s, want %s from %s",
				m.File(GoModFile),
				m.GoVersion,
				w.GoVersion,
				GoVersionFile,
			),
		})
	}

	return problems
}

// checkPath reports a module path the go command wouldn't find this
// module's tags under: anything other than RepoPath/Dir, which also puts a
// new major version in its own directory, logger/v2.
func (w Workspace) checkPath(m Module) (report.Problem, bool) {
	p := report.Problem{File: m.File(GoModFile), Module: m.Dir}

	if err := module.CheckPath(m.Path); err != nil {
		p.Message = fmt.Sprintf("%s declares an invalid module path: %v", m.File(GoModFile), err)
		return p, false
	}

	if want := w.RepoPath + "/" + m.Dir; m.Path != want {
		p.Message = fmt.Sprintf("%s declares module %s, want %s", m.File(GoModFile), m.Path, want)
		return p, false
	}

	return report.Problem{}, true
}

// sameLanguage reports whether go directives a and b name the same Go
// language version, so go 1.27.1 matches 1.27.
func sameLanguage(a, b string) bool {
	lang := goversion.Lang("go" + a)

	return lang != "" && lang == goversion.Lang("go"+b)
}
