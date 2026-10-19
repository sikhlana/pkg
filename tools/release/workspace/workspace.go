// Package workspace loads the repository's library modules from go.work and
// checks the invariants every pull request must keep.
package workspace

import (
	"errors"
	"fmt"
	goversion "go/version"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"

	"github.com/sikhlana/pkg/tools/release/report"
)

// ToolsDir holds repository tooling. Nothing under it is a library module,
// even with a go.mod.
const ToolsDir = "tools"

// ToolModuleDir is the release tool's own module. Its module path, minus
// this directory, is the repository's path.
const ToolModuleDir = ToolsDir + "/release"

// ErrToolModule reports a release tool go.mod whose module path doesn't end
// in ToolModuleDir, so the repository's path can't be derived from it.
var ErrToolModule = errors.New("unexpected release tool module path")

// Root files the workspace reads.
const (
	GoVersionFile = ".go-version"
	GoWorkFile    = "go.work"
)

// Files every library module carries.
const (
	GoModFile   = "go.mod"
	VersionFile = "VERSION"
	ReadmeFile  = "README.md"
)

// Module is a library module: a go.work entry outside ToolsDir with a
// go.mod.
type Module struct {
	// Dir is the module's slash-separated directory relative to the
	// repository root, such as http/server.
	Dir string
	// Path is the module path declared in go.mod.
	Path string
	// GoVersion is go.mod's go directive.
	GoVersion string
	// Version is the VERSION file's contents, unvalidated.
	Version string
	// HasVersion reports whether the module has a VERSION file.
	HasVersion bool
	// HasReadme reports whether the module has a README.md.
	HasReadme bool
}

// Tag returns the module's tag for v, such as http/server/v0.2.0. v may
// carry a v prefix or not. A module in a major version directory drops it,
// as the go command expects: logger/v2 at 2.1.0 is tagged logger/v2.1.0.
func (m Module) Tag(v string) string {
	v = "v" + strings.TrimPrefix(v, "v")

	if prefix := m.TagPrefix(); prefix != "" {
		return prefix + "/" + v
	}

	return v
}

// TagPrefix returns the directory the module's tags start with: Dir, minus
// a trailing major version directory matching the module path's /vN.
func (m Module) TagPrefix() string {
	major := m.PathMajor()

	switch {
	case major == "":
		return m.Dir
	case m.Dir == strings.TrimPrefix(major, "/"):
		return ""
	}

	if prefix, ok := strings.CutSuffix(m.Dir, major); ok {
		return prefix
	}

	return m.Dir
}

// PathMajor returns the module path's major version suffix, such as /v2,
// or "" for a v0 or v1 module path or an invalid one.
func (m Module) PathMajor() string {
	_, major, ok := module.SplitPathVersion(m.Path)

	if !ok {
		return ""
	}

	return major
}

// Owns reports whether v, such as v2.1.0, is a version of this module: its
// major must match the module path, so logger owns v0 and v1 and logger/v2
// owns v2, though both share the tag prefix logger.
func (m Module) Owns(v string) bool {
	return module.CheckPathMajor(v, m.PathMajor()) == nil
}

// Releases returns the versions the module owns, keeping their order.
func (m Module) Releases(versions []string) []string {
	var owned []string

	for _, v := range versions {
		if m.Owns(v) {
			owned = append(owned, v)
		}
	}

	return owned
}

// FirstVersion returns the version a new module starts at: v0.1.0, or
// vN.0.0 for a module path ending in /vN.
func (m Module) FirstVersion() string {
	if major := module.PathMajorPrefix(m.PathMajor()); major != "" {
		return major + ".0.0"
	}

	return "v0.1.0"
}

// File returns the slash-separated path, relative to the repository root, of
// the named file in the module.
func (m Module) File(name string) string {
	return path.Join(m.Dir, name)
}

// Workspace is the repository's module layout.
type Workspace struct {
	// Root is the repository root.
	Root string
	// RepoPath is the repository's module path prefix, such as
	// github.com/sikhlana/pkg, taken from the release tool's go.mod.
	RepoPath string
	// GoVersion is the contents of .go-version.
	GoVersion string
	// WorkGoVersion is go.work's go directive.
	WorkGoVersion string
	// Uses are go.work's use entries as slash-separated directories relative
	// to the root.
	Uses []string
	// GoModDirs are the directories outside ToolsDir that contain a go.mod,
	// slash-separated and relative to the root.
	GoModDirs []string
	// Modules are the library modules, sorted by directory.
	Modules []Module
}

// Load reads the workspace rooted at root. A go.work entry without a go.mod
// is kept in Uses but isn't a Module; Check reports it.
func Load(root string) (Workspace, error) {
	goVersion, err := os.ReadFile(filepath.Join(root, GoVersionFile))
	if err != nil {
		return Workspace{}, fmt.Errorf("reading %s: %w", GoVersionFile, err)
	}

	repoPath, err := readRepoPath(root)
	if err != nil {
		return Workspace{}, err
	}

	workData, err := os.ReadFile(filepath.Join(root, GoWorkFile))
	if err != nil {
		return Workspace{}, fmt.Errorf("reading %s: %w", GoWorkFile, err)
	}

	work, err := parseWork(workData)
	if err != nil {
		return Workspace{}, err
	}

	ws := Workspace{
		Root:      root,
		RepoPath:  repoPath,
		GoVersion: strings.TrimSpace(string(goVersion)),
		Uses:      uses(work),
	}

	if work.Go != nil {
		ws.WorkGoVersion = work.Go.Version
	}

	if ws.GoModDirs, err = findGoModDirs(root); err != nil {
		return Workspace{}, err
	}

	if ws.Modules, err = loadModules(root, ws.Uses, ws.GoModDirs); err != nil {
		return Workspace{}, err
	}

	return ws, nil
}

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

// Owner returns the library module that owns file, a slash-separated path
// relative to the root: the module with the deepest directory containing it.
func (w Workspace) Owner(file string) (Module, bool) {
	dir, ok := OwnerDir(w.Dirs(), file)

	if !ok {
		return Module{}, false
	}

	i := slices.IndexFunc(w.Modules, func(m Module) bool { return m.Dir == dir })

	return w.Modules[i], true
}

// Dirs returns the library modules' directories, sorted.
func (w Workspace) Dirs() []string {
	dirs := make([]string, 0, len(w.Modules))

	for _, m := range w.Modules {
		dirs = append(dirs, m.Dir)
	}

	return dirs
}

// OwnerDir returns the deepest of dirs containing file, both slash-separated
// and relative to the root.
func OwnerDir(dirs []string, file string) (string, bool) {
	owner := ""

	for _, dir := range dirs {
		if strings.HasPrefix(file, dir+"/") && len(dir) > len(owner) {
			owner = dir
		}
	}

	return owner, owner != ""
}

// ParseUses parses a go.work file and returns its use entries as library
// module candidates: slash-separated directories relative to the root,
// sorted, outside ToolsDir.
func ParseUses(data []byte) ([]string, error) {
	work, err := parseWork(data)
	if err != nil {
		return nil, err
	}

	return slices.DeleteFunc(uses(work), IsTool), nil
}

// IsTool reports whether dir, slash-separated and relative to the root, is
// under ToolsDir.
func IsTool(dir string) bool {
	return dir == ToolsDir || strings.HasPrefix(dir, ToolsDir+"/")
}

// Nested returns the directories of the library modules inside m's
// directory, whose files belong to them rather than to m.
func (w Workspace) Nested(m Module) []string {
	var dirs []string

	for _, other := range w.Modules {
		if strings.HasPrefix(other.Dir, m.Dir+"/") {
			dirs = append(dirs, other.Dir)
		}
	}

	return dirs
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

func readRepoPath(root string) (string, error) {
	file := path.Join(ToolModuleDir, GoModFile)

	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file)))
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", file, err)
	}

	modPath := modfile.ModulePath(data)

	repoPath, ok := strings.CutSuffix(modPath, "/"+ToolModuleDir)

	if !ok || repoPath == "" {
		return "", fmt.Errorf("%w: %s declares module %q, want <repository path>/%s",
			ErrToolModule, file, modPath, ToolModuleDir)
	}

	return repoPath, nil
}

func parseWork(data []byte) (*modfile.WorkFile, error) {
	work, err := modfile.ParseWork(GoWorkFile, data, nil)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", GoWorkFile, err)
	}

	return work, nil
}

func uses(work *modfile.WorkFile) []string {
	dirs := make([]string, 0, len(work.Use))

	for _, use := range work.Use {
		dirs = append(dirs, path.Clean(filepath.ToSlash(use.Path)))
	}

	slices.Sort(dirs)

	return slices.Compact(dirs)
}

// loadModules loads the go.work entries outside ToolsDir that have a go.mod.
func loadModules(root string, uses, goModDirs []string) ([]Module, error) {
	var modules []Module

	for _, dir := range uses {
		if IsTool(dir) || !slices.Contains(goModDirs, dir) {
			continue
		}

		m, err := loadModule(root, dir)
		if err != nil {
			return nil, err
		}

		modules = append(modules, m)
	}

	return modules, nil
}

func loadModule(root, dir string) (Module, error) {
	m := Module{Dir: dir}
	modDir := filepath.Join(root, filepath.FromSlash(dir))

	data, err := os.ReadFile(filepath.Join(modDir, GoModFile))
	if err != nil {
		return Module{}, fmt.Errorf("reading %s: %w", m.File(GoModFile), err)
	}

	mod, err := modfile.Parse(m.File(GoModFile), data, nil)
	if err != nil {
		return Module{}, fmt.Errorf("parsing %s: %w", m.File(GoModFile), err)
	}

	if mod.Module != nil {
		m.Path = mod.Module.Mod.Path
	}

	if mod.Go != nil {
		m.GoVersion = mod.Go.Version
	}

	versionData, err := os.ReadFile(filepath.Join(modDir, VersionFile))

	switch {
	case err == nil:
		m.Version, m.HasVersion = string(versionData), true
	case !errors.Is(err, fs.ErrNotExist):
		return Module{}, fmt.Errorf("reading %s: %w", m.File(VersionFile), err)
	}

	if m.HasReadme, err = exists(filepath.Join(modDir, ReadmeFile)); err != nil {
		return Module{}, err
	}

	return m, nil
}

// findGoModDirs walks the repository the way the go command does, skipping
// directories named testdata or starting with a dot or underscore, and
// ToolsDir.
func findGoModDirs(root string) ([]string, error) {
	var dirs []string

	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}

		rel = filepath.ToSlash(rel)

		if d.IsDir() {
			name := d.Name()

			if rel != "." && (Skipped(name) || rel == ToolsDir) {
				return filepath.SkipDir
			}

			return nil
		}

		if d.Name() == GoModFile && rel != GoModFile {
			dirs = append(dirs, path.Dir(rel))
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("finding go.mod files: %w", err)
	}

	slices.Sort(dirs)

	return dirs, nil
}

// Skipped reports whether the go command ignores a directory named name when it matches packages: testdata, and any
// name starting with . or _.
func Skipped(name string) bool {
	return name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
}

// ToolDirs returns the tools: the directories directly under ToolsDir that contain a go.mod and that the go command
// doesn't skip, slash-separated, relative to root and sorted. It returns nothing when ToolsDir doesn't exist.
func ToolDirs(root string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(root, ToolsDir))

	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("listing %s: %w", ToolsDir, err)
	}

	var dirs []string

	for _, e := range entries {
		if !e.IsDir() || Skipped(e.Name()) {
			continue
		}

		var ok bool

		if ok, err = exists(filepath.Join(root, ToolsDir, e.Name(), GoModFile)); err != nil {
			return nil, err
		}

		if ok {
			dirs = append(dirs, path.Join(ToolsDir, e.Name()))
		}
	}

	return dirs, nil
}

func exists(p string) (bool, error) {
	_, err := os.Stat(p)

	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	default:
		return false, fmt.Errorf("checking %s: %w", p, err)
	}
}
