package vulncheck

import "errors"

// ErrIncomplete is wrapped by Scan's error when a directory's scan fails, so a caller can tell a partial scan from a
// failure after scanning.
var ErrIncomplete = errors.New("scan incomplete")
