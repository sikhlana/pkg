package publish

import "errors"

// ErrPathMajor reports a VERSION whose major the module path doesn't take,
// which the go command would never resolve.
var ErrPathMajor = errors.New("VERSION's major doesn't match the module path")

// ErrProxyStatus reports a non-200 response from the module proxy.
var ErrProxyStatus = errors.New("unexpected module proxy status")
