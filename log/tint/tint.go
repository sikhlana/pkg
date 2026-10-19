// Package tint renders log's text format through tint, colouring the output only when it goes to a terminal and
// NO_COLOR is unset. ProvideTextHandler makes it the text handler of an app that lists log.Module.
package tint

import (
	"io"
	"log/slog"
	"os"

	lmtint "github.com/lmittmann/tint"
	"go.uber.org/fx"
	"golang.org/x/term"

	"github.com/sikhlana/pkg/log"
)

// New returns a tint handler writing to w with the level, source setting and ReplaceAttr in opts. It colours its
// output only when w is a terminal and NO_COLOR is unset or empty. It's a log.TextHandlerProvider.
func New(w io.Writer, opts *slog.HandlerOptions) slog.Handler {
	o := &lmtint.Options{NoColor: !colour(w, term.IsTerminal)}

	if opts != nil {
		o.AddSource = opts.AddSource
		o.Level = opts.Level
		o.ReplaceAttr = opts.ReplaceAttr
	}

	return lmtint.NewTextHandler(w, o)
}

// ProvideTextHandler supplies New as the log module's text handler.
func ProvideTextHandler() fx.Option {
	return log.WithTextHandler(New)
}

// colour reports whether output to w should be coloured, using isTerminal to check a file's descriptor. It reaches the
// descriptor through SyscallConn, since os.File.Fd would put a non-blocking file, such as a pipe, into blocking mode.
func colour(w io.Writer, isTerminal func(fd int) bool) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}

	f, ok := w.(*os.File)

	if !ok {
		return false
	}

	conn, err := f.SyscallConn()
	if err != nil {
		return false
	}

	var terminal bool

	err = conn.Control(func(fd uintptr) {
		terminal = isTerminal(int(fd))
	})

	return err == nil && terminal
}
