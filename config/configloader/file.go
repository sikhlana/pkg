package configloader

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"

	"github.com/spf13/viper"
)

// readFile reads the config file at filename, or the first file named config the search finds when filename is
// empty, into the loader's viper.
func (l *loader) readFile(filename string, searchPaths []string) error {
	if filename != "" {
		l.v.SetConfigFile(filename)

		if err := l.v.ReadInConfig(); err != nil {
			return fmt.Errorf("reading config file %q: %w", filename, err)
		}

		l.logger.Info("loaded config file", slog.String("path", filename))

		return l.dropNulls()
	}

	dirs := l.searchDirs(searchPaths)

	l.v.SetConfigName("config")

	for _, dir := range dirs {
		l.v.AddConfigPath(dir)
	}

	err := l.v.ReadInConfig()

	if _, ok := errors.AsType[viper.ConfigFileNotFoundError](err); ok {
		l.logger.Info("no config file found", slog.Any("searched", dirs))

		return nil
	}

	if err != nil {
		return fmt.Errorf("reading config file %q: %w", l.v.ConfigFileUsed(), err)
	}

	l.logger.Info("loaded config file", slog.String("path", l.v.ConfigFileUsed()))

	return l.dropNulls()
}

// dropNulls rebuilds the loader's viper from the file's settings without the keys the file leaves empty. Viper takes a
// null key, such as a section whose children are all commented out, for a leaf that hides every key beneath it, so
// an env variable bound beneath it would be read but never decoded.
func (l *loader) dropNulls() error {
	settings := l.v.AllSettings()

	l.v = viper.New()

	if err := l.v.MergeConfigMap(settings); err != nil {
		return fmt.Errorf("reading config file: %w", err)
	}

	return nil
}

// searchDirs returns the directories the search tries, in order: searchPaths, /etc/<name>, $HOME/.<name> when
// there's a home directory, and the working directory.
func (l *loader) searchDirs(searchPaths []string) []string {
	dirs := slices.Concat(searchPaths, []string{filepath.Join("/etc", l.name)})

	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, "."+l.name))
	}

	return append(dirs, ".")
}
