package configvalidator_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/config/configvalidator"
)

var errBoom = errors.New("boom")

func TestErrorRendering(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "key", err: &configvalidator.Error{Key: "http.port", Err: errBoom}, want: "http.port: boom"},
		{name: "no key", err: &configvalidator.Error{Err: errBoom}, want: "boom"},
		{name: "rule", err: &configvalidator.RuleError{Tag: "min", Param: "1024"}, want: "fails min=1024"},
		{name: "rule without a parameter", err: &configvalidator.RuleError{Tag: "required"}, want: "fails required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.EqualError(t, tt.err, tt.want)
		})
	}
}

func TestErrorUnwraps(t *testing.T) {
	err := &configvalidator.Error{Key: "http", Err: fmt.Errorf("checking ports: %w", errBoom)}

	require.ErrorIs(t, err, errBoom)
	require.ErrorIs(t, errors.Join(err, &configvalidator.Error{Key: "cache", Err: errors.New("other")}), errBoom)
}

func TestPrefix(t *testing.T) {
	opaque := fmt.Errorf("checking tls: %w", &configvalidator.Error{Key: "cert", Err: errBoom})

	tests := []struct {
		name string
		key  string
		err  error
		want []string
	}{
		{
			name: "keyed error",
			key:  "http",
			err:  &configvalidator.Error{Key: "ssl.cert_path", Err: errBoom},
			want: []string{"http.ssl.cert_path: boom"},
		},
		{
			name: "keyed error at the root",
			key:  "",
			err:  &configvalidator.Error{Key: "port", Err: errBoom},
			want: []string{"port: boom"},
		},
		{
			name: "index key",
			key:  "hosts",
			err:  &configvalidator.Error{Key: "[2]", Err: errBoom},
			want: []string{"hosts[2]: boom"},
		},
		{
			name: "unkeyed error",
			key:  "messaging",
			err:  &configvalidator.Error{Err: errBoom},
			want: []string{"messaging: boom"},
		},
		{
			name: "plain error",
			key:  "messaging",
			err:  errBoom,
			want: []string{"messaging: boom"},
		},
		{
			name: "joined errors",
			key:  "amqp",
			err: errors.Join(
				&configvalidator.Error{Key: "url", Err: errBoom},
				&configvalidator.Error{Key: "queue", Err: errBoom},
			),
			want: []string{"amqp.url: boom", "amqp.queue: boom"},
		},
		{
			name: "nested join",
			key:  "amqp",
			err: errors.Join(
				errors.Join(&configvalidator.Error{Key: "url", Err: errBoom}, errBoom),
				&configvalidator.Error{Key: "queue", Err: errBoom},
			),
			want: []string{"amqp.url: boom", "amqp: boom", "amqp.queue: boom"},
		},
		{
			name: "wrapped keyed error is opaque",
			key:  "http",
			err:  opaque,
			want: []string{"http: checking tls: cert: boom"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := configvalidator.Prefix(tt.key, tt.err)

			messages := make([]string, 0, len(got))

			for _, err := range got {
				_, ok := err.(*configvalidator.Error) //nolint:errorlint // Each result must be an *Error, not wrap one.
				require.True(t, ok, "%T isn't a *configvalidator.Error", err)
				require.ErrorIs(t, err, errBoom)

				messages = append(messages, err.Error())
			}

			assert.Equal(t, tt.want, messages)
		})
	}
}

func TestPrefixLeavesItsInputAlone(t *testing.T) {
	child := &configvalidator.Error{Key: "url", Err: errBoom}
	joined := errors.Join(child, &configvalidator.Error{Key: "queue", Err: errBoom})

	configvalidator.Prefix("amqp", child)
	configvalidator.Prefix("messaging", joined)

	assert.Equal(t, &configvalidator.Error{Key: "url", Err: errBoom}, child)
	assert.EqualError(t, joined, "url: boom\nqueue: boom")
}
