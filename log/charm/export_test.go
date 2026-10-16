package charm

import (
	"log/slog"

	charmlog "charm.land/log/v2"
)

// UseLogfmt switches the charm logger behind h, a handler from New, and every handler derived from it, to
// charm's logfmt formatter, so the conformance tests can parse its output.
func UseLogfmt(h slog.Handler) {
	h.(*handler).logger.SetFormatter(charmlog.LogfmtFormatter)
}
