package vulncheck

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sikhlana/pkg/tools/release/parallel"
	"github.com/sikhlana/pkg/tools/release/workspace"
)

// RunFunc runs govulncheck over the module in dir and returns its -format
// json output. An error means the scan didn't complete.
type RunFunc func(ctx context.Context, dir string) ([]byte, error)

// Govulncheck runs a govulncheck binary.
type Govulncheck struct {
	path string
}

// NewGovulncheck finds the govulncheck executable named by path, a name looked up on PATH or a path relative to the
// working directory or absolute, and resolves it to an absolute path once, since every scan runs in another
// directory.
func NewGovulncheck(path string) (Govulncheck, error) {
	bin, err := exec.LookPath(path)
	if err != nil {
		return Govulncheck{}, fmt.Errorf("finding govulncheck: %w", err)
	}

	if bin, err = filepath.Abs(bin); err != nil {
		return Govulncheck{}, fmt.Errorf("resolving govulncheck: %w", err)
	}

	return Govulncheck{path: bin}, nil
}

// Run scans the packages of the module in dir, and the tools its go.mod pins
// with tool directives, which the go command builds from the same
// requirements, with GOWORK=off, so sibling modules resolve as a consumer sees
// them. A module without tool directives gets only a warning for the tool
// pattern. In JSON mode govulncheck exits
// zero whatever it finds, so a non-zero exit is an error, carrying its
// standard error.
func (g Govulncheck) Run(ctx context.Context, dir string) ([]byte, error) {
	var stdout, stderr bytes.Buffer

	// path is the binary --govulncheck named or the one on PATH.
	args := []string{"-format", "json", "./...", "tool"}
	cmd := exec.CommandContext(ctx, g.path, args...) //nolint:gosec // Resolved by NewGovulncheck.
	cmd.Dir = dir

	// cmd.Environ, not os.Environ, so PWD matches Dir.
	cmd.Env = append(cmd.Environ(), "GOWORK=off")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("running govulncheck in %s: %w: %s", dir, err, strings.TrimSpace(stderr.String()))
	}

	return stdout.Bytes(), nil
}

// Dirs returns the directories to scan, slash-separated and relative to the
// root: every library module in ws, then every tool workspace.ToolDirs finds.
func Dirs(ws workspace.Workspace) ([]string, error) {
	tools, err := workspace.ToolDirs(ws.Root)
	if err != nil {
		return nil, err
	}

	return append(ws.Dirs(), tools...), nil
}

// Scan runs run in each of dirs, relative to root, up to jobs at once, and
// returns a Result per directory in the same order. Jobs below 1 mean 1. A
// directory whose scan fails doesn't stop the others: Scan returns the
// results of the ones that completed together with an error joining every
// failure, so a caller can still report them, marked incomplete. A caller
// must not treat a partial scan as complete: it would read as fixed
// advisories.
func Scan(ctx context.Context, root string, dirs []string, jobs int, run RunFunc) ([]Result, error) {
	scans := parallel.Map(ctx, jobs, dirs, func(ctx context.Context, dir string) ([]Advisory, error) {
		return scanDir(ctx, root, dir, run)
	})

	results := make([]Result, 0, len(dirs))

	var errs []error

	for i, s := range scans {
		// A scan that never ran, or panicked, fails like any other: it has no
		// findings to report, but isn't clean either.
		if s.Err != nil {
			errs = append(errs, fmt.Errorf("scanning %s: %w", dirs[i], s.Err))
			continue
		}

		results = append(results, Result{Dir: dirs[i], Advisories: s.Value})
	}

	if len(errs) > 0 {
		return results, fmt.Errorf("%w: %w", ErrIncomplete, errors.Join(errs...))
	}

	return results, nil
}

func scanDir(ctx context.Context, root, dir string, run RunFunc) ([]Advisory, error) {
	out, err := run(ctx, filepath.Join(root, filepath.FromSlash(dir)))
	if err != nil {
		return nil, err
	}

	return Parse(bytes.NewReader(out))
}
