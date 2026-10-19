package httpserver

import (
	"context"
	"fmt"

	"go.uber.org/fx"

	"github.com/sikhlana/pkg/kernelrun"
)

// runnerServer is the server a ProvideRunner runs, or nil when none is provided under its name. kernelrun.ProvideRunner
// annotates its constructor itself, and fx can't annotate a constructor twice, so the name tag is resolved into this
// type first.
type runnerServer struct {
	server *Server
}

// ProvideRunner opts the command it's listed in in to running the server name, as the kernelrun runner
// "httpserver:<name>". The server binds its address while the app starts, so a bind failure fails startup and a
// started app is accepting connections; it serves until the runner's context ends, then drains; and a listener it
// bound but never served, because a later start step failed, is released when the app stops. Naming a server no
// ProvideServer provides fails startup with ErrUnknownServer.
func ProvideRunner(name string) fx.Option {
	return fx.Module("httpserver",
		fx.Provide(fx.Private, fx.Annotate(
			func(s *Server) runnerServer { return runnerServer{server: s} },
			fx.ParamTags(NameTag(name)+` optional:"true"`),
		)),
		kernelrun.ProvideRunner("httpserver:"+name, func(lc fx.Lifecycle, rs runnerServer) (kernelrun.Func, error) {
			s := rs.server

			if s == nil {
				return nil, fmt.Errorf("%w: %q", ErrUnknownServer, name)
			}

			lc.Append(fx.Hook{
				OnStart: func(ctx context.Context) error {
					if err := s.Listen(ctx); err != nil {
						return fmt.Errorf("binding server %q: %w", name, err)
					}

					return nil
				},
				OnStop: func(context.Context) error {
					return s.Close()
				},
			})

			return s.Serve, nil
		}),
	)
}
