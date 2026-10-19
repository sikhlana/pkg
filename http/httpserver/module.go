package httpserver

import (
	"fmt"
	"slices"

	"go.uber.org/fx"
)

// serverNames and middlewareTargets carry the names ProvideServer and ProvideMiddleware were given, for Module's
// check. They're strings, so collecting them builds nothing.
const (
	serverNames       = `group:"httpserver:server_names"`
	middlewareTargets = `group:"httpserver:middleware_targets"`
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
