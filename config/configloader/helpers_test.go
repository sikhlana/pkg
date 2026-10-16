package configloader_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const name = "my-svc"

type testConfig struct {
	HTTP      HTTPConfig          `mapstructure:"http"`
	DB        DBConfig            `mapstructure:"db"`
	Cache     *CacheConfig        `mapstructure:"cache"`
	Broadcast BroadcastConfig     `mapstructure:"broadcast"`
	Hosts     []string            `mapstructure:"hosts"`
	Labels    map[string]string   `mapstructure:"labels"`
	Databases map[string]DBConfig `mapstructure:"databases"`
	Cutoff    time.Time           `mapstructure:"cutoff"`
	Trusted   netip.Prefix        `mapstructure:"trusted"`
	Log       LogConfig           `mapstructure:"log"`
}

type HTTPConfig struct {
	Host        string        `mapstructure:"host"`
	Port        int           `mapstructure:"port"         validate:"omitempty,min=1024"`
	ReadTimeout time.Duration `mapstructure:"read_timeout"`
}

type DBConfig struct {
	Host     string `mapstructure:"host"`
	Password string `mapstructure:"password"`
}

type CacheConfig struct {
	TTL time.Duration `mapstructure:"ttl"`
}

type BroadcastConfig struct {
	HTTP SquashedEndpoint `mapstructure:"http"`
}

type SquashedEndpoint struct {
	Endpoint `mapstructure:",squash"`
}

type Endpoint struct {
	Host string `mapstructure:"host"`
}

type LogConfig struct {
	Level slog.Level `mapstructure:"level"`
}

// isolate points the home and working directories at empty temporary ones, so the search finds only what a test
// puts there.
func isolate(t *testing.T) {
	t.Helper()

	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())
}

// writeFile writes contents to name in dir and returns its path.
func writeFile(t *testing.T, dir, name, contents string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))

	return path
}

// configFile writes contents as a YAML config file in a new directory and returns its path.
func configFile(t *testing.T, contents string) string {
	t.Helper()

	return writeFile(t, t.TempDir(), "config.yaml", contents)
}

// record is a captured log record, with its attributes keyed by name.
type record struct {
	Level   slog.Level
	Message string
	Attrs   map[string]any
}

// String renders r with every attribute, so a test can check that no value leaked into it.
func (r record) String() string {
	return fmt.Sprintf("%s %s %v", r.Level, r.Message, r.Attrs)
}

// capture is a slog.Handler that keeps every record it's handed.
type capture struct {
	mu      sync.Mutex
	records []record
}

func (c *capture) Enabled(context.Context, slog.Level) bool {
	return true
}

func (c *capture) Handle(_ context.Context, r slog.Record) error {
	attrs := map[string]any{}

	r.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.Any()

		return true
	})

	c.mu.Lock()
	defer c.mu.Unlock()

	c.records = append(c.records, record{Level: r.Level, Message: r.Message, Attrs: attrs})

	return nil
}

func (c *capture) WithAttrs([]slog.Attr) slog.Handler {
	return c
}

func (c *capture) WithGroup(string) slog.Handler {
	return c
}

// all returns every record captured so far.
func (c *capture) all() []record {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.records
}

// rendered returns every record captured so far, rendered on its own line.
func (c *capture) rendered() string {
	var b strings.Builder

	for _, r := range c.all() {
		b.WriteString(r.String() + "\n")
	}

	return b.String()
}

// newCapture returns a capture and a logger writing to it.
func newCapture() (*capture, *slog.Logger) {
	c := &capture{}

	return c, slog.New(c)
}
