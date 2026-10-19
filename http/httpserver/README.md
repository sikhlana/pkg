# http/httpserver

Named HTTP servers for an fx app. An app declares its servers once, for every command, and each command opts in to the ones it runs. A server binds its port while the app starts, serves until the app stops, then drains in-flight requests. Each server has its own ordered middleware chain, and TLS comes from [tls/tlsconfig](../../tls/tlsconfig/README.md), with rotating key pairs and HTTP/2.

## Configuration

One Config per server. configloader validates it when it loads it; the Provider validates the Config it's handed too.

| Field | Type | Description |
|-------|------|-------------|
| host | string | The hostname or IP address to listen on, IPv6 written bare (::1). Empty listens on every interface. |
| port | int | The TCP port, from 0 to 65535. 0 picks a free port, which Server.Addr reports once bound, so set it explicitly in production: a forgotten port listens somewhere random rather than failing. |
| read_timeout | duration | Bounds reading a whole request, body included. Zero means 5s. |
| read_header_timeout | duration | Bounds reading a request's headers. Zero means 1s. With TLS it usually bounds the handshake too; see below. |
| write_timeout | duration | Bounds writing a response, from the end of the request's headers. Zero means 10s. |
| idle_timeout | duration | Bounds the wait for a keep-alive connection's next request. Zero means 60s. |
| shutdown_timeout | duration | Bounds draining in-flight requests once the server is told to stop. Zero means 10s. See the note on shutdown below. |
| tls | tlsconfig.ServerConfig | Serves HTTPS when enabled, offering HTTP/2 and HTTP/1.1 through ALPN, and re-reads the key pair and client CAs as they rotate. Its problems are reported under tls. See [tls/tlsconfig](../../tls/tlsconfig/README.md) for its fields. |

A negative timeout fails validation with ErrNegativeTimeout under its key. Plaintext servers speak HTTP/1.1 only; cleartext HTTP/2 (h2c) isn't served.

**Timeouts can't be turned off.** Zero means the default, so every server bounds reads and writes. A handler that streams (server-sent events, long polling) or takes large uploads extends its own deadlines per request with `http.ResponseController`'s `SetWriteDeadline` and `SetReadDeadline`, leaving the rest of the server protected from slow clients.

**TLS handshakes share the shortest timeout.** net/http bounds each TLS handshake by the smallest of read_header_timeout, read_timeout and write_timeout, which with the defaults is read_header_timeout's 1s. A handshake that takes longer, from a client on a high-latency link or with client certificates to verify, fails with an i/o timeout that's logged only at Debug. Raise read_header_timeout when clients are far away or connections fail during the handshake.

**Shutdown.** When a server's runner is cancelled it stops accepting connections and waits up to shutdown_timeout for in-flight requests, whose contexts carry the runner context's values but aren't cancelled with it. Connections still active then are closed, and the runner returns an error matching ErrShutdownTimeout, which kernelrun reports as a crash: requests were cut, and the process exits non-zero. Keep shutdown_timeout below your orchestrator's grace period (Kubernetes' terminationGracePeriodSeconds, 30s by default), so the drain finishes before a SIGKILL.

## Exported Members

| Name | Description |
|------|-------------|
| Module | Provides the Provider and checks at startup that every ProvideMiddleware names a provided server. Builds no server itself. Takes the app's *slog.Logger when there is one. |
| ProvideServer | Provides the *Server a constructor builds under a name, wrapped in that name's middleware. The constructor's Provider logs with the name as the server attribute, so the server's, net/http's and tlsconfig's records carry it. Built only when something injects it. A name provided twice fails startup, a nil *Server fails it with ErrNilServer, and an invalid name with ErrInvalidName. The constructor returns a server of its own: its handler is wrapped in place. |
| ProvideRunner | Opts a command in to running the named server as the kernelrun runner httpserver:name: binds in OnStart, serves in the runner, releases an unserved listener in OnStop. Fails startup with ErrUnknownServer for an unknown name and ErrInvalidName for an invalid one. |
| ProvideMiddleware | Adds the middleware a constructor builds to the named server's chain. The constructor returns any type implementing Middleware and runs only when the server is built. A constructor returning a nil Middleware leaves it out. Fails startup with ErrUnknownServer for an unknown name and ErrInvalidName for an invalid one. |
| NameTag | Returns the name tag a server is provided under: name:"httpserver:server:NAME". |
| MiddlewareGroup | Returns the group tag a server collects its middleware from, group:"httpserver:middleware:NAME", for injecting it elsewhere. Add to it with ProvideMiddleware: registering into the group directly skips the unknown-name check. |
| Provider | Builds a Server from a Config and a handler. Validates the Config, reads the TLS files once to build the TLS configuration, and fails for a nil handler or a key pair that can't be loaded. |
| ProviderParams | NewProvider's input: an optional Logger. |
| ProviderParams.Logger | Receives the servers' records, and net/http's own error reports, such as failed TLS handshakes, at Debug. Nil discards them. |
| NewProvider | Returns a Provider whose servers log through ProviderParams.Logger. |
| Server | An HTTP server split into Listen, Serve and Close. Building one opens no socket. Used once; safe for concurrent use. |
| Server.Listen | Binds the configured host and port and logs http server listening at Info with the bound address. Fails with ErrAlreadyListening once the server has bound, and with net.Listen's error, which names the address, when the bind fails. |
| Server.Serve | Serves on the bound listener, over TLS when enabled, until its context ends, then drains for up to shutdown_timeout. Requests' contexts carry its context's values, not its cancellation. Returns nil after a clean drain, an error matching ErrShutdownTimeout when the drain runs out of time, and a listener failure wrapped. Fails with ErrNotListening before Listen or after Close, and ErrAlreadyServing while serving. |
| Server.Close | Releases a listener Listen bound that Serve never took over. A no-op otherwise. |
| Server.Addr | Returns the bound address, with the real port, or nil before Listen. |
| Config | A server's operator settings. |
| Config.Validate | Returns ErrNegativeTimeout for each negative timeout and the tls settings' own errors, each under its config key. Host and port are checked by their validate tags. |
| DefaultReadTimeout | 5s, used when read_timeout is zero. |
| DefaultReadHeaderTimeout | 1s, used when read_header_timeout is zero. |
| DefaultWriteTimeout | 10s, used when write_timeout is zero. |
| DefaultIdleTimeout | 60s, used when idle_timeout is zero. |
| DefaultShutdownTimeout | 10s, used when shutdown_timeout is zero. |
| Middleware | Handles a request on its way to the handler: ServeHTTP(w, r, next). Calls next to pass it on, or writes a response and returns to end it there. next is built once per position in the chain, the same for every request. |
| OrderedMiddleware | A Middleware with Order() int. Lower orders run first, outermost. A Middleware without it counts as 0. Equal orders run in an unspecified relative order. |
| MiddlewareFunc | Adapts a function to a Middleware. |
| MiddlewareFunc.ServeHTTP | Calls the function. |
| Ordered | Returns a Middleware at a given order, replacing any order it declares. |
| Chain | Wraps a handler in middleware, lowest order outermost, keeping the given order for ties. For servers built without fx. Leaves out a nil Middleware, a nil pointer, func, map, slice or channel behind one, and either wrapped by Ordered. |
| ErrNegativeTimeout | A negative timeout. Carried under the timeout's key. |
| ErrNilHandler | Returned by a Provider handed a nil handler, rather than serving http.DefaultServeMux. |
| ErrNotListening | Server.Serve on a server that hasn't bound, or has been closed. |
| ErrAlreadyListening | Server.Listen on a server that has bound. |
| ErrAlreadyServing | Server.Serve on a server another call is serving. |
| ErrShutdownTimeout | Returned by Server.Serve when connections were still active once shutdown_timeout passed, and were closed. Deliberately doesn't match context.DeadlineExceeded, so kernelrun reports it as a crash however the run ended. |
| ErrNilServer | A ProvideServer constructor returned a nil *Server without an error. Carries the name. |
| ErrInvalidName | A ProvideServer, ProvideMiddleware or ProvideRunner name that's empty or holds a character other than a letter, a digit, -, _ or the full stop. Carries the name. |
| ErrUnknownServer | A ProvideMiddleware or ProvideRunner naming no provided server. Carries the name. |

## Usage

### Standalone

Build a server through a Provider, bind it, and serve it until the context ends. Close releases the listener if something fails before Serve takes it over:

```go
p := httpserver.NewProvider(httpserver.ProviderParams{Logger: logger})

s, err := p(httpserver.Config{Port: 8080}, httpserver.Chain(mux, recovery{logger: logger}, requestCounter{}))
if err != nil {
	return err
}

if err := s.Listen(ctx); err != nil {
	return err
}

defer s.Close()

return s.Serve(ctx)
```

### With fx

A module that serves HTTP owns its server. Its Config embeds `httpserver.Config`, squashed so the server's keys sit beside the module's own, and its `Module` provides the server with `ProvideServer` and a constructor that injects the Provider, the Config and the handler:

```go
package api

// Config is the API's operator settings, the server's among them.
type Config struct {
	httpserver.Config `mapstructure:",squash"`

	MaxUploadSize int64 `mapstructure:"max_upload_size" validate:"gte=0"`
}

var Module = fx.Module("api",
	fx.Provide(NewHandler),
	httpserver.ProvideServer("api", newServer),
)

type serverParams struct {
	fx.In

	Provider httpserver.Provider
	Config   Config
	Handler  *Handler
}

func newServer(p serverParams) (*httpserver.Server, error) {
	return p.Provider(p.Config.Config, p.Handler)
}
```

The app's config gives each module its section, and embeds `fx.Out` so configloader provides each one on its own. Each module's Config is its own type, so no name tags are needed:

```go
type Config struct {
	fx.Out

	API   api.Config   `mapstructure:"api"`
	Admin admin.Config `mapstructure:"admin"`
}
```

```toml
[api]
port = 8443
max_upload_size = 10485760

[api.tls]
enabled = true
cert_file = "/etc/notary/tls/tls.crt"
key_file = "/etc/notary/tls/tls.key"

[admin]
host = "127.0.0.1"
port = 9090
```

Loaded under `notary`, the env names follow the keys: `NOTARY_API_PORT`, `NOTARY_API_MAX_UPLOAD_SIZE`, `NOTARY_API_TLS_CERT_FILE`.

Embedding `httpserver.Config` gives `api.Config` its `Validate` method, so configvalidator calls it for the whole `api` section and looks no deeper for other `Validate` methods; tags on every field still run. When the module's Config holds another section with its own `Validate`, give the Config a `Validate` that checks both (see configvalidator's [Embedding a Validator](../../config/configvalidator/README.md#embedding-a-validator)):

```go
func (c Config) Validate() error {
	return errors.Join(
		configvalidator.ValidateField(&c, &c.Config),
		configvalidator.ValidateField(&c, &c.Storage),
	)
}
```

The app lists the modules once, in `kernel.Options.Shared`, and each command lists `ProvideRunner` for the servers it runs. A command that neither runs nor injects a server never builds it, its middleware or its TLS configuration, and never binds its port:

```go
app := kernel.New(kernel.Options{
	Format: log.FormatJSON,
	Config: func(path string) fx.Option {
		return configloader.Provide(configloader.Options[Config]{Name: "notary", Filename: path})
	},
	Shared: fx.Options(
		httpserver.Module,
		api.Module,
		admin.Module,
	),
})

cmd := app.RootCommand("notary", "Notarises documents")

cmd.AddCommand(
	app.Command("serve", "Serve the API and the admin endpoints",
		httpserver.ProvideRunner("api"),
		httpserver.ProvideRunner("admin"),
	),
	app.Command("serve-admin", "Serve the admin endpoints only", httpserver.ProvideRunner("admin")),
	app.Command("migrate", "Migrate the database", migrate.ProvideRunner()),
)
```

`serve` binds both ports before either runner starts, so a port in use fails startup naming the server and the address. If either server fails while serving, the other drains and the app's error names the failed runner, `httpserver:api` or `httpserver:admin`. Injecting a server elsewhere, under its `NameTag`, builds it without binding anything.

#### Middleware

`ProvideMiddleware` adds to one server's chain. Most middleware doesn't care where it runs and declares no order, counting as 0:

```go
type requestCounter struct {
	count *atomic.Int64
}

func (c requestCounter) ServeHTTP(w http.ResponseWriter, r *http.Request, next http.Handler) {
	c.count.Add(1)
	next.ServeHTTP(w, r)
}

httpserver.ProvideMiddleware("api", func(m *Metrics) requestCounter {
	return requestCounter{count: m.Requests}
})
```

A constructor whose middleware depends on a condition known only when the server is built, such as a config flag, returns nil to leave it out. The constructor has to return the `Middleware` interface for that, since fx keeps whatever it's given:

```go
httpserver.ProvideMiddleware("api", func(c api.Config) httpserver.Middleware {
	if !c.CORS.Enabled {
		return nil
	}

	return newCORS(c.CORS)
})
```

A nil pointer, func, map, slice or channel behind the interface is left out the same way, and so is either wrapped by `Ordered`.

Middleware that has to run at a particular place declares `Order() int`; lower runs outer. A recovery middleware belongs outermost: net/http reports a panicking handler only through its error log, which this module logs at Debug, so without one a panic shows up as a dropped connection and a Debug record:

```go
type recovery struct {
	logger *slog.Logger
}

func (recovery) Order() int {
	return -1000
}

func (m recovery) ServeHTTP(w http.ResponseWriter, r *http.Request, next http.Handler) {
	defer func() {
		v := recover()

		switch {
		case v == nil:
		case v == http.ErrAbortHandler:
			panic(v)
		default:
			m.logger.LogAttrs(r.Context(), slog.LevelError, "handler panicked", slog.Any("panic", v))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}
	}()

	next.ServeHTTP(w, r)
}

httpserver.ProvideMiddleware("api", func(logger *slog.Logger) recovery {
	return recovery{logger: logger}
})
```

Middleware with equal orders run in an unspecified order, which can change between starts, so give one an order when its place matters. `Ordered` sets or overrides the order at wiring time, for middleware you didn't write. Middleware written as `func(http.Handler) http.Handler`, as most of the ecosystem's is, adapts through `MiddlewareFunc`. The adapter calls the wrapper on every request, so do its setup once, outside, as otelhttp's `NewMiddleware` does:

```go
httpserver.ProvideMiddleware("api", func(tp trace.TracerProvider, prop propagation.TextMapPropagator) httpserver.Middleware {
	wrap := otelhttp.NewMiddleware("api", otelhttp.WithTracerProvider(tp), otelhttp.WithPropagators(prop))

	return httpserver.Ordered(-900, httpserver.MiddlewareFunc(
		func(w http.ResponseWriter, r *http.Request, next http.Handler) {
			wrap(next).ServeHTTP(w, r)
		},
	))
})
```

A `ProvideMiddleware` or `ProvideRunner` whose name matches no `ProvideServer` fails startup with `ErrUnknownServer`, without building anything.

#### Logging

Records go through the app's `*slog.Logger`, each carrying the server's name as `server`: `http server listening` at Info with the bound `address`, `http server draining` at Info when the runner is cancelled, and `http server stopped` at Debug after a clean drain. net/http's own error reports, such as TLS handshakes from scanners, are logged at Debug through the same logger and never reach the standard library's `log` package. tlsconfig logs key pair loads and rotations with their file paths, through the same named logger, so with several TLS servers each rotation names its server.
