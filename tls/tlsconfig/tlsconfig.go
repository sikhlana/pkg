// Package tlsconfig turns operator TLS settings into the *tls.Config a client module dials with or a server module
// serves with: an explicit on switch, the CAs to trust, key pairs and a client certificate policy. Key pairs and CA
// files are re-read during handshakes as they rotate, with nothing for the module using them to run.
package tlsconfig
