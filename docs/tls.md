# TLS

[tls/tlsconfig/README.md](../tls/tlsconfig/README.md) says how to turn operator settings into a `*tls.Config`. This page says why the module is shaped that way. The full discussion, including the alternatives rejected, is in the add-tls change's design.md under [openspec/changes](../openspec/changes).

## One module for both sides

Client and server settings barely overlap as config, but they share the hardest code in the module: the reloader that keeps a key pair current as its files rotate. Two modules would have had to copy it, or put it in a third module both require, published first, with its API made public since the repo has no `internal/` packages. Neither side has a dependency outside the standard library, so the usual reason to split a module, keeping one consumer from building another's dependencies, doesn't apply.

## Disabled means nil

`enabled: false` makes `TLSConfig` return a nil `*tls.Config`, before it validates anything or reads any file. nil is the one value every consumer reads as "no TLS settings": plaintext for redis and pgx, plaintext for a server, and default TLS for an https URL in `http.Transport`. A zero `&tls.Config{}` would mean "TLS on" to every consumer that checks for nil.

The switch is explicit rather than inferred from other fields being set. Inferring it meant that setting `insecure_skip_verify` alone turned TLS on, and every consumer had to remember to ask whether the settings were active. While TLS is off the other fields are ignored rather than rejected, so an operator can switch it off without deleting the paths.

## The key pair is served through GetCertificate only

crypto/tls calls a config's `GetCertificate` hook only when `Certificates` is empty or the client sent a server name. With both set, a client that sends no SNI, such as one dialling by IP address, gets `Certificates[0]`. After a rotation that's the certificate loaded at startup, however current the hook is. So the server leaves `Certificates` empty and serves the pair through the hook alone, and a consumer must not fill it in. The client does the same with `GetClientCertificate`, for symmetry: crypto/tls ignores `Certificates` when that hook is set.

`Clone` copies function fields, so the hook survives `http.Server` cloning the config to add `h2`, and any copy a server module makes.

## Refreshing is lazy and lives in the module

Key pairs and CA files, on both sides, are re-read during a handshake, at most once per `reload_interval`, rather than by a file watcher:

- **There's nothing to run.** A watcher needs a loop, so every consumer would have to remember to start one, and a forgotten one fails silently with a stale certificate. Under the constitution's runner rule it would also need its own `ProvideRunner`. A check inside the handshake runs whenever the certificate is needed, which is exactly when it matters.
- **A watcher wasn't the real guarantee anyway.** go-modules' fsnotify reloader also ran a safety-net ticker, because events get lost on atomic renames and network filesystems, and the ticker is what actually made rotation reliable. Kubernetes secret updates lag by about a minute too, so a 30-second check costs nothing an operator would notice.
- **It costs one handshake per interval.** The handshake that finds the interval passed takes a lock with `TryLock`, re-reads the files and compares their SHA-256 digests with the last ones seen; others keep the current value and don't wait. Reading two small PEM files takes microseconds.

Contents are compared rather than modification times, by digest so that a second plaintext copy of a private key isn't kept for the life of the process, and because Kubernetes swaps a `..data` symlink and a file's own mtime can stay the same. Contents that fail to parse are remembered as seen, so a half-written pair (the certificate replaced before the key) keeps the last good one and warns once, then swaps when the key catches up.

"The last good one" means more than "the last one that parses". A refreshed key pair whose leaf has expired, or isn't valid yet, is rejected the same way, so a broken renewal pipeline can't swap a working certificate for one that fails every handshake. The first load isn't checked: refusing to start helps nobody, and an expired certificate at startup is the operator's to see.

Each value is logged at Info when it's first loaded and whenever it's swapped, and a key pair's records name its leaf by subject, serial and expiry. During a rotation incident the first questions are which certificate a process is using and when it expires, and the log answers both without anything secret in it.

Rotation lives in this module rather than in each server module, as go-modules had it in `http/server`, so a second server module, or any client, gets it by calling `TLSConfig` instead of writing it again.

A client's key pair refreshes the same way, through `GetClientCertificate`, which crypto/tls calls on every handshake in which the server asks for a certificate. Without it, mutual TLS against a cert-manager issuer would fail once the client's first certificate expired, and gradually, as kept-alive connections were recycled, so it would look like a flaky network.

## A client's CAs are verified in its hooks, with a backstop

A client's `ca_file` refreshes too, which is harder: crypto/tls has no per-handshake hook for `RootCAs`. Read once, it would make every CA rotation need a client restart between distributing the new CA and re-issuing the servers' certificates, a manual step that's easy to forget because CA rotations are rare.

So with `ca_file`, the client follows crypto/tls's own `ExampleConfig_verifyConnection`: `InsecureSkipVerify` turns native verification off, and `VerifyConnection` verifies the chain against the current CAs and the leaf against `ConnectionState.ServerName`. crypto/tls leaves that empty for an IP address, since SNI can't carry one, so `server_name` stands in, and x509 matches it against IP SANs. A dialled IP address can't stand in: `tls.Dial` and `http.Transport` set it on a copy of the config the hook never sees. So a client dialling an IP address with `ca_file` must set `server_name` to it, where native verification would check the dialled address on its own, and a handshake with no name at all fails, saying so. That pattern fails open: a consumer that replaced the hook instead of wrapping it would verify nothing. Two things narrow it:

- **`VerifyPeerCertificate` checks the chain as well,** against the current CAs. Replacing `VerifyConnection`, the likely mistake when adding a pin or a tenant check, still rejects a certificate no current CA issued. It can't check the server name, since that hook isn't given one, so that check is what's lost; an impersonator would need a certificate from the operator's own private CA. The chain is verified twice per full handshake, which costs microseconds. Resumed connections skip `VerifyPeerCertificate` but still run `VerifyConnection`, and their session came from a handshake that was verified in full.
- **`RootCAs` holds the CAs first read.** A copy that sets `InsecureSkipVerify` back to false gets native verification against them, which fails once the CAs rotate rather than trusting too much.

Without `ca_file`, or with `insecure_skip_verify`, none of this applies: the system roots don't come from a file, and nothing needs verifying.

## A server's client CAs are verified in VerifyConnection

crypto/tls verifies client certificates against `ClientCAs`, a field read straight off the config in use. Swapping it needs a `GetConfigForClient` hook returning a clone with the new pool, but that hook can only clone a config it captured earlier, never the one actually handling the handshake. Anything a server module changed on its own copy would vanish: `http.Server` adds `h2` to `NextProtos` on its clone, so HTTP/2 would stop negotiating whenever mutual TLS was on.

So for the verifying policies the native `ClientAuth` is set one step down (`RequireAnyClientCert` or `RequestClientCert`), and a `VerifyConnection` hook does what crypto/tls would: it verifies the leaf against the current pool, with the rest of the chain as intermediates and `ExtKeyUsageClientAuth`. Two consequences follow:

- **No CA list is sent.** `ClientCAs` stays nil, so the CertificateRequest names no CAs. A list built at startup would go stale after a rotation, and a Go client holding only a certificate from the new CA would then send none.
- **`VerifiedChains` stays empty.** Consumers read `PeerCertificates`, which hold the verified client chain once the handshake succeeds. A consumer should wrap `VerifyConnection` rather than replace it; `VerifyPeerCertificate` backs it up on full handshakes, as on the client.

## Hooks must do what crypto/tls would have done

Every hook here replaces a native behaviour, so it has to match it, not just approximate it. Two places were caught short in review:

- **A client's certificate choice.** With `Certificates`, crypto/tls sends a certificate only if the server's request accepts it, by signature scheme and by the CAs the server lists, and otherwise sends none. `GetClientCertificate` replaces that choice, so it makes the same check: a server that only asks for a certificate, listing CAs ours isn't from, completes the handshake as it would with a native client, instead of rejecting a certificate it never needed.
- **The server's backstop.** The client checks the chain in `VerifyPeerCertificate` as well as `VerifyConnection` (above); the server does the same, so a server module replacing `VerifyConnection` to add a tenant check still rejects certificates no current client CA issued.

Two native checks aren't matched, and don't need to be yet. In FIPS mode crypto/tls filters verified chains through its FIPS policy, which the hooks don't. And when Encrypted Client Hello is rejected, a native client checks the public name against `RootCAs`, which holds a client's first CAs. Neither FIPS mode nor ECH is in use.
