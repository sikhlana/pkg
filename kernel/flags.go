package kernel

import (
	"errors"
	"fmt"

	"github.com/spf13/pflag"

	"github.com/sikhlana/pkg/log"
)

const (
	// DebugFlag is the name of the persistent flag RootCommand adds to log from Debug up, --debug.
	DebugFlag = "debug"

	// QuietFlag is the name of the persistent flag RootCommand adds to log from Warn up, --quiet or -q.
	QuietFlag = "quiet"

	// FormatFlag is the name of the persistent flag RootCommand adds to pick the log format, --format.
	FormatFlag = "format"

	// ConfigFlag is the name of the persistent flag RootCommand adds, when Options.Config is set, to pass the config
	// file, --config.
	ConfigFlag = "config"
)

// flagValues holds the root's flags as Run reads them.
type flagValues struct {
	debug  bool
	quiet  bool
	format log.Format
	config string
}

// readFlags reads the kernel's flags from flags, --config only when config is set, failing with ErrMissingFlag for the
// first one not defined.
func readFlags(flags *pflag.FlagSet, config bool) (flagValues, error) {
	names := []string{DebugFlag, QuietFlag, FormatFlag}

	if config {
		names = append(names, ConfigFlag)
	}

	for _, name := range names {
		if flags.Lookup(name) == nil {
			return flagValues{}, fmt.Errorf("%w: %s", ErrMissingFlag, name)
		}
	}

	var v flagValues

	debug, debugErr := flags.GetBool(DebugFlag)
	quiet, quietErr := flags.GetBool(QuietFlag)
	formatErr := flags.GetText(FormatFlag, &v.format)

	var configErr error

	if config {
		v.config, configErr = flags.GetString(ConfigFlag)
	}

	// A flag of the right name but the wrong type, defined by hand, fails here.
	if err := errors.Join(debugErr, quietErr, formatErr, configErr); err != nil {
		return flagValues{}, fmt.Errorf("reading flags: %w", err)
	}

	v.debug, v.quiet = debug, quiet

	return v, nil
}
