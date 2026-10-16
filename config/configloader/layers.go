package configloader

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

// maxSecretSize is the largest file a _FILE variable may name, in bytes.
const maxSecretSize = 1 << 20

var (
	// ErrEnvAndFileSet is returned when both a key's env variable and its _FILE variable are non-empty. The error
	// names both variables.
	ErrEnvAndFileSet = errors.New("env variable and its file variable both set")

	// ErrSecretFileTooLarge is returned when a _FILE variable names a file larger than 1 MiB. The error names the
	// variable and the path, never the contents.
	ErrSecretFileTooLarge = errors.New("secret file too large")
)

// setDefaults sets every leaf's default that Options.Defaults reaches, one key at a time, so a higher layer replaces
// a slice as a whole and map entries merge entry by entry.
func (l *loader) setDefaults(t *table) {
	for _, leaf := range t.leaves {
		if leaf.def.IsValid() && leaf.def.CanInterface() {
			l.v.SetDefault(leaf.key, leaf.def.Interface())
		}
	}
}

// bindEnv makes every leaf readable from its env variable, or sets it from the file its _FILE variable names.
func (l *loader) bindEnv(t *table) error {
	for _, leaf := range t.leaves {
		if err := l.bindLeaf(leaf); err != nil {
			return err
		}
	}

	return nil
}

// bindLeaf makes leaf readable from its env variable, or sets it from the file its _FILE variable names. The file is
// read here, once, and its contents go into viper's override layer, which sits at env priority because the two
// variables are exclusive. The process environment is never touched.
func (l *loader) bindLeaf(leaf leaf) error {
	fileEnv := leaf.env + fileSuffix
	value := os.Getenv(leaf.env)
	path := os.Getenv(fileEnv)

	switch {
	case value != "" && path != "":
		return fmt.Errorf("%w: %s and %s", ErrEnvAndFileSet, leaf.env, fileEnv)
	case path != "":
		secret, err := readSecret(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", fileEnv, err)
		}

		l.v.Set(leaf.key, secret)
		l.logger.Debug("config key read from file",
			slog.String("key", leaf.key),
			slog.String("env", fileEnv),
			slog.String("path", path),
		)

		return nil
	}

	if err := l.v.BindEnv(leaf.key, leaf.env); err != nil {
		return fmt.Errorf("binding %s: %w", leaf.env, err)
	}

	if value != "" {
		l.logger.Debug("config key set from environment", slog.String("key", leaf.key), slog.String("env", leaf.env))
	}

	return nil
}

// readSecret returns the contents of the file at path with trailing \r and \n removed, failing with
// ErrSecretFileTooLarge when it's larger than maxSecretSize. Errors name the path, never the contents.
func readSecret(path string) (string, error) {
	f, err := os.Open(path) //nolint:gosec // The operator chose the path; reading it is the point.
	if err != nil {
		return "", err
	}
	defer f.Close()

	b, err := io.ReadAll(io.LimitReader(f, maxSecretSize+1))
	if err != nil {
		return "", err
	}

	if len(b) > maxSecretSize {
		return "", fmt.Errorf("%w: %s is larger than 1 MiB", ErrSecretFileTooLarge, path)
	}

	return strings.TrimRight(string(b), "\r\n"), nil
}
