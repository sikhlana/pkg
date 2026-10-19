package workspace

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// ToolsDir holds repository tooling. Nothing under it is a library module,
// even with a go.mod.
const ToolsDir = "tools"

// ToolModuleDir is the release tool's own module. Its module path, minus
// this directory, is the repository's path.
const ToolModuleDir = ToolsDir + "/release"

// IsTool reports whether dir, slash-separated and relative to the root, is
// under ToolsDir.
func IsTool(dir string) bool {
	return dir == ToolsDir || strings.HasPrefix(dir, ToolsDir+"/")
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
