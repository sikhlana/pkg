# cli

Cobra scaffolding for a program's `main`: a context that a signal cancels, exit codes that commands return instead of calling `os.Exit`, consistent root and version commands, and build information that works with or without link-time flags.

## Configuration

None. `cli` runs in `main`, before any configuration is loaded. The root command's `--config` flag only carries a path, for the program to load however it likes.

## Exported Members

| Name | Description |
|------|-------------|
| SignalContext | Returns a copy of a parent context that the first SIGINT or SIGTERM cancels, with a cause naming the signal. A second signal ends the process. |
| Execute | Runs a root command with a context and returns the process exit code for its result. |
| ExitError | An error that chooses the process exit code, returned from a command directly or wrapped. |
| ExitError.Code | The exit code. Execute turns zero or less into 1. |
| ExitError.Err | The underlying error, or nil. |
| ExitError.Error | Returns the underlying error's message, or exit status N when Err is nil. |
| ExitError.Unwrap | Returns the underlying error. |
| Exit | Returns an ExitError with a code and an underlying error, which may be nil. |
| NewRootCommand | Returns a root command with a persistent --config (-c) flag, a --version flag, and no usage text on errors. |
| NewVersionCommand | Returns a version subcommand that prints the same line as --version. |
| ConfigFlag | The name of the config flag, for reading it with cmd.Flags().GetString. |
| Version | The version, set at link time. Empty unless the build sets it. |
| Revision | The version-control revision, set at link time. Empty unless the build sets it. |
| Time | The time of the revision, set at link time. Empty unless the build sets it. |
| BuildInfo | How the running program was built. |
| BuildInfo.Version | The version; dev when nothing set or stamped one. |
| BuildInfo.Revision | The version-control revision, or empty when unknown. |
| BuildInfo.Time | The time of the revision, or empty when unknown. |
| BuildInfo.Modified | Whether the go command recorded uncommitted changes at build time. |
| BuildInfo.String | Renders the version line, such as v1.2.3 (abc123, 2026-10-16T09:00:00Z, modified). |
| ReadBuildInfo | Resolves the running program's BuildInfo from link-time values, then from what the go command stamped. |

## Usage

### Standalone

`main` calls `os.Exit` with the code from a separate function, so deferred calls such as `stop` run first:

```go
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/sikhlana/pkg/cli"
)

func main() {
	os.Exit(run())
}

func run() int {
	ctx, stop := cli.SignalContext(context.Background())
	defer stop()

	root := cli.NewRootCommand("myapp", "Does my thing")
	root.AddCommand(cli.NewVersionCommand(), newServeCommand())

	return cli.Execute(ctx, root)
}

func newServeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Serve requests",
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := cmd.Flags().GetString(cli.ConfigFlag)
			if err != nil {
				return err
			}

			fmt.Fprintln(cmd.OutOrStdout(), "serving with", path)

			<-cmd.Context().Done()

			return nil
		},
	}
}
```

A failing command prints `Error: <message>` on standard error and exits 1. The usage text isn't printed for any failure, flag errors included; `--help` still prints it.

#### Signals

The first Ctrl-C (SIGINT) or SIGTERM cancels the context every command receives, and `context.Cause(ctx)` names the signal. The program then shuts down at its own pace, and exits 0 if it shuts down cleanly. A second Ctrl-C ends the process straight away, with the operating system's usual status (130 in a shell).

That second signal only gets through if nothing else in the program subscribes to SIGINT or SIGTERM with `signal.Notify`. Another subscriber keeps receiving it, so the process carries on.

#### Exit codes

Return an `ExitError` to choose the exit code. It can be wrapped like any other error:

```go
return cli.Exit(2, fmt.Errorf("reading %s: %w", path, err))

return fmt.Errorf("serving: %w", cli.Exit(3, err))
```

A code of zero or less exits 1, so a failed command never reports success. A command that wants an interrupted run to exit 130, as a shell reports it, returns the code once its context is done:

```go
if cmd.Context().Err() != nil {
	return cli.Exit(130, nil)
}
```

#### Quiet exits

A command that reports through its exit code alone, as `grep` and `diff` do, silences cobra's error output and returns an `ExitError` with no underlying error:

```go
RunE: func(cmd *cobra.Command, _ []string) error {
	if !found {
		cmd.SilenceErrors = true

		return cli.Exit(1, nil)
	}

	return nil
},
```

#### Version information

`myapp --version` and `myapp version` print the same line. A plain `go build` from a clean checkout tagged `v1.2.3` prints:

```text
myapp v1.2.3 (9b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c, 2026-10-16T09:00:00Z)
```

The revision and commit time come from version control, as the go command stamps them. With uncommitted changes the version gains `+dirty` and the list ends in `modified`. With nothing to go on, such as a `-buildvcs=false` build, the line is `myapp dev`.

A build without the `.git` directory, such as a Docker build that excludes it, sets the values at link time instead:

```bash
go build -ldflags "\
  -X github.com/sikhlana/pkg/cli.Version=${VERSION} \
  -X github.com/sikhlana/pkg/cli.Revision=${REVISION} \
  -X github.com/sikhlana/pkg/cli.Time=${COMMIT_TIME}" \
  ./cmd/myapp
```

Each value set at link time wins, and each one left unset still comes from version control, so a CI job can set `Version` alone. `ReadBuildInfo` returns the same values as a `BuildInfo`, for a program that also wants them elsewhere, such as a log attribute.

### With fx

`cli` doesn't provide an fx module: it runs before any fx app exists. A command builds and runs the app under its own context:

```go
func newServeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Serve requests",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			app := fx.New(
				// The modules this command needs.
			)

			if err := app.Start(ctx); err != nil {
				return fmt.Errorf("starting: %w", err)
			}

			<-ctx.Done()

			stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()

			if err := app.Stop(stopCtx); err != nil {
				return fmt.Errorf("stopping: %w", err)
			}

			return nil
		},
	}
}
```

The command waits on its context rather than calling `app.Run`, `app.Done` or `app.Wait`. Those subscribe to SIGINT and SIGTERM themselves, which would stop a second Ctrl-C from ending a hung shutdown.
