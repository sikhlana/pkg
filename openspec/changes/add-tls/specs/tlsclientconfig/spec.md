## Purpose

Turns a client's operator TLS settings (whether TLS is on, which CAs to trust, the server name to verify, and a key pair for mutual TLS) into the TLS configuration a client module hands to its connection, so every TLS-speaking client reads the same keys and behaves the same way.

## ADDED Requirements

### Requirement: A disabled config yields no TLS settings

When TLS isn't enabled, the module SHALL return no TLS configuration (nil) and no error, and SHALL read no files, whatever else the config sets. Validation SHALL pass for a disabled config whatever else it sets.

#### Scenario: The zero config
- **WHEN** a client asks the zero config for its TLS configuration
- **THEN** it gets nil and no error

#### Scenario: Disabled with paths that don't exist
- **WHEN** TLS isn't enabled, the CA file and the key pair name files that don't exist, and the client asks for its TLS configuration
- **THEN** it gets nil and no error, and validating the config passes

### Requirement: An enabled config verifies the server against the system roots by default

When TLS is enabled and no CA file is set, the TLS configuration SHALL verify the server's certificate against the system's trusted roots and the name the client dials.

#### Scenario: A server whose certificate the system doesn't trust
- **WHEN** TLS is enabled with nothing else set and the client handshakes with a server whose certificate chains to a private CA
- **THEN** the handshake fails with an unknown-authority error

### Requirement: A CA file replaces the system roots

When a CA file is set, the server's certificate SHALL be verified against the certificates in that file only. The file SHALL be read once, when the TLS configuration is built. A file that can't be read SHALL fail with an error that names the path and matches the underlying file error. A file holding no PEM certificates SHALL fail with an error matching the module's no-certificates error and naming the path.

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

#### Scenario: The CA file changes later
- **WHEN** the client has its TLS configuration and the CA file is then replaced with a different CA
- **THEN** handshakes still verify against the CA that was read first

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

### Requirement: A key pair enables mutual TLS

When both a certificate file and a key file are set, the client SHALL present that key pair to servers that ask for a client certificate. Setting only one of the two on an enabled config SHALL fail validation and building the TLS configuration with an error matching the module's incomplete-key-pair error. A pair that can't be read or parsed SHALL fail with an error naming the certificate path.

#### Scenario: A server that requires a client certificate
- **WHEN** both files hold a key pair a server's client CA issued, and the client handshakes with that server
- **THEN** the handshake succeeds and the server sees the client's certificate

#### Scenario: Only the certificate file
- **WHEN** TLS is enabled with a certificate file and no key file
- **THEN** validating the config and building the TLS configuration both fail with an error matching the incomplete-key-pair error

#### Scenario: A key that doesn't match the certificate
- **WHEN** the key file holds a key for a different certificate and the client asks for its TLS configuration
- **THEN** it gets an error naming the certificate path

### Requirement: Errors never contain file contents

Errors SHALL name the config field or the file path involved and SHALL NOT include any part of a file's contents.

#### Scenario: A malformed key file
- **WHEN** the key file holds garbage bytes that include a recognisable marker string and the client asks for its TLS configuration
- **THEN** the error's text doesn't contain the marker
