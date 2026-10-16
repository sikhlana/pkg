package configloader

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"

	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/viper"
)

// ErrMalformedFile is returned when the config file can't be parsed. The error names the file, its format and, where
// the parser reports one, the position, but never the parser's message, which can quote the file's contents.
var ErrMalformedFile = errors.New("malformed config file")

// lineNumber finds the line a parser's message names, as YAML's "yaml: line 2: ..." does. Only the digits are kept.
var lineNumber = regexp.MustCompile(`\bline (\d+)\b`)

// readFile reads the config file at filename into the loader's viper or, when filename is empty, the first file the
// search finds. An explicit filename is the only file read.
func (l *loader) readFile(filename string, searchPaths []string) error {
	if filename == "" {
		found, searched, err := l.search(searchPaths)
		if err != nil {
			return err
		}

		if found == "" {
			l.logger.Info("no config file found", slog.Any("searched", searched))

			return nil
		}

		filename = found
	}

	l.v.SetConfigFile(filename)

	if err := l.v.ReadInConfig(); err != nil {
		return fmt.Errorf("reading config file %q: %w", filename, fileError(filename, err))
	}

	l.logger.Info("loaded config file", slog.String("path", filename))

	return l.dropNulls()
}

// search returns the path of the first config file among the candidates, trying each format viper can decode in turn,
// or an empty path when there's none. searched lists what it looked for, as one pattern per candidate. A file it can't
// check, such as one in a directory it can't read, fails the search rather than being skipped.
func (l *loader) search(searchPaths []string) (string, []string, error) {
	candidates := l.candidates(searchPaths)
	searched := make([]string, 0, len(candidates))

	for _, c := range candidates {
		searched = append(searched, filepath.Join(c.dir, c.base+".*"))

		for _, ext := range searchExts {
			path := filepath.Join(c.dir, c.base+"."+ext)

			info, err := os.Stat(path)

			switch {
			case errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR):
				continue
			case err != nil:
				return "", nil, fmt.Errorf("searching for config file: %w", err)
			case info.Mode().IsRegular():
				return path, searched, nil
			}
		}
	}

	return "", searched, nil
}

// configBase is the name, without its extension, of the config file the search looks for in a directory of its own.
const configBase = "config"

// candidate is a place the search looks for a config file: a directory and the file's name without its extension.
type candidate struct {
	dir  string
	base string
}

// candidates returns where the search looks, most specific first: config in each of searchPaths, .<name> in the
// working directory, then config in $HOME/.config/<name>, $HOME/.<name> and /etc/<name>. The two under $HOME are
// skipped when there's no home directory.
func (l *loader) candidates(searchPaths []string) []candidate {
	candidates := make([]candidate, 0, len(searchPaths)+4)

	for _, dir := range searchPaths {
		candidates = append(candidates, candidate{dir: dir, base: configBase})
	}

	candidates = append(candidates, candidate{dir: ".", base: "." + l.name})

	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates,
			candidate{dir: filepath.Join(home, ".config", l.name), base: configBase},
			candidate{dir: filepath.Join(home, "."+l.name), base: configBase},
		)
	}

	return append(candidates, candidate{dir: filepath.Join("/etc", l.name), base: configBase})
}

// searchExts are the extensions the search tries, in viper's order: those it supports and has a decoder for, so a file
// in a format it can't read, such as config.ini, never stops the search.
var searchExts = decodableExts()

// decodableExts returns viper's supported extensions that its codec registry can decode.
func decodableExts() []string {
	registry := viper.NewCodecRegistry()

	return slices.DeleteFunc(slices.Clone(viper.SupportedExts), func(ext string) bool {
		_, err := registry.Decoder(ext)

		return err != nil
	})
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

// fileError returns err, a failure to read the config file at path, in a form safe to show. A file whose format has no
// decoder says so. A parse failure loses the parser's message, which can quote the file's contents, and keeps the
// format and the position the parser reports. Any other failure, such as a missing file, is returned as it is.
func fileError(path string, err error) error {
	if _, ok := errors.AsType[viper.ConfigParseError](err); !ok {
		return err
	}

	format := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))

	if _, noDecoder := viper.NewCodecRegistry().Decoder(format); noDecoder != nil {
		return fmt.Errorf("no decoder for the %s format", format)
	}

	if at := position(path, err); at != "" {
		return fmt.Errorf("%w: %s syntax error at %s", ErrMalformedFile, format, at)
	}

	return fmt.Errorf("%w: %s syntax error", ErrMalformedFile, format)
}

// position returns where in the file at path the parse failure err happened, as the parser reports it, or an empty
// string when it reports nothing usable.
func position(path string, err error) string {
	if decodeErr, ok := errors.AsType[*toml.DecodeError](err); ok {
		row, column := decodeErr.Position()

		return fmt.Sprintf("line %d, column %d", row, column)
	}

	if syntaxErr, ok := errors.AsType[*json.SyntaxError](err); ok {
		return offsetPosition(path, syntaxErr.Offset)
	}

	if match := lineNumber.FindStringSubmatch(err.Error()); match != nil {
		return "line " + match[1]
	}

	return ""
}

// offsetPosition turns a byte offset into the file at path into a line and column, or reports the offset alone when
// the file can't be read again.
func offsetPosition(path string, offset int64) string {
	data, err := os.ReadFile(path)

	if err != nil || offset < 0 || offset > int64(len(data)) {
		return fmt.Sprintf("byte %d", offset)
	}

	before := data[:offset]
	line := bytes.Count(before, []byte("\n")) + 1
	column := len(before) - bytes.LastIndexByte(before, '\n')

	return fmt.Sprintf("line %d, column %d", line, column)
}
