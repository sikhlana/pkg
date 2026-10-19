package workspace

import "errors"

// ErrToolModule reports a release tool go.mod whose module path doesn't end
// in ToolModuleDir, so the repository's path can't be derived from it.
var ErrToolModule = errors.New("unexpected release tool module path")
