package kernel_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"

	"github.com/sikhlana/pkg/cli"
	"github.com/sikhlana/pkg/kernel"
	"github.com/sikhlana/pkg/kernelrun"
	"github.com/sikhlana/pkg/log"
)

// lockedBuffer is a log destination safe for the concurrent writes of an app's runners.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

// records decodes every JSON line written to b.
func (b *lockedBuffer) records(t *testing.T) []map[string]any {
	t.Helper()

	var out []map[string]any

	lines := bufio.NewScanner(bytes.NewReader([]byte(b.String())))

	for lines.Scan() {
		var rec map[string]any

		require.NoError(t, json.Unmarshal(lines.Bytes(), &rec), "line %q", lines.Text())

		out = append(out, rec)
	}

	return out
}

// messages returns the message of every JSON record written to b at level, or at every level when level is empty.
func (b *lockedBuffer) messages(t *testing.T, level string) []string {
	t.Helper()

	var out []string

	for _, rec := range b.records(t) {
		if level == "" || rec["level"] == level {
			msg, _ := rec["msg"].(string)
			out = append(out, msg)
		}
	}

	return out
}

// program returns a root built by an App from o, with a run command made by Command from opts, and the buffer every
// command's app logs to.
func program(o kernel.Options, opts ...fx.Option) (*cobra.Command, *lockedBuffer) {
	out := &lockedBuffer{}

	shared := []fx.Option{log.WithDestination(out)}

	if o.Shared != nil {
		shared = append(shared, o.Shared)
	}

	o.Shared = fx.Options(shared...)

	app := kernel.New(o)

	root := app.RootCommand("myapp", "A test program")
	root.AddCommand(app.Command("run", "Runs the app", opts...))

	return root, out
}

// execute runs root through cli.Execute with args and returns the exit code and what cobra printed as errors.
func execute(t *testing.T, root *cobra.Command, args ...string) (int, string) {
	t.Helper()

	var stderr bytes.Buffer

	root.SetErr(&stderr)
	root.SetOut(&bytes.Buffer{})
	root.SetArgs(args)

	return cli.Execute(t.Context(), root), stderr.String()
}

// flagRunner returns a runner that sets *ran and returns.
func flagRunner(ran *bool) fx.Option {
	return kernelrun.WithRunner("app", func(context.Context) error {
		*ran = true

		return nil
	})
}

// levelRunner is a runner that logs a record at Debug, Info and Warn through the app's logger.
func levelRunner() fx.Option {
	return kernelrun.ProvideRunner("app", func(logger *slog.Logger) kernelrun.Func {
		return func(ctx context.Context) error {
			logger.DebugContext(ctx, "debug record")
			logger.InfoContext(ctx, "info record")
			logger.WarnContext(ctx, "warn record")

			return nil
		}
	})
}
