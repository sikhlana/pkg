package httpserver

// Option sets a per-build setting of a Provider.
type Option func(*options)

// options holds the settings a Provider's Options set for one build.
type options struct {
	name string
}

// WithName names the server: every record it logs carries the name as the server attribute, net/http's error reports
// and tlsconfig's key pair records included. An empty name adds nothing.
func WithName(name string) Option {
	return func(o *options) {
		o.name = name
	}
}
