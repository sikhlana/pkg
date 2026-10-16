package log_test

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"

	"github.com/sikhlana/pkg/log"
)

func TestAsDestinationTakesAConstructorOfAConcreteWriter(t *testing.T) {
	var logger *slog.Logger

	path := filepath.Join(t.TempDir(), "app.log")

	app := fxtest.New(t,
		log.Module,
		fx.Provide(log.AsDestination(func(lc fx.Lifecycle) (*os.File, error) {
			f, err := os.Create(path)
			if err != nil {
				return nil, err
			}

			lc.Append(fx.StopHook(f.Close))

			return f, nil
		})),
		fx.Populate(&logger),
	)
	app.RequireStart()

	logger.InfoContext(t.Context(), "to the file")

	app.RequireStop()

	b, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, []any{"to the file"}, values(decodeLines(t, b), slog.MessageKey))
}

func TestAsDestinationTakesAValue(t *testing.T) {
	var (
		buf    bytes.Buffer
		logger *slog.Logger
	)

	app := fxtest.New(t, log.Module, fx.Supply(log.AsDestination(&buf)), fx.Populate(&logger))
	app.RequireStart()

	logger.InfoContext(t.Context(), "to the buffer")

	app.RequireStop()

	assert.Equal(t, []any{"to the buffer"}, values(decodeLines(t, buf.Bytes()), slog.MessageKey))
}

func TestProvideMiddlewareTwiceInOneApp(t *testing.T) {
	var logger *slog.Logger

	rec := &recording{t: t}

	app := fxtest.New(t,
		log.Module,
		log.WithFormat(log.FormatText),
		log.WithTextHandler(rec.provide),
		log.ProvideMiddleware(20, func() log.Middleware {
			return adding(slog.String("step", "second"))
		}),
		fx.Module("nested", log.ProvideMiddleware(10, func() log.Middleware {
			return adding(slog.String("step", "first"))
		})),
		fx.Populate(&logger),
	)
	app.RequireStart()

	logger.InfoContext(t.Context(), "twice")

	app.RequireStop()

	assert.Equal(t, []string{"[step=first step=second]"}, rec.attrs())
}

func TestAsTextHandlerTakesAConstructor(t *testing.T) {
	var logger *slog.Logger

	rec := &recording{t: t}

	app := fxtest.New(t,
		log.Module,
		log.WithFormat(log.FormatText),
		fx.Provide(log.AsTextHandler(func() log.TextHandlerProvider {
			return rec.provide
		})),
		fx.Populate(&logger),
	)
	app.RequireStart()

	logger.InfoContext(t.Context(), "through the constructor")

	app.RequireStop()

	assert.Equal(t, []string{"through the constructor"}, messages(rec.all()))
}
