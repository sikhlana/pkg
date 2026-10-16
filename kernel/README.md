# kernel

Turns cobra commands into running fx apps: a root command with the logging flags, and commands that build each app from those flags, the program's shared modules and their own, then run it with `kernelrun`. Failures are reported once, in the log's format, with their exit code intact.

## Configuration

None. `Options` holds the program's developer settings, fixed in code: how it loads config, the default log format, the text handler, the start and stop timeouts and the modules every command shares. The root command's flags are the operator's per-run choices; see Flags under Usage.

## Exported Members

| Name | Description |
|------|-------------|
| New | Returns an App with the program's Options. Panics, with an error matching log.ErrUnknownFormat, when Options.Format is neither json nor text. |
| Options | The program's developer settings, shared by every command. |
| Options.Config | Returns the program's config loading as an fx option, given --config's path, empty when the operator passed none. Nil means the program loads no config: no --config flag and nothing in the graph. |
| Options.Format | The default of --format: log.FormatJSON for a service, log.FormatText for an operator tool. |
| Options.TextHandler | Renders text records. Nil means tint.New. |
| Options.StartTimeout | Bounds the OnStart hooks. Zero means kernelrun's default. |
| Options.StopTimeout | Bounds the OnStop hooks. Zero means kernelrun's default. |
| Options.Shared | Part of every command's app, ahead of the command's own options. Nil means nothing. |
| App | Builds the program's commands and runs their apps. A small value, passed by value. |
| App.RootCommand | Returns cli.NewRootCommand's root with --debug, --quiet and --format added as persistent flags, with --debug and --quiet mutually exclusive, and --config too when Options.Config is set. |
| App.Command | Returns a command that takes no arguments and runs the app built from its options. |
| App.Run | Builds a command's app from the root's flags, Options.Shared and its options, and runs it with kernelrun.Run on the command's context. For a command with arguments or flags of its own, called from its RunE. |
| DebugFlag | The name of the --debug flag. |
| QuietFlag | The name of the --quiet flag. |
| FormatFlag | The name of the --format flag. |
| ConfigFlag | The name of the --config flag. |
| ErrMissingFlag | Returned by App.Run, naming the flag, for a command whose root wasn't built by App.RootCommand. |

## Usage

### Standalone

`kernel` has no use outside cobra and fx: it exists to build cobra commands that run fx apps. A program without cobra calls `kernelrun.Run` directly and wires `log` itself.

### With fx

Build one `App` for the program, its root with `RootCommand`, and its commands with `Command` or `Run`. Config loading goes in `Options.Config`, which passes `--config`'s path on as the loader's `Filename`, and other shared modules in `Options.Shared`:

```go
package main

import (
	"context"

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
		Config: func(path string) fx.Option {
			return configloader.Provide(configloader.Options[Config]{Name: "mytool", Filename: path})
		},
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

#### Flags

| Flag | Description |
|------|-------------|
| --debug | Logs from Debug up, fx's own events included. Can't be combined with --quiet. |
| --quiet, -q | Logs from Warn up. Can't be combined with --debug. |
| --format | json or text. Defaults to Options.Format; any other value fails before an app is built. |
| --config | The config file, given to Options.Config. Present only when Options.Config is set. |

While `cli`'s root still defines `--config` (before `cli` v0.3.0), `RootCommand` adjusts that flag rather than defining its own. With `Options.Config` set, the flag gets the kernel's help. Without it, the flag is hidden from help, and passing it fails before any app is built, naming `--config`.

#### What each app gets

In order:

- fx's events through `log.NewEventLogger`, at Debug, with its failures at Error;
- a module of its own whose invoke makes the app's logger slog's default, so third-party `slog` and standard `log` output share its format. fx runs a module's invokes before its parent's, in the order modules are listed, so this one runs before any other module's invokes;
- `log.Module`, with the level and format from the flags, and `Options.TextHandler` or tint for text;
- `Options.Config`, given `--config`'s path, when the program loads config, so the loader reports through the app's logger and the shared modules can take the config;
- `Options.Shared`, then the command's own options.

While the app runs, the `log` package writes into its logger at Warn. The `log` package has no levels, and what libraries write through it is mostly error reports, net/http's ErrorLog for one, so they survive `--quiet`. When the command returns, for any reason, slog's default, the `log` package's output and flags, and that level are put back.

A command checks its config by registering a runner that returns once the config has loaded, as `check` does above: an app needs a runner to run.

#### Moving from kernel 0.1.0

Before 0.2.0 the kernel supplied `configloader.WithFilename` with `--config`'s value. It no longer imports `configloader`, so a program with `configloader.Provide` in `Options.Shared` still compiles, but `--config` no longer reaches the loader. Passing `--config` to that program fails before any app is built, naming the flag, so the missing migration shows up at once; without `--config`, the loader searches as it always did. Move the loader into `Options.Config` and pass the path as `configloader.Options.Filename`, as the example above does.

#### How failures are reported

| Phase | Fails with | Reported by | Printed by cobra |
|-------|------------|-------------|------------------|
| flags | a parse error, an unknown --format, --debug with --quiet | cobra | yes |
| the kernel's wiring | ErrMissingFlag | cobra | yes |
| building and starting | the root cause | fx's event logger, with the full chain, at Error | no |
| runner checks | ErrNoRunners, ErrDuplicateRunner or ErrInvalidRunner, from kernelrun | the kernel, at Error | no |
| run and stop | the crash and stop errors | kernelrun, at Error, per runner | no |

Once an app has been built, `Run` sets the command's `SilenceErrors` on any failure, so a JSON service never ends its log stream with a plain-text line and nothing is reported twice. The error is still returned, so `cli.Execute` exits with its code: a runner returning `cli.Exit(3, err)` exits 3, and any other failure exits 1. If the logger itself can't be built, fx falls back to its console logger, which still prints the failure.
