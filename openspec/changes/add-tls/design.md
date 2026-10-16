## Context

The sources are go-modules' `tlsconfig` (a client-only `Config` with `Active` and `Build`, pinning TLS 1.2) and `http/server`'s `CertReloader` (fsnotify on the parent directories, a 5-minute safety-net ticker, last good pair kept). See proposal.md for why they're rebuilt as two modules. The requirements are in specs/tlsclientconfig/spec.md and specs/tlsserverconfig/spec.md. This document covers how they're met and what was rejected.

Facts that shape the approach, checked against the Go 1.27 source:

- **`Config.getCertificate` only calls `GetCertificate` when `len(Certificates) == 0` or the client sent a server name** (crypto/tls common.go). With both set, a client without SNI gets `Certificates[0]`.
- **`ClientHelloInfo` keeps the config it came from in an unexported field,** so a `GetConfigForClient` hook can't clone the config actually in use. It can only clone one it captured earlier.
- **`net/http.Server` clones its `TLSConfig` and sets `NextProtos` on the clone** (`ServeTLS` → `setupTLSConfig` with `adjustNextProtos`). A config that a `GetConfigForClient` hook returns, cloned from the pre-`http.Server` original, would lose `h2`.
- **The server verifies client certificates only when `ClientAuth >= VerifyClientCertIfGiven`,** against `ClientCAs`, with `ExtKeyUsageClientAuth`. It fills `VerifiedChains` only on that path.
- **When `ClientCAs` is non-nil, the server lists its subjects in the CertificateRequest.** A Go client sends no certificate when none of its certificates matches that list.
- **`Clone` copies function fields** (`GetCertificate`, `VerifyConnection`), so closures survive any copy. `VerifyConnection` also runs on resumed connections.
- **The default minimum version is TLS 1.2** for clients (since 1.18) and servers (since 1.22).
- **`testing/synctest` fakes time inside a bubble, and `net.Pipe` works inside one,** so refresh tests can step 30 seconds instantly and handshake over in-memory connections.

## Goals / Non-Goals

**Goals:**

- One way for any client or server module to get a `*tls.Config` from operator settings, with nothing to wire under fx.
- Server rotation that any server gets by calling `TLSConfig`, with no loop to run and nothing to forget.
- A returned config that consumers can clone and change (as `http.Server` does) without breaking rotation.

**Non-Goals:**

- Rotation on the client side, of either its key pair or its CA pool. A client has no per-handshake hook for `RootCAs`, and the workaround (`InsecureSkipVerify` plus manual verification) isn't worth it yet.
- A generic file or secret watcher (`secretloader`). It waits for a consumer that needs push notification, such as a database pool reconnecting on a new password.
- Operator control over versions, cipher suites, curves or ALPN. A consumer sets those on the returned config; promoting one to Config later is cheap.
- OCSP stapling, SCTs, and multiple certificates chosen by SNI.

## Decisions

### 1. The shape of the packages

```go
package tlsclientconfig

var (
	ErrIncompleteKeyPair = errors.New("cert_file and key_file must be set together")
	ErrNoCertificates    = errors.New("no certificates in file")
)

type Config struct {
	Enabled            bool   `mapstructure:"enabled"`
	CAFile             string `mapstructure:"ca_file"`
	ServerName         string `mapstructure:"server_name"`
	InsecureSkipVerify bool   `mapstructure:"insecure_skip_verify"`
	CertFile           string `mapstructure:"cert_file"`
	KeyFile            string `mapstructure:"key_file"`
}

func (c Config) Validate() error
func (c Config) TLSConfig() (*tls.Config, error)
```

```go
package tlsserverconfig

const DefaultReloadInterval = 30 * time.Second

type ClientAuth string

const (
	ClientAuthNone             ClientAuth = "none"
	ClientAuthRequest          ClientAuth = "request"
	ClientAuthRequire          ClientAuth = "require"
	ClientAuthVerifyIfGiven    ClientAuth = "verify_if_given"
	ClientAuthRequireAndVerify ClientAuth = "require_and_verify"
)

var (
	ErrMissingKeyPair         = errors.New("cert_file and key_file are required when enabled")
	ErrUnknownClientAuth      = errors.New("unknown client_auth")
	ErrClientCARequired       = errors.New("client_ca_file is required to verify client certificates")
	ErrNegativeReloadInterval = errors.New("reload_interval must not be negative")
	ErrNoCertificates         = errors.New("no certificates in file")
)

type Config struct {
	Enabled        bool          `mapstructure:"enabled"`
	CertFile       string        `mapstructure:"cert_file"`
	KeyFile        string        `mapstructure:"key_file"`
	ClientCAFile   string        `mapstructure:"client_ca_file"`
	ClientAuth     ClientAuth    `mapstructure:"client_auth"`
	ReloadInterval time.Duration `mapstructure:"reload_interval"`
}

func (c Config) Validate() error
func (c Config) TLSConfig(logger *slog.Logger) (*tls.Config, error)
```

Files: `config.go` (Config, ClientAuth, sentinels, Validate, resolvers), `<package>.go` (`TLSConfig`), and in the server only `reloader.go` (the refreshing state behind the hooks, unexported).

`ClientAuth` is a named string, so mapstructure decodes it with no hook, and a developer filling a Config by hand gets constants. `TLSConfig` takes a logger argument rather than an `fx` value because the module has no fx side; the method is the API.

Alternatives: `Unmarshal` as the method name (rejected: in Go it means decoding bytes, and this does the opposite plus I/O); a free function `tlsserverconfig.New(cfg, logger)` (rejected: the user wanted the method on Config, and it reads well at the call site, `cfg.TLS.TLSConfig(logger)`).

### 2. Disabled means nil

`enabled: false` returns `(nil, nil)` before anything else, Validate included. nil is the only `*tls.Config` every consumer reads as "no TLS settings": plaintext for redis and pgx, default TLS for an https URL in `http.Transport`, plaintext for a server. A zero `&tls.Config{}` would mean "TLS on" to any consumer that checks for nil.

The other fields are ignored rather than rejected, so an operator can switch TLS off without deleting paths.

Alternative: go-modules' `Active()` (any field set means on). Rejected: setting `insecure_skip_verify` alone turned TLS on, and every consumer had to remember to call `Active`.

### 3. Validation lives in Validate, and TLSConfig calls it

Every rule is cross-field (each only applies when `enabled` is true), so all of them go in `Validate`, returning the sentinels, with no `validate` tags. configloader runs `Validate` at load, so an operator sees the error at startup with the section's key. `TLSConfig` calls `Validate` first, so a Config built by hand gets the same errors rather than `open : no such file or directory`. Neither module imports `config/configvalidator`.

A consumer whose own Config has a `Validate` method owns its subtree, so it delegates with `configvalidator.ValidateField(&c, &c.TLS)`. Both READMEs say so.

Alternative: `required_if=Enabled true` and `oneof` tags. Rejected: tags can't skip a disabled config's other rules, and a hand-built Config passed straight to `TLSConfig` would skip them.

### 4. Client: read once, CA file replaces the roots

`TLSConfig` reads the CA file into a fresh `x509.CertPool` (not the system pool plus extras), loads the key pair with `tls.LoadX509KeyPair`, and sets `ServerName` and `InsecureSkipVerify`. Replacing rather than adding is the safer default for a private CA: it stops a public CA from vouching for an internal host. A consumer that wants both can build the pool itself.

`InsecureSkipVerify` carries `//nolint:gosec // operator opt-in for development, documented as unsafe`.

### 5. Server: the key pair only through GetCertificate

`Certificates` stays empty and `GetCertificate` returns the reloader's current pair. Given the first fact above, setting both would serve a stale `Certificates[0]` to clients without SNI after a rotation. Since `Clone` copies the hook, an `http.Server` clone keeps rotating.

### 6. Server: lazy refresh inside the hooks

The reloader holds, per refreshed resource (the key pair; the client CA pool when verifying):

- the current parsed value in an `atomic.Pointer`, read by every handshake without locking;
- the last raw bytes seen and the time of the last check, guarded by a mutex taken with `TryLock`.

On each handshake the hook loads the current value. If the interval has passed since the last check and `TryLock` succeeds, that handshake re-reads the files, compares the bytes with the last seen, and parses and swaps only when they differ. Handshakes that lose `TryLock` use the current value and don't wait. The check time is updated whether the refresh succeeds or fails, so a broken file is retried once per interval, not on every handshake.

Comparing bytes rather than modification times avoids timestamp granularity and Kubernetes' `..data` symlink swap, where the file's own mtime can stay the same. "Last seen" includes contents that failed to parse, which is what makes a broken pair warn once (spec: refreshes are reported); a read error has no contents to compare, so it warns at every check.

The refresh runs synchronously in the winning handshake. Reading two or three small files costs microseconds, and a goroutine per refresh would outlive the handshake for nothing.

Alternatives:

- **fsnotify with a `Watch(ctx)` loop** (go-modules). Rejected: every consumer has to remember to run the loop, a forgotten one fails silently, and the constitution's runner rule would apply. Its 5-minute ticker was already the real guarantee, and Kubernetes secret updates lag by about a minute anyway.
- **Rotation in each server module** (go-modules' placement). Rejected: every new server module would write it again.
- **A shared secret watcher module.** Deferred (Non-Goals).

### 7. Server: client CAs verified in VerifyConnection, not through GetConfigForClient

When the policy verifies, the server's native `ClientAuth` is set one step down (`RequireAnyClientCert` for `require_and_verify`, `RequestClientCert` for `verify_if_given`), `ClientCAs` stays nil, and a `VerifyConnection` hook verifies `PeerCertificates[0]` against the reloader's current pool. It uses the remaining certificates as intermediates, `ExtKeyUsageClientAuth`, and the config's time, which is the same check crypto/tls does natively.

| Policy | Native ClientAuth | VerifyConnection |
|--------|-------------------|------------------|
| none | NoClientCert | not set |
| request | RequestClientCert | not set |
| require | RequireAnyClientCert | not set |
| verify_if_given | RequestClientCert | verifies when a certificate is present |
| require_and_verify | RequireAnyClientCert | verifies |

Leaving `ClientCAs` nil means the CertificateRequest lists no CA names. A list built at startup would go stale after a rotation, and a Go client holding only a certificate from the new CA would then send none.

Alternative: `GetConfigForClient` returning a clone of the base config with the current `ClientCAs`, which keeps native verification and `VerifiedChains`. Rejected: the hook can only clone the config it captured, never the one in use, so anything a server module changes on its own copy disappears from the handshake. `http.Server` adds `h2` to `NextProtos` on a clone, so HTTP/2 would stop negotiating whenever mTLS is on.

The cost is that `ConnectionState.VerifiedChains` stays empty. The spec states it, and the README says to read `PeerCertificates`, which hold the verified client chain once the handshake succeeds.

### 8. No TLS version floor

go-modules pinned `MinVersion: tls.VersionTLS12`. Go's defaults have matched that on both sides since 1.22, and this repo requires 1.27, so the pin does nothing, and leaving the field zero lets future Go raise the floor for free.

### 9. Two modules, a duplicated helper

Both modules read a PEM file into a `CertPool` and fail with `ErrNoCertificates` when it holds none. That's about ten lines, duplicated rather than shared, because a shared package would be a third module that both require, released first, for one function. Each module exports its own `ErrNoCertificates`.

### 10. `*_file` versus `*_path` in the constitution

The constitution's "a module's own path fields are named `*_path`" becomes: a `*_file` field names a file the module reads itself and may re-read, a live counterpart to configloader's `_FILE`; a `*_path` field is a path used as a path. A rotating credential belongs in a `*_file` field. "A Config never has both `x` and `x_file`" stays: `tls.cert_file`'s env name would be the `_FILE` twin of a `tls.cert` key, and configloader's collision check would reject the pair.

### Rollup into docs/tls.md at archive

Decisions 2, 5, 6 and 7 (nil means disabled, `GetCertificate` only, lazy refresh over a watcher with rotation in the module, `VerifyConnection` over `GetConfigForClient`) are durable rationale. They go into `docs/tls.md`, written in this change, and are rechecked at archive against what was built.

## Risks / Trade-offs

- **A consumer replaces `VerifyConnection` on the returned config** → client certificate verification is silently gone, because native `ClientAuth` doesn't verify. Both godoc and the README say to wrap the existing hook rather than replace it. A test pins down that the hook is set for both verifying policies.
- **`VerifiedChains` is empty** → a consumer that authorises on it sees nothing. The spec, godoc and README point to `PeerCertificates`.
- **A broken key file warns every interval when the read fails** (as opposed to a parse failure, which warns once) → up to 2880 records a day per server until it's fixed. It's a broken state worth the noise, and `reload_interval` limits it.
- **A rotation is seen at most one interval late, and only once a handshake happens** → fine for issuers that renew well before expiry (cert-manager renews at two-thirds of the lifetime). A server with no handshakes for days still holds the old pair, but nothing needs it until the next handshake, which triggers the check.
- **The handshake that runs the check pays for the file reads** → microseconds for PEM files. Only one handshake per interval pays.
- **Client CA rotation isn't supported on the client side** → recorded as a Non-Goal; a client restart picks up a new CA.

## Migration Plan

Nothing consumes these modules yet. One pull request carries both new modules, the constitution amendment, `docs/tls.md`, the `docs/config.md` edit and the two README-only patch bumps (`config/configloader`, `config/configvalidator`). Neither new module requires a sibling, so there's no release order. Merging publishes all four tags.
