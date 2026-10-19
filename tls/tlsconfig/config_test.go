package tlsconfig_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/tls/tlsconfig"
)

func TestClientConfigValidate(t *testing.T) {
	t.Parallel()

	t.Run("disabled, with paths that don't exist", func(t *testing.T) {
		t.Parallel()

		missing := filepath.Join(t.TempDir(), "missing")
		c := tlsconfig.ClientConfig{CAFile: missing, CertFile: missing, KeyFile: missing}

		assert.NoError(t, c.Validate())
	})

	t.Run("disabled, with half a key pair", func(t *testing.T) {
		t.Parallel()

		assert.NoError(t, tlsconfig.ClientConfig{CertFile: "client.crt"}.Validate())
	})

	t.Run("only the certificate file", func(t *testing.T) {
		t.Parallel()

		c := tlsconfig.ClientConfig{Enabled: true, CertFile: "client.crt"}

		require.ErrorIs(t, c.Validate(), tlsconfig.ErrIncompleteKeyPair)
	})

	t.Run("only the key file", func(t *testing.T) {
		t.Parallel()

		c := tlsconfig.ClientConfig{Enabled: true, KeyFile: "client.key"}

		require.ErrorIs(t, c.Validate(), tlsconfig.ErrIncompleteKeyPair)
	})

	t.Run("a negative interval", func(t *testing.T) {
		t.Parallel()

		c := tlsconfig.ClientConfig{Enabled: true, ReloadInterval: -time.Second}

		require.ErrorIs(t, c.Validate(), tlsconfig.ErrNegativeReloadInterval)
	})
}

func TestServerConfigValidate(t *testing.T) {
	t.Parallel()

	t.Run("disabled and incomplete", func(t *testing.T) {
		t.Parallel()

		c := tlsconfig.ServerConfig{ClientAuth: "mandatory", ReloadInterval: -time.Second}

		require.NoError(t, c.Validate())
	})

	t.Run("without a key file", func(t *testing.T) {
		t.Parallel()

		c := tlsconfig.ServerConfig{Enabled: true, CertFile: "server.crt"}

		require.ErrorIs(t, c.Validate(), tlsconfig.ErrMissingKeyPair)
	})

	t.Run("a negative interval", func(t *testing.T) {
		t.Parallel()

		c := tlsconfig.ServerConfig{
			Enabled:        true,
			CertFile:       "server.crt",
			KeyFile:        "server.key",
			ReloadInterval: -time.Second,
		}

		require.ErrorIs(t, c.Validate(), tlsconfig.ErrNegativeReloadInterval)
	})

	t.Run("a verifying policy without a client CA file", func(t *testing.T) {
		t.Parallel()

		for _, policy := range []tlsconfig.ClientAuth{
			tlsconfig.ClientAuthVerifyIfGiven,
			tlsconfig.ClientAuthRequireAndVerify,
		} {
			c := tlsconfig.ServerConfig{
				Enabled:    true,
				CertFile:   "server.crt",
				KeyFile:    "server.key",
				ClientAuth: policy,
			}

			require.ErrorIs(t, c.Validate(), tlsconfig.ErrClientCARequired, policy)
		}
	})

	t.Run("a client CA file beside a policy that doesn't verify", func(t *testing.T) {
		t.Parallel()

		for _, policy := range []tlsconfig.ClientAuth{
			tlsconfig.ClientAuthNone,
			tlsconfig.ClientAuthRequest,
			tlsconfig.ClientAuthRequire,
		} {
			c := tlsconfig.ServerConfig{
				Enabled:      true,
				CertFile:     "server.crt",
				KeyFile:      "server.key",
				ClientCAFile: "client-ca.crt",
				ClientAuth:   policy,
			}

			require.ErrorIs(t, c.Validate(), tlsconfig.ErrClientCAUnused, policy)
		}
	})

	t.Run("an unknown policy", func(t *testing.T) {
		t.Parallel()

		c := tlsconfig.ServerConfig{
			Enabled:    true,
			CertFile:   "server.crt",
			KeyFile:    "server.key",
			ClientAuth: "mandatory",
		}

		require.ErrorIs(t, c.Validate(), tlsconfig.ErrUnknownClientAuth)
	})
}
