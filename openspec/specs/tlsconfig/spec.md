# tlsconfig Specification

## Purpose

Turns a client's or server's operator TLS settings into the TLS configuration a client module dials with or a server module serves with, and keeps key pairs and CAs current as their files rotate, without a restart or anything for the module using them to run.

## Requirements

### Requirement: A disabled config yields no TLS settings

When TLS isn't enabled, a client or server config SHALL return no TLS configuration (nil) and no error, and SHALL read no files, whatever else it sets. Validation SHALL pass for a disabled config whatever else it sets.

#### Scenario: The zero client config
- **WHEN** a client asks the zero config for its TLS configuration
- **THEN** it gets nil and no error

#### Scenario: A disabled client config with paths that don't exist
- **WHEN** TLS isn't enabled, the CA file and the key pair name files that don't exist, and the client asks for its TLS configuration
- **THEN** it gets nil and no error, and validating the config passes

#### Scenario: The zero server config
- **WHEN** a server asks the zero config for its TLS configuration
- **THEN** it gets nil and no error

#### Scenario: A disabled server config that would be invalid
- **WHEN** TLS isn't enabled, no key pair is set, and the client authentication policy names an unknown value
- **THEN** validating the config passes and the server gets nil and no error

### Requirement: An enabled client config verifies the server against the system roots by default

When TLS is enabled and no CA file is set, the TLS configuration SHALL verify the server's certificate against the system's trusted roots and the name the client dials.

#### Scenario: A server whose certificate the system doesn't trust
- **WHEN** TLS is enabled with nothing else set and the client handshakes with a server whose certificate chains to a private CA
- **THEN** the handshake fails with a certificate verification error

### Requirement: A CA file replaces the system roots

When a CA file is set, the server's certificate SHALL be verified against the certificates in that file only, and against the server name the client sends, or, when the client sends none, as for an IP address, against the configured server name, which MAY be an IP address. A file that can't be read SHALL fail with an error that names the path and matches the underlying file error. A file holding no PEM certificates SHALL fail with an error matching the module's no-certificates error and naming the path. A handshake with neither SHALL fail with an error matching the module's no-server-name error, saying to set the server name. Unlike native verification, a dialled IP address SHALL NOT be verified against without a configured server name, since the module can't see it.

#### Scenario: A server certificate from the CA
- **WHEN** the CA file holds a private CA and the client handshakes with a server whose certificate that CA issued
- **THEN** the handshake succeeds

#### Scenario: A server certificate from a publicly trusted CA
- **WHEN** the CA file holds a private CA and the client handshakes with a server whose certificate doesn't chain to it
- **THEN** the handshake fails with an unknown-authority error

#### Scenario: A missing CA file
- **WHEN** the CA file names a path that doesn't exist and the client asks for its TLS configuration
- **THEN** it gets an error naming the path that matches `fs.ErrNotExist`

#### Scenario: A CA file with no certificates
- **WHEN** the CA file holds no PEM certificate and the client asks for its TLS configuration
- **THEN** it gets an error naming the path that matches the no-certificates error

#### Scenario: A certificate from the CA for another name
- **WHEN** the CA file holds a private CA and the client dials `server.test`, and the server's certificate from that CA is valid for `other.test` only
- **THEN** the handshake fails with a hostname error

#### Scenario: No server name
- **WHEN** the CA file is set and the client handshakes without a server name or a dialled host
- **THEN** the handshake fails with an error matching the no-server-name error

#### Scenario: The server name set to an IP address
- **WHEN** the CA file holds a private CA, the server name is `10.0.0.5`, and the client dials a server whose certificate from that CA is valid for the IP address `10.0.0.5`
- **THEN** the handshake succeeds, and a certificate valid only for `10.0.0.6` fails with a hostname error

#### Scenario: An IP address dialled without a server name
- **WHEN** the CA file is set, no server name is configured, and the client dials `10.0.0.5`, whose certificate is valid for that IP address
- **THEN** the handshake fails with an error matching the no-server-name error

### Requirement: The client CA file refreshes from its file

During a handshake, when at least the reload interval has passed since the CA file was last checked, the client SHALL re-read it and, if its contents changed and hold at least one certificate, SHALL verify servers against the new CAs from that handshake on. Within the interval it SHALL verify against the CAs it has without reading the file. When the file can't be read or holds no certificates, it SHALL keep the last good CAs. With a CA file, the returned configuration SHALL report verification skipped, SHALL verify in its connection-verification hook, and SHALL check the chain against the current CAs in its peer-certificate hook too, so that a consumer replacing the connection-verification hook still rejects certificates no current CA issued. Its connection state SHALL carry the server's certificates as peer certificates and SHALL NOT carry verified chains. Without a CA file, or with verification skipped by the operator, the configuration SHALL verify natively.

#### Scenario: A rotated CA
- **WHEN** the CA file is replaced with a new CA and the client dials a server whose certificate the new CA issued, 30 seconds after the CAs were loaded
- **THEN** the handshake succeeds, and a server whose certificate only the old CA issued is rejected with an unknown-authority error

#### Scenario: A rotated CA within the interval
- **WHEN** the CA file is replaced with a new CA and the client dials 29 seconds after the CAs were loaded
- **THEN** a server whose certificate the old CA issued is accepted and one from the new CA is rejected

#### Scenario: A CA file emptied
- **WHEN** the CA file is replaced with a file holding no certificates and the client dials a server from the old CA after the interval
- **THEN** the handshake succeeds

#### Scenario: The connection-verification hook replaced
- **WHEN** a client module replaces the configuration's connection-verification hook with one that accepts everything, and dials a server whose certificate no CA in the CA file issued
- **THEN** the handshake fails with an unknown-authority error

#### Scenario: The system roots
- **WHEN** TLS is enabled without a CA file
- **THEN** the configuration doesn't report verification skipped and sets no verification hooks

### Requirement: The server name can be overridden

When a server name is set, the server's certificate SHALL be verified against that name instead of the dialled host, and the client SHALL send it as the server name indication.

#### Scenario: Dialling by IP address
- **WHEN** the server name is `db.internal`, the client dials the server by IP address, and the server's certificate is valid for `db.internal` only
- **THEN** the handshake succeeds and the server sees `db.internal` as the requested server name

### Requirement: Verification can be skipped

When skipping verification is set, the client SHALL accept any server certificate.

#### Scenario: An untrusted server
- **WHEN** skipping verification is set and the client handshakes with a server whose certificate chains to no trusted CA
- **THEN** the handshake succeeds

### Requirement: A client key pair enables mutual TLS

When both a certificate file and a key file are set, the client SHALL present that key pair to servers that ask for a client certificate and whose request accepts it, by signature scheme and by the CAs the server lists, and SHALL send no certificate to a server whose request doesn't, as a native client does. Setting only one of the two on an enabled config SHALL fail validation and building the TLS configuration with an error matching the module's incomplete-key-pair error. A pair that can't be read or parsed SHALL fail with an error naming both paths.

#### Scenario: A server that requires a client certificate
- **WHEN** both files hold a key pair a server's client CA issued, and the client handshakes with that server
- **THEN** the handshake succeeds and the server sees the client's certificate

#### Scenario: A server whose request doesn't accept the pair
- **WHEN** a server asks for a client certificate without requiring one, listing only a CA that didn't issue the client's pair
- **THEN** the client sends no certificate and the handshake succeeds

#### Scenario: Only the certificate file
- **WHEN** TLS is enabled with a certificate file and no key file
- **THEN** validating the config and building the TLS configuration both fail with an error matching the incomplete-key-pair error

#### Scenario: A key that doesn't match the certificate
- **WHEN** the key file holds a key for a different certificate and the client asks for its TLS configuration
- **THEN** it gets an error naming the certificate and key paths

### Requirement: The client key pair refreshes from its files

During a handshake in which the server asks for a client certificate, when at least the reload interval has passed since the key pair files were last checked, the client SHALL re-read them and, if their contents changed and parse as a key pair, SHALL present the new pair from that handshake on. Within the interval it SHALL present the pair it has without reading the files. When the files can't be read, don't parse, or hold a pair whose leaf has expired or isn't valid yet, it SHALL keep presenting the last good pair and try again at the next check. The reload interval SHALL default to 30 seconds when unset, and a negative interval SHALL fail validation and building the TLS configuration with an error matching the module's negative-reload-interval error. Refreshing SHALL need nothing started or run by the client module.

#### Scenario: Files replaced, interval elapsed
- **WHEN** the client's key pair files are replaced with a new pair and the client handshakes with a server that requires a client certificate 30 seconds after the pair was loaded
- **THEN** the server sees the new certificate

#### Scenario: Files replaced, interval not elapsed
- **WHEN** the files are replaced and the client handshakes 29 seconds after the pair was loaded
- **THEN** the server sees the old certificate

#### Scenario: The certificate written before the key
- **WHEN** the certificate file is replaced but the key file still holds the old key, and the client handshakes after the interval
- **THEN** the server sees the old certificate and the handshake succeeds, and once the key file is replaced to match, a handshake after another interval presents the new certificate

#### Scenario: An expired pair
- **WHEN** the client's key pair files are replaced with a pair whose leaf has expired, and the client handshakes after the interval
- **THEN** the server sees the old certificate

#### Scenario: A negative interval
- **WHEN** TLS is enabled with a reload interval of -1s
- **THEN** validating the config fails with an error matching the negative-reload-interval error

### Requirement: An enabled server config requires a key pair

When TLS is enabled, both a certificate file and a key file SHALL be set; otherwise validation and building the TLS configuration SHALL fail with an error matching the module's missing-key-pair error. Building the TLS configuration SHALL read the pair, and SHALL fail when it can't be read or parsed, with an error naming both paths.

#### Scenario: Enabled without a key file
- **WHEN** TLS is enabled with a certificate file and no key file
- **THEN** validating the config and building the TLS configuration both fail with an error matching the missing-key-pair error

#### Scenario: A key pair that doesn't parse
- **WHEN** TLS is enabled and the key file holds a key for a different certificate
- **THEN** building the TLS configuration fails with an error naming the certificate and key paths

### Requirement: Every client receives the current key pair

The server SHALL present its current key pair to every client, including clients that send no server name indication.

#### Scenario: A client without a server name
- **WHEN** a client handshakes by IP address without sending a server name
- **THEN** it receives the certificate from the certificate file

#### Scenario: A client without a server name after a rotation
- **WHEN** the key pair has been refreshed and a client then handshakes without sending a server name
- **THEN** it receives the new certificate

### Requirement: The server key pair refreshes from its files

During a handshake, when at least the reload interval has passed since the files were last checked, the server SHALL re-read the certificate and key files and, if their contents changed and parse as a key pair, SHALL present the new pair from that handshake on. Within the interval it SHALL present the pair it has without reading the files. The reload interval SHALL default to 30 seconds when unset, and a negative interval SHALL fail validation and building the TLS configuration with an error matching the module's negative-reload-interval error. Refreshing SHALL need nothing started or run by the server module.

#### Scenario: Files replaced, interval elapsed
- **WHEN** the certificate and key files are replaced with a new pair and a client handshakes after the reload interval has passed
- **THEN** it receives the new certificate

#### Scenario: Files replaced, interval not elapsed
- **WHEN** the files are replaced and a client handshakes before the reload interval has passed since the last check
- **THEN** it receives the old certificate

#### Scenario: The default interval
- **WHEN** the reload interval is unset, the files are replaced, and clients handshake 29 seconds and 31 seconds after the pair was loaded
- **THEN** the first receives the old certificate and the second the new one

#### Scenario: A negative interval
- **WHEN** TLS is enabled with a reload interval of -1s
- **THEN** validating the config fails with an error matching the negative-reload-interval error

### Requirement: A failed server refresh keeps the last good key pair

When a refresh can't read the files, the new contents don't parse as a key pair, or the new pair's leaf has expired or isn't valid yet, the server SHALL keep presenting the last good pair, and SHALL try again at the next check once the interval has passed. A pair building the TLS configuration SHALL NOT be rejected for its validity period.

#### Scenario: The certificate written before the key
- **WHEN** the certificate file is replaced but the key file still holds the old key, and a client handshakes after the interval
- **THEN** it receives the old certificate and the handshake succeeds

#### Scenario: The key catches up
- **WHEN** after that the key file is replaced to match, and a client handshakes after another interval
- **THEN** it receives the new certificate

#### Scenario: A file removed
- **WHEN** the key file is deleted and a client handshakes after the interval
- **THEN** it receives the old certificate and the handshake succeeds

#### Scenario: A pair outside its validity period
- **WHEN** the files are replaced with a pair whose leaf has expired, or isn't valid until an hour from now, and a client handshakes after the interval
- **THEN** it receives the old certificate, and once the files hold a current pair, a handshake after another interval receives that

#### Scenario: An expired pair at the start
- **WHEN** TLS is enabled and the files hold a pair whose leaf has expired
- **THEN** building the TLS configuration succeeds

### Requirement: Client authentication follows the configured policy

The client authentication policy SHALL be one of `none`, `request`, `require`, `verify_if_given` or `require_and_verify`:

| Policy | Asks for a certificate | Requires one | Verifies a presented one |
|--------|------------------------|--------------|--------------------------|
| none | no | no | no |
| request | yes | no | no |
| require | yes | yes | no |
| verify_if_given | yes | no | yes |
| require_and_verify | yes | yes | yes |

An unset policy SHALL mean `require_and_verify` when a client CA file is set and `none` otherwise. Any other value SHALL fail validation and building the TLS configuration with an error matching the module's unknown-client-auth error. The two verifying policies SHALL require a client CA file, failing validation and building the TLS configuration with an error matching the module's client-CA-required error. A client CA file beside `none`, `request` or `require` SHALL fail validation and building the TLS configuration with an error matching the module's client-CA-unused error, since those policies would never read it. A verified client certificate SHALL chain to a CA in the client CA file and be valid for client authentication. A server module that replaces the configuration's connection-verification hook SHALL still have a presented certificate's chain checked against the current client CAs on full handshakes. When the server verifies client certificates, the connection state SHALL carry the client's certificates as its peer certificates, and SHALL NOT carry verified chains.

#### Scenario: Required and verified
- **WHEN** the policy is `require_and_verify` and a client presents a certificate the client CA issued
- **THEN** the handshake succeeds and the connection's peer certificates hold the client's certificate

#### Scenario: A certificate from another CA
- **WHEN** the policy is `require_and_verify` and a client presents a certificate from a CA not in the client CA file
- **THEN** the handshake fails

#### Scenario: The connection-verification hook replaced
- **WHEN** a server module replaces the configuration's connection-verification hook with one that accepts everything, and a client presents a certificate from a CA not in the client CA file
- **THEN** the handshake fails, and a certificate from the client CA is still accepted

#### Scenario: No certificate when one is required
- **WHEN** the policy is `require_and_verify` or `require` and a client presents no certificate
- **THEN** the handshake fails

#### Scenario: Verify if given, none given
- **WHEN** the policy is `verify_if_given` and a client presents no certificate
- **THEN** the handshake succeeds

#### Scenario: A client CA file and no policy
- **WHEN** a client CA file is set, the policy is unset, and a client presents no certificate
- **THEN** the handshake fails

#### Scenario: A verifying policy without a client CA file
- **WHEN** TLS is enabled, the policy is `verify_if_given`, and no client CA file is set
- **THEN** validating the config fails with an error matching the client-CA-required error

#### Scenario: A client CA file beside a policy that doesn't verify
- **WHEN** TLS is enabled, a client CA file is set, and the policy is `require`
- **THEN** validating the config fails with an error matching the client-CA-unused error

#### Scenario: An unknown policy
- **WHEN** TLS is enabled and the policy is `mandatory`
- **THEN** validating the config fails with an error matching the unknown-client-auth error

### Requirement: The client CAs refresh from their file

When the server verifies client certificates, the client CA file SHALL refresh on the same terms as the key pair: re-read during a handshake once the interval has passed, swapped in when its contents change and hold at least one certificate, and kept at the last good set otherwise. A client CA file holding no PEM certificates when the TLS configuration is built SHALL fail with an error matching the module's no-certificates error and naming the path. The server SHALL NOT tell clients which CAs it accepts, so a client chooses its certificate the same way before and after a rotation.

#### Scenario: A rotated client CA
- **WHEN** the client CA file is replaced with a new CA and, after the interval, a client presents a certificate the new CA issued
- **THEN** the handshake succeeds

#### Scenario: The old CA after a rotation
- **WHEN** the client CA file has been refreshed to a new CA and a client presents a certificate only the old CA issued
- **THEN** the handshake fails

#### Scenario: A client CA file emptied
- **WHEN** the client CA file is replaced with a file holding no certificates and a client presents a certificate from the old CA after the interval
- **THEN** the handshake succeeds

### Requirement: Refreshing survives copies of the configuration

A copy of a client or server TLS configuration made with its clone method, and any changes the module using it makes to that copy, SHALL keep refreshing the key pair and the CAs, and SHALL keep a server's client authentication policy.

#### Scenario: A server that clones its configuration
- **WHEN** a server module clones the TLS configuration, adds an application protocol to the clone and serves with it, and the key pair is then rotated
- **THEN** after the interval, clients receive the new certificate and can negotiate the added protocol

#### Scenario: A client that clones its configuration
- **WHEN** a client module clones its TLS configuration and dials with the clone, and the client key pair is then rotated
- **THEN** after the interval, the server sees the new certificate

#### Scenario: A client's CAs after cloning
- **WHEN** a client module clones its TLS configuration, the CA file is then rotated, and the clone dials a server from the new CA after the interval
- **THEN** the handshake succeeds

### Requirement: Refreshes are reported through the given logger

Each value SHALL be logged at Info when building the TLS configuration loads it and when a refresh swaps it in, and a refresh that fails or is rejected and keeps the last good value SHALL be logged at Warn, each naming the file paths involved. Records about a key pair SHALL also name its leaf's subject, serial number and expiry. Contents that failed to parse or were rejected SHALL be reported once, not again at every check while the files stay the same. Records SHALL NOT contain any part of a file's contents. Without a logger, a client or server SHALL refresh the same way and report nothing. Failures while building the TLS configuration SHALL be returned, not logged.

#### Scenario: The first load
- **WHEN** a server builds its TLS configuration
- **THEN** one Info record names the certificate path and the leaf's subject, serial number and expiry

#### Scenario: A successful refresh
- **WHEN** the key pair files are replaced and a handshake after the interval picks them up
- **THEN** one Info record names the certificate path and the new leaf's serial number

#### Scenario: A client refresh
- **WHEN** the client's key pair files are replaced and a handshake after the interval picks them up
- **THEN** one Info record names the client's certificate path

#### Scenario: A pair that keeps failing
- **WHEN** the certificate file is replaced with one that doesn't match the key, and clients handshake across three intervals without the files changing again
- **THEN** exactly one Warn record names the certificate path

#### Scenario: No logger
- **WHEN** the TLS configuration was built without a logger and the key pair is rotated
- **THEN** clients receive the new certificate after the interval and nothing panics

### Requirement: Errors and records never contain file contents

Errors and log records SHALL name the config field or the file path involved and SHALL NOT include any part of a file's contents.

#### Scenario: A malformed key file
- **WHEN** the key file holds garbage bytes that include a recognisable marker string, both when the TLS configuration is built and when a refresh reads it
- **THEN** neither the returned error nor the Warn record contains the marker

#### Scenario: A malformed client key file
- **WHEN** the client's key file holds garbage bytes that include a recognisable marker string and the client asks for its TLS configuration
- **THEN** the error's text doesn't contain the marker
