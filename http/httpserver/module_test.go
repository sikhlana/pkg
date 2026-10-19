package httpserver_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"

	"github.com/sikhlana/pkg/http/httpserver"
)

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
