// Package plan decides which library modules a pull request changes.
package plan

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/sikhlana/pkg/tools/release/git"
	"github.com/sikhlana/pkg/tools/release/workspace"
)

// rootConfig lists the root files that shape how every module is linted and
// tested.
var rootConfig = []string{workspace.GoVersionFile, ".golangci.yml", workspace.GoWorkFile}

// Change is what a pull request changes between the merge base of its base
// branch and HEAD.
type Change struct {
	// Files are the files added, deleted or modified.
	Files []string
	// BaseDirs are the library module directories at the merge base.
	BaseDirs []string

	base map[string]bool
	head map[string]bool
}

// Diff works out the Change between the merge base of base and HEAD.
func Diff(ctx context.Context, repo git.Repo, base string) (Change, error) {
	mergeBase, err := repo.MergeBase(ctx, base, "HEAD")
	if err != nil {
		return Change{}, err
	}

	files, err := repo.ChangedFiles(ctx, mergeBase, "HEAD")
	if err != nil {
		return Change{}, err
	}

	baseFiles, err := repo.Files(ctx, mergeBase)
	if err != nil {
		return Change{}, err
	}

	headFiles, err := repo.Files(ctx, "HEAD")
	if err != nil {
		return Change{}, err
	}

	c := Change{Files: files, base: set(baseFiles), head: set(headFiles)}

	if c.BaseDirs, err = baseDirs(ctx, repo, mergeBase, c.base); err != nil {
		return Change{}, err
	}

	return c, nil
}

// Changed returns the library modules whose contents the pull request
// changes, sorted by directory. Version rules apply to these.
//
// A file counts for the module that owns it in each layout it exists in:
// an added file for its owner at HEAD, a deleted one for its owner at the
// merge base, a modified one for both. A file that didn't change but now
// belongs to another module, because a go.mod was added or removed below a
// module's root, counts for both owners. Only modules that exist at HEAD are
// returned.
func Changed(ws workspace.Workspace, c Change) []workspace.Module {
	headDirs := ws.Dirs()
	marked := map[string]bool{}

	mark := func(dirs []string, file string) {
		if dir, ok := workspace.OwnerDir(dirs, file); ok {
			marked[dir] = true
		}
	}

	for _, file := range c.Files {
		if c.head[file] {
			mark(headDirs, file)
		}

		if c.base[file] {
			mark(c.BaseDirs, file)
		}
	}

	if !slices.Equal(headDirs, c.BaseDirs) {
		for file := range c.head {
			if !c.base[file] {
				continue
			}

			headOwner, _ := workspace.OwnerDir(headDirs, file)
			baseOwner, _ := workspace.OwnerDir(c.BaseDirs, file)

			if headOwner != baseOwner {
				mark(headDirs, file)
				mark(c.BaseDirs, file)
			}
		}
	}

	return slices.DeleteFunc(slices.Clone(ws.Modules), func(m workspace.Module) bool {
		return !marked[m.Dir]
	})
}

// Select returns the library modules to lint and test: every module when
// the change includes root configuration (.go-version, .golangci.yml or
// go.work), otherwise the Changed ones.
func Select(ws workspace.Workspace, c Change) []workspace.Module {
	for _, file := range c.Files {
		if slices.Contains(rootConfig, file) {
			return ws.Modules
		}
	}

	return Changed(ws, c)
}

// Matrix encodes modules' directories as a JSON array for a GitHub Actions
// matrix.
func Matrix(modules []workspace.Module) (string, error) {
	dirs := make([]string, 0, len(modules))

	for _, m := range modules {
		dirs = append(dirs, m.Dir)
	}

	data, err := json.Marshal(dirs)
	if err != nil {
		return "", fmt.Errorf("encoding matrix: %w", err)
	}

	return string(data), nil
}

// baseDirs returns the library module directories at ref: go.work's entries
// outside the tools directory that have a go.mod in files.
func baseDirs(ctx context.Context, repo git.Repo, ref string, files map[string]bool) ([]string, error) {
	data, found, err := repo.FileAt(ctx, ref, workspace.GoWorkFile)

	if err != nil || !found {
		return nil, err
	}

	uses, err := workspace.ParseUses(data)
	if err != nil {
		return nil, fmt.Errorf("reading %s at %s: %w", workspace.GoWorkFile, ref, err)
	}

	return slices.DeleteFunc(uses, func(dir string) bool {
		return !files[dir+"/"+workspace.GoModFile]
	}), nil
}

func set(items []string) map[string]bool {
	m := make(map[string]bool, len(items))

	for _, item := range items {
		m[item] = true
	}

	return m
}
