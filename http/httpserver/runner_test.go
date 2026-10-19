package httpserver_test

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"

	"github.com/sikhlana/pkg/http/httpserver"
	"github.com/sikhlana/pkg/kernelrun"
)

func TestEachCommandRunsWhatItOptsIn(t *testing.T) {
	t.Parallel()

	var apiCalls, adminCalls, mwCalls atomic.Int32

	command := func(opts ...fx.Option) error {
		return run(t, shared(slog.New(slog.DiscardHandler), configs{"api": plain(), "admin": plain()},
			provide("api", &apiCalls),
			provide("admin", &adminCalls),
			httpserver.ProvideMiddleware("api", func() httpserver.Middleware {
				mwCalls.Add(1)

				return step{&trail{}, "api"}
			}),
		), fx.Options(opts...))
	}

	require.NoError(t, command(idle), "migrate")
	assert.Zero(t, apiCalls.Load(), "migrate builds no server")
	assert.Zero(t, adminCalls.Load(), "migrate builds no server")
	assert.Zero(t, mwCalls.Load(), "migrate builds no middleware")

	var answers []string

	require.NoError(t, command(
		httpserver.ProvideRunner("api"),
		httpserver.ProvideRunner("admin"),
		probe(func(ctx context.Context, s servers) error {
			_, api, err := fetch(ctx, s.API.Addr())
			if err != nil {
				return err
			}

			_, admin, err := fetch(ctx, s.Admin.Addr())
			answers = []string{api, admin}

			return err
		}),
	), "serve")
	assert.Equal(t, []string{"api", "admin"}, answers)
	assert.Equal(t, int32(1), apiCalls.Load())
	assert.Equal(t, int32(1), adminCalls.Load())
	assert.Equal(t, int32(1), mwCalls.Load())
}

func TestBindFailureFailsStartup(t *testing.T) {
	t.Parallel()

	var lc net.ListenConfig

	taken, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	t.Cleanup(func() { _ = taken.Close() })

	var (
		calls atomic.Int32
		ran   atomic.Bool
	)

	cfg := hostPort(t, taken.Addr())
	cfg.ShutdownTimeout = time.Second

	err = run(t,
		shared(slog.New(slog.DiscardHandler), configs{"api": cfg}, provide("api", &calls)),
		httpserver.ProvideRunner("api"),
		probe(func(context.Context, servers) error {
			ran.Store(true)

			return nil
		}),
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), `"api"`)
	assert.Contains(t, err.Error(), taken.Addr().String())
	assert.False(t, ran.Load(), "no runner runs")
}

func TestFailedStartReleasesTheSocket(t *testing.T) {
	t.Parallel()

	var (
		calls atomic.Int32
		addr  string
	)

	errLater := errors.New("a later start step failed")

	// Injecting the runners makes this hook append after the server's.
	type afterRunners struct {
		fx.In

		Lifecycle fx.Lifecycle
		Runners   []kernelrun.Runner `group:"kernelrun:runner"`
		Servers   servers
	}

	err := run(t,
		shared(slog.New(slog.DiscardHandler), configs{"api": plain()}, provide("api", &calls)),
		httpserver.ProvideRunner("api"),
		fx.Invoke(func(p afterRunners) {
			p.Lifecycle.Append(fx.StartHook(func() error {
				addr = p.Servers.API.Addr().String()

				return errLater
			}))
		}),
	)

	require.ErrorIs(t, err, errLater)
	require.NotEmpty(t, addr, "the server bound before the failing hook")

	var lc net.ListenConfig

	l, err := lc.Listen(t.Context(), "tcp", addr)
	require.NoError(t, err, "the socket was released")
	require.NoError(t, l.Close())
}

func TestOneServerFailingStopsTheOther(t *testing.T) {
	t.Parallel()

	var apiCalls, adminCalls atomic.Int32

	err := run(t,
		shared(slog.New(slog.DiscardHandler), configs{"api": plain(), "admin": plain()},
			provide("api", &apiCalls),
			provide("admin", &adminCalls),
		),
		httpserver.ProvideRunner("api"),
		httpserver.ProvideRunner("admin"),
		probe(func(ctx context.Context, s servers) error {
			if err := httpserver.BreakListener(s.Admin); err != nil {
				return err
			}

			<-ctx.Done()

			return nil
		}),
	)

	require.ErrorIs(t, err, net.ErrClosed)
	assert.Contains(t, err.Error(), "httpserver:admin")
	assert.NotContains(t, err.Error(), "httpserver:api", "api drained cleanly")
}

func TestUnknownRunnerFailsStartup(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	err := run(t,
		shared(slog.New(slog.DiscardHandler), configs{"api": plain()}, provide("api", &calls)),
		httpserver.ProvideRunner("apii"),
	)

	require.ErrorIs(t, err, httpserver.ErrUnknownServer)
	assert.Contains(t, err.Error(), `"apii"`)
	assert.Zero(t, calls.Load())
}
