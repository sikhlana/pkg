## MODIFIED Requirements

### Requirement: Servers are named and run only where a command opts in

Under fx, every server SHALL be provided under a name, and a server provided under a name SHALL be injectable by that name. Providing a server SHALL NOT run it. A command SHALL run a named server only when it opts in to that name, and in a command that neither runs nor injects a named server, that server's constructor, its middleware constructors and its TLS files SHALL NOT be touched. A server that is built but not run SHALL NOT bind a socket. A running server's runner SHALL be named `httpserver:<name>`. A server's constructor SHALL say what the server serves, its config and its handler, and the module SHALL build the server from them through the app's Provider; a constructor that gives no handler, or a nil pointer or function as one, SHALL fail startup with an error matching the nil-handler sentinel that names the server, and every other failure to build the server SHALL name it too. Server and middleware constructors SHALL accept annotations for their inputs and hooks, applied after the helper's own, so they can take dependencies provided under name tags.

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

#### Scenario: A constructor returns no server
- **WHEN** the constructor for server `api` gives a config and no handler, so no server can be built, and a command runs `api`
- **THEN** startup fails with an error matching the nil-handler sentinel, naming `api`

#### Scenario: Constructors take tagged dependencies
- **WHEN** server `api`'s constructor takes its config through an annotation naming the tag it's provided under, and a middleware constructor for `api` takes a string the same way
- **THEN** the server is built from that config and serves through that middleware

### Requirement: Logging through the injected logger

A server SHALL log through the injected logger, with the server's name on every record it logs when it has one, tlsconfig's records about its key pair and client CAs included. Under fx every server has its name; a server built by hand has one when it's given one. It SHALL log at Info when it starts listening, with the bound address, and when it starts draining. net/http's own error reports, such as a failed TLS handshake, SHALL be logged at Debug through the injected logger, and SHALL NOT be written through the standard library's `log` package.

#### Scenario: Listening
- **WHEN** server `api` binds `127.0.0.1:0`
- **THEN** an Info record names the server `api` and the bound address with its real port

#### Scenario: A failed handshake
- **WHEN** a client sends plaintext to a server with TLS enabled
- **THEN** a Debug record from the injected logger names the server and the handshake error, and nothing is written to the standard library's log output

#### Scenario: Key pair records name the server
- **WHEN** server `api` is built under fx with TLS enabled
- **THEN** tlsconfig's record of loading the key pair names the server `api`

#### Scenario: A server built by hand with a name
- **WHEN** a server is built without fx and given the name `admin`, then binds
- **THEN** its listening record names the server `admin`

## ADDED Requirements

### Requirement: A decoration of the Provider applies to every server

Every server the module builds under fx SHALL be built through the app's Provider as the app provides it, so a decoration of the Provider applies to every named server within its reach: one declared at the app's root, or in a module that encloses the server's registration. A decoration declared in a sibling module SHALL NOT apply, as fx scopes decorations. A decoration that wraps the handler SHALL see the handler already wrapped in the server's middleware, and its wrap SHALL run outside that middleware. A decoration that passes the build's options on SHALL keep the server's name on its records.

#### Scenario: Metrics around every server
- **WHEN** an app decorates the Provider to wrap each handler in a counter, and runs servers `api` and `admin`
- **THEN** requests to either server are counted

#### Scenario: Outside the middleware
- **WHEN** a decoration's wrap records requests, and server `api` has a middleware that answers 401 without calling the next handler
- **THEN** the decoration's wrap records the request that got the 401

#### Scenario: A decoration in a sibling module
- **WHEN** a decoration of the Provider is declared in one module and server `api` is registered in a sibling module
- **THEN** requests to `api` don't pass through the decoration

#### Scenario: The name survives a decoration
- **WHEN** a decoration passes the build's options on, and server `api` binds
- **THEN** its listening record names the server `api`
