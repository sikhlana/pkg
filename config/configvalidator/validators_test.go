package configvalidator_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/config/configvalidator"
)

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

func TestPromotedValidatorStopsTheWalk(t *testing.T) {
	type config struct {
		Section Embeds `mapstructure:"section"`
	}

	cfg := config{Section: Embeds{Check: Check{Reason: "embedded"}, Other: PointerCheck{Reason: "other"}}}

	assert.Equal(t, []string{"section: embedded"}, messages(t, configvalidator.Validate(&cfg)))
}

func TestTagFailureInOneEntrySuppressesOnlyThatEntry(t *testing.T) {
	type config struct {
		Sites map[string]Site `mapstructure:"sites" validate:"dive"`
	}

	cfg := config{Sites: map[string]Site{
		"acme":    {URL: "https://acme.example", Reason: "acme"},
		"acme.eu": {Reason: "eu"},
	}}

	assert.Equal(
		t,
		[]string{"sites.acme.eu.url: fails required", "sites.acme: acme"},
		messages(t, configvalidator.Validate(&cfg)),
	)
}

func TestTagFailureBesideASquashedValidatorDoesntSuppressIt(t *testing.T) {
	type config struct {
		Section Check `mapstructure:",squash"`
		Port    int   `mapstructure:"port"    validate:"min=1024"`
	}

	cfg := config{Section: Check{Reason: "section"}, Port: 80}

	assert.Equal(t, []string{"port: fails min=1024", "section"}, messages(t, configvalidator.Validate(&cfg)))
}

func TestWalkEntersUnexportedEmbeddedStructs(t *testing.T) {
	type config struct {
		common `mapstructure:",squash"`
	}

	type wrapper struct {
		Section UsesCommon `mapstructure:"section"`
	}

	assert.Equal(
		t,
		[]string{"audit: audit"},
		messages(t, configvalidator.Validate(&config{Audit: Check{Reason: "audit"}})),
	)
	assert.Equal(
		t,
		[]string{"section.tls.url: fails required", "section.tls.queue: fails required"},
		messages(t, configvalidator.Validate(&wrapper{})),
	)
}

func TestValidatorReturningTypedNilOrNoCause(t *testing.T) {
	type config struct {
		OK  TypedNil `mapstructure:"ok"`
		Bad TypedNil `mapstructure:"bad"`
	}

	assert.Equal(
		t,
		[]string{"bad.cert_path: invalid"},
		messages(t, configvalidator.Validate(&config{Bad: TypedNil{Bad: true}})),
	)
}

func TestPromotedValidatorOfAMissingSection(t *testing.T) {
	type config struct {
		Secured   Secured   `mapstructure:"secured"`
		Shadowing Shadowing `mapstructure:"shadowing"`
	}

	require.NoError(t, configvalidator.Validate(&config{}))

	cfg := config{Secured: Secured{SSL: &SSL{}}, Shadowing: Shadowing{SSL: &SSL{}}}

	assert.Equal(
		t,
		[]string{"secured.cert_path: missing cert", "shadowing: shadowing"},
		messages(t, configvalidator.Validate(&cfg)),
	)
}

func TestValidatorWrappingSeveralErrorsKeepsItsText(t *testing.T) {
	type config struct {
		Ports Overlap `mapstructure:"ports"`
	}

	err := configvalidator.Validate(&config{})

	assert.Equal(t, []string{"ports: ports overlap between http and grpc"}, messages(t, err))
	require.ErrorIs(t, err, errHTTPPort)
	require.ErrorIs(t, err, errGRPCPort)
}
