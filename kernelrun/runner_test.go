package kernelrun_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"

	"github.com/sikhlana/pkg/kernelrun"
)

func TestProvideRunnerTakesAGraphValue(t *testing.T) {
	var got string

	err := run(t, kernelrun.Options{},
		fx.Supply("from the graph"),
		kernelrun.ProvideRunner("api", func(s string) kernelrun.Func {
			return func(context.Context) error {
				got = s

				return nil
			}
		}),
	)

	require.NoError(t, err)
	assert.Equal(t, "from the graph", got)
}

func TestWithRunnerRunsTheValue(t *testing.T) {
	var ran bool

	err := run(t, kernelrun.Options{}, kernelrun.WithRunner("api", func(context.Context) error {
		ran = true

		return nil
	}))

	require.NoError(t, err)
	assert.True(t, ran)
}

func TestProvideRunnerTwiceInOneApp(t *testing.T) {
	ran := make(chan string, 2)

	runner := func(name string) kernelrun.Func {
		return func(ctx context.Context) error {
			ran <- name

			return waitDone(ctx)
		}
	}

	err := run(t, kernelrun.Options{},
		kernelrun.ProvideRunner("api", func() kernelrun.Func { return runner("api") }),
		kernelrun.ProvideRunner("worker", func() kernelrun.Func { return runner("worker") }),
		kernelrun.WithRunner("done", returnNil),
	)

	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"api", "worker"}, []string{<-ran, <-ran})
}

func TestProvideRunnerForwardsAnnotations(t *testing.T) {
	var got string

	err := run(t, kernelrun.Options{},
		fx.Supply(fx.Annotate("hello", fx.ResultTags(`name:"greeting"`))),
		kernelrun.ProvideRunner("api", func(s string) kernelrun.Func {
			return func(context.Context) error {
				got = s

				return nil
			}
		}, fx.ParamTags(`name:"greeting"`)),
	)

	require.NoError(t, err)
	assert.Equal(t, "hello", got)
}

func TestProvideRunnerRejectsARepeatedResultTag(t *testing.T) {
	err := run(t, kernelrun.Options{},
		kernelrun.ProvideRunner("api", func() kernelrun.Func { return returnNil }, fx.ResultTags(`name:"mine"`)),
	)

	require.Error(t, err)
}
