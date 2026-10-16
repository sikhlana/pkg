package kernel

import (
	"errors"
	"fmt"
	stdlog "log" //nolint:depguard // Run restores the log package's output, which slog.SetDefault redirects.
	"log/slog"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"go.uber.org/fx"

	"github.com/sikhlana/pkg/cli"
	"github.com/sikhlana/pkg/config/configloader"
	"github.com/sikhlana/pkg/kernelrun"
	"github.com/sikhlana/pkg/log"
	"github.com/sikhlana/pkg/log/tint"
)

const (
	// DebugFlag is the name of the persistent flag RootCommand adds to log from Debug up, --debug.
	DebugFlag = "debug"

	// QuietFlag is the name of the persistent flag RootCommand adds to log from Warn up, --quiet or -q.
	QuietFlag = "quiet"

	// FormatFlag is the name of the persistent flag RootCommand adds to pick the log format, --format.
	FormatFlag = "format"
)

// ErrMissingFlag is returned by App.Run for a command whose root wasn't built by App.RootCommand. The error names the
// first flag missing.
var ErrMissingFlag = errors.New("missing kernel flag")

// Options holds a program's developer settings, shared by every command it builds.
type Options struct {
	// Format is --format's default: log.FormatJSON for a service, log.FormatText for an operator tool.
	Format log.Format

	// TextHandler renders log.FormatText records. Nil means tint.New.
	TextHandler log.TextHandlerProvider

	// StartTimeout bounds the app's OnStart hooks. Zero means kernelrun.DefaultStartTimeout.
	StartTimeout time.Duration

	// StopTimeout bounds the app's OnStop hooks. Zero means kernelrun.DefaultStopTimeout.
	StopTimeout time.Duration

	// Shared is part of every command's app, ahead of the command's own options. Nil means nothing.
	Shared fx.Option
}

// App builds a program's cobra commands and runs their fx apps, with the logger wired from the root's flags.
type App struct {
	o Options
}

// New returns an App with the program's Options.
func New(o Options) App {
	return App{o: o}
}

// RootCommand returns cli.NewRootCommand's root with three persistent flags added: --debug, --quiet with the
// shorthand -q, and --format, a log.Format defaulting to Options.Format. --debug and --quiet are mutually exclusive,
// on the root and every command under it, and an unknown --format fails while the flags are parsed.
func (a App) RootCommand(name, short string) *cobra.Command {
	cmd := cli.NewRootCommand(name, short)

	flags := cmd.PersistentFlags()

	flags.Bool(DebugFlag, false, "log from debug up")
	flags.BoolP(QuietFlag, "q", false, "log from warn up")
	flags.TextVar(new(log.Format), FormatFlag, a.o.Format, "log format: json or text")

	cmd.MarkFlagsMutuallyExclusive(DebugFlag, QuietFlag)

	return cmd
}

// Command returns a command that takes no arguments and runs the app built from opts, through Run. A command with
// arguments or flags of its own calls Run from its RunE instead, supplying them as fx values.
func (a App) Command(use, short string, opts ...fx.Option) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.Run(cmd, opts...)
		},
	}
}

// Run runs cmd's app with kernelrun.Run on cmd's context. The app gets, in order: fx's events through
// log.NewEventLogger, log.Module with the level and format from the root's flags and Options.TextHandler (tint.New
// when nil), the --config value through configloader.WithFilename, an invoke making the app's logger slog's default,
// Options.Shared, then opts. An app that also supplies configloader.WithFilename fails to build.
//
// slog's default logger, and the log package's output it redirects, are restored when Run returns. A flag the root
// lacks fails with ErrMissingFlag before any app is built. Once kernelrun.Run returns an error, Run sets
// cmd.SilenceErrors, since the app's logger has already reported the failure; the error is still returned, so its exit
// code reaches cli.Execute.
func (a App) Run(cmd *cobra.Command, opts ...fx.Option) error {
	flags, err := readFlags(cmd.Flags())
	if err != nil {
		return err
	}

	textHandler := a.o.TextHandler

	if textHandler == nil {
		textHandler = tint.New
	}

	options := []fx.Option{
		fx.WithLogger(log.NewEventLogger),
		log.Module,
		log.WithDebug(flags.debug),
		log.WithQuiet(flags.quiet),
		log.WithFormat(flags.format),
		log.WithTextHandler(textHandler),
		configloader.WithFilename(flags.config),
		fx.Invoke(slog.SetDefault),
	}

	if a.o.Shared != nil {
		options = append(options, a.o.Shared)
	}

	defer restoreDefault()()

	err = kernelrun.Run(
		cmd.Context(),
		kernelrun.Options{StartTimeout: a.o.StartTimeout, StopTimeout: a.o.StopTimeout},
		append(options, opts...)...,
	)
	if err != nil {
		cmd.SilenceErrors = true
	}

	return err
}

// flagValues holds the root's flags as Run reads them.
type flagValues struct {
	debug  bool
	quiet  bool
	format log.Format
	config string
}

// readFlags reads the kernel's and cli's flags from flags, failing with ErrMissingFlag for the first one not defined.
func readFlags(flags *pflag.FlagSet) (flagValues, error) {
	for _, name := range []string{DebugFlag, QuietFlag, FormatFlag, cli.ConfigFlag} {
		if flags.Lookup(name) == nil {
			return flagValues{}, fmt.Errorf("%w: %s", ErrMissingFlag, name)
		}
	}

	var v flagValues

	debug, debugErr := flags.GetBool(DebugFlag)
	quiet, quietErr := flags.GetBool(QuietFlag)
	formatErr := flags.GetText(FormatFlag, &v.format)
	config, configErr := flags.GetString(cli.ConfigFlag)

	// A flag of the right name but the wrong type, defined by hand, fails here.
	if err := errors.Join(debugErr, quietErr, formatErr, configErr); err != nil {
		return flagValues{}, fmt.Errorf("reading flags: %w", err)
	}

	v.debug, v.quiet, v.config = debug, quiet, config

	return v, nil
}

// restoreDefault saves slog's default logger and the log package's output and flags, and returns a function that puts
// them back. slog.SetDefault redirects the log package's output to the new logger's handler, and setting back a
// default that writes through the log package doesn't undo that, so the output is restored on its own.
func restoreDefault() func() {
	logger, writer, flags := slog.Default(), stdlog.Writer(), stdlog.Flags()

	return func() {
		slog.SetDefault(logger)
		stdlog.SetOutput(writer)
		stdlog.SetFlags(flags)
	}
}
