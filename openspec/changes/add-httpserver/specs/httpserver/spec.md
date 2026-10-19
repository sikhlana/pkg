## Purpose

Serves HTTP for an fx app: named servers provided once for every command, bound and served only by the commands that run them, with operator-configured addresses, timeouts and rotating TLS, a per-server middleware chain, and graceful shutdown.

## ADDED Requirements

### Requirement: Config defaults and validation

A server's config SHALL require an address in host:port form, where the host MAY be empty and the port MAY be 0 to pick a free port. Each timeout (read, read header, write, idle and shutdown) SHALL fall back to its default when zero: 5s, 1s, 10s, 60s and 10s. A negative timeout SHALL make the config invalid with an error matching an exported sentinel that names the field. The TLS section SHALL be validated as tlsconfig defines for a server, and its problems SHALL be reported under the config's own key. A config loaded through configloader SHALL be validated at startup.

#### Scenario: Zero timeouts use the defaults
- **WHEN** a server is built from a config that sets only an address
- **THEN** it serves with a 5s read timeout, a 1s read header timeout, a 10s write timeout, a 60s idle timeout and a 10s shutdown timeout

#### Scenario: A missing address
- **WHEN** an app loads a server config with no address
- **THEN** startup fails, naming the address field

#### Scenario: A negative timeout
- **WHEN** a config sets `shutdown_timeout` to `-1s`
- **THEN** validating it fails with an error matching the negative-timeout sentinel, naming `shutdown_timeout`

#### Scenario: An incomplete TLS section
- **WHEN** a config enables TLS without a key file
- **THEN** validating it fails with an error matching tlsconfig's missing-key-pair sentinel

### Requirement: Building a server binds nothing

Building a server SHALL NOT open a socket, and SHALL fail for a nil handler rather than fall back to a default one. Building it SHALL read its TLS files once when TLS is enabled, and a key pair that can't be loaded SHALL fail the build with tlsconfig's error. The server SHALL bind only when told to listen, and SHALL serve only on the listener it bound.

#### Scenario: Built, not listening
- **WHEN** a server is built for address `127.0.0.1:0` and never told to listen
- **THEN** no socket is open for it

#### Scenario: An unreadable key pair
- **WHEN** a server is built with TLS enabled and a key file that doesn't exist
- **THEN** the build fails with an error matching `fs.ErrNotExist`

#### Scenario: No handler
- **WHEN** a server is built with a nil handler
- **THEN** the build fails with an error matching the nil-handler sentinel

#### Scenario: Serving before listening
- **WHEN** a server is told to serve without having been told to listen
- **THEN** serving fails with an error matching the not-listening sentinel, and no socket is opened

#### Scenario: Listening twice
- **WHEN** a server that is listening is told to listen again
- **THEN** the second call fails with an error matching the already-listening sentinel, and the first listener keeps serving

### Requirement: Servers are named and run only where a command opts in

Under fx, every server SHALL be provided under a name, and a server provided under a name SHALL be injectable by that name. Providing a server SHALL NOT run it. A command SHALL run a named server only when it opts in to that name, and in a command that neither runs nor injects a named server, that server's constructor, its middleware constructors and its TLS files SHALL NOT be touched. A server that is built but not run SHALL NOT bind a socket. A running server's runner SHALL be named `httpserver:<name>`.

#### Scenario: Provided for every command, run by one
- **WHEN** servers `api` and `admin` are provided in options shared by every command, the `serve` command opts in to both, and the `migrate` command opts in to neither
- **THEN** `serve` binds and serves both, and `migrate` builds neither and binds nothing

#### Scenario: Not built when not needed
- **WHEN** server `api` has TLS enabled with a key file that doesn't exist, and a command neither runs nor injects `api`
- **THEN** that command starts and runs normally

#### Scenario: Injected, not run
- **WHEN** a command injects server `api` without opting in to run it
- **THEN** `api` is built and no socket is bound for it

#### Scenario: The runner's name
- **WHEN** the running server `api` fails while serving
- **THEN** the app's error names the runner `httpserver:api`

### Requirement: Binding happens at start

A server a command runs SHALL bind its address while the app starts, before any runner runs. A bind failure SHALL fail startup with an error naming the server and the address. Once the app has started, every server it runs SHALL be accepting connections. A server that bound but never served, because a later step of startup failed, SHALL release its socket when the app stops.

#### Scenario: Accepting once started
- **WHEN** a command runs server `api` on `127.0.0.1:0` and the app finishes starting
- **THEN** a connection to the server's bound address succeeds

#### Scenario: The address is taken
- **WHEN** a command runs a server on an address another socket already holds
- **THEN** startup fails with an error naming the server and the address, and no runner runs

#### Scenario: A later start step fails
- **WHEN** a server binds and a start hook after it fails
- **THEN** startup fails and the server's socket is released

### Requirement: Graceful shutdown bounded by shutdown_timeout

When a running server's context ends, it SHALL stop accepting new connections, let in-flight requests finish for up to its shutdown timeout, and then return cleanly. Requests still in flight when the shutdown timeout passes SHALL have their connections closed, and serving SHALL return an error matching `context.DeadlineExceeded`. A server that fails while serving SHALL return that failure, which stops every other runner in the command.

#### Scenario: Draining finishes in time
- **WHEN** a request is in flight, the server's context ends, and the request finishes within the shutdown timeout
- **THEN** the request completes, new connections are refused, and serving returns without an error

#### Scenario: Draining runs out of time
- **WHEN** a request is still in flight when the shutdown timeout passes
- **THEN** its connection is closed and serving returns an error matching `context.DeadlineExceeded`

#### Scenario: One server failing stops the other
- **WHEN** a command runs servers `api` and `admin`, and `admin`'s listener fails while serving
- **THEN** `api` shuts down gracefully and the app's error names `httpserver:admin`

### Requirement: TLS through tlsconfig, with HTTP/2

With TLS enabled, a server SHALL serve TLS using the `tls.Config` tlsconfig builds from its TLS section, so key pairs and client CAs rotate and client certificates are checked as tlsconfig defines. It SHALL offer HTTP/2 and HTTP/1.1 through ALPN. With TLS disabled, it SHALL serve plaintext HTTP/1.1 and SHALL NOT offer cleartext HTTP/2.

#### Scenario: HTTP/2 over TLS
- **WHEN** a client offering `h2` and `http/1.1` connects to a server with TLS enabled
- **THEN** the connection negotiates HTTP/2

#### Scenario: HTTP/1.1 over TLS
- **WHEN** a client offering only `http/1.1` connects to a server with TLS enabled
- **THEN** the request is served over HTTP/1.1

#### Scenario: A rotated key pair
- **WHEN** a running server's key pair files are replaced with a new valid pair and the reload interval passes
- **THEN** a new connection is served with the new certificate

#### Scenario: Plaintext
- **WHEN** a client connects to a server with TLS disabled
- **THEN** the request is served over plaintext HTTP/1.1

### Requirement: Middleware runs in order around the handler

A named server's handler SHALL be wrapped in every middleware provided for that name, and in no other server's. Middleware SHALL run from the lowest order, outermost, to the highest, innermost, with the handler inside them all. A middleware that declares an order SHALL use it, one wrapped with an order at wiring time SHALL use that order instead of its own, and any other SHALL count as order 0. Middleware with equal orders SHALL run in an unspecified relative order, and SHALL NOT fail startup. Each middleware SHALL receive, as the next handler, the rest of the chain for its position: built once, the same for every request, and safe for concurrent use. A middleware that doesn't call the next handler SHALL end the request there. A middleware constructor SHALL run only when its server is built. The same ordering SHALL apply to a chain built by hand, outside fx.

#### Scenario: Lower orders run outer
- **WHEN** server `api` has middleware with orders 200, 10 and 100, each recording its order before calling the next handler
- **THEN** a request records 10, 100, 200, then reaches the handler

#### Scenario: Undeclared order counts as zero
- **WHEN** server `api` has a middleware that declares no order and one with order 5
- **THEN** a request passes through the undeclared one first

#### Scenario: An order set at wiring time
- **WHEN** a middleware that declares order 100 is wrapped with order 1 for server `api`, beside a middleware with order 50
- **THEN** a request passes through the wrapped one first

#### Scenario: Equal orders
- **WHEN** server `api` has two middleware that both declare no order
- **THEN** the app starts, and every request passes through both before reaching the handler

#### Scenario: Middleware stays with its server
- **WHEN** a middleware is provided for server `admin` only
- **THEN** requests to `api` don't pass through it

#### Scenario: Short-circuiting
- **WHEN** a middleware writes a 401 response without calling the next handler
- **THEN** the client receives the 401 and the handler and later middleware don't run

#### Scenario: Built once
- **WHEN** a middleware records the next handler it receives across many concurrent requests
- **THEN** it receives the same next handler every time

### Requirement: Names that match no server fail startup

Providing middleware for a name, or opting a command in to running a name, when no server is provided under that name SHALL fail startup with an error matching the unknown-server sentinel that names it. The check SHALL NOT build any server or middleware.

#### Scenario: A misspelled middleware name
- **WHEN** a server is provided as `api` and middleware is provided for `apii`
- **THEN** startup fails with an error matching the unknown-server sentinel, naming `apii`

#### Scenario: A misspelled runner name
- **WHEN** a server is provided as `api` and a command opts in to running `apii`
- **THEN** startup fails with an error matching the unknown-server sentinel, naming `apii`

#### Scenario: The check builds nothing
- **WHEN** a command neither runs nor injects any server, and every name used matches a provided server
- **THEN** startup succeeds without building any server or middleware

### Requirement: Logging through the injected logger

A server SHALL log through the injected logger, with the server's name on every record it logs under fx. It SHALL log at Info when it starts listening, with the bound address, and when it starts draining. net/http's own error reports, such as a failed TLS handshake, SHALL be logged at Debug through the injected logger, and SHALL NOT be written through the standard library's `log` package.

#### Scenario: Listening
- **WHEN** server `api` binds `127.0.0.1:0`
- **THEN** an Info record names the server `api` and the bound address with its real port

#### Scenario: A failed handshake
- **WHEN** a client sends plaintext to a server with TLS enabled
- **THEN** a Debug record from the injected logger names the server and the handshake error, and nothing is written to the standard library's log output
