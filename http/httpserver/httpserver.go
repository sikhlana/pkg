// Package httpserver runs named HTTP servers in an fx app. An app declares its servers once, for every command, and
// each command opts in to the ones it runs; a server binds its port while the app starts, serves through its own
// ordered middleware chain until the app stops, then drains in-flight requests. TLS comes from tls/tlsconfig, with
// rotating key pairs and HTTP/2.
package httpserver
