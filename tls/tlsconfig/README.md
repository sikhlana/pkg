# tlsconfig

Turns operator TLS settings into the `*tls.Config` a client module dials with or a server module serves with: an explicit on switch, the CAs to trust, key pairs and a client certificate policy. Key pairs and CA files are re-read during handshakes as they rotate, with nothing for the module using them to run.

## Configuration

### ClientConfig

| Field | Type | Description |
|-------|------|-------------|
| enabled | bool | Turns TLS on. While false, TLSConfig returns nil and every other field is ignored. |
| ca_file | string | A PEM file whose certificates replace the system roots for verifying the server. Empty means the system roots. Re-read as it rotates. |
| server_name | string | The name the server's certificate is verified against, and sent as SNI, in place of the dialled host. |
| insecure_skip_verify | bool | Accepts any server certificate. For development only. |
| cert_file | string | A PEM certificate presented to servers that ask for one. Set with key_file. Re-read as it rotates. |
| key_file | string | The PEM private key for cert_file. Set with cert_file. Re-read as it rotates. |
| reload_interval | duration | The least time between checks of the CA and key pair files for changes. Zero means 30s. Must not be negative. |

### ServerConfig

| Field | Type | Description |
|-------|------|-------------|
| enabled | bool | Turns TLS on. While false, TLSConfig returns nil and every other field is ignored. |
| cert_file | string | The server's PEM certificate, with any intermediates after it. Required when enabled. Re-read as it rotates. |
| key_file | string | The PEM private key for cert_file. Required when enabled. Re-read as it rotates. |
| client_ca_file | string | A PEM file of the CAs client certificates must chain to. Required by the verifying policies, and rejected beside the others, which would never read it. Re-read as it rotates. |
| client_auth | string | The client certificate policy: none, request, require, verify_if_given or require_and_verify. Unset means require_and_verify with client_ca_file and none without. |
| reload_interval | duration | The least time between checks of the files for changes. Zero means 30s. Must not be negative. |

## Exported Members

| Name | Description |
|------|-------------|
| ClientConfig | A client's TLS settings, for a client module to embed in its own Config. |
| ClientConfig.Validate | Returns every problem with an enabled ClientConfig, joined: ErrIncompleteKeyPair and ErrNegativeReloadInterval. A disabled ClientConfig is always valid. Reads no files. |
| ClientConfig.TLSConfig | Returns the client's tls.Config, refreshing its CAs and key pair and logging through the given logger, or discarding the records when it's nil. Returns nil and no error when disabled. Runs Validate, then reads the files. Errors name the path, never a file's contents. |
| ServerConfig | A server's TLS settings, for a server module to embed in its own Config. |
| ServerConfig.Validate | Returns every problem with an enabled ServerConfig, joined: ErrMissingKeyPair, ErrNegativeReloadInterval, ErrUnknownClientAuth, ErrClientCARequired and ErrClientCAUnused. A disabled ServerConfig is always valid. Reads no files. |
| ServerConfig.TLSConfig | Returns the server's tls.Config, refreshing its key pair and client CAs and logging through the given logger, or discarding the records when it's nil. Returns nil and no error when disabled. Runs Validate, then reads the files. Errors name the path, never a file's contents. |
| ClientAuth | A server's client certificate policy. A named string, so it decodes from config with no hook. |
| ClientAuthNone | Doesn't ask clients for a certificate. |
| ClientAuthRequest | Asks for a certificate, but neither requires nor verifies one. |
| ClientAuthRequire | Requires a certificate, but doesn't verify it. |
| ClientAuthVerifyIfGiven | Asks for a certificate, and verifies one when it's presented. |
| ClientAuthRequireAndVerify | Requires a certificate, and verifies it. |
| DefaultReloadInterval | 30s, the reload interval when reload_interval is zero. |
| ErrIncompleteKeyPair | Returned by ClientConfig's Validate and TLSConfig when only one of cert_file and key_file is set. |
| ErrMissingKeyPair | Returned by ServerConfig's Validate and TLSConfig when an enabled ServerConfig lacks cert_file or key_file. |
| ErrUnknownClientAuth | Returned by ServerConfig's Validate and TLSConfig for a client_auth other than the ClientAuth constants. |
| ErrClientCARequired | Returned by ServerConfig's Validate and TLSConfig for verify_if_given or require_and_verify without client_ca_file. |
| ErrClientCAUnused | Returned by ServerConfig's Validate and TLSConfig for client_ca_file beside none, request or require, which would never read it. |
| ErrNegativeReloadInterval | Returned by either Validate and TLSConfig for a negative reload_interval. |
| ErrNoCertificates | Returned by either TLSConfig, naming the path, when ca_file or client_ca_file holds no PEM certificates. |
| ErrNoServerName | Fails a client's handshake when, with ca_file, it has no server name to verify against: it dialled an IP address, or nothing, without server_name. |
| ErrNoServerCertificate | Fails a client's handshake, with ca_file, when the server presented no certificate. |
| ErrCertificateNotCurrent | Carried by the Warn record when a refreshed key pair's leaf has expired or isn't valid yet, and the last good pair stays. Never returned. |

## Usage

### Standalone

Both TLSConfig methods return nil when TLS is off, which every consumer reads as "no TLS settings": plaintext for redis and pgx, default TLS for an https URL in `http.Transport`, and plaintext for a server. Don't replace it with `&tls.Config{}`, which those consumers read as TLS on. Neither sets a minimum version, so Go's default, TLS 1.2, applies and rises with Go. Set versions, cipher suites or ALPN on the returned config, or on a clone of it: copies keep refreshing.

Both call `Validate` themselves, so a config built by hand gets the module's sentinels rather than a file error. They return failures to read the files when they're called, and never log them. A missing file matches `fs.ErrNotExist`, and a key pair that can't be loaded fails naming both `cert_file` and `key_file`.

#### Clients

Embed `ClientConfig` in the client module's own Config, under a `tls` key:

```go
type Config struct {
	Address string                 `mapstructure:"address"`
	TLS     tlsconfig.ClientConfig `mapstructure:"tls"`
}
```

Build the `*tls.Config` once, when the client is created, and hand it over as it is:

```go
tlsCfg, err := cfg.TLS.TLSConfig(logger)
if err != nil {
	return nil, fmt.Errorf("building tls config: %w", err)
}

client := redis.NewClient(&redis.Options{Addr: cfg.Address, TLSConfig: tlsCfg})
```

`ca_file` replaces the system roots rather than adding to them, so a public CA can't vouch for an internal host. A client that needs both builds its own pool. Without `ca_file`, or with `insecure_skip_verify`, the config uses crypto/tls's native verification and nothing below applies.

crypto/tls can't swap `RootCAs` per handshake, so with `ca_file` the server's certificate is verified in the returned config's hooks, against the current CAs ([docs/tls.md](../../docs/tls.md) says why). That has four consequences for a client module:

- **`InsecureSkipVerify` reads true, and that's expected.** It turns native verification off so the hooks can verify against rotated CAs. `RootCAs` holds the CAs first read, so a copy that sets `InsecureSkipVerify` back to false still verifies, and fails rather than trusting too much once the CAs rotate.
- **Wrap `VerifyConnection`, never replace it.** It verifies the chain and the server name. `VerifyPeerCertificate` also checks the chain, as a backstop, so replacing `VerifyConnection` still rejects a certificate no current CA issued, but no longer checks the server name. Call the original first, as on a server (below).
- **Read `PeerCertificates`, not `VerifiedChains`.** `VerifiedChains` stays empty. Once the handshake succeeds, `PeerCertificates` holds the server's chain, verified, leaf first.
- **Set `Time` on the returned config, not on a copy.** The hooks check expiry against the returned config's `Time`, read at each handshake.

The server name verified is the one the client sends: `server_name` when it's set, otherwise the dialled host, which `tls.Dial` and `http.Transport` fill in. crypto/tls sends no name for an IP address, and the dialled address is filled in on a copy the hooks never see, so **to dial an IP address with `ca_file`, set `server_name` to it**; the certificate is then checked against its IP SANs. Without it, every handshake fails with `ErrNoServerName`. Native verification, without `ca_file`, checks a dialled IP address on its own.

The key pair is presented through `GetClientCertificate` only, so leave `Certificates` empty. As crypto/tls does with `Certificates`, it isn't sent when the server's request doesn't accept it, by signature scheme or by the CAs the server lists, so a server that only asks for a certificate still completes the handshake. It's refreshed during handshakes in which the server asks for a certificate. Connections kept alive across a rotation keep the CAs and certificate they were opened with, and new connections use the current ones.

An operator sets it in the client's section:

```toml
[redis]
address = "redis.internal:6380"

[redis.tls]
enabled = true
ca_file = "/etc/my-svc/tls/ca.pem"
cert_file = "/etc/my-svc/tls/client.crt"
key_file = "/etc/my-svc/tls/client.key"
```

Loaded through `config/configloader` under `my-svc`, the env names are `MY_SVC_REDIS_TLS_ENABLED`, `MY_SVC_REDIS_TLS_CA_FILE`, `MY_SVC_REDIS_TLS_SERVER_NAME`, `MY_SVC_REDIS_TLS_INSECURE_SKIP_VERIFY`, `MY_SVC_REDIS_TLS_CERT_FILE`, `MY_SVC_REDIS_TLS_KEY_FILE` and `MY_SVC_REDIS_TLS_RELOAD_INTERVAL`. The `*_file` fields hold paths that the module reads and re-reads itself, rather than configloader's read-once `_FILE` secrets, so there's no `tls.cert` key whose `_FILE` twin they'd collide with.

#### Servers

Embed `ServerConfig` in the server module's own Config, under a `tls` key:

```go
type Config struct {
	Address string                 `mapstructure:"address"`
	TLS     tlsconfig.ServerConfig `mapstructure:"tls"`
}
```

Build the `*tls.Config` once, when the server is created, and serve with it:

```go
tlsCfg, err := cfg.TLS.TLSConfig(logger)
if err != nil {
	return nil, fmt.Errorf("building tls config: %w", err)
}

srv := &http.Server{Addr: cfg.Address, Handler: h, TLSConfig: tlsCfg}

if tlsCfg != nil {
	err = srv.ListenAndServeTLS("", "")
} else {
	err = srv.ListenAndServe()
}
```

The key pair is served through `GetCertificate` only, so leave `Certificates` empty: crypto/tls would serve a stale `Certificates[0]` to clients that send no server name.

| Policy | Asks for a certificate | Requires one | Verifies a presented one |
|--------|------------------------|--------------|--------------------------|
| none | no | no | no |
| request | yes | no | no |
| require | yes | yes | no |
| verify_if_given | yes | no | yes |
| require_and_verify | yes | yes | yes |

A verified certificate must chain to a CA in `client_ca_file` and be valid for client authentication. Verification happens in the returned config's `VerifyConnection` hook rather than through `ClientCAs`, so that the client CAs can rotate ([docs/tls.md](../../docs/tls.md) says why). That has four consequences for a server module:

- **Wrap `VerifyConnection`, never replace it.** The native client authentication is set one step down and doesn't verify. `VerifyPeerCertificate` checks the client's chain too, as a backstop, so replacing `VerifyConnection` still rejects a certificate no current client CA issued, but wrapping keeps both checks on every connection, resumed ones included. Call the original first:

  ```go
  verify := tlsCfg.VerifyConnection
  tlsCfg.VerifyConnection = func(cs tls.ConnectionState) error {
  	if verify != nil {
  		if err := verify(cs); err != nil {
  			return err
  		}
  	}

  	return checkTenant(cs)
  }
  ```

- **Read `PeerCertificates`, not `VerifiedChains`.** `VerifiedChains` stays empty. Once the handshake succeeds, `PeerCertificates` holds the client's chain, verified, leaf first.
- **Set `Time` on the returned config, not on a copy.** A handshake doesn't say which copy it's using, so the hook checks expiry against the returned config's `Time`, read at each handshake.
- **Leave `ClientCAs` nil.** The server lists no acceptable CAs in its certificate request, so a client picks its certificate the same way before and after a rotation.

An operator sets it in the server's section:

```toml
[http]
address = ":8443"

[http.tls]
enabled = true
cert_file = "/etc/my-svc/tls/tls.crt"
key_file = "/etc/my-svc/tls/tls.key"
client_ca_file = "/etc/my-svc/tls/clients.pem"
client_auth = "require_and_verify"
reload_interval = "1m"
```

Loaded through `config/configloader` under `my-svc`, the env names are `MY_SVC_HTTP_TLS_ENABLED`, `MY_SVC_HTTP_TLS_CERT_FILE`, `MY_SVC_HTTP_TLS_KEY_FILE`, `MY_SVC_HTTP_TLS_CLIENT_CA_FILE`, `MY_SVC_HTTP_TLS_CLIENT_AUTH` and `MY_SVC_HTTP_TLS_RELOAD_INTERVAL`.

#### Refreshing and logging

During a handshake, once `reload_interval` has passed since the files were last checked, that one handshake re-reads them; the rest use the current values without waiting. When the contents changed and parse, they're swapped in from that handshake on. When they can't be read, or don't parse as a key pair or as at least one CA certificate, or bring a key pair whose leaf has expired or isn't valid yet, the last good value stays and the next check is one interval later. A broken renewal can't replace a pair that still works; the first load accepts any pair that parses, so a process can start on whatever it's given. Changes are seen by content, so a Kubernetes secret's symlink swap is picked up. A key pair's two files are read one after the other, so a swap that lands between the two reads gives one Warn that the pair doesn't parse, and the new pair is swapped in at the next check, one interval later. A rotation is picked up by the first handshake at least one interval after the previous check, so a busy client or server uses it within one interval.

Records go to the logger passed to `TLSConfig`; a nil one logs nothing and refreshes the same way. Each names the files by their keys, never their contents. Records about a key pair also name its leaf, by `subject`, `serial` and `not_after`, which say which certificate is in use and when it expires:

| Event | Level | Message | Attributes |
|-------|-------|---------|------------|
| A key pair is loaded by TLSConfig | Info | tls client key pair loaded, or tls server key pair loaded | cert_file, key_file, subject, serial, not_after |
| A key pair is swapped | Info | tls client key pair reloaded (server likewise) | cert_file, key_file, subject, serial, not_after |
| A refreshed key pair is outside its validity period | Warn | rejecting tls client key pair, keeping the last good one (server likewise) | cert_file, key_file, subject, serial, not_after, error |
| A key pair's files can't be read | Warn | reading tls client key pair failed, keeping the last good one (server likewise) | cert_file, key_file, error |
| A key pair's files don't parse | Warn | parsing tls client key pair failed, keeping the last good one (server likewise) | cert_file, key_file, error |
| A client's CAs are loaded by TLSConfig | Info | tls CAs loaded | ca_file |
| A client's CAs are swapped | Info | tls CAs reloaded | ca_file |
| A client's CA file can't be read | Warn | reading tls CAs failed, keeping the last good one | ca_file, error |
| A client's CA file holds no certificates | Warn | parsing tls CAs failed, keeping the last good one | ca_file, error |
| A server's client CAs are loaded by TLSConfig | Info | tls client CAs loaded | client_ca_file |
| The client CAs are swapped | Info | tls client CAs reloaded | client_ca_file |
| The client CA file can't be read | Warn | reading tls client CAs failed, keeping the last good one | client_ca_file, error |
| The client CA file holds no certificates | Warn | parsing tls client CAs failed, keeping the last good one | client_ca_file, error |

Contents that don't parse, or bring a key pair outside its validity period, are reported once, not at every check while they stay the same, so a certificate written before its key warns once and then swaps when the key catches up. A file that can't be read warns at every check until it's back.

### With fx

`tlsconfig` has no Module and no runner: refreshing happens inside the returned config's hooks. The client or server module's constructor calls `TLSConfig` on the Config it's given, with its injected logger:

```go
func New(p Params) (*Server, error) {
	tlsCfg, err := p.Config.TLS.TLSConfig(p.Logger)
	if err != nil {
		return nil, fmt.Errorf("building tls config: %w", err)
	}

	return newServer(p.Config.Address, tlsCfg), nil
}
```

`config/configloader` runs `Validate` when it loads the app's config, so an incomplete key pair or an unknown policy fails at startup under its key, such as `http.tls`. A Config with its own `Validate` method owns its subtree, so it checks the TLS section itself with `configvalidator.ValidateField`:

```go
func (c Config) Validate() error {
	return errors.Join(
		configvalidator.ValidateField(&c, &c.TLS),
		c.validateAddress(),
	)
}
```
