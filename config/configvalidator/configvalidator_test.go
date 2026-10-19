package configvalidator_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/config/configvalidator"
)

func TestEveryViolationIsReported(t *testing.T) {
	type config struct {
		HTTP  HTTP  `mapstructure:"http"`
		Cache Cache `mapstructure:"cache"`
	}

	err := configvalidator.Validate(&config{})

	want := []string{"http.host: fails required", "http.port: fails min=1024", "cache.ttl: fails required"}

	assert.Equal(t, want, messages(t, err))

	errs, ok := errors.AsType[configvalidator.Errors](fmt.Errorf("loading config: %w", err))
	require.True(t, ok)
	assert.Equal(t, want, messages(t, errs))
}

func TestEmptyErrorsReportsNothing(t *testing.T) {
	type config struct {
		Section NoViolations `mapstructure:"section"`
	}

	var cfg config

	require.NoError(t, configvalidator.Validate(&cfg))
	require.NoError(t, configvalidator.ValidateField(&cfg, &cfg.Section))
}

func TestValidConfig(t *testing.T) {
	type config struct {
		HTTP      HTTP      `mapstructure:"http"`
		Messaging Messaging `mapstructure:"messaging"`
	}

	require.NoError(t, configvalidator.Validate(&config{HTTP: validHTTP(), Messaging: Messaging{Mode: "file"}}))
}

func TestDelegatedViolationsKeepTheirFullKey(t *testing.T) {
	type httpConfig struct {
		SSL SSL `mapstructure:"ssl"`
	}

	type config struct {
		Messaging Messaging        `mapstructure:"messaging"`
		Pointer   PointerMessaging `mapstructure:"pointer"`
		HTTP      httpConfig       `mapstructure:"http"`
	}

	cfg := config{
		Messaging: Messaging{Mode: "amqp", AMQP: AMQP{Queue: "jobs"}},
		Pointer:   PointerMessaging{Mode: "amqp", AMQP: AMQP{URL: "amqp://mq"}},
	}

	assert.Equal(t, []string{
		"messaging.amqp.url: fails required",
		"pointer.amqp.queue: fails required",
		"http.ssl.cert_path: missing cert",
	}, messages(t, configvalidator.Validate(&cfg)))

	cfg.Messaging.AMQP = AMQP{}
	cfg.Pointer.AMQP.Queue = "jobs"
	cfg.HTTP.SSL.CertPath = "/etc/ssl/cert.pem"

	assert.Equal(
		t,
		[]string{"messaging.amqp.url: fails required", "messaging.amqp.queue: fails required"},
		messages(t, configvalidator.Validate(&cfg)),
	)
}

func TestValidateFieldKeys(t *testing.T) {
	type broadcast struct {
		HTTP HTTP `mapstructure:"http"`
	}

	type config struct {
		Messaging Messaging  `mapstructure:"messaging"`
		Cache     *Cache     `mapstructure:"cache"`
		Hosts     []Server   `mapstructure:"hosts"`
		Broadcast *broadcast `mapstructure:"broadcast"`
	}

	cfg := config{
		Cache:     &Cache{},
		Hosts:     []Server{{Host: "a"}, {}},
		Broadcast: &broadcast{HTTP: HTTP{Port: 8080}},
	}

	tests := []struct {
		name string
		err  error
		want []string
	}{
		{
			name: "skipped field",
			err:  configvalidator.ValidateField(&cfg, &cfg.Messaging.AMQP),
			want: []string{"messaging.amqp.url: fails required", "messaging.amqp.queue: fails required"},
		},
		{
			name: "behind a pointer",
			err:  configvalidator.ValidateField(&cfg, cfg.Cache),
			want: []string{"cache.ttl: fails required"},
		},
		{
			name: "slice element",
			err:  configvalidator.ValidateField(&cfg, &cfg.Hosts[1]),
			want: []string{"hosts[1].host: fails required"},
		},
		{
			name: "squashed field",
			err:  configvalidator.ValidateField(&cfg.Broadcast.HTTP, &cfg.Broadcast.HTTP.Server),
			want: []string{"host: fails required"},
		},
		{
			name: "valid field",
			err:  configvalidator.ValidateField(&cfg, &cfg.Hosts[0]),
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, messages(t, tt.err))
		})
	}
}

func TestValidateFieldBehindAnInterface(t *testing.T) {
	type config struct {
		Plugin any `mapstructure:"plugin"`
	}

	amqp := &AMQP{URL: "amqp://mq"}
	cfg := config{Plugin: amqp}

	assert.Equal(t, []string{"plugin.queue: fails required"}, messages(t, configvalidator.ValidateField(&cfg, amqp)))
}

func TestValidateFieldOutsideItsParentPanics(t *testing.T) {
	var a, b Messaging

	assert.Panics(t, func() { _ = configvalidator.ValidateField(&a, &b.AMQP) })
	assert.Panics(t, func() { _ = configvalidator.ValidateField(&a, &a) })
	assert.Panics(t, func() { _ = configvalidator.ValidateField((*Messaging)(nil), &a.AMQP) })
}

func TestValidateFieldNilField(t *testing.T) {
	type twoSections struct {
		A *AMQP `mapstructure:"a"`
		B *AMQP `mapstructure:"b"`
	}

	var (
		cfg Messaging
		two twoSections
	)

	require.ErrorIs(t, configvalidator.ValidateField(&cfg, (*AMQP)(nil)), configvalidator.ErrNilConfig)

	err := configvalidator.ValidateField(&two, two.A)

	require.ErrorIs(t, err, configvalidator.ErrNilConfig)

	_, keyed := errors.AsType[configvalidator.Errors](err)
	assert.False(t, keyed, "two nil sections can't say which one is missing")
}

func TestValidateFieldNamesAMissingSection(t *testing.T) {
	type config struct {
		Messaging OptionalAMQP `mapstructure:"messaging"`
	}

	err := configvalidator.Validate(&config{Messaging: OptionalAMQP{Mode: "amqp"}})

	assert.Equal(t, []string{"messaging.amqp: config is nil"}, messages(t, err))
	require.ErrorIs(t, err, configvalidator.ErrNilConfig)
}

func TestValidateFieldBehindAMap(t *testing.T) {
	type config struct {
		Databases Databases `mapstructure:"databases"`
	}

	cfg := config{Databases: Databases{
		Active: "main",
		DBs:    map[string]*AMQP{"main": {URL: "amqp://mq"}, "spare": {}},
	}}

	assert.Equal(t, []string{"databases.dbs.main.queue: fails required"}, messages(t, configvalidator.Validate(&cfg)))
}

func TestValidateFieldKeepsEntryNamesWhole(t *testing.T) {
	type config struct {
		S Hosts `mapstructure:"s"`
	}

	cfg := config{S: Hosts{Hosts: map[string]PointerCheck{"[::1]:5432": {Reason: "bad"}}}}

	assert.Equal(t, []string{"s.hosts.[::1]:5432: bad"}, messages(t, configvalidator.Validate(&cfg)))
}

func TestValidateFieldChecksTagsOfASliceField(t *testing.T) {
	type config struct {
		Messaging Brokers `mapstructure:"messaging"`
	}

	cfg := config{Messaging: Brokers{Brokers: []AMQP{{URL: "amqp://mq"}}}}

	assert.Equal(
		t,
		[]string{"messaging.brokers[0].queue: fails required"},
		messages(t, configvalidator.Validate(&cfg)),
	)
}

func TestKeysOfUnusualRoots(t *testing.T) {
	type config struct {
		Internal Check `mapstructure:"-"`
		Count    int   `mapstructure:"-" validate:"min=1"`
	}

	databases := map[string]PointerCheck{"main": {Reason: "main"}, "reporting": {Reason: "reporting"}}
	hosts := []Check{{Reason: "a"}}

	var boxed any = HTTP{Host: "localhost", Port: 80}

	assert.Equal(
		t,
		[]string{"Count: fails min=1", "Internal: internal"},
		messages(t, configvalidator.Validate(&config{Internal: Check{Reason: "internal"}})),
	)
	assert.Equal(t, []string{"main: main", "reporting: reporting"}, messages(t, configvalidator.Validate(&databases)))
	assert.Equal(t, []string{"[0]: a"}, messages(t, configvalidator.Validate(&hosts)))
	assert.Equal(t, []string{"port: fails min=1024"}, messages(t, configvalidator.Validate(&boxed)))
}

func TestNilConfig(t *testing.T) {
	var cfg *Root

	require.ErrorIs(t, configvalidator.Validate(cfg), configvalidator.ErrNilConfig)
}

func ExampleValidate() {
	type config struct {
		Messaging Messaging `mapstructure:"messaging"`
	}

	err := configvalidator.Validate(&config{Messaging: Messaging{Mode: "amqp", AMQP: AMQP{Queue: "jobs"}}})

	fmt.Println(err)
	// Output: messaging.amqp.url: fails required
}
