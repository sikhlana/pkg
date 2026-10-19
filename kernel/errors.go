package kernel

import "errors"

// ErrMissingFlag is returned by App.Run for a command whose root wasn't built by App.RootCommand. The error names the
// first flag missing.
var ErrMissingFlag = errors.New("missing kernel flag")
