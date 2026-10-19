package httpserver_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"

	"github.com/sikhlana/pkg/http/httpserver"
	"github.com/sikhlana/pkg/kernelrun"
	"github.com/sikhlana/pkg/tls/tlsconfig"
)

// configs holds each test server's Config by name, supplied whole so constructors can pick their own.
type configs map[string]httpserver.Config

// servers is what a test runner injects to reach the named servers, as an app would.
type servers struct {
	fx.In

	API   *httpserver.Server `name:"httpserver:server:api"   optional:"true"`
	Admin *httpserver.Server `name:"httpserver:server:admin" optional:"true"`
}

// plain returns a Config for a plaintext server on 127.0.0.1:0.
func plain() httpserver.Config {
	return httpserver.Config{Host: "127.0.0.1", ShutdownTimeout: time.Second}
}

// provide provides the server name, answering with its own name, and counts its constructor's calls in calls.
func provide(name string, calls *atomic.Int32) fx.Option {
	return httpserver.ProvideServer(name, func(c configs) httpserver.Spec {
		calls.Add(1)

		return httpserver.Spec{Config: c[name], Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, name)
		})}
	})
}

// shared returns the options every command of a test app lists: the Module, a logger and the configs.
func shared(logger *slog.Logger, c configs, opts ...fx.Option) fx.Option {
	return fx.Options(append([]fx.Option{httpserver.Module, fx.Supply(logger, c)}, opts...)...)
}

// run runs an app of opts under kernelrun.
func run(t *testing.T, opts ...fx.Option) error {
	t.Helper()

	return kernelrun.Run(t.Context(), kernelrun.Options{StopTimeout: 5 * time.Second}, opts...)
}

// probe registers a runner that calls f with the servers once the app has started, and returns f's result.
func probe(f func(ctx context.Context, s servers) error) fx.Option {
	return kernelrun.ProvideRunner("probe", func(s servers) kernelrun.Func {
		return func(ctx context.Context) error { return f(ctx, s) }
	})
}

// idle registers a runner that returns at once, for commands that run nothing else.
var idle = kernelrun.WithRunner("idle", func(context.Context) error { return nil })

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

func TestModuleWorksWithoutALogger(t *testing.T) {
	t.Parallel()

	var (
		calls atomic.Int32
		got   servers
	)

	app := fxtest.New(t,
		httpserver.Module,
		fx.Supply(configs{"api": plain()}),
		provide("api", &calls),
		fx.Invoke(func(s servers) { got = s }),
	)
	app.RequireStart()

	require.NotNil(t, got.API)

	app.RequireStop()
}

func TestUnusedServerIsNotBuilt(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	broken := httpserver.Config{
		Host: "127.0.0.1",
		TLS: tlsconfig.ServerConfig{
			Enabled:  true,
			CertFile: filepath.Join(t.TempDir(), "missing.crt"),
			KeyFile:  filepath.Join(t.TempDir(), "missing.key"),
		},
	}

	require.NoError(
		t,
		run(t, shared(slog.New(slog.DiscardHandler), configs{"api": broken}, provide("api", &calls)), idle),
	)
	assert.Zero(t, calls.Load())
}

func TestInjectedServerDoesNotBind(t *testing.T) {
	t.Parallel()

	var (
		calls atomic.Int32
		got   servers
	)

	app := fxtest.New(t,
		shared(slog.New(slog.DiscardHandler), configs{"api": plain()}, provide("api", &calls)),
		fx.Invoke(func(s servers) { got = s }),
	)
	app.RequireStart()

	require.NotNil(t, got.API)
	assert.Nil(t, got.API.Addr(), "no socket")
	assert.Equal(t, int32(1), calls.Load())

	app.RequireStop()
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

func TestMiddlewareUnderFx(t *testing.T) {
	t.Parallel()

	tr := &trail{}

	var calls atomic.Int32

	err := run(t,
		shared(slog.New(slog.DiscardHandler), configs{"api": plain(), "admin": plain()},
			provide("api", &calls),
			provide("admin", &calls),
			httpserver.ProvideMiddleware("api", func() orderedStep { return orderedStep{step{tr, "200"}, 200} }),
			httpserver.ProvideMiddleware("api", func() orderedStep { return orderedStep{step{tr, "10"}, 10} }),
			httpserver.ProvideMiddleware("api", func() httpserver.OrderedMiddleware {
				return httpserver.Ordered(1, orderedStep{step{tr, "100 wrapped as 1"}, 100})
			}),
			httpserver.ProvideMiddleware("api", func() step { return step{tr, "undeclared"} }),
			httpserver.ProvideMiddleware("api", func() step { return step{tr, "undeclared"} }),
			httpserver.ProvideMiddleware("admin", func() step { return step{tr, "admin only"} }),
		),
		httpserver.ProvideRunner("api"),
		httpserver.ProvideRunner("admin"),
		probe(func(ctx context.Context, s servers) error {
			_, _, err := fetch(ctx, s.API.Addr())

			return err
		}),
	)

	require.NoError(t, err)
	assert.Equal(t, []string{"undeclared", "undeclared", "100 wrapped as 1", "10", "200"}, tr.all(), "not admin only")
}

func TestUnknownMiddlewareTargetFailsStartup(t *testing.T) {
	t.Parallel()

	var serverCalls, mwCalls atomic.Int32

	app := fx.New(
		fx.NopLogger,
		shared(slog.New(slog.DiscardHandler), configs{"api": plain()},
			provide("api", &serverCalls),
			httpserver.ProvideMiddleware("apii", func() step {
				mwCalls.Add(1)

				return step{&trail{}, "x"}
			}),
		),
	)

	require.ErrorIs(t, app.Err(), httpserver.ErrUnknownServer)
	assert.Contains(t, app.Err().Error(), `"apii"`)
	assert.Zero(t, serverCalls.Load())
	assert.Zero(t, mwCalls.Load())
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

func TestDuplicateServerNameFailsStartup(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	app := fx.New(
		fx.NopLogger,
		shared(slog.New(slog.DiscardHandler), configs{"api": plain()}, provide("api", &calls), provide("api", &calls)),
		fx.Invoke(func(servers) {}),
	)

	require.Error(t, app.Err())
}

func TestRecordsCarryTheServerName(t *testing.T) {
	t.Parallel()

	rec := newRecorder()
	ca := newAuthority(t)
	pair := ca.issue(t, t.TempDir())

	secure := plain()
	secure.TLS = tlsconfig.ServerConfig{Enabled: true, CertFile: pair.CertFile, KeyFile: pair.KeyFile}

	var calls atomic.Int32

	err := run(t,
		shared(slog.New(rec), configs{"api": secure}, provide("api", &calls)),
		httpserver.ProvideRunner("api"),
		probe(func(ctx context.Context, s servers) error {
			var d net.Dialer

			conn, err := d.DialContext(ctx, "tcp", s.API.Addr().String())
			if err != nil {
				return err
			}

			_, _ = io.WriteString(conn, "hello, server\r\n\r\n")
			_, _ = io.Copy(io.Discard, conn)
			_ = conn.Close()

			assert.Eventually(t, func() bool { return handshakeRecord(rec) != nil }, 5*time.Second, 10*time.Millisecond)

			return nil
		}),
	)
	require.NoError(t, err)

	listening, found := rec.find("http server listening")
	require.True(t, found)
	assert.Equal(t, "api", listening.Attrs["server"])

	draining, found := rec.find("http server draining")
	require.True(t, found)
	assert.Equal(t, "api", draining.Attrs["server"])

	handshake := handshakeRecord(rec)
	require.NotNil(t, handshake)
	assert.Equal(t, slog.LevelDebug, handshake.Level)
	assert.Equal(t, "api", handshake.Attrs["server"])

	loaded, found := rec.find("tls server key pair loaded")
	require.True(t, found)
	assert.Equal(t, "api", loaded.Attrs["server"], "tlsconfig's records carry the name too")
}

func TestNilHandlerFailsStartup(t *testing.T) {
	t.Parallel()

	err := run(t,
		shared(slog.New(slog.DiscardHandler), configs{},
			httpserver.ProvideServer("api", func() httpserver.Spec {
				return httpserver.Spec{Config: plain()}
			}),
		),
		httpserver.ProvideRunner("api"),
	)

	require.ErrorIs(t, err, httpserver.ErrNilHandler)
	assert.Contains(t, err.Error(), `server "api"`)
}

func TestConstructorErrorFailsStartup(t *testing.T) {
	t.Parallel()

	errHandler := errors.New("handler unavailable")

	err := run(t,
		shared(slog.New(slog.DiscardHandler), configs{},
			httpserver.ProvideServer("api", func() (httpserver.Spec, error) {
				return httpserver.Spec{}, errHandler
			}),
		),
		httpserver.ProvideRunner("api"),
	)

	require.ErrorIs(t, err, errHandler)
}

// handshakeRecord returns net/http's record of a failed TLS handshake, or nil.
func handshakeRecord(rec *recorder) *record {
	for _, r := range rec.all() {
		if strings.Contains(r.Message, "TLS handshake error") {
			return &r
		}
	}

	return nil
}

func TestAServerNamedBuilt(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	err := run(t,
		shared(slog.New(slog.DiscardHandler), configs{"built": plain()}, provide("built", &calls)),
		httpserver.ProvideRunner("built"),
		probe(func(context.Context, servers) error { return nil }),
	)

	require.NoError(t, err, "the name can't collide with the module's private tags")
	assert.Equal(t, int32(1), calls.Load())
}

func TestConditionalMiddlewareIsLeftOut(t *testing.T) {
	t.Parallel()

	tr := &trail{}

	var calls atomic.Int32

	// Each constructor decides from a condition known only at build time, as one reading the app's config would.
	conditional := func(enabled bool, label string) func() httpserver.Middleware {
		return func() httpserver.Middleware {
			if !enabled {
				return nil
			}

			return step{tr, label}
		}
	}

	err := run(t,
		shared(slog.New(slog.DiscardHandler), configs{"api": plain()},
			provide("api", &calls),
			httpserver.ProvideMiddleware("api", conditional(true, "enabled")),
			httpserver.ProvideMiddleware("api", conditional(false, "disabled")),
			httpserver.ProvideMiddleware("api", func() *pointerStep { return nil }),
		),
		httpserver.ProvideRunner("api"),
		probe(func(ctx context.Context, s servers) error {
			status, _, err := fetch(ctx, s.API.Addr())
			if err != nil {
				return err
			}

			assert.Equal(t, http.StatusOK, status)

			return nil
		}),
	)

	require.NoError(t, err)
	assert.Equal(t, []string{"enabled"}, tr.all())
}

func TestInvalidNamesFailStartup(t *testing.T) {
	t.Parallel()

	helpers := map[string]func(name string) fx.Option{
		"ProvideServer": func(name string) fx.Option {
			return httpserver.ProvideServer(name, func() (httpserver.Spec, error) {
				return httpserver.Spec{}, errors.New("never built")
			})
		},
		"ProvideMiddleware": func(name string) fx.Option {
			return httpserver.ProvideMiddleware(name, func() httpserver.Middleware { return nil })
		},
		"ProvideRunner": httpserver.ProvideRunner,
	}

	for helper, option := range helpers {
		for _, name := range []string{"", `a"b`, "a b", "a:b", "café"} {
			t.Run(helper+"/"+name, func(t *testing.T) {
				t.Parallel()

				err := run(t, shared(slog.New(slog.DiscardHandler), configs{}, option(name)), idle)

				require.ErrorIs(t, err, httpserver.ErrInvalidName)
				assert.Contains(t, err.Error(), fmt.Sprintf("%q", name))
			})
		}
	}
}

func TestValidNamesAreAccepted(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	require.NoError(t, run(t,
		shared(slog.New(slog.DiscardHandler), configs{"API_v2.internal-1": plain()},
			provide("API_v2.internal-1", &calls),
		),
		httpserver.ProvideRunner("API_v2.internal-1"),
		probe(func(context.Context, servers) error { return nil }),
	))
}

func TestProvideServerAndMiddlewareForwardAnnotations(t *testing.T) {
	t.Parallel()

	tr := &trail{}

	err := run(t,
		httpserver.Module,
		fx.Supply(slog.New(slog.DiscardHandler)),
		fx.Supply(fx.Annotate(plain(), fx.ResultTags(`name:"api"`))),
		fx.Supply(fx.Annotate("tagged", fx.ResultTags(`name:"label"`))),
		httpserver.ProvideServer("api", func(c httpserver.Config) httpserver.Spec {
			return httpserver.Spec{Config: c, Handler: ok}
		}, fx.ParamTags(`name:"api"`)),
		httpserver.ProvideMiddleware("api", func(label string) step {
			return step{tr, label}
		}, fx.ParamTags(`name:"label"`)),
		httpserver.ProvideRunner("api"),
		probe(func(ctx context.Context, s servers) error {
			_, _, err := fetch(ctx, s.API.Addr())

			return err
		}),
	)

	require.NoError(t, err)
	assert.Equal(t, []string{"tagged"}, tr.all())
}

// statusWriter remembers the status a handler wrote.
type statusWriter struct {
	http.ResponseWriter

	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

// recording decorates the app's Provider, as an app adding metrics would: it wraps every server's handler to record
// each response's status in tr, and passes the build's options on.
func recording(tr *trail) fx.Option {
	return fx.Decorate(func(p httpserver.Provider) httpserver.Provider {
		return func(cfg httpserver.Config, h http.Handler, opts ...httpserver.Option) (*httpserver.Server, error) {
			return p(cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
				h.ServeHTTP(sw, r)
				tr.add(strconv.Itoa(sw.status))
			}), opts...)
		}
	})
}

func TestADecorationOfTheProviderReachesEveryServer(t *testing.T) {
	t.Parallel()

	tr := &trail{}

	var calls atomic.Int32

	err := run(t,
		shared(slog.New(slog.DiscardHandler), configs{"api": plain(), "admin": plain()},
			provide("api", &calls),
			provide("admin", &calls),
			recording(tr),
		),
		httpserver.ProvideRunner("api"),
		httpserver.ProvideRunner("admin"),
		probe(func(ctx context.Context, s servers) error {
			if _, _, err := fetch(ctx, s.API.Addr()); err != nil {
				return err
			}

			_, _, err := fetch(ctx, s.Admin.Addr())

			return err
		}),
	)

	require.NoError(t, err)
	assert.Equal(t, []string{"200", "200"}, tr.all(), "one request to each server")
}

func TestADecorationWrapsOutsideTheMiddleware(t *testing.T) {
	t.Parallel()

	tr := &trail{}

	var calls atomic.Int32

	err := run(t,
		shared(slog.New(slog.DiscardHandler), configs{"api": plain()},
			provide("api", &calls),
			httpserver.ProvideMiddleware("api", func() httpserver.Middleware {
				return httpserver.MiddlewareFunc(func(w http.ResponseWriter, _ *http.Request, _ http.Handler) {
					w.WriteHeader(http.StatusUnauthorized)
				})
			}),
			recording(tr),
		),
		httpserver.ProvideRunner("api"),
		probe(func(ctx context.Context, s servers) error {
			status, _, err := fetch(ctx, s.API.Addr())
			assert.Equal(t, http.StatusUnauthorized, status)

			return err
		}),
	)

	require.NoError(t, err)
	assert.Equal(t, []string{"401"}, tr.all(), "the decoration saw the request the middleware answered")
}

func TestTheNameSurvivesADecoration(t *testing.T) {
	t.Parallel()

	rec := newRecorder()

	var calls atomic.Int32

	err := run(t,
		shared(slog.New(rec), configs{"api": plain()}, provide("api", &calls), recording(&trail{})),
		httpserver.ProvideRunner("api"),
		probe(func(context.Context, servers) error { return nil }),
	)

	require.NoError(t, err)

	listening, found := rec.find("http server listening")
	require.True(t, found)
	assert.Equal(t, "api", listening.Attrs["server"])
}
