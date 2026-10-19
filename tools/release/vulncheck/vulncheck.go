// Package vulncheck scans every library module and tool with govulncheck and
// keeps one GitHub issue in step with the reachable vulnerabilities it finds.
package vulncheck

import (
	"context"

	"github.com/sikhlana/pkg/tools/release/workspace"
)

// Options configures Run.
type Options struct {
	// Workspace is the repository; Run scans the directories Dirs returns for it.
	Workspace workspace.Workspace
	// Run runs govulncheck in one directory.
	Run RunFunc
	// Tracker keeps the issue. Nil skips the issue, for a dry run.
	Tracker Tracker
	// RunURL is linked from the closing comment, as RunURL returns it.
	RunURL string
	// Jobs is how many directories Run scans at once. Zero means 1.
	Jobs int
}

// Run scans every directory and, unless opts.Tracker is nil, reconciles the
// issue with the results. When any scan fails it returns the completed
// scans' results with the error, for the caller to report as incomplete, and
// never touches the tracker. When reconciling fails it still returns the
// results, so the caller can report them.
func Run(ctx context.Context, opts Options) ([]Result, Outcome, error) {
	dirs, err := Dirs(opts.Workspace)
	if err != nil {
		return nil, Outcome{}, err
	}

	results, err := Scan(ctx, opts.Workspace.Root, dirs, opts.Jobs, opts.Run)
	if err != nil {
		return results, Outcome{}, err
	}

	if opts.Tracker == nil {
		return results, Outcome{}, nil
	}

	outcome, err := Reconcile(ctx, opts.Tracker, results, opts.RunURL)

	return results, outcome, err
}
