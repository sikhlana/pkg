package httpserver

import (
	"errors"
	"fmt"
	"net/http"
	"slices"

	"go.uber.org/fx"
)

// builtSpec names a ProvideServer constructor's result inside the private module that builds the server from it.
const builtSpec = `name:"httpserver:spec::built"`

// serverNames and middlewareTargets carry the names ProvideServer and ProvideMiddleware were given, for Module's
// check. They're strings, so collecting them builds nothing.
const (
	serverNames       = `group:"httpserver:server_names"`
	middlewareTargets = `group:"httpserver:middleware_targets"`
)

var (
	// ErrUnknownServer fails startup for a ProvideMiddleware or ProvideRunner naming no server ProvideServer provided.
	// The error carries the name.
	ErrUnknownServer = errors.New("unknown server")

	// ErrInvalidName fails startup for a ProvideServer, ProvideMiddleware or ProvideRunner given an empty name, or one
	// with a character other than a letter, a digit, "-", "_" or ".". The error carries the name.
	ErrInvalidName = errors.New("invalid server name")
)

// Module provides the Provider, over the app's *slog.Logger when it has one, and checks at startup that every name
// given to ProvideMiddleware matches a ProvideServer. The check reads names only, so it builds no server or
// middleware: a server is built only when something injects it, such as ProvideRunner.
var Module = fx.Module("httpserver",
	fx.Provide(NewProvider),
	fx.Invoke(checkNames),
)

// namesParams is the input of Module's name check.
type namesParams struct {
	fx.In

	Servers []string `group:"httpserver:server_names"`
	Targets []string `group:"httpserver:middleware_targets"`
}

// Spec is what a ProvideServer constructor says the server serves: its Config and its handler.
type Spec struct {
	Config  Config
	Handler http.Handler
}

// NameTag returns the fx name tag ProvideServer provides the server name under, for injecting it with fx.ParamTags or
// a name struct tag.
func NameTag(name string) string {
	return fmt.Sprintf(`name:"httpserver:server:%s"`, name)
}

// MiddlewareGroup returns the fx group tag the server name collects its middleware from, for injecting a server's
// middleware elsewhere. Add to it with ProvideMiddleware: a constructor registered into the group directly isn't
// checked against the provided servers, so a misspelled name is silently never served.
func MiddlewareGroup(name string) string {
	return fmt.Sprintf(`group:"httpserver:middleware:%s"`, name)
}

// ProvideServer provides the server name, injectable under NameTag(name), built from the Spec constructor returns.
// constructor can take any parameters fx can supply, and returns a Spec, optionally followed by an error. ProvideServer
// wraps the Spec's handler in the middleware in MiddlewareGroup(name), ordered as Chain orders them, then builds the
// server by calling the app's Provider with WithName(name), so the name is on every record the server logs. A
// decoration of the Provider applies to the server, and a handler it wraps already runs the middleware. A Spec with a
// nil handler fails startup with an error matching ErrNilHandler that names the server.
//
// Providing a server doesn't run it, and nothing is built until something injects it: list ProvideRunner(name) in
// the commands that serve it. A name provided twice fails startup, and an invalid one with ErrInvalidName.
//
// anns are applied to constructor after ProvideServer's own result tag, for its inputs and hooks: fx.ParamTags to
// take a tagged dependency, fx.From, fx.OnStart, fx.OnStop. One that tags or converts the result fails startup.
func ProvideServer(name string, constructor any, anns ...fx.Annotation) fx.Option {
	if err := checkName(name); err != nil {
		return fx.Error(err)
	}

	return fx.Module(
		"httpserver",
		fx.Provide(
			fx.Private,
			fx.Annotate(constructor, append([]fx.Annotation{fx.ResultTags(builtSpec)}, anns...)...),
		),
		fx.Provide(fx.Annotate(
			func(spec Spec, p Provider, mws []Middleware) (*Server, error) {
				if spec.Handler == nil {
					return nil, fmt.Errorf("%w: server %q", ErrNilHandler, name)
				}

				return p(spec.Config, Chain(spec.Handler, mws...), WithName(name))
			},
			fx.ParamTags(builtSpec, "", MiddlewareGroup(name)),
			fx.ResultTags(NameTag(name)),
		)),
		fx.Supply(fx.Annotate(name, fx.ResultTags(serverNames))),
	)
}

// ProvideMiddleware adds the middleware constructor builds to the server name's chain. constructor can take any
// parameters fx can supply, and returns any type that implements Middleware, optionally followed by an error. It's
// called only when the server is built. Naming a server no ProvideServer provides fails startup with
// ErrUnknownServer, and an invalid name with ErrInvalidName. A constructor that returns a nil Middleware, as Chain
// describes it, leaves its middleware out, for a condition known only when the server is built. anns are applied to
// constructor after ProvideMiddleware's own, for its inputs and hooks: fx.ParamTags to take a tagged dependency,
// fx.From, fx.OnStart, fx.OnStop. One that tags or converts the result fails startup.
func ProvideMiddleware(name string, constructor any, anns ...fx.Annotation) fx.Option {
	if err := checkName(name); err != nil {
		return fx.Error(err)
	}

	return fx.Options(
		fx.Provide(fx.Annotate(
			constructor,
			append([]fx.Annotation{fx.As(new(Middleware)), fx.ResultTags(MiddlewareGroup(name))}, anns...)...,
		)),
		fx.Supply(fx.Annotate(name, fx.ResultTags(middlewareTargets))),
	)
}

// checkNames fails with ErrUnknownServer for the first middleware target, in sorted order, that names no server.
func checkNames(p namesParams) error {
	for _, target := range slices.Compact(slices.Sorted(slices.Values(p.Targets))) {
		if !slices.Contains(p.Servers, target) {
			return fmt.Errorf("%w: %q", ErrUnknownServer, target)
		}
	}

	return nil
}

// checkName returns ErrInvalidName for an empty name, or one with a character other than an ASCII letter, a digit,
// "-", "_" or ".".
func checkName(name string) error {
	valid := name != ""

	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
		default:
			valid = false
		}
	}

	if !valid {
		return fmt.Errorf("%w: %q", ErrInvalidName, name)
	}

	return nil
}
