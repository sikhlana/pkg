## Why

Every TLS-speaking client and server still to come (an HTTP server, redis, a database pool) needs to turn operator settings into a `*tls.Config`. Without a shared module, each one rebuilds the same file loading and validation, and each server rebuilds certificate rotation. go-modules has a client-only `tlsconfig` and puts its `CertReloader` in `http/server`, so a second server module would have to write rotation again. It's rebuilt here as two modules, one per side, and server rotation ships inside the server module: any server gets it just by calling `TLSConfig`.

## What Changes

- New module `tls/tlsclientconfig` (`github.com/sikhlana/pkg/tls/tlsclientconfig`), starting at 0.1.0. A `Config` that a client module embeds in its own, and `Config.TLSConfig()`, which returns the `*tls.Config` the client uses:
  - **Off by default.** `enabled` is the switch. When it's false, `TLSConfig` returns a nil `*tls.Config` and reads no files, whatever else is set. nil is the only value every consumer reads as "no TLS settings".
  - **Server verification.** `ca_file` replaces the system roots with its certificates. `server_name` overrides the name the server's certificate is checked against. `insecure_skip_verify` turns verification off, for development only.
  - **Mutual TLS.** `cert_file` and `key_file` load a client key pair. Setting only one of them fails validation.
  - **Files are read once,** when `TLSConfig` is called. Errors name the path, never the file's contents.
- New module `tls/tlsserverconfig` (`github.com/sikhlana/pkg/tls/tlsserverconfig`), starting at 0.1.0. A `Config` that a server module embeds, and `Config.TLSConfig(logger)`, which returns the `*tls.Config` the server uses:
  - **Off by default,** like the client side. When it's enabled, `cert_file` and `key_file` are required.
  - **Mutual TLS.** `client_ca_file` holds the CAs that client certificates must chain to, and `client_auth` picks the policy (`none`, `request`, `require`, `verify_if_given`, `require_and_verify`).
  - **Rotation without a restart.** The key pair and the client CAs are re-read during a handshake, at most once per `reload_interval` (default 30s). Changed files are swapped in, and a file that fails to read or parse keeps the last good value. There's no watcher, goroutine or runner to start, so a consumer can't forget to run one.
  - **No stale certificate for clients without SNI.** The key pair is served through `GetCertificate`, never through `Certificates`. crypto/tls would serve `Certificates[0]` to a client that sends no server name, even when a `GetCertificate` hook is set.
  - **Logging.** It logs a successful reload at Info, and a failed reload that keeps the last good value at Warn, through the `*slog.Logger` passed to `TLSConfig`. Records name paths, never contents. Failures when `TLSConfig` is first called are returned, not logged.
- How it differs from go-modules' `tlsconfig` and `CertReloader`:
  - Two modules, client and server, instead of a client-only module with rotation buried in `http/server`.
  - `Build` plus `Active` becomes `TLSConfig`, gated by an explicit `enabled` rather than "any field set".
  - The pinned TLS 1.2 floor is gone. Go has defaulted to TLS 1.2 for clients since 1.18 and for servers since 1.22.
  - The fsnotify watcher and its run loop become a lazy refresh inside the handshake hooks. go-modules' safety-net ticker was already what actually guaranteed rotation.
  - New: `server_name`, client certificate authentication on the server, refreshing client CAs on the server, and serving the key pair only through `GetCertificate`.
- Constitution:
  - **`*_file` and `*_path` are split.** A `*_file` field names a file the module reads itself, and the module may re-read it, which makes it a live counterpart to configloader's `_FILE`. A `*_path` field is a path used as a path (a socket, a directory). A credential that rotates belongs in a `*_file` field. The rule that a Config never has both `x` and `x_file` stays: it's what keeps their env names apart.
- `docs/config.md` follows the amended rule wherever it sends rotating credentials to `*_path` fields.
- New `docs/tls.md`: why lazy refresh rather than a watcher, why rotation lives in `tlsserverconfig` rather than in each server, the `GetCertificate`/SNI pitfall, and why nil means disabled.
- The root README gains two Modules rows, and `go.work` registers both modules.

## Capabilities

### New Capabilities

- `tlsclientconfig`: the `enabled` gate and the nil result, CA trust, the server-name override, skipping verification, the mutual-TLS key pair and its both-or-neither rule, and what errors may contain.
- `tlsserverconfig`: the `enabled` gate and the nil result, the required key pair, client authentication policies, lazy refresh of the key pair and client CAs, keeping the last good value, serving the key pair only through `GetCertificate`, and what gets logged.

### Modified Capabilities

None. `configloader`'s behaviour doesn't change. Only the naming rule it documents for module path fields changes, and that rule lives in the constitution and the READMEs, not in a spec requirement.

## Impact

- **`tls/tlsclientconfig` (new module):** all of its exported API is new. Its exposure shape is a plain function: a method on `Config`. It has a Config, no fx Module and no runner.
- **`tls/tlsserverconfig` (new module):** all of its exported API is new. Its exposure shape is a plain function: a method on `Config` that takes a `*slog.Logger` as developer wiring. Refreshing happens inside the returned `*tls.Config` rather than in a loop, so it has a Config, no fx Module and no runner.
- **`config/configloader`:** README only. Its env-collision and rotation paragraphs move from `*_path` to the amended `*_file`/`*_path` rule. Its exported API is unchanged.
- **`config/configvalidator`:** README only. Its delegation example renames `cert_path` to `cert_file`. Its exported API is unchanged.
- **New dependencies:** none beyond the standard library. testify is used by tests only. Validation uses only `validate` tags, so neither module imports `config/configvalidator`.
- **Docs and repo files:** `openspec/config.yaml` (the `*_file`/`*_path` amendment), `docs/config.md`, `docs/tls.md` (new), `README.md` (two Modules rows), `go.work` (`use ./tls/tlsclientconfig`, `use ./tls/tlsserverconfig`).
- **Release:** one pull request. Neither module depends on the other or on any sibling. Merging publishes `tls/tlsclientconfig/v0.1.0` and `tls/tlsserverconfig/v0.1.0`, plus patch releases of `config/configloader` and `config/configvalidator` for their README edits.
