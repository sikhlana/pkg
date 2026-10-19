// Command release runs this repository's release CI: plan and check on pull
// requests, publish on push to main, and vulncheck every week.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/spf13/cobra"

	"github.com/sikhlana/pkg/tools/release/git"
	"github.com/sikhlana/pkg/tools/release/parallel"
	"github.com/sikhlana/pkg/tools/release/workspace"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)

	err := newRootCommand().ExecuteContext(ctx)

	stop()

	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "release:", err)
		printPanicStack(os.Stderr, err)

		os.Exit(1)
	}
}

// printPanicStack writes the stack of every panic err carries, since a
// parallel.PanicError's message leaves it out.
func printPanicStack(w io.Writer, err error) {
	for _, panicErr := range panics(err) {
		_, _ = fmt.Fprintf(w, "%s", panicErr.Stack)
	}
}

// panics returns every *parallel.PanicError in err's tree, depth first, since
// errors.As finds only the first and a joined error can hold several.
func panics(err error) []*parallel.PanicError {
	switch e := err.(type) { //nolint:errorlint // Walks the tree itself.
	case nil:
		return nil
	case *parallel.PanicError:
		return []*parallel.PanicError{e}
	case interface{ Unwrap() []error }:
		var out []*parallel.PanicError

		for _, inner := range e.Unwrap() {
			out = append(out, panics(inner)...)
		}

		return out
	default:
		return panics(errors.Unwrap(err))
	}
}

func newRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "release",
		Short:         "Plan, check and publish library module releases, and scan for vulnerabilities",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.CompletionOptions.DisableDefaultCmd = true
	root.AddCommand(newPlanCommand(), newCheckCommand(), newPublishCommand(), newVulncheckCommand())

	return root
}

func open(ctx context.Context) (git.Repo, workspace.Workspace, error) {
	repo, err := git.Open(ctx, ".")
	if err != nil {
		return git.Repo{}, workspace.Workspace{}, err
	}

	ws, err := workspace.Load(repo.Root)
	if err != nil {
		return git.Repo{}, workspace.Workspace{}, err
	}

	return repo, ws, nil
}
