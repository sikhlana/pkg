package configvalidator_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/config/configvalidator"
)

var (
	errUnknownMode = errors.New("unknown mode")
	errNoCert      = errors.New("missing cert")
)

type AMQP struct {
	URL   string `mapstructure:"url"   validate:"required"`
	Queue string `mapstructure:"queue" validate:"required"`
}

// Messaging dispatches on Mode: only the active mode's struct is validated.
type Messaging struct {
	Mode string `mapstructure:"mode" validate:"oneof=file amqp"`
	AMQP AMQP   `mapstructure:"amqp" validate:"-"`
}

func (m Messaging) Validate() error {
	switch m.Mode {
	case "file":
		return nil
	case "amqp":
		return configvalidator.ValidateField(&m, &m.AMQP)
	default:
		return errUnknownMode
	}
}

// PointerMessaging dispatches like Messaging, through a pointer receiver.
type PointerMessaging struct {
	Mode string `mapstructure:"mode" validate:"oneof=file amqp"`
	AMQP AMQP   `mapstructure:"amqp" validate:"-"`
}

func (m *PointerMessaging) Validate() error {
	if m.Mode == "amqp" {
		return configvalidator.ValidateField(m, &m.AMQP)
	}

	return nil
}

// Check fails with its Reason through a value receiver.
type Check struct {
	Reason string `mapstructure:"reason"`
}

func (c Check) Validate() error {
	return errors.New(c.Reason)
}

// PointerCheck fails with its Reason through a pointer receiver.
type PointerCheck struct {
	Reason string `mapstructure:"reason"`
}

func (c *PointerCheck) Validate() error {
	return errors.New(c.Reason)
}

// Parent fails without delegating to its Child.
type Parent struct {
	Child Check `mapstructure:"child"`
}

func (Parent) Validate() error {
	return errors.New("parent")
}

// Root fails as a whole through a pointer receiver.
type Root struct {
	HTTP Check `mapstructure:"http"`
}

func (*Root) Validate() error {
	return errors.New("ports overlap")
}

// TextCheck decodes itself from text and has its own Validate method.
type TextCheck struct {
	Reason string
}

func (c *TextCheck) UnmarshalText(text []byte) error {
	c.Reason = string(text)

	return nil
}

func (c TextCheck) Validate() error {
	return errors.New(c.Reason)
}

// OpaqueText decodes itself from text and holds a Validator that's never searched for.
type OpaqueText struct {
	Inner Check
}

func (*OpaqueText) UnmarshalText([]byte) error {
	return nil
}

type SSL struct {
	CertPath string `mapstructure:"cert_path"`
}

func (s SSL) Validate() error {
	if s.CertPath == "" {
		return &configvalidator.Error{Key: "cert_path", Err: errNoCert}
	}

	return nil
}

// Embeds gains Check's Validate method by embedding it, which hides Other from the walk.
type Embeds struct {
	Check

	Other PointerCheck `mapstructure:"other"`
}

// violations returns the violations in err, which Validate returns alone or joined.
func violations(t *testing.T, err error) []*configvalidator.Error {
	t.Helper()

	if err == nil {
		return nil
	}

	parts := []error{err}

	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		parts = joined.Unwrap()
	}

	result := make([]*configvalidator.Error, 0, len(parts))

	for _, part := range parts {
		violation, ok := part.(*configvalidator.Error) //nolint:errorlint // Each part must be an *Error, not wrap one.
		require.True(t, ok, "%T isn't a *configvalidator.Error", part)

		result = append(result, violation)
	}

	return result
}

// messages returns the text of each violation in err.
func messages(t *testing.T, err error) []string {
	t.Helper()

	var result []string

	for _, violation := range violations(t, err) {
		result = append(result, violation.Error())
	}

	return result
}

func TestOutermostValidatorWins(t *testing.T) {
	type config struct {
		Parent Parent `mapstructure:"parent"`
	}

	err := configvalidator.Validate(&config{Parent: Parent{Child: Check{Reason: "child"}}})

	assert.Equal(t, []string{"parent: parent"}, messages(t, err))
}

func TestWalkFindsValidatorsBelowPlainParents(t *testing.T) {
	type config struct {
		HTTP  Check        `mapstructure:"http"`
		Cache PointerCheck `mapstructure:"cache"`
		Inner struct {
			Deep *PointerCheck `mapstructure:"deep"`
		} `mapstructure:"inner"`
	}

	cfg := config{HTTP: Check{Reason: "http"}, Cache: PointerCheck{Reason: "cache"}}
	cfg.Inner.Deep = &PointerCheck{Reason: "deep"}

	assert.Equal(
		t,
		[]string{"http: http", "cache: cache", "inner.deep: deep"},
		messages(t, configvalidator.Validate(&cfg)),
	)
}

func TestRootValidatorWithPointerReceiver(t *testing.T) {
	err := configvalidator.Validate(&Root{HTTP: Check{Reason: "http"}})

	got := violations(t, err)
	require.Len(t, got, 1)
	assert.Empty(t, got[0].Key)
	assert.EqualError(t, err, "ports overlap")
}

func TestWalkVisitsElements(t *testing.T) {
	type config struct {
		Hosts     []PointerCheck           `mapstructure:"hosts"`
		Ports     [2]Check                 `mapstructure:"ports"`
		Databases map[string]PointerCheck  `mapstructure:"databases"`
		Replicas  map[string]*PointerCheck `mapstructure:"replicas"`
	}

	cfg := config{
		Hosts:     []PointerCheck{{Reason: "a"}, {Reason: "b"}},
		Ports:     [2]Check{{Reason: "http"}, {Reason: "grpc"}},
		Databases: map[string]PointerCheck{"reporting": {Reason: "reporting"}, "main": {Reason: "main"}},
		Replicas:  map[string]*PointerCheck{"eu": {Reason: "eu"}, "us": nil},
	}

	assert.Equal(t, []string{
		"hosts[0]: a",
		"hosts[1]: b",
		"ports[0]: http",
		"ports[1]: grpc",
		"databases.main: main",
		"databases.reporting: reporting",
		"replicas.eu: eu",
	}, messages(t, configvalidator.Validate(&cfg)))
}

func TestWalkSeesThroughInterfaces(t *testing.T) {
	type config struct {
		Plugin  any `mapstructure:"plugin"`
		Nil     any `mapstructure:"nil"`
		NilPtr  any `mapstructure:"nil_ptr"`
		Unknown any `mapstructure:"unknown"`
	}

	cfg := config{
		Plugin:  PointerCheck{Reason: "plugin"},
		NilPtr:  (*Check)(nil),
		Unknown: map[string]any{"a": 1},
	}

	assert.Equal(t, []string{"plugin: plugin"}, messages(t, configvalidator.Validate(&cfg)))
}

func TestSkippedFieldHidesItsValidator(t *testing.T) {
	type config struct {
		Dormant Check `mapstructure:"dormant" validate:"-"`
	}

	require.NoError(t, configvalidator.Validate(&config{Dormant: Check{Reason: "dormant"}}))
}

func TestTextValuesAreSingleValues(t *testing.T) {
	type config struct {
		Cutoff TextCheck  `mapstructure:"cutoff"`
		Opaque OpaqueText `mapstructure:"opaque"`
	}

	cfg := config{
		Cutoff: TextCheck{Reason: "too late"},
		Opaque: OpaqueText{Inner: Check{Reason: "searched inside"}},
	}

	assert.Equal(t, []string{"cutoff: too late"}, messages(t, configvalidator.Validate(&cfg)))
}

func TestTagFailureSuppressesItsBranchOnly(t *testing.T) {
	type config struct {
		Messaging Messaging `mapstructure:"messaging"`
		HTTP      Check     `mapstructure:"http"`
	}

	err := configvalidator.Validate(&config{Messaging: Messaging{Mode: "kafka"}, HTTP: Check{Reason: "http"}})

	assert.Equal(t, []string{"messaging.mode: fails oneof=file amqp", "http: http"}, messages(t, err))
	assert.NotErrorIs(t, err, errUnknownMode)
}

func TestEveryViolationIsReported(t *testing.T) {
	type config struct {
		HTTP  HTTP  `mapstructure:"http"`
		Cache Cache `mapstructure:"cache"`
	}

	assert.Equal(
		t,
		[]string{"http.host: fails required", "http.port: fails min=1024", "cache.ttl: fails required"},
		messages(t, configvalidator.Validate(&config{})),
	)
}

func TestSentinelStaysReachable(t *testing.T) {
	type config struct {
		HTTP  SSL   `mapstructure:"http"`
		Other Check `mapstructure:"other"`
	}

	err := configvalidator.Validate(&config{Other: Check{Reason: "other"}})

	require.ErrorIs(t, err, errNoCert)

	violation, ok := errors.AsType[*configvalidator.Error](err)
	require.True(t, ok)
	assert.Equal(t, "http.cert_path", violation.Key)
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
	var cfg Messaging

	require.ErrorIs(t, configvalidator.ValidateField(&cfg, (*AMQP)(nil)), configvalidator.ErrNilConfig)
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

func TestPromotedValidatorStopsTheWalk(t *testing.T) {
	type config struct {
		Section Embeds `mapstructure:"section"`
	}

	cfg := config{Section: Embeds{Check: Check{Reason: "embedded"}, Other: PointerCheck{Reason: "other"}}}

	assert.Equal(t, []string{"section: embedded"}, messages(t, configvalidator.Validate(&cfg)))
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
