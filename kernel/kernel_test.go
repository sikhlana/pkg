package kernel_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"

	"github.com/sikhlana/pkg/cli"
	"github.com/sikhlana/pkg/config/configloader"
	"github.com/sikhlana/pkg/kernel"
	"github.com/sikhlana/pkg/kernelrun"
	"github.com/sikhlana/pkg/log"
)

func TestDebugAndQuietExclusiveOnASubcommand(t *testing.T) {
	var ran bool

	root, _ := program(kernel.Options{}, flagRunner(&ran))

	code, stderr := execute(t, root, "run", "--debug", "--quiet")

	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "[debug quiet]")
	assert.False(t, ran, "the app ran")
}

func TestQuietOnASubcommand(t *testing.T) {
	root, out := program(kernel.Options{}, levelRunner())

	code, _ := execute(t, root, "run", "-q")

	require.Equal(t, 0, code)
	assert.Equal(t, []string{"warn record"}, out.messages(t, ""))
}

func TestUnknownFormat(t *testing.T) {
	var ran bool

	root, _ := program(kernel.Options{}, flagRunner(&ran))

	code, stderr := execute(t, root, "run", "--format", "yaml")

	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "unknown log format")
	assert.False(t, ran, "the app ran")
}

func TestFormatHelpShowsAPlaceholder(t *testing.T) {
	root, _ := program(kernel.Options{})

	var help bytes.Buffer

	root.SetOut(&help)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"run", "--help"})

	require.Equal(t, 0, cli.Execute(t.Context(), root))
	assert.Contains(t, help.String(), "--format format")
}

func TestNewPanicsOnAnUnknownFormat(t *testing.T) {
	defer func() {
		err, ok := recover().(error)
		require.True(t, ok, "New panicked with an error")
		require.ErrorIs(t, err, log.ErrUnknownFormat)
		assert.EqualError(t, err, "kernel: Options.Format: unknown log format: Format(5)")
	}()

	kernel.New(kernel.Options{Format: log.Format(5)})
}

func TestProgramDefaultFormatWithTint(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	root, out := program(kernel.Options{Format: log.FormatText}, levelRunner())

	code, _ := execute(t, root, "run")

	require.Equal(t, 0, code)
	assert.Contains(t, out.String(), "INF info record")
	assert.NotContains(t, out.String(), "{")
}

func TestProgramTextHandler(t *testing.T) {
	custom := func(w io.Writer, opts *slog.HandlerOptions) slog.Handler {
		return slog.NewTextHandler(w, &slog.HandlerOptions{
			Level: opts.Level,
			ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
				if a.Key == slog.MessageKey {
					a.Key = "custom_msg"
				}

				return a
			},
		})
	}

	root, out := program(kernel.Options{TextHandler: custom}, levelRunner())

	code, _ := execute(t, root, "run", "--format", "text")

	require.Equal(t, 0, code)
	assert.Contains(t, out.String(), `custom_msg="info record"`)
}

func TestDebugLevelAndFxEvents(t *testing.T) {
	hook := fx.Invoke(func(lc fx.Lifecycle) {
		lc.Append(fx.StartHook(func() {}))
	})

	root, out := program(kernel.Options{}, levelRunner(), hook)

	code, _ := execute(t, root, "run", "--debug")

	require.Equal(t, 0, code)

	messages := out.messages(t, "")

	assert.Contains(t, messages, "debug record")
	assert.Contains(t, messages, "provided")
	assert.Contains(t, messages, "invoking")
	assert.Contains(t, messages, "OnStart hook executed")
}

func TestConfigReachesTheLoader(t *testing.T) {
	type config struct {
		Greeting string `mapstructure:"greeting"`
	}

	path := filepath.Join(t.TempDir(), "app.yaml")

	require.NoError(t, os.WriteFile(path, []byte("greeting: hello\n"), 0o600))

	var got string

	root, _ := program(
		kernel.Options{Shared: configloader.Provide(configloader.Options[config]{Name: "kerneltest"})},
		kernelrun.ProvideRunner("app", func(c config) kernelrun.Func {
			return func(context.Context) error {
				got = c.Greeting

				return nil
			}
		}),
	)

	code, _ := execute(t, root, "run", "--config", path)

	require.Equal(t, 0, code)
	assert.Equal(t, "hello", got)
}

func TestSharedAndOwnOptions(t *testing.T) {
	type shared string

	var got shared

	root, _ := program(
		kernel.Options{Shared: fx.Supply(shared("from the program"))},
		kernelrun.ProvideRunner("app", func(s shared) kernelrun.Func {
			return func(context.Context) error {
				got = s

				return nil
			}
		}),
	)

	code, _ := execute(t, root, "run")

	require.Equal(t, 0, code)
	assert.Equal(t, shared("from the program"), got)
}

func TestRootWithoutTheKernelsFlags(t *testing.T) {
	var (
		ran bool
		err error
	)

	app := kernel.New(kernel.Options{})

	root := cli.NewRootCommand("myapp", "A test program")
	root.AddCommand(&cobra.Command{
		Use: "run",
		RunE: func(cmd *cobra.Command, _ []string) error {
			err = app.Run(cmd, flagRunner(&ran))

			return err
		},
	})

	code, _ := execute(t, root, "run")

	assert.Equal(t, 1, code)
	require.ErrorIs(t, err, kernel.ErrMissingFlag)
	require.EqualError(t, err, "missing kernel flag: debug")
	assert.False(t, ran, "the app ran")
}

func TestCommandTakesNoArguments(t *testing.T) {
	var ran bool

	root, _ := program(kernel.Options{}, flagRunner(&ran))

	code, stderr := execute(t, root, "run", "extra")

	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, `unknown command "extra"`)
	assert.False(t, ran, "the app ran")
}

func TestDefaultLoggerDuringTheRun(t *testing.T) {
	root, out := program(kernel.Options{}, kernelrun.WithRunner("app", func(ctx context.Context) error {
		//nolint:sloglint // The test checks that slog's default logger writes through the app's logger.
		slog.InfoContext(ctx, "through the default")

		return nil
	}))

	code, _ := execute(t, root, "run")

	require.Equal(t, 0, code)
	assert.Contains(t, out.messages(t, "INFO"), "through the default")
}

func TestDefaultLoggerAheadOfOtherModules(t *testing.T) {
	early := fx.Module("early", fx.Invoke(func() {
		//nolint:sloglint // The test checks that a module's invoke already writes through the app's logger.
		slog.Info("from a module's invoke")
	}))

	root, out := program(kernel.Options{Shared: early}, kernelrun.WithRunner("app", returnNil))

	code, _ := execute(t, root, "run")

	require.Equal(t, 0, code)
	assert.Contains(t, out.messages(t, "INFO"), "from a module's invoke")
}

func TestDefaultLoggerRestored(t *testing.T) {
	tests := []struct {
		name string
		opts []fx.Option
		code int
	}{
		{name: "after a clean run", opts: []fx.Option{kernelrun.WithRunner("app", returnNil)}},
		{name: "after a startup failure", opts: []fx.Option{failingConstructor()}, code: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, writer, flags := saveDefault(t)
			level := bridgeLevel()

			root, _ := program(kernel.Options{}, tt.opts...)

			code, _ := execute(t, root, "run")

			require.Equal(t, tt.code, code)
			assert.Same(t, logger, slog.Default())
			assertLogOutput(t, writer, flags)
			assert.Equal(t, level, bridgeLevel(), "the log package's level wasn't restored")
		})
	}
}

func TestCrashPrintsNothing(t *testing.T) {
	root, out := program(kernel.Options{}, kernelrun.WithRunner("app", func(context.Context) error {
		return errors.New("crashed")
	}))

	code, stderr := execute(t, root, "run")

	assert.Equal(t, 1, code)
	assert.Empty(t, stderr)
	assert.Contains(t, out.messages(t, "ERROR"), "runner crashed")
}

func TestExitCodeSurvives(t *testing.T) {
	root, _ := program(kernel.Options{}, kernelrun.WithRunner("app", func(context.Context) error {
		return cli.Exit(3, errors.New("partial failure"))
	}))

	code, stderr := execute(t, root, "run")

	assert.Equal(t, 3, code)
	assert.Empty(t, stderr)
}

func TestStartupFailurePrintsNothing(t *testing.T) {
	root, out := program(kernel.Options{}, failingConstructor())

	code, stderr := execute(t, root, "run")

	assert.Equal(t, 1, code)
	assert.Empty(t, stderr)
	assert.Contains(t, out.String(), "validating config: port required")
	assert.NotEmpty(t, out.messages(t, "ERROR"))
}

func TestRunnerCheckFailureIsLogged(t *testing.T) {
	root, out := program(kernel.Options{})

	code, stderr := execute(t, root, "run")

	assert.Equal(t, 1, code)
	assert.Empty(t, stderr)

	var failure map[string]any

	for _, rec := range out.records(t) {
		if rec["msg"] == "checking runners failed" {
			failure = rec
		}
	}

	require.NotNil(t, failure, "no record of the check failure in %s", out)
	assert.Equal(t, "ERROR", failure["level"])
	assert.Equal(t, "checking runners: no runners", failure["error"])
}

func TestStartTimeoutReachesKernelrun(t *testing.T) {
	root, out := program(kernel.Options{StartTimeout: 50 * time.Millisecond},
		kernelrun.WithRunner("app", returnNil),
		lifecycle(fx.Hook{OnStart: waitDone}),
	)

	begin := time.Now()
	code, _ := execute(t, root, "run")

	assert.Equal(t, 1, code)
	assert.Less(t, time.Since(begin), 5*time.Second, "the start timeout didn't apply")
	assert.Contains(t, out.String(), "context deadline exceeded")
}

func TestStopTimeoutReachesKernelrun(t *testing.T) {
	root, out := program(kernel.Options{StopTimeout: 50 * time.Millisecond},
		kernelrun.WithRunner("app", returnNil),
		lifecycle(fx.Hook{OnStop: waitDone}),
	)

	begin := time.Now()
	code, _ := execute(t, root, "run")

	assert.Equal(t, 1, code)
	assert.Less(t, time.Since(begin), 5*time.Second, "the stop timeout didn't apply")
	assert.Contains(t, out.messages(t, "ERROR"), "stopping the app failed")
}

func TestUnknownFlagPrinted(t *testing.T) {
	var ran bool

	root, _ := program(kernel.Options{}, flagRunner(&ran))

	code, stderr := execute(t, root, "run", "--verbose")

	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "unknown flag: --verbose")
	assert.False(t, ran, "the app ran")
}

// failingConstructor is a runner whose constructor's dependency fails to build.
func failingConstructor() fx.Option {
	type config struct{}

	return fx.Options(
		fx.Provide(func() (config, error) { return config{}, errors.New("validating config: port required") }),
		kernelrun.ProvideRunner("app", func(config) kernelrun.Func { return returnNil }),
	)
}

// returnNil is a runner that returns at once.
func returnNil(context.Context) error {
	return nil
}

// waitDone is a hook that waits for its context and returns its error.
func waitDone(ctx context.Context) error {
	<-ctx.Done()

	return ctx.Err()
}

// lifecycle registers hook on the app's lifecycle.
func lifecycle(hook fx.Hook) fx.Option {
	return fx.Invoke(func(lc fx.Lifecycle) {
		lc.Append(hook)
	})
}
