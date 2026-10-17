## 1. Scaffold

Both modules are new, so they start at 0.1.0 with no bump task. Neither has a mocked interface, so there's no go generate task. Nothing is copied from go-modules' `tlsconfig` or `CertReloader`: code, godoc and comments are all written here, from what this code does.

- [x] 1.1 Branch `add-tls` off `origin/main` after `git fetch`.
- [x] 1.2 Create `tls/tlsclientconfig/` and `tls/tlsserverconfig/`, each with:
  - a `go.mod` for `github.com/sikhlana/pkg/tls/<package>`, with the go directive from `.go-version`;
  - a `VERSION` containing `0.1.0`;
  - stretchr/testify as the only requirement, for tests.
  - Done when `go build ./...` passes in both.
- [x] 1.3 Register `./tls/tlsclientconfig` and `./tls/tlsserverconfig` in the root `go.work`.

## 2. tlsclientconfig

- [x] 2.1 `config.go`: `Config`, `ErrIncompleteKeyPair`, `ErrNoCertificates` and `Validate`, which returns nil when disabled and `ErrIncompleteKeyPair` for half a key pair. See design decisions 1–3.
- [x] 2.2 `tlsclientconfig.go`: `TLSConfig` returns nil when disabled, then runs `Validate`, then builds a fresh pool from `ca_file`, the key pair and `ServerName`, with `InsecureSkipVerify` behind a justified `//nolint:gosec`. Errors are wrapped with `%w` and name the path, and no `MinVersion` is set. See design decisions 4 and 8.
- [x] 2.3 Test fixtures: a helper that writes a CA, and a server or client leaf it issues, as PEM into `t.TempDir()`, using ecdsa and `x509.CreateCertificate` and failing through `require.NoError`.
- [x] 2.4 Tests for every scenario in specs/tlsclientconfig/spec.md, handshaking over `net.Pipe` against a `tls.Server`:
  - disabled, including missing paths, and validation passing;
  - the system roots rejecting a private CA;
  - the CA file accepting its own leaf and rejecting another, missing, holding no certificates, and changed after the build;
  - the server name override seen by the server;
  - skipping verification;
  - the key pair seen by a server that requires it, half a pair, and a mismatched key;
  - a marker string kept out of the error.

## 3. tlsserverconfig

- [x] 3.1 `config.go`: `Config`, `ClientAuth` and its constants, `DefaultReloadInterval`, the five sentinels, `Validate`, and the unexported resolvers for the interval (zero means the default) and the policy (unset means `require_and_verify` with a client CA file and `none` without). See design decisions 1–3.
- [x] 3.2 `reloader.go`: the unexported refreshing state, an `atomic.Pointer` for the current value plus a `TryLock`-guarded record of the last bytes seen and the last check. It re-reads, compares bytes, and parses and swaps only on change. On failure it keeps the last good value, logs a parse failure once per distinct content, logs a read failure at every check, and logs a swap at Info, all with typed attributes naming paths only. A nil logger means no records. See design decision 6.
- [x] 3.3 `tlsserverconfig.go`: `TLSConfig(logger)` returns nil when disabled, then runs `Validate`, then loads the key pair and, when verifying, the client CA pool, returning errors that name the path. The key pair is served through `GetCertificate` only, the native `ClientAuth` follows the policy table, and `ClientCAs` stays nil. For the verifying policies, a `VerifyConnection` verifies against the current pool with intermediates, `ExtKeyUsageClientAuth` and the config's time. See design decisions 5 and 7.
- [x] 3.4 Test fixtures: the CA and leaf helper from 2.3, written again here because modules can't share test code, plus client leaves for mTLS. Add a recording `slog.Handler` for the logging scenarios.
- [x] 3.5 Tests for every scenario in specs/tlsserverconfig/spec.md, inside `testing/synctest` bubbles so intervals pass instantly, handshaking over `net.Pipe`:
  - disabled, and validation passing with an incomplete config;
  - the missing pair and a mismatched pair;
  - a client without SNI, before and after a rotation;
  - refresh after the interval and not before, the 29s and 31s default, and a negative interval;
  - keeping the last good pair while the key lags behind, then catching up, and after a deletion;
  - every row of the policy table, the unset policy with and without a client CA file, a verifying policy without one, and an unknown policy;
  - client CA rotation accepting the new CA and rejecting the old, and a CA file emptied;
  - a cloned config with an added ALPN protocol rotating and negotiating it;
  - one Info record per swap, one Warn record across three intervals of the same broken pair, no logger;
  - a marker string kept out of errors and records.
- [x] 3.6 A test that `VerifyConnection` is set for `verify_if_given` and `require_and_verify` and unset otherwise, so a refactor can't drop verification silently. See design Risks.

## 4. Constitution and docs

- [x] 4.1 In `openspec/config.yaml`, under Config vs developer-time wiring, replace "A module's own path fields are named *_path." with the `*_file`/`*_path` split and the rotating-credential rule from design decision 10. Keep "never both x and x_file".
- [x] 4.2 In `docs/config.md`'s `_FILE` and rotation section, point the two `*_path` sentences at `*_file` fields that the module re-reads, and link `docs/tls.md` as the example.
- [x] 4.3 Write `docs/tls.md` from the rollup list in design.md: why nil means disabled, why the key pair is served through `GetCertificate` only, why refreshing is lazy and lives in the module, and why client CAs are verified in `VerifyConnection` with no CA list sent.
- [x] 4.4 Write `tls/tlsclientconfig/README.md` in the constitution's order:
  - the overview;
  - Configuration, with every field;
  - Exported Members: `Config`, `Config.Validate`, `Config.TLSConfig` and both sentinels;
  - Usage, Standalone: embedding in a client's Config, handing the result to a client, and nil meaning no TLS;
  - Usage, With fx: none of its own, so say how a consumer's provider calls it and how a consumer's `Validate` delegates with `configvalidator.ValidateField`;
  - a TOML example and the env names.
  - No backticks in table cells.
- [x] 4.5 Write `tls/tlsserverconfig/README.md` in the same order, with every export (including the `ClientAuth` constants and `DefaultReloadInterval`), and with:
  - the policy table;
  - how refreshing works and what's logged;
  - wrapping, never replacing, `VerifyConnection`;
  - reading `PeerCertificates`, not `VerifiedChains`.
- [x] 4.6 In the root `README.md`, add the `tls/tlsclientconfig` and `tls/tlsserverconfig` rows to the Modules table in alphabetical order.
- [x] 4.7 In `config/configloader/README.md`, change the env-collision paragraph's "Name a module's own path fields `*_path` instead" and the secrets paragraph's rotation sentence to the `*_file` rule.
- [x] 4.8 In `config/configvalidator/README.md`, rename the delegation example's `cert_path` and `CertPath` to `cert_file` and `CertFile`, including the rendered error line.

## 5. Version bumps

- [x] 5.1 `config/configloader/VERSION`: patch bump, for the README-only change.
- [x] 5.2 `config/configvalidator/VERSION`: patch bump, for the README-only change.

## 6. Verification

- [x] 6.1 In `tls/tlsclientconfig/`: `modernize -fix ./...`, then `golangci-lint run --fix ./...` (and again without `--fix` if it changed files), then the full `go test ./...`.
- [x] 6.2 The same chain in `tls/tlsserverconfig/`, plus `go test -race ./...` for the reloader's concurrency.
- [x] 6.3 The same chain in `config/configloader/`.
- [x] 6.4 The same chain in `config/configvalidator/`.
- [x] 6.5 Run `go test -short ./...` across every module in `go.work`. If it fails, fix it and repeat 6.1–6.5.

## 7. Code review fixes

The review raised nine findings. Two were rejected: keyed `configvalidator.Error` values would mean importing `configvalidator` (design decision 3), and a hung filesystem only blocks the one handshake that runs the check. The design and specs record the rest.

- [x] 7.1 `Validate` rejects `client_ca_file` beside `none`, `request` or `require` with the new `ErrClientCAUnused`. The policy-table and `VerifyConnection` tests set a client CA file only for the verifying policies, and a test covers the rejected combination.
- [x] 7.2 `TLSConfig`'s godoc and the server README say that `VerifyConnection` checks expiry against the returned config's `Time`, so `Time` belongs on that config, not on a copy.
- [x] 7.3 Key pair load errors name both `cert_file` and `key_file`, in both modules.
- [x] 7.4 The reloader remembers a SHA-256 digest of each file instead of its raw bytes.
- [x] 7.5 The client's two `ca_file` errors share the wording `loading ca_file <path>`.
- [x] 7.6 Repeat 6.1, 6.2 and 6.5.

## 8. One module: tls/tlsconfig

Review found that the client's read-once key pair breaks mutual TLS against a rotating issuer such as cert-manager. Refreshing it means sharing the server's reloader, so the two modules merge into `tls/tlsconfig` (design decision 9). Sections 1–7 above record the work as first built, under the old module names. `tls/tlsconfig` is new, so it starts at 0.1.0.

- [x] 8.1 Create `tls/tlsconfig/` with `go.mod`, `VERSION` 0.1.0 and the same requirements, and swap the two `go.work` entries for `./tls/tlsconfig`.
- [x] 8.2 `config.go`: `ClientConfig` (gaining `reload_interval`) and `ServerConfig`, `ClientAuth`, `DefaultReloadInterval` and the seven sentinels, with one `ErrNoCertificates` and one `ErrNegativeReloadInterval` shared by both sides.
- [x] 8.3 `reloader.go` unchanged, plus the PEM parsers; `server.go` from `tlsserverconfig.go`, with the key pair named "tls server key pair" in records.
- [x] 8.4 `client.go`: `ClientConfig.TLSConfig(logger)` loads `ca_file` once and presents the key pair through a reloader and `GetClientCertificate`, leaving `Certificates` empty, named "tls client key pair" in records.
- [x] 8.5 Port both test suites as `client_test.go` and `server_test.go` with one shared `helpers_test.go`, and add client refresh tests in synctest bubbles: `GetClientCertificate` only, refresh at 30s and not at 29s, the certificate written before the key, a cloned config, an Info record per swap, and a negative interval. Confirm the refresh tests fail with refreshing disabled.
- [x] 8.6 Write `tls/tlsconfig/README.md` from the two READMEs, in the constitution's order, with a Configuration table per side, every export, and client key pair refresh. Delete `tls/tlsclientconfig/` and `tls/tlsserverconfig/`.
- [x] 8.7 Point the root README row, `docs/config.md` and `docs/tls.md` at `tls/tlsconfig`. `docs/tls.md` gains why one module and why a client's key pair refreshes but its CA doesn't.
- [x] 8.8 Merge the two specs into specs/tlsconfig/spec.md, adding the client refresh requirement and client scenarios for copies, logging and contents, and update the proposal and design.
- [x] 8.9 In `tls/tlsconfig/`: `modernize -fix ./...`, then `golangci-lint run --fix ./...` (and again without `--fix` if it changed files), then the full `go test ./...` and `go test -race ./...`.
- [x] 8.10 Run `go test -short ./...` across every module in `go.work`. If it fails, fix it and repeat 8.9 and 8.10.

## 9. Refreshing a client's CAs

Asked for after the merge: a client that reads `ca_file` once needs a restart at every CA rotation. Option B+ from the discussion: verify in the hooks, with a backstop (design decision 11).

- [x] 9.1 `client.go`: with `ca_file` and without `insecure_skip_verify`, load the CAs through a reloader ("tls CAs"), set `InsecureSkipVerify` with a comment saying why, keep the first pool in `RootCAs`, verify chain and server name in `VerifyConnection` (failing without a name), and verify the chain again in `VerifyPeerCertificate`. Otherwise stay native.
- [x] 9.2 Tests in synctest bubbles: the hooks set only with a CA file; a rotated CA accepted at 30s, not at 29s, and the old one rejected after; an emptied CA file keeping the last good CAs; a hostname error for a certificate from the CA for another name; no server name; the backstop rejecting an unknown CA with `VerifyConnection` replaced; a cloned config; an Info record naming `ca_file`. Confirm the backstop test fails without the backstop and the refresh tests fail with refreshing disabled.
- [x] 9.3 The `ClientConfig` godoc, README (configuration, members, the four consequences for a client module, log rows), `docs/tls.md` and `docs/config.md` describe refreshing CAs and the backstop.
- [x] 9.4 The spec gains the client CA refresh requirement and cloned-CA scenario and drops "read once"; the proposal and design follow.
- [x] 9.5 Repeat 8.9 and 8.10.

## 10. End-to-end test

- [x] 10.1 `tlsconfig_test.go`: mutual TLS between an `http.Server` built from a `ServerConfig` and an `http.Client` built from a `ClientConfig`, over loopback, with HTTP/2 and a new connection per request. It goes through a leaf rotation, then a CA rotation done as cert-manager and trust-manager do it (new CAs beside the old, leaves re-issued, old CAs dropped), with neither side restarting, and a client holding a certificate from the dropped CA is turned away. It runs in real time with a 100ms reload interval, since synctest can't drive real sockets; the server's error log is discarded through `slog.NewLogLogger`, as the `log` package is banned.
- [x] 10.2 Run it 10 times, and 10 more under `-race`, to check for flakiness, and confirm it fails with refreshing disabled.
- [x] 10.3 Repeat 8.9 and 8.10.

## 11. Review fixes

A second review found one regression and five smaller issues. The design, spec and README record the outcomes.

- [x] 11.1 HIGH: with `ca_file`, a client couldn't connect to a server by IP address, because crypto/tls empties `ConnectionState.ServerName` for IPs. `VerifyConnection` now falls back to the `ClientConfig`'s `ServerName`, captured in the closure, which x509 matches against IP SANs. A dialled IP with no `server_name` still fails, and `errNoServerName` now says to set it. The godoc, README, `docs/tls.md`, spec (a requirement sentence and two new scenarios) and design decision 11 and its Risks describe this and how it differs from native verification.
- [x] 11.2 Tests in synctest bubbles: `serverLeafFor` issues IP SANs for IP names; `server_name` set to `10.0.0.5` accepts a leaf for that IP and rejects one for `10.0.0.6` with a hostname error; dialling `10.0.0.5` without `server_name` fails with the new message. Confirmed the first fails without the fallback.
- [x] 11.3 `ca_file` beside `insecure_skip_verify` stays tolerated rather than rejected; design decision 11 records why, unlike `ErrClientCAUnused`.
- [x] 11.4 The root README's Modules row says key pairs and CA files are re-read.
- [x] 11.5 The spec's system-roots scenario expects a certificate verification error, not an unknown-authority one, which macOS and Windows platform verifiers don't return. The design's testing fact names the buffered in-memory connection the tests use rather than `net.Pipe`, and records the deviation.
- [x] 11.6 The end-to-end test requires the server's `bad certificate` alert when the stale client is turned away, so a timeout or dial failure can't pass.
- [x] 11.7 The README's Refreshing and logging section says a swap landing between a key pair's two reads gives one spurious Warn, then the swap one interval later.
- [x] 11.8 The CA-file setup moves out of `ClientConfig.TLSConfig` into `verifyWithCAFile`, since the fallback took `TLSConfig` past gocognit's limit. Re-confirmed that the IP-address test fails without the fallback.
- [x] 11.9 Repeat 8.9 and 8.10.

## 12. A nil logger becomes a discarding one

- [x] 12.1 Both `TLSConfig` methods replace a nil logger with `slog.New(slog.DiscardHandler)` after `Validate`, so the reloader always has one and drops its own nil check. The godoc and README say a nil logger discards the records; the spec's "without a logger" behaviour is unchanged, and the existing no-logger tests still cover it.
- [x] 12.2 Repeat 8.9 and 8.10.

## 13. Principal review fixes

The review checked every hook against the native behaviour it replaces, and the module's operability. Findings 1–4, 6 and 7 are applied; 5 (retrying sooner after a key pair's two reads straddle a swap) is left as documented. Design decision 12 records the outcome.

- [x] 13.1 `GetClientCertificate` sends an empty certificate when `CertificateRequestInfo.SupportsCertificate` rejects the pair, as crypto/tls does with `Certificates`. Test: a server that asks for a certificate without requiring one, listing another CA, completes the handshake without one, and listing ours gets it.
- [x] 13.2 The server checks a presented client chain in `VerifyPeerCertificate` too, against the current client CAs. Tests: with `VerifyConnection` replaced, a certificate from another CA is rejected and one from the client CA accepted; the hook-presence test covers `VerifyPeerCertificate`.
- [x] 13.3 Each value is logged at Info when it's loaded as well as when it's swapped, and key pair records add the leaf's `subject`, `serial` and `not_after`. A `format` per kind of file carries its parser, describer and check. The recorder test helper filters records by message.
- [x] 13.4 A refreshed key pair whose leaf has expired or isn't valid yet is rejected and the last good pair kept, warned once with the new `ErrCertificateNotCurrent`; the first load isn't checked. Tests: expired and not-yet-valid server pairs, an expired client pair, and an expired pair at the start. A fixture issues leaves with a chosen validity window.
- [x] 13.5 `verifyServer` and `verifyClient` share `verifyChain` and `parseCertificates`, in `verify.go`.
- [x] 13.6 `errNoServerName` and `errNoServerCertificate` become `ErrNoServerName` and `ErrNoServerCertificate`; the IP-address tests match the first with `errors.Is`.
- [x] 13.7 The constitution gains the exported-sentinel rule, with `package main` exempt. There's no linter for it, so it stays a written rule.
- [x] 13.8 Confirmed each new test fails with its fix removed. The godoc, README (members, client certificate choice, server backstop, validity check, log table), `docs/tls.md` (a new section on hooks matching native behaviour), spec, proposal and design follow.
- [x] 13.9 Repeat 8.9 and 8.10.

## 14. kernelrun under the exported-sentinel rule

- [x] 14.1 `kernelrun`: `errExited` becomes `ErrExited`, with godoc, a README Exported Members row and a mention in the README's crash rules. The Goexit test matches it with `errors.Is`, and a spec delta adds the guarantee.
- [x] 14.2 `kernelrun/VERSION`: minor bump, 0.1.0 to 0.2.0, for the new export.
- [x] 14.3 In `kernelrun/`: `modernize -fix ./...`, then `golangci-lint run --fix ./...` (and again without `--fix` if it changed files), then the full `go test ./...`; then repeat 8.10.
