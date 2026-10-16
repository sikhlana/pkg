// Package configloader reads an application's configuration from typed defaults, a config file and the environment
// into its config struct, then validates it with config/configvalidator.
package configloader

import (
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"

	"github.com/spf13/viper"

	"github.com/sikhlana/pkg/config/configvalidator"
)

// ErrInvalidName is returned when Options.Name is empty, starts with a digit, or holds anything other than ASCII
// letters, digits, - and _.
var ErrInvalidName = errors.New("invalid name")

// Options says where Load reads a config of type T from.
type Options[T any] struct {
	// Name names the application. It's required, holds only ASCII letters, digits, - and _, and doesn't start with a
	// digit, since no shell can export a variable that does. The search looks for .<name>.<ext> in the working directory
	// and in directories named after it, and every env name starts with it, uppercased with - replaced by _.
	Name string

	// Filename is the config file to read, with its extension choosing the format. It must exist, and it's the only
	// file read. Empty means search instead, where finding none is fine: config.<ext> in each of SearchPaths, then
	// .<name>.<ext> in the working directory, then config.<ext> in $HOME/.config/<name>/, $HOME/.<name>/ and
	// /etc/<name>/. The first file found is read, trying each format viper can decode.
	Filename string

	// SearchPaths are directories the search tries for config.<ext>, in order, before any other place.
	SearchPaths []string

	// Defaults holds the value of every key neither the file nor the environment sets. Leaves under a nil pointer get
	// no default, and the zero value means no defaults. Map entries load with their names lowercased, as every key
	// does, so a default entry named "Team" comes back as "team". Load copies what it takes from Defaults, so the
	// loaded config shares nothing with it.
	Defaults T

	// AllowUnknownKeys makes Load ignore, and log at Warn, keys in the file that match no field, instead of failing
	// with ErrUnknownKeys.
	AllowUnknownKeys bool

	// Logger is where Load reports which file it read, which keys came from the environment and which unknown keys
	// it ignored, never their values. Nil reports nothing.
	Logger *slog.Logger
}

// Load reads a T from o.Defaults, then the config file, then the environment, each overriding the one before for the
// keys it sets, and validates it with configvalidator.Validate. Every leaf of T is settable through its env name and
// that name followed by _FILE, which names a file holding the value. Load never modifies the process environment or
// state shared with other loads. On any failure it returns the zero T and an error naming the keys, env variables or
// paths involved, never a value.
func Load[T any](o Options[T]) (T, error) {
	var zero T

	l, err := newLoader(o.Name, o.Logger)
	if err != nil {
		return zero, err
	}

	err = l.readFile(o.Filename, o.SearchPaths)
	if err != nil {
		return zero, err
	}

	// Before any default or env binding, so these are the file's keys alone.
	fileKeys := l.v.AllKeys()

	t, err := newTable(l.prefix, reflect.TypeFor[T](), reflect.ValueOf(&o.Defaults).Elem(), fileKeys)
	if err != nil {
		return zero, err
	}

	err = t.checkCollisions()
	if err != nil {
		return zero, err
	}

	err = l.checkUnknownKeys(t, fileKeys, o.AllowUnknownKeys)
	if err != nil {
		return zero, err
	}

	l.setDefaults(t)

	err = l.bindEnv(t)
	if err != nil {
		return zero, err
	}

	var cfg T

	err = l.decode(&cfg, o.AllowUnknownKeys)
	if err != nil {
		return zero, fmt.Errorf("decoding config: %w", err)
	}

	err = configvalidator.Validate(&cfg)
	if err != nil {
		return zero, fmt.Errorf("validating config: %w", err)
	}

	return cfg, nil
}

// loader holds one load's state. Every load builds its own, so loads never share a viper instance.
type loader struct {
	v      *viper.Viper
	name   string
	prefix string
	logger *slog.Logger
}

// newLoader checks name and returns a loader for it, reporting through logger, or nowhere when it's nil.
func newLoader(name string, logger *slog.Logger) (*loader, error) {
	if !validName(name) {
		return nil, fmt.Errorf("%w: %q", ErrInvalidName, name)
	}

	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	return &loader{
		v:      viper.New(),
		name:   name,
		prefix: strings.ToUpper(strings.ReplaceAll(name, "-", "_")),
		logger: logger,
	}, nil
}

// validName reports whether name is non-empty, doesn't start with a digit, and holds only ASCII letters, digits, - and
// _.
func validName(name string) bool {
	if name == "" || name[0] >= '0' && name[0] <= '9' {
		return false
	}

	return strings.IndexFunc(name, func(r rune) bool {
		return !isEnvRune(r) && r != '-'
	}) < 0
}

// isEnvRune reports whether r can appear in an env name as it is: an ASCII letter, a digit or _.
func isEnvRune(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_'
}
