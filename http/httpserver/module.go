package httpserver

import (
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"go.uber.org/fx"
)

// builtServer names a ProvideServer constructor's result inside the private module that wraps it in its middleware.
const builtServer = `name:"httpserver:server:built"`

// serverNames and middlewareTargets carry the names ProvideServer and ProvideMiddleware were given, for Module's
// check. They're strings, so collecting them builds nothing.
const (
	serverNames       = `group:"httpserver:server_names"`
	middlewareTargets = `group:"httpserver:middleware_targets"`
)

// ErrUnknownServer fails startup for a ProvideMiddleware or ProvideRunner naming no server ProvideServer provided. The
// error carries the name.
var ErrUnknownServer = errors.New("unknown server")

// Module provides the Provider, over the app's *slog.Logger, and checks at startup that every name given to
// ProvideMiddleware matches a ProvideServer. The check reads names only, so it builds no server or middleware: a
// server is built only when something injects it, such as ProvideRunner.
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

// NameTag returns the fx name tag ProvideServer provides the server name under, for injecting it with fx.ParamTags or
// a name struct tag.
func NameTag(name string) string {
	return fmt.Sprintf(`name:"httpserver:server:%s"`, name)
}

// MiddlewareGroup returns the fx group tag the server name collects its middleware from. ProvideMiddleware registers
// into it; a constructor annotated with fx.As(new(Middleware)) and this tag joins it too.
func MiddlewareGroup(name string) string {
	return fmt.Sprintf(`group:"httpserver:middleware:%s"`, name)
}

// ProvideServer provides the *Server that constructor builds as the server name, injectable under NameTag(name).
// constructor can take any parameters fx can supply, typically the Provider, a Config and a handler, and returns a
// *Server and optionally an error. The server's handler is wrapped in the middleware in MiddlewareGroup(name), ordered
// as Chain orders them, and its records carry the name as the server attribute.
//
// Providing a server doesn't run it, and nothing is built until something injects it: list ProvideRunner(name) in
// the commands that serve it. A name provided twice fails startup.
func ProvideServer(name string, constructor any) fx.Option {
	return fx.Module("httpserver",
		fx.Provide(fx.Private, fx.Annotate(constructor, fx.ResultTags(builtServer))),
		fx.Provide(fx.Annotate(
			func(s *Server, mws []Middleware) *Server {
				s.handler = Chain(s.handler, mws...)
				s.logger = s.logger.With(slog.String("server", name))

				return s
			},
			fx.ParamTags(builtServer, MiddlewareGroup(name)),
			fx.ResultTags(NameTag(name)),
		)),
		fx.Supply(fx.Annotate(name, fx.ResultTags(serverNames))),
	)
}

// ProvideMiddleware adds the middleware constructor builds to the server name's chain. constructor can take any
// parameters fx can supply, and returns any type that implements Middleware, optionally followed by an error. It's
// called only when the server is built. Naming a server no ProvideServer provides fails startup with
// ErrUnknownServer.
func ProvideMiddleware(name string, constructor any) fx.Option {
	return fx.Options(
		fx.Provide(fx.Annotate(constructor, fx.As(new(Middleware)), fx.ResultTags(MiddlewareGroup(name)))),
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
