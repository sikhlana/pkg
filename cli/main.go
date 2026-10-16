package cli

import (
	"context"
	"os"

	"github.com/spf13/cobra"
)

// CommandFunc returns a program's root command with its subcommands added. Main calls it once.
type CommandFunc func() *cobra.Command

// Main runs a program and ends the process with its exit code; it never returns. It starts SignalContext, builds the
// command tree with root, runs it with Execute, and exits with Execute's code once the signal handler is released. A
// program's main function is typically just cli.Main(cmd.Root).
//
// root is called after the signal handler is installed, so a signal that arrives while the tree is being built
// cancels the context the commands run with instead of killing the process.
func Main(root CommandFunc) {
	os.Exit(run(root))
}

// run is Main without the exit. os.Exit skips deferred calls, so stop has to run here, before Main exits.
func run(root CommandFunc) int {
	ctx, stop := SignalContext(context.Background())

	defer stop()

	return Execute(ctx, root())
}
