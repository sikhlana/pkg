# kernel

Turns cobra commands into running fx apps: a root command with the logging flags, and commands that build each app from those flags, the program's shared modules and their own, then run it with `kernelrun`. Failures are reported once, in the log's format, with their exit code intact.

## Configuration

None. `Options` holds the program's developer settings, fixed in code: the default log format, the text handler, the start and stop timeouts and the modules every command shares. The flags are the operator's per-run choices:

| Flag | Description |
|------|-------------|
| --debug | Logs from Debug up, fx's own events included. Can't be combined with --quiet. |
| --quiet, -q | Logs from Warn up. Can't be combined with --debug. |
| --format | json or text. Defaults to Options.Format; any other value fails before an app is built. |
| --config | The config file, from cli's root, handed to config/configloader. |

## Exported Members

| Name | Description |
|------|-------------|
| New | Returns an App with the program's Options. |
| Options | The program's developer settings, shared by every command. |
| Options.Format | The default of --format: log.FormatJSON for a service, log.FormatText for an operator tool. |
| Options.TextHandler | Renders text records. Nil means tint.New. |
| Options.StartTimeout | Bounds the OnStart hooks. Zero means kernelrun's default. |
| Options.StopTimeout | Bounds the OnStop hooks. Zero means kernelrun's default. |
| Options.Shared | Part of every command's app, ahead of the command's own options. Nil means nothing. |
| App | Builds the program's commands and runs their apps. A small value, passed by value. |
| App.RootCommand | Returns cli.NewRootCommand's root with --debug, --quiet and --format added as persistent flags, with --debug and --quiet mutually exclusive. |
| App.Command | Returns a command that takes no arguments and runs the app built from its options. |
| App.Run | Builds a command's app from the root's flags, Options.Shared and its options, and runs it with kernelrun.Run on the command's context. For a command with arguments or flags of its own, called from its RunE. |
| DebugFlag | The name of the --debug flag. |
| QuietFlag | The name of the --quiet flag. |
| FormatFlag | The name of the --format flag. |
| ErrMissingFlag | Returned by App.Run, naming the flag, for a command whose root wasn't built by App.RootCommand. |

## Usage

### Standalone

`kernel` has no use outside cobra and fx: it exists to build cobra commands that run fx apps. A program without cobra calls `kernelrun.Run` directly and wires `log` itself.

### With fx

Build one `App` for the program, its root with `RootCommand`, and its commands with `Command` or `Run`. Shared modules, such as the config loader, go in `Options.Shared`:

```go
package main

import (
	"github.com/spf13/cobra"
	"go.uber.org/fx"

	"github.com/sikhlana/pkg/cli"
	"github.com/sikhlana/pkg/config/configloader"
	"github.com/sikhlana/pkg/kernel"
	"github.com/sikhlana/pkg/kernelrun"
	"github.com/sikhlana/pkg/log"
)

func main() {
	cli.Main(root)
}

func root() *cobra.Command {
	app := kernel.New(kernel.Options{
		Format: log.FormatText,
		Shared: configloader.Provide(configloader.Options[Config]{Name: "mytool"}),
	})

	cmd := app.RootCommand("mytool", "Does my thing")

	cmd.AddCommand(
		cli.NewVersionCommand(),
		app.Command("serve", "Serve the API", httpserver.Module, httpserver.ProvideRunner()),
		app.Command("check", "Check the config", kernelrun.WithRunner("check", func(context.Context) error {
			return nil
		})),
		&cobra.Command{
			Use:   "import <file>",
			Short: "Import a file",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return app.Run(cmd, fx.Supply(ImportPath(args[0])), importer.ProvideRunner())
			},
		},
	)

	return cmd
}
```

Each command's app gets, in order:

- fx's events through `log.NewEventLogger`, at Debug, with its failures at Error;
- `log.Module`, with the level and format from the flags, and `Options.TextHandler` or tint for text;
- the `--config` value through `configloader.WithFilename`. The kernel owns it, so an app that supplies its own fails to build. In an app that doesn't load config, nothing reads it;
- an invoke that makes the app's logger slog's default, so third-party `slog` and standard `log` output share its format. When the command returns, for any reason, slog's default and the `log` package's output and flags are put back;
- `Options.Shared`, then the command's own options.

A command checks its config by registering a runner that returns once the config has loaded, as `check` does above: an app needs a runner to run.

#### How failures are reported

| Phase | Fails with | Reported by | Printed by cobra |
|-------|------------|-------------|------------------|
| flags | a parse error, an unknown --format, --debug with --quiet | cobra | yes |
| the kernel's wiring | ErrMissingFlag | cobra | yes |
| startup | the root cause | fx's event logger, with the full chain, at Error | no |
| run and stop | the crash and stop errors | kernelrun, at Error, per runner | no |

Once an app has been built, `Run` sets the command's `SilenceErrors` on any failure, so a JSON service never ends its log stream with a plain-text line and nothing is reported twice. The error is still returned, so `cli.Execute` exits with its code: a runner returning `cli.Exit(3, err)` exits 3, and any other failure exits 1. If the logger itself can't be built, fx falls back to its console logger, which still prints the failure.
