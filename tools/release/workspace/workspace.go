// Package workspace loads the repository's library modules from go.work and
// checks the invariants every pull request must keep.
package workspace

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/mod/modfile"
)

// Root files the workspace reads.
const (
	GoVersionFile = ".go-version"
	GoWorkFile    = "go.work"
)

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
