## 1. Scaffold

Both modules are new, so they start at 0.1.0 with no bump task. Neither has a mocked interface, so there's no go generate task. Nothing is copied from go-modules' `tlsconfig` or `CertReloader`: code, godoc and comments are all written here, from what this code does.

- [ ] 1.1 Branch `add-tls` off `origin/main` after `git fetch`.
- [ ] 1.2 Create `tls/tlsclientconfig/` and `tls/tlsserverconfig/`, each with:
  - a `go.mod` for `github.com/sikhlana/pkg/tls/<package>`, with the go directive from `.go-version`;
  - a `VERSION` containing `0.1.0`;
  - stretchr/testify as the only requirement, for tests.
  - Done when `go build ./...` passes in both.
- [ ] 1.3 Register `./tls/tlsclientconfig` and `./tls/tlsserverconfig` in the root `go.work`.

## 2. tlsclientconfig

- [ ] 2.1 `config.go`: `Config`, `ErrIncompleteKeyPair`, `ErrNoCertificates` and `Validate`, which returns nil when disabled and `ErrIncompleteKeyPair` for half a key pair. See design decisions 1–3.
- [ ] 2.2 `tlsclientconfig.go`: `TLSConfig` returns nil when disabled, then runs `Validate`, then builds a fresh pool from `ca_file`, the key pair and `ServerName`, with `InsecureSkipVerify` behind a justified `//nolint:gosec`. Errors are wrapped with `%w` and name the path, and no `MinVersion` is set. See design decisions 4 and 8.
- [ ] 2.3 Test fixtures: a helper that writes a CA, and a server or client leaf it issues, as PEM into `t.TempDir()`, using ecdsa and `x509.CreateCertificate` and failing through `require.NoError`.
- [ ] 2.4 Tests for every scenario in specs/tlsclientconfig/spec.md, handshaking over `net.Pipe` against a `tls.Server`:
  - disabled, including missing paths, and validation passing;
  - the system roots rejecting a private CA;
  - the CA file accepting its own leaf and rejecting another, missing, holding no certificates, and changed after the build;
  - the server name override seen by the server;
  - skipping verification;
  - the key pair seen by a server that requires it, half a pair, and a mismatched key;
  - a marker string kept out of the error.

## 3. tlsserverconfig

- [ ] 3.1 `config.go`: `Config`, `ClientAuth` and its constants, `DefaultReloadInterval`, the five sentinels, `Validate`, and the unexported resolvers for the interval (zero means the default) and the policy (unset means `require_and_verify` with a client CA file and `none` without). See design decisions 1–3.
- [ ] 3.2 `reloader.go`: the unexported refreshing state, an `atomic.Pointer` for the current value plus a `TryLock`-guarded record of the last bytes seen and the last check. It re-reads, compares bytes, and parses and swaps only on change. On failure it keeps the last good value, logs a parse failure once per distinct content, logs a read failure at every check, and logs a swap at Info, all with typed attributes naming paths only. A nil logger means no records. See design decision 6.
- [ ] 3.3 `tlsserverconfig.go`: `TLSConfig(logger)` returns nil when disabled, then runs `Validate`, then loads the key pair and, when verifying, the client CA pool, returning errors that name the path. The key pair is served through `GetCertificate` only, the native `ClientAuth` follows the policy table, and `ClientCAs` stays nil. For the verifying policies, a `VerifyConnection` verifies against the current pool with intermediates, `ExtKeyUsageClientAuth` and the config's time. See design decisions 5 and 7.
- [ ] 3.4 Test fixtures: the CA and leaf helper from 2.3, written again here because modules can't share test code, plus client leaves for mTLS. Add a recording `slog.Handler` for the logging scenarios.
- [ ] 3.5 Tests for every scenario in specs/tlsserverconfig/spec.md, inside `testing/synctest` bubbles so intervals pass instantly, handshaking over `net.Pipe`:
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
- [ ] 3.6 A test that `VerifyConnection` is set for `verify_if_given` and `require_and_verify` and unset otherwise, so a refactor can't drop verification silently. See design Risks.

## 4. Constitution and docs

- [ ] 4.1 In `openspec/config.yaml`, under Config vs developer-time wiring, replace "A module's own path fields are named *_path." with the `*_file`/`*_path` split and the rotating-credential rule from design decision 10. Keep "never both x and x_file".
- [ ] 4.2 In `docs/config.md`'s `_FILE` and rotation section, point the two `*_path` sentences at `*_file` fields that the module re-reads, and link `docs/tls.md` as the example.
- [ ] 4.3 Write `docs/tls.md` from the rollup list in design.md: why nil means disabled, why the key pair is served through `GetCertificate` only, why refreshing is lazy and lives in the module, and why client CAs are verified in `VerifyConnection` with no CA list sent.
- [ ] 4.4 Write `tls/tlsclientconfig/README.md` in the constitution's order:
  - the overview;
  - Configuration, with every field;
  - Exported Members: `Config`, `Config.Validate`, `Config.TLSConfig` and both sentinels;
  - Usage, Standalone: embedding in a client's Config, handing the result to a client, and nil meaning no TLS;
  - Usage, With fx: none of its own, so say how a consumer's provider calls it and how a consumer's `Validate` delegates with `configvalidator.ValidateField`;
  - a TOML example and the env names.
  - No backticks in table cells.
- [ ] 4.5 Write `tls/tlsserverconfig/README.md` in the same order, with every export (including the `ClientAuth` constants and `DefaultReloadInterval`), and with:
  - the policy table;
  - how refreshing works and what's logged;
  - wrapping, never replacing, `VerifyConnection`;
  - reading `PeerCertificates`, not `VerifiedChains`.
- [ ] 4.6 In the root `README.md`, add the `tls/tlsclientconfig` and `tls/tlsserverconfig` rows to the Modules table in alphabetical order.
- [ ] 4.7 In `config/configloader/README.md`, change the env-collision paragraph's "Name a module's own path fields `*_path` instead" and the secrets paragraph's rotation sentence to the `*_file` rule.
- [ ] 4.8 In `config/configvalidator/README.md`, rename the delegation example's `cert_path` and `CertPath` to `cert_file` and `CertFile`, including the rendered error line.

## 5. Version bumps

- [ ] 5.1 `config/configloader/VERSION`: patch bump, for the README-only change.
- [ ] 5.2 `config/configvalidator/VERSION`: patch bump, for the README-only change.

## 6. Verification

- [ ] 6.1 In `tls/tlsclientconfig/`: `modernize -fix ./...`, then `golangci-lint run --fix ./...` (and again without `--fix` if it changed files), then the full `go test ./...`.
- [ ] 6.2 The same chain in `tls/tlsserverconfig/`, plus `go test -race ./...` for the reloader's concurrency.
- [ ] 6.3 The same chain in `config/configloader/`.
- [ ] 6.4 The same chain in `config/configvalidator/`.
- [ ] 6.5 Run `go test -short ./...` across every module in `go.work`. If it fails, fix it and repeat 6.1–6.5.
