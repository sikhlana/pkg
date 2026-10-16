## Purpose

Turns a server's operator TLS settings (whether TLS is on, its key pair, and how it authenticates clients) into the TLS configuration a server module serves with, and keeps the key pair and client CAs current as their files rotate, without a restart or anything for the server module to run.

## ADDED Requirements

### Requirement: A disabled config yields no TLS settings

When TLS isn't enabled, the module SHALL return no TLS configuration (nil) and no error, and SHALL read no files, whatever else the config sets. Validation SHALL pass for a disabled config whatever else it sets.

#### Scenario: The zero config
- **WHEN** a server asks the zero config for its TLS configuration
- **THEN** it gets nil and no error

#### Scenario: Disabled with an incomplete config
- **WHEN** TLS isn't enabled, no key pair is set, and the client authentication policy names an unknown value
- **THEN** validating the config passes and the server gets nil and no error

### Requirement: An enabled config requires a key pair

When TLS is enabled, both a certificate file and a key file SHALL be set; otherwise validation and building the TLS configuration SHALL fail with an error matching the module's missing-key-pair error. Building the TLS configuration SHALL read the pair, and SHALL fail when it can't be read or parsed, with an error naming the certificate path.

#### Scenario: Enabled without a key file
- **WHEN** TLS is enabled with a certificate file and no key file
- **THEN** validating the config and building the TLS configuration both fail with an error matching the missing-key-pair error

#### Scenario: A key pair that doesn't parse
- **WHEN** TLS is enabled and the key file holds a key for a different certificate
- **THEN** building the TLS configuration fails with an error naming the certificate path

### Requirement: Every client receives the current key pair

The server SHALL present its current key pair to every client, including clients that send no server name indication.

#### Scenario: A client without a server name
- **WHEN** a client handshakes by IP address without sending a server name
- **THEN** it receives the certificate from the certificate file

#### Scenario: A client without a server name after a rotation
- **WHEN** the key pair has been refreshed and a client then handshakes without sending a server name
- **THEN** it receives the new certificate

### Requirement: The key pair refreshes from its files

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

### Requirement: A failed refresh keeps the last good key pair

When a refresh can't read the files or the new contents don't parse as a key pair, the server SHALL keep presenting the last good pair, and SHALL try again at the next check once the interval has passed.

#### Scenario: The certificate written before the key
- **WHEN** the certificate file is replaced but the key file still holds the old key, and a client handshakes after the interval
- **THEN** it receives the old certificate and the handshake succeeds

#### Scenario: The key catches up
- **WHEN** after that the key file is replaced to match, and a client handshakes after another interval
- **THEN** it receives the new certificate

#### Scenario: A file removed
- **WHEN** the key file is deleted and a client handshakes after the interval
- **THEN** it receives the old certificate and the handshake succeeds

### Requirement: Client authentication follows the configured policy

The client authentication policy SHALL be one of `none`, `request`, `require`, `verify_if_given` or `require_and_verify`:

| Policy | Asks for a certificate | Requires one | Verifies a presented one |
|--------|------------------------|--------------|--------------------------|
| none | no | no | no |
| request | yes | no | no |
| require | yes | yes | no |
| verify_if_given | yes | no | yes |
| require_and_verify | yes | yes | yes |

An unset policy SHALL mean `require_and_verify` when a client CA file is set and `none` otherwise. Any other value SHALL fail validation and building the TLS configuration with an error matching the module's unknown-client-auth error. The two verifying policies SHALL require a client CA file, failing validation and building the TLS configuration with an error matching the module's client-CA-required error. A verified client certificate SHALL chain to a CA in the client CA file and be valid for client authentication. When the server verifies client certificates, the connection state SHALL carry the client's certificates as its peer certificates, and SHALL NOT carry verified chains.

#### Scenario: Required and verified
- **WHEN** the policy is `require_and_verify` and a client presents a certificate the client CA issued
- **THEN** the handshake succeeds and the connection's peer certificates hold the client's certificate

#### Scenario: A certificate from another CA
- **WHEN** the policy is `require_and_verify` and a client presents a certificate from a CA not in the client CA file
- **THEN** the handshake fails

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

A copy of the TLS configuration made with its clone method, and any changes the server module makes to that copy, SHALL keep refreshing the key pair and client CAs and SHALL keep the client authentication policy.

#### Scenario: A server that clones its configuration
- **WHEN** a server module clones the TLS configuration, adds an application protocol to the clone and serves with it, and the key pair is then rotated
- **THEN** after the interval, clients receive the new certificate and can negotiate the added protocol

### Requirement: Refreshes are reported through the given logger

A refresh that swaps in new contents SHALL be logged at Info, and a refresh that fails and keeps the last good value SHALL be logged at Warn, each naming the file paths involved. Contents that failed to parse SHALL be reported once, not again at every check while the files stay the same. Records SHALL NOT contain any part of a file's contents. Without a logger, the server SHALL refresh the same way and report nothing. Failures while building the TLS configuration SHALL be returned, not logged.

#### Scenario: A successful refresh
- **WHEN** the key pair files are replaced and a handshake after the interval picks them up
- **THEN** one Info record names the certificate path

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
