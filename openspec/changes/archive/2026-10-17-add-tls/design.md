## Context

The sources are go-modules' `tlsconfig` (a client-only `Config` with `Active` and `Build`, pinning TLS 1.2) and `http/server`'s `CertReloader` (fsnotify on the parent directories, a 5-minute safety-net ticker, last good pair kept). See proposal.md for why they're rebuilt as one module. The requirements are in specs/tlsconfig/spec.md. This document covers how they're met and what was rejected.

Facts that shape the approach, checked against the Go 1.27 source:

- **`Config.getCertificate` only calls `GetCertificate` when `len(Certificates) == 0` or the client sent a server name** (crypto/tls common.go). With both set, a client without SNI gets `Certificates[0]`.
- **`ClientHelloInfo` keeps the config it came from in an unexported field,** so a `GetConfigForClient` hook can't clone the config actually in use. It can only clone one it captured earlier.
- **`net/http.Server` clones its `TLSConfig` and sets `NextProtos` on the clone** (`ServeTLS` → `setupTLSConfig` with `adjustNextProtos`). A config that a `GetConfigForClient` hook returns, cloned from the pre-`http.Server` original, would lose `h2`.
- **The server verifies client certificates only when `ClientAuth >= VerifyClientCertIfGiven`,** against `ClientCAs`, with `ExtKeyUsageClientAuth`. It fills `VerifiedChains` only on that path.
- **When `ClientCAs` is non-nil, the server lists its subjects in the CertificateRequest.** A Go client sends no certificate when none of its certificates matches that list.
- **A client calls `GetClientCertificate` on every handshake in which the server asks for a certificate,** and ignores `Certificates` when it's set. `RootCAs` has no per-handshake hook.
- **A client with `InsecureSkipVerify` still calls `VerifyPeerCertificate` on full handshakes and `VerifyConnection` on every connection, resumed ones included.** `VerifyPeerCertificate` gets the raw chain but no server name; `ConnectionState.ServerName` carries the name the client sent in SNI, which `hostnameInSNI` empties for an IP address (handshake_client.go), even though native verification checks the IP. crypto/tls's `ExampleConfig_verifyConnection` uses exactly this to replace native verification.
- **`Clone` copies function fields** (`GetCertificate`, `GetClientCertificate`, `VerifyConnection`), so closures survive any copy. `VerifyConnection` also runs on resumed connections.
- **The default minimum version is TLS 1.2** for clients (since 1.18) and servers (since 1.22).
- **`testing/synctest` fakes time inside a bubble, and in-memory connections work inside one,** so refresh tests can step 30 seconds instantly. `net.Pipe` doesn't serve: its writes block until read, so a side that aborts a handshake mid-flight deadlocks against the other's pending write. The tests use a buffered in-memory connection (`memConn` in helpers_test.go) instead, a deviation from tasks 2.4 and 3.5, which named `net.Pipe`.

## Goals / Non-Goals

**Goals:**

- One way for any client or server module to get a `*tls.Config` from operator settings, with nothing to wire under fx.
- Key pair and CA rotation that any client or server gets by calling `TLSConfig`, with no loop to run and nothing to forget.
- A returned config that consumers can clone and change (as `http.Server` does) without breaking rotation.

**Non-Goals:**

- A generic file or secret watcher (`secretloader`). It waits for a consumer that needs push notification, such as a database pool reconnecting on a new password.
- Operator control over versions, cipher suites, curves or ALPN. A consumer sets those on the returned config; promoting one to Config later is cheap.
- OCSP stapling, SCTs, and multiple certificates chosen by SNI.

## Decisions

### 1. The shape of the package

```go
package tlsconfig

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
	ErrIncompleteKeyPair      = errors.New("cert_file and key_file must be set together")
	ErrMissingKeyPair         = errors.New("cert_file and key_file are required when enabled")
	ErrUnknownClientAuth      = errors.New("unknown client_auth")
	ErrClientCARequired       = errors.New("client_ca_file is required to verify client certificates")
	ErrClientCAUnused         = errors.New("client_ca_file is only used by verify_if_given and require_and_verify")
	ErrNegativeReloadInterval = errors.New("reload_interval must not be negative")
	ErrNoCertificates         = errors.New("no certificates in file")
	ErrNoServerName           = errors.New("no server name to verify the server certificate against: set server_name, ...")
	ErrNoServerCertificate    = errors.New("server presented no certificate")
	ErrCertificateNotCurrent  = errors.New("certificate outside its validity period")
)

type ClientConfig struct {
	Enabled            bool          `mapstructure:"enabled"`
	CAFile             string        `mapstructure:"ca_file"`
	ServerName         string        `mapstructure:"server_name"`
	InsecureSkipVerify bool          `mapstructure:"insecure_skip_verify"`
	CertFile           string        `mapstructure:"cert_file"`
	KeyFile            string        `mapstructure:"key_file"`
	ReloadInterval     time.Duration `mapstructure:"reload_interval"`
}

func (c ClientConfig) Validate() error
func (c ClientConfig) TLSConfig(logger *slog.Logger) (*tls.Config, error)

type ServerConfig struct {
	Enabled        bool          `mapstructure:"enabled"`
	CertFile       string        `mapstructure:"cert_file"`
	KeyFile        string        `mapstructure:"key_file"`
	ClientCAFile   string        `mapstructure:"client_ca_file"`
	ClientAuth     ClientAuth    `mapstructure:"client_auth"`
	ReloadInterval time.Duration `mapstructure:"reload_interval"`
}

func (c ServerConfig) Validate() error
func (c ServerConfig) TLSConfig(logger *slog.Logger) (*tls.Config, error)
```

Files: `config.go` (both Configs, ClientAuth, sentinels, Validate, resolvers), `client.go` and `server.go` (each `TLSConfig`), `reloader.go` (the refreshing state behind the hooks, and each kind of file's `format`: how to parse, describe and check it), and `verify.go` (the chain verification both sides' hooks share), all unexported but the Configs' surface.

`ClientAuth` is a named string, so mapstructure decodes it with no hook, and a developer filling a Config by hand gets constants. `TLSConfig` takes a logger argument rather than an `fx` value because the module has no fx side; the method is the API. Both methods take one, since both refresh a key pair.

Alternatives: `Unmarshal` as the method name (rejected: in Go it means decoding bytes, and this does the opposite plus I/O); a free function `tlsconfig.NewServer(cfg, logger)` (rejected: the user wanted the method on the Config, and it reads well at the call site, `cfg.TLS.TLSConfig(logger)`).

### 2. Disabled means nil

`enabled: false` returns `(nil, nil)` before anything else, Validate included. nil is the only `*tls.Config` every consumer reads as "no TLS settings": plaintext for redis and pgx, default TLS for an https URL in `http.Transport`, plaintext for a server. A zero `&tls.Config{}` would mean "TLS on" to any consumer that checks for nil.

The other fields are ignored rather than rejected, so an operator can switch TLS off without deleting paths.

Alternative: go-modules' `Active()` (any field set means on). Rejected: setting `insecure_skip_verify` alone turned TLS on, and every consumer had to remember to call `Active`.

### 3. Validation lives in Validate, and TLSConfig calls it

Every rule is cross-field (each only applies when `enabled` is true), so all of them go in `Validate`, returning the sentinels, with no `validate` tags. configloader runs `Validate` at load, so an operator sees the error at startup with the section's key. `TLSConfig` calls `Validate` first, so a Config built by hand gets the same errors rather than `open : no such file or directory`. The module doesn't import `config/configvalidator`.

A consumer whose own Config has a `Validate` method owns its subtree, so it delegates with `configvalidator.ValidateField(&c, &c.TLS)`. The README says so.

Alternative: `required_if=Enabled true` and `oneof` tags. Rejected: tags can't skip a disabled config's other rules, and a hand-built Config passed straight to `TLSConfig` would skip them.

### 4. Client: the CA file replaces the roots; the key pair refreshes

`TLSConfig` reads the CA file into a fresh `x509.CertPool` (not the system pool plus extras), and sets `ServerName` and `InsecureSkipVerify`. Replacing rather than adding is the safer default for a private CA: it stops a public CA from vouching for an internal host. A consumer that wants both can build the pool itself. The CA file refreshes too (decision 11).

The key pair goes through the same reloader as the server's (decision 6) and is presented through `GetClientCertificate`, with `Certificates` left empty. crypto/tls calls the hook on every handshake in which the server asks for a certificate, so a client refreshes exactly when it needs the pair. Connections kept alive across a rotation keep the certificate they opened with.

`InsecureSkipVerify` carries `//nolint:gosec // operator opt-in for development, documented as unsafe`.

### 5. Server: the key pair only through GetCertificate

`Certificates` stays empty and `GetCertificate` returns the reloader's current pair. Given the first fact above, setting both would serve a stale `Certificates[0]` to clients without SNI after a rotation. Since `Clone` copies the hook, an `http.Server` clone keeps rotating.

### 6. Lazy refresh inside the hooks

The reloader holds, per refreshed resource (a client's or server's key pair; a server's client CA pool when verifying):

- the current parsed value in an `atomic.Pointer`, read by every handshake without locking;
- a SHA-256 digest of each file's last contents and the time of the last check, guarded by a mutex taken with `TryLock`.

On each handshake the hook loads the current value. If the interval has passed since the last check and `TryLock` succeeds, that handshake re-reads the files, compares their digests with the last seen, and parses and swaps only when they differ. Handshakes that lose `TryLock` use the current value and don't wait. The check time is updated whether the refresh succeeds or fails, so a broken file is retried once per interval, not on every handshake.

Comparing contents rather than modification times avoids timestamp granularity and Kubernetes' `..data` symlink swap, where the file's own mtime can stay the same. "Last seen" includes contents that failed to parse, which is what makes a broken pair warn once (spec: refreshes are reported); a read error has no contents to compare, so it warns at every check. Digests rather than the raw bytes, so the reloader doesn't keep a second plaintext copy of the private key for the life of the process.

A `format` per kind of file says how to parse it, how to describe a value in log records (a key pair by its leaf's subject, serial and expiry; a CA pool by nothing but its path), and what a refreshed value must pass. A key pair whose leaf has expired or isn't valid yet is rejected like contents that don't parse, with `ErrCertificateNotCurrent` in the Warn record, so "the last good value" means one that works, not just one that parses. The first load isn't checked: an expired certificate at startup is the operator's to see, and refusing to start helps nobody. Each value is logged at Info when it's loaded as well as when it's swapped, so the log says which certificate a process is using and when it expires.

The refresh runs synchronously in the winning handshake. Reading two or three small files costs microseconds, and a goroutine per refresh would outlive the handshake for nothing.

Alternatives:

- **fsnotify with a `Watch(ctx)` loop** (go-modules). Rejected: every consumer has to remember to run the loop, a forgotten one fails silently, and the constitution's runner rule would apply. Its 5-minute ticker was already the real guarantee, and Kubernetes secret updates lag by about a minute anyway.
- **Rotation in each server module** (go-modules' placement). Rejected: every new server module would write it again, and clients wouldn't get it.
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

The costs are that `ConnectionState.VerifiedChains` stays empty, and that the hook checks expiry against the `Time` of the config `TLSConfig` returned, since a handshake doesn't say which copy it's using; a `Time` set on a clone is ignored. The spec states it, and the README says to read `PeerCertificates`, which hold the verified client chain once the handshake succeeds.

### 8. No TLS version floor

go-modules pinned `MinVersion: tls.VersionTLS12`. Go's defaults have matched that on both sides since 1.22, and this repo requires 1.27, so the pin does nothing, and leaving the field zero lets future Go raise the floor for free.

### 9. One module, not one per side

Client and server settings barely overlap as config, but they share the reloader, the hardest code in the change. Splitting them would mean copying it, or moving it into a third module both require, published first, with its API made public since the constitution bans `internal/`. Neither side has a dependency outside the standard library, so the reason the constitution gives for small modules, keeping one consumer from building another's dependencies, doesn't apply.

The change was first built as `tls/tlsclientconfig` and `tls/tlsserverconfig`, with the client reading its key pair once. Review found that it broke mutual TLS against a rotating issuer such as cert-manager, and that `GetClientCertificate` makes client key pair refresh as safe as the server's. Sharing the reloader then outweighed the split. Copying it was rejected as two copies of concurrency-sensitive code, and a `tls/tlsreloader` module as a public API and a two-PR release for an implementation detail.

### 10. `*_file` versus `*_path` in the constitution

The constitution's "a module's own path fields are named `*_path`" becomes: a `*_file` field names a file the module reads itself and may re-read, a live counterpart to configloader's `_FILE`; a `*_path` field is a path used as a path. A rotating credential belongs in a `*_file` field. "A Config never has both `x` and `x_file`" stays: `tls.cert_file`'s env name would be the `_FILE` twin of a `tls.cert` key, and configloader's collision check would reject the pair.

### 11. Client: rotating CAs verified in the hooks, with a backstop

With `ca_file` set and `insecure_skip_verify` off, the CA file goes through a reloader, and:

- `InsecureSkipVerify` is set, turning native verification off, with a comment saying why;
- `VerifyConnection` verifies `PeerCertificates[0]` against the current pool, with the rest as intermediates, the default server-auth usage, the config's `Time`, and `DNSName` set to `ConnectionState.ServerName`, or to the `ClientConfig`'s `ServerName` when that's empty, as it is for an IP address, failing when both are;
- `VerifyPeerCertificate` parses the raw chain and verifies it against the current pool, without a name;
- `RootCAs` holds the pool first read.

The backstop exists because the pattern fails open: replacing `VerifyConnection`, the likely mistake when adding a pin or a tenant check, would otherwise verify nothing. With the backstop, it still rejects certificates no current CA issued and only loses the name check, so an impersonator would need a certificate from the operator's private CA. `RootCAs` covers the other mistake, a copy setting `InsecureSkipVerify` back to false: native verification then runs against the first CAs, failing after a rotation rather than trusting too much. Full handshakes verify the chain twice, which costs microseconds.

Without `ca_file`, or with `insecure_skip_verify`, the config is native and sets no hooks.

The first build failed every handshake to an IP address, a regression found in review: it verified against `ConnectionState.ServerName` alone, which crypto/tls empties for IPs. `server_name`, captured when `TLSConfig` runs, now stands in, and x509 matches it against IP SANs. A dialled IP address with no `server_name` still fails, with an error saying to set it, because the dialled address only reaches a copy of the config.

With `insecure_skip_verify`, a `ca_file` is ignored rather than rejected, unlike a server's `client_ca_file` beside a policy that doesn't verify (`ErrClientCAUnused`). The server case is a trap: `require` reads like verification and isn't. Skipping verification says plainly that nothing is verified, and it's a development switch an operator flips on and off; rejecting the pair would mean deleting `ca_file` to flip it, the reason a disabled config ignores its paths too.

Alternatives:

- **Read once** (the first build). Rejected: every CA rotation would need a client restart between distributing the new CA and re-issuing the servers' certificates, a manual step that's easy to forget because CA rotations are rare.
- **`VerifyConnection` alone.** Rejected: replacing it would verify nothing, not even the chain.
- **A per-dial config factory.** Rejected: each consumer would need its own hook for it (`http.Transport` has only `DialTLSContext`), and it breaks the "hand the `*tls.Config` over as it is" shape.

### 12. Hooks match what crypto/tls would have done

A principal-level review checked every hook against the native behaviour it replaces, and two fell short:

- **A client's certificate choice.** crypto/tls sends a certificate from `Certificates` only if `CertificateRequestInfo.SupportsCertificate` accepts it (signature schemes, and the CAs the server lists), and otherwise none. `GetClientCertificate` now makes the same check and sends an empty certificate when it fails, so a server that only asks for one, listing other CAs, completes the handshake instead of rejecting a certificate it never needed, and a client doesn't show its identity to a server that didn't ask for it.
- **The server's backstop.** The server now checks a presented client chain in `VerifyPeerCertificate` too, as the client does for servers, so replacing `VerifyConnection` (which the README's tenant-check example touches) still rejects certificates no current client CA issued.

Both sides' hooks share `verifyChain`, so a later fix lands once. Two native checks aren't matched: FIPS mode's chain filter, and, when Encrypted Client Hello is rejected, a client's check of the public name against `RootCAs`, which holds its first CAs. Neither is in use.

`ErrNoServerName` and `ErrNoServerCertificate` are exported: the first is a configuration mistake a consumer can now spot with `errors.Is` and report. That follows the constitution's new rule that every sentinel is exported.

### Rollup into docs/tls.md at archive

Decisions 2, 5, 6, 7, 9, 11 and 12 (nil means disabled, `GetCertificate` only, lazy refresh over a watcher with rotation in the module and its validity check and logging, `VerifyConnection` over `GetConfigForClient`, one module, a client's CAs verified in its hooks with a backstop, and hooks matching native behaviour) are durable rationale. They go into `docs/tls.md`, written in this change, and are rechecked at archive against what was built.

## Risks / Trade-offs

- **A server module replaces `VerifyConnection` on the returned config** → native `ClientAuth` doesn't verify, but the `VerifyPeerCertificate` backstop still checks a presented chain on full handshakes; resumed connections then go unchecked against a rotated-out CA. The godoc and README say to wrap the hook. A test pins down that both hooks are set for both verifying policies.
- **A refreshed pair is checked against the local clock** → a host whose clock is badly off could reject a valid renewal. It keeps the last good pair and warns, which is the safer failure.
- **`client_ca_file` beside a policy that doesn't verify** → an operator who reads `require` as "require a cert from this CA" would get no verification. `Validate` rejects the combination with `ErrClientCAUnused`.
- **`VerifiedChains` is empty** → a consumer that authorises on it sees nothing. The spec, godoc and README point to `PeerCertificates`.
- **A broken key file warns every interval when the read fails** (as opposed to a parse failure, which warns once) → up to 2880 records a day per client or server until it's fixed. It's a broken state worth the noise, and `reload_interval` limits it.
- **A rotation is seen at most one interval late, and only once a handshake happens** → fine for issuers that renew well before expiry (cert-manager renews at two-thirds of the lifetime). A server with no handshakes for days still holds the old pair, but nothing needs it until the next handshake, which triggers the check.
- **The handshake that runs the check pays for the file reads** → microseconds for PEM files. Only one handshake per interval pays.
- **A client dials an IP address with `ca_file` and no `server_name`** → every handshake fails, where native verification would check the IP. The error says to set `server_name`, and the godoc and README say so up front.
- **A client module replaces `VerifyConnection`** → the server name is no longer checked, though the backstop still checks the chain against the current CAs. The godoc and README say to wrap it.
- **A client's returned config reads `InsecureSkipVerify: true`** → alarming to a reader and to linters in consuming code. The godoc and README say why, and that `RootCAs` keeps a copy that flips it back safe.
- **A client's `VerifiedChains` is empty** → as on the server, consumers read `PeerCertificates`.
- **One module serves both sides** → a client-only consumer builds the server code too. It's a few hundred lines of standard library, so the cost is negligible.

## Migration Plan

Nothing consumes the module yet. One pull request carries the new module, the constitution amendments, `kernelrun`'s exported `ErrExited`, `docs/tls.md`, the `docs/config.md` edit and the two README-only patch bumps (`config/configloader`, `config/configvalidator`). The new module requires no sibling, so there's no release order. Merging publishes all four tags.

## What actually happened

Delivered in one pull request, #19, merged as 9724a18. It published `tls/tlsconfig/v0.1.0`, `kernelrun/v0.2.0`, `config/configloader/v0.1.2` and `config/configvalidator/v0.1.1`.

Deviations from the design as first written:

- **Two modules became one.** The change was built as `tls/tlsclientconfig` and `tls/tlsserverconfig` (task sections 1–7), then merged into `tls/tlsconfig` once review showed the client's key pair had to refresh too (decision 9, task section 8). The two first modules were never released.
- **A client's CAs refresh.** The first design read `ca_file` once. It now goes through a reloader and is verified in the hooks, with a backstop (decision 11, task section 9).
- **An end-to-end test was added** (task section 10): mutual TLS over HTTP/2 between an `http.Server` and an `http.Client` through a leaf rotation and a cert-manager-style CA rotation, in real time with a 100ms interval, since synctest can't drive real sockets.
- **A nil logger discards** rather than being checked for at every record (task section 12).
- **Tests use a buffered in-memory connection, not `net.Pipe`,** which deadlocks when one side aborts a handshake mid-flight (see Context).
- **Validity checks, load-time records and leaf details in records** came out of the third review (task section 13), not the first design.
- **`kernelrun` exports `ErrExited`** under the new exported-sentinel rule (task section 14), so the change touched a fourth released module.

## Fixes from code review

The first review raised nine findings (task section 7). Seven were fixed:

- `client_ca_file` beside a policy that doesn't verify is rejected with `ErrClientCAUnused`.
- `TLSConfig`'s godoc says expiry is checked against the returned config's `Time`.
- Key pair load errors name both files.
- The reloader keeps a SHA-256 digest of each file, not its bytes.
- The client's two `ca_file` errors share one wording.
- The review's finding on the client's read-once key pair led to the merge into one module (decision 9).
- The review's finding on read-once CAs led to decision 11.

Two were rejected: keyed `configvalidator.Error` values would mean importing `configvalidator` (decision 3), and a hung filesystem blocks only the one handshake that runs the check.

The second review (task section 11):

- **A regression.** With `ca_file`, a client couldn't reach a server by IP address, because crypto/tls empties `ConnectionState.ServerName` for IPs. → `server_name` stands in, and the error says to set it.
- `ca_file` beside `insecure_skip_verify` stays tolerated (decision 11).
- The system-roots scenario expects any certificate verification error, since platform verifiers don't all report an unknown authority.
- The end-to-end test requires the `bad certificate` alert.
- The README notes the spurious Warn when a swap lands between a key pair's two reads.
- The CA-file setup moved into `verifyWithCAFile`, under gocognit's limit.

The third review checked every hook against native behaviour (task section 13, decision 12):

- `GetClientCertificate` honours `SupportsCertificate`.
- The server has a `VerifyPeerCertificate` backstop.
- Values are logged when first loaded, with leaf details.
- A refreshed pair outside its validity window is rejected with `ErrCertificateNotCurrent`.
- The two verifiers share `verifyChain`.
- `ErrNoServerName` and `ErrNoServerCertificate` are exported.
- The constitution gained the exported-sentinel rule.
- One finding was left as documented: retrying sooner after a key pair's two reads straddle a swap.

## Follow-ups left open

- **Retrying sooner after a straddled swap,** left documented (third review, finding 5).
- **A generic file or secret watcher (`secretloader`),** when a consumer needs push notification.
- **Operator control over versions, cipher suites, curves or ALPN.** A consumer sets them on the returned config.
- **OCSP stapling, SCTs and SNI-selected certificates.**
- **FIPS mode's chain filter and ECH's public-name check** aren't matched by the hooks (decision 12). Neither is in use.

## Rollup at archive

- **Constitution:** the `*_file`/`*_path` split and the exported-sentinel rule shipped in #19.
- **docs/tls.md** (new): decisions 2, 5, 6, 7, 9, 11 and 12, rechecked against what was built: one module, nil means disabled, `GetCertificate` only, lazy refresh in the module, a client's CAs in its hooks with a backstop, a server's client CAs in `VerifyConnection`, and hooks matching native behaviour.
- **docs/config.md:** rotating credentials go in `*_file` fields that a module re-reads, with `tls/tlsconfig` as the example.
- **The rest** (the policy table, error wordings, log records) is specific to the module and lives in its spec, README and this archive.
