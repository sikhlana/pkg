package httpserver

import (
	"context"
	"fmt"

	"go.uber.org/fx"

	"github.com/sikhlana/pkg/kernelrun"
)

// ProvideRunner opts the command it's listed in in to running the server name, as the kernelrun runner
// "httpserver:<name>". The server binds its address while the app starts, so a bind failure fails startup and a
// started app is accepting connections; it serves until the runner's context ends, then drains; and a listener it
// bound but never served, because a later start step failed, is released when the app stops. Naming a server no
// ProvideServer provides fails startup with ErrUnknownServer, and an invalid name with ErrInvalidName.
func ProvideRunner(name string) fx.Option {
	if err := checkName(name); err != nil {
		return fx.Error(err)
	}

	return kernelrun.ProvideRunner("httpserver:"+name, func(lc fx.Lifecycle, s *Server) (kernelrun.Func, error) {
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
	}, fx.ParamTags("", NameTag(name)+` optional:"true"`))
}
