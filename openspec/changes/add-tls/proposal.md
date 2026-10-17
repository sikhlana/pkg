## Why

Every TLS-speaking client and server still to come (an HTTP server, redis, a database pool) needs to turn operator settings into a `*tls.Config`. Without a shared module, each one rebuilds the same file loading and validation, and each one rebuilds certificate rotation. go-modules has a client-only `tlsconfig` that reads its files once, and puts its `CertReloader` in `http/server`, so a second server would have to write rotation again and a client never gets it. Mutual TLS between two services with cert-manager rotating both sides' certificates is the common case that breaks. It's rebuilt here as one module covering both sides, with rotation built into the configuration it returns, so any client or server gets it just by calling `TLSConfig`.

## What Changes

- New module `tls/tlsconfig` (`github.com/sikhlana/pkg/tls/tlsconfig`), starting at 0.1.0, with a `ClientConfig` and a `ServerConfig` that client and server modules embed in their own Configs. Each has a `TLSConfig(logger)` method returning the `*tls.Config` to dial or serve with:
  - **Off by default.** `enabled` is the switch. When it's false, `TLSConfig` returns a nil `*tls.Config` and reads no files, whatever else is set. nil is the only value every consumer reads as "no TLS settings".
  - **Clients verify servers.** `ca_file` replaces the system roots with its certificates. `server_name` overrides the name the server's certificate is checked against. `insecure_skip_verify` turns verification off, for development only.
  - **Clients present a key pair for mutual TLS.** `cert_file` and `key_file` are set together; setting only one of them fails validation.
  - **Servers require a key pair** when enabled, and authenticate clients by policy: `client_ca_file` holds the CAs client certificates must chain to, and `client_auth` picks the policy (`none`, `request`, `require`, `verify_if_given`, `require_and_verify`). A CA file beside a policy that doesn't verify is rejected rather than silently unused.
  - **Rotation without a restart, on both sides.** Key pairs and CA files are re-read during a handshake, at most once per `reload_interval` (default 30s). Changed files are swapped in, and a file that fails to read or parse keeps the last good value. There's no watcher, goroutine or runner to start, so a consumer can't forget to run one.
  - **A client's rotating CAs, verified with a backstop.** crypto/tls can't swap `RootCAs` per handshake, so with `ca_file` the client verifies the server in its own hooks: `VerifyConnection` checks the chain and the server name, and `VerifyPeerCertificate` checks the chain again, so a consumer that replaces `VerifyConnection` still rejects certificates no current CA issued.
  - **No stale certificate for servers' clients without SNI.** A server's key pair is served through `GetCertificate`, never through `Certificates`. crypto/tls would serve `Certificates[0]` to a client that sends no server name, even when a `GetCertificate` hook is set.
  - **Logging.** A successful refresh is logged at Info, and a failed refresh that keeps the last good value at Warn, through the `*slog.Logger` passed to `TLSConfig`. Records name paths, never contents. Failures when `TLSConfig` is first called are returned, not logged.
- How it differs from go-modules' `tlsconfig` and `CertReloader`:
  - One module for clients and servers, instead of a client-only module with server rotation buried in `http/server`.
  - `Build` plus `Active` becomes `TLSConfig`, gated by an explicit `enabled` rather than "any field set".
  - The pinned TLS 1.2 floor is gone. Go has defaulted to TLS 1.2 for clients since 1.18 and for servers since 1.22.
  - The fsnotify watcher and its run loop become a lazy refresh inside the handshake hooks. go-modules' safety-net ticker was already what actually guaranteed rotation.
  - New: client key pair and CA rotation, `server_name`, client certificate authentication on the server, refreshing client CAs on the server, and serving the server's key pair only through `GetCertificate`.
- Constitution:
  - **Every sentinel is exported.** An error sentinel is an exported `Err*` variable even when only a log record or a wrapped error carries it today, so a caller can always match it with `errors.Is`. `package main` is exempt, since nothing can import it.
  - **`*_file` and `*_path` are split.** A `*_file` field names a file the module reads itself, and the module may re-read it, which makes it a live counterpart to configloader's `_FILE`. A `*_path` field is a path used as a path (a socket, a directory). A credential that rotates belongs in a `*_file` field. The rule that a Config never has both `x` and `x_file` stays: it's what keeps their env names apart.
- `docs/config.md` follows the amended rule wherever it sends rotating credentials to `*_path` fields.
- New `docs/tls.md`: why one module, why nil means disabled, why the server's key pair is served through `GetCertificate` only, why refreshing is lazy and lives in the module, how a client verifies against rotating CAs, and why a server's client CAs are verified in `VerifyConnection`.
- The root README gains a Modules row, and `go.work` registers the module.

## Capabilities

### New Capabilities

- `tlsconfig`: the `enabled` gate and the nil result; a client's CA trust and its refresh with a verification backstop, server-name override, skipped verification and mutual-TLS key pair; a server's required key pair and client authentication policies; lazy refresh of key pairs and CAs, keeping the last good value, surviving copies; serving a server's key pair only through `GetCertificate`; what gets logged; and what errors and records may contain.

### Modified Capabilities

- `kernelrun`: a runner that exits through `runtime.Goexit` crashes with an error matching the newly exported `ErrExited`, under the exported-sentinel rule. `configloader`'s behaviour doesn't change. Only the naming rule it documents for module path fields changes, and that rule lives in the constitution and the READMEs, not in a spec requirement.

## Impact

- **`tls/tlsconfig` (new module):** all of its exported API is new. Its exposure shape is a plain function: a `TLSConfig` method on each of `ClientConfig` and `ServerConfig`, taking a `*slog.Logger` as developer wiring. Refreshing happens inside the returned `*tls.Config` rather than in a loop, so it has two Configs, no fx Module and no runner.
- **`kernelrun`:** exported API grows by one sentinel: `errExited` becomes `ErrExited`, under the exported-sentinel rule. Behaviour is unchanged.
- **`config/configloader`:** README only. Its env-collision and rotation paragraphs move from `*_path` to the amended `*_file`/`*_path` rule. Its exported API is unchanged.
- **`config/configvalidator`:** README only. Its delegation example renames `cert_path` to `cert_file`. Its exported API is unchanged.
- **New dependencies:** none beyond the standard library. testify is used by tests only. Validation lives in `Validate` methods returning sentinels, so the module doesn't import `config/configvalidator`.
- **Docs and repo files:** `openspec/config.yaml` (the `*_file`/`*_path` and exported-sentinel amendments), `docs/config.md`, `docs/tls.md` (new), `README.md` (a Modules row), `go.work` (`use ./tls/tlsconfig`).
- **Release:** one pull request, since the module requires no sibling. Merging publishes `tls/tlsconfig/v0.1.0`, `kernelrun/v0.2.0` for its new export, and patch releases of `config/configloader` and `config/configvalidator` for their README edits.
