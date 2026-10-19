package kernelrun

import (
	"context"

	"go.uber.org/fx"
)

// RunnerGroup is the fx group tag Run collects Runners from. WithRunner and ProvideRunner register into it.
const RunnerGroup = `group:"kernelrun:runner"`

// builtRunner names a ProvideRunner constructor's result inside the private module that pairs it with its name.
// builtParams carries the same tag.
const builtRunner = `name:"kernelrun:runner::built"`

// Func is a runner's body. It runs until its work is done or ctx is done, and its return, whatever it returns, stops
// the app. Returning nil, or ctx's error once ctx is done, is a clean exit; any other error is a crash.
type Func func(ctx context.Context) error

// Runner is a named Func, as Run collects it from RunnerGroup. The name identifies the runner in errors and logs, so
// it must be non-empty and unique within an app.
type Runner struct {
	Name string
	Func Func
}

// builtParams is the input of the adapter ProvideRunner registers to pair a built Func with its name.
type builtParams struct {
	fx.In

	Func Func `name:"kernelrun:runner::built"`
}

// WithRunner registers f as the runner name.
func WithRunner(name string, f Func) fx.Option {
	return fx.Supply(fx.Annotate(Runner{Name: name, Func: f}, fx.ResultTags(RunnerGroup)))
}

// ProvideRunner registers the Func that constructor builds from the graph as the runner name. The constructor can
// take any parameters fx can supply, and its first result must have the Func type. anns are applied to constructor
// after ProvideRunner's own result tag, for its inputs and hooks: fx.ParamTags to take a tagged dependency, fx.From,
// fx.OnStart, fx.OnStop. One that tags or converts the result conflicts with ProvideRunner's and fails startup.
func ProvideRunner(name string, constructor any, anns ...fx.Annotation) fx.Option {
	return fx.Module(
		"kernelrun",
		fx.Provide(
			fx.Private,
			fx.Annotate(constructor, append([]fx.Annotation{fx.ResultTags(builtRunner)}, anns...)...),
		),
		fx.Provide(fx.Annotate(
			func(p builtParams) Runner {
				return Runner{Name: name, Func: p.Func}
			},
			fx.ResultTags(RunnerGroup),
		)),
	)
}
