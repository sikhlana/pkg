package publish

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/mod/module"
)

// DefaultProxyURL is the public Go module proxy.
const DefaultProxyURL = "https://proxy.golang.org"

// DefaultProxyTimeout bounds one announcement, including the proxy fetching
// the version from GitHub.
const DefaultProxyTimeout = 30 * time.Second

// ErrProxyStatus reports a non-200 response from the module proxy.
var ErrProxyStatus = errors.New("unexpected module proxy status")

// Proxy announces new versions to a Go module proxy by requesting them, so
// they resolve, and reach the checksum database and pkg.go.dev, straight
// away.
type Proxy struct {
	// URL is the proxy's base URL. Empty means DefaultProxyURL.
	URL string
	// Client sends the request. Nil means http.DefaultClient.
	Client *http.Client
	// Timeout bounds one announcement. Zero means DefaultProxyTimeout.
	Timeout time.Duration
}

// Announce requests version, such as v0.4.0, of the module at path, giving
// up after the timeout.
func (p Proxy) Announce(ctx context.Context, path, version string) error {
	ctx, cancel := context.WithTimeout(ctx, p.timeout())

	defer cancel()

	escapedPath, err := module.EscapePath(path)
	if err != nil {
		return fmt.Errorf("escaping module path %s: %w", path, err)
	}

	escapedVersion, err := module.EscapeVersion(version)
	if err != nil {
		return fmt.Errorf("escaping version %s: %w", version, err)
	}

	url := strings.TrimSuffix(p.baseURL(), "/") + "/" + escapedPath + "/@v/" + escapedVersion + ".info"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return fmt.Errorf("requesting %s@%s from the module proxy: %w", path, version, err)
	}

	resp, err := p.client().Do(req)
	if err != nil {
		return fmt.Errorf("requesting %s@%s from the module proxy: %w", path, version, err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))

		return fmt.Errorf("requesting %s@%s from the module proxy: %w %s: %s",
			path, version, ErrProxyStatus, resp.Status, strings.TrimSpace(string(body)))
	}

	return nil
}

func (p Proxy) baseURL() string {
	if p.URL == "" {
		return DefaultProxyURL
	}

	return p.URL
}

func (p Proxy) client() *http.Client {
	if p.Client == nil {
		return http.DefaultClient
	}

	return p.Client
}

func (p Proxy) timeout() time.Duration {
	if p.Timeout == 0 {
		return DefaultProxyTimeout
	}

	return p.Timeout
}
