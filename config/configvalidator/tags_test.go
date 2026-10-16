package configvalidator_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/config/configvalidator"
)

type Server struct {
	Host string `mapstructure:"host" validate:"required"`
}

type HTTP struct {
	Server `mapstructure:",squash"`

	Port int `mapstructure:"port" validate:"min=1024"`
}

type Cache struct {
	TTL time.Duration `mapstructure:"ttl" validate:"required"`
}

type Named struct {
	Name string `mapstructure:"name" validate:"required"`
}

type Tenant struct {
	URL string `mapstructure:"url" validate:"required,url"`
}

type Box[T any] struct {
	V T `mapstructure:"v"`
}

func validHTTP() HTTP {
	return HTTP{Host: "localhost", Port: 8080}
}

func TestTagViolationNamesTheRule(t *testing.T) {
	type config struct {
		HTTP HTTP `mapstructure:"http"`
	}

	cfg := config{HTTP: validHTTP()}
	cfg.HTTP.Port = 80

	err := configvalidator.Validate(&cfg)

	got := violations(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "http.port", got[0].Key)
	assert.Equal(t, &configvalidator.RuleError{Tag: "min", Param: "1024"}, got[0].Err)
	assert.EqualError(t, err, "http.port: fails min=1024")
}

func TestTagsSkipDormantStruct(t *testing.T) {
	type config struct {
		Messaging Messaging `mapstructure:"messaging"`
	}

	require.NoError(t, configvalidator.Validate(&config{Messaging: Messaging{Mode: "file"}}))

	assert.Equal(
		t,
		[]string{"url: fails required", "queue: fails required"},
		messages(t, configvalidator.Validate(&AMQP{})),
	)
}

func TestTagsSkipNilSection(t *testing.T) {
	type config struct {
		Cache *Cache `mapstructure:"cache"`
	}

	require.NoError(t, configvalidator.Validate(&config{}))
	assert.Equal(
		t,
		[]string{"cache.ttl: fails required"},
		messages(t, configvalidator.Validate(&config{Cache: &Cache{}})),
	)
}

func TestTagViolationKeys(t *testing.T) {
	type broadcast struct {
		HTTP HTTP `mapstructure:"http"`
	}

	type config struct {
		Broadcast broadcast `mapstructure:"broadcast"`
		Untagged  string    `                         validate:"required"`
		Plugin    any       `mapstructure:"plugin"`

		Named
	}

	plugin := validHTTP()
	plugin.Port = 80

	cfg := config{
		Broadcast: broadcast{HTTP: HTTP{Port: 8080}},
		Plugin:    &plugin,
	}

	assert.Equal(t, []string{
		"broadcast.http.host: fails required",
		"Untagged: fails required",
		"plugin.port: fails min=1024",
		"Named.name: fails required",
	}, messages(t, configvalidator.Validate(&cfg)))
}

func TestTagViolationKeysOfElements(t *testing.T) {
	type config struct {
		Hosts     []string          `mapstructure:"hosts"     validate:"dive,hostname"`
		Databases map[string]string `mapstructure:"databases" validate:"dive,url"`
		Tenants   map[string]Tenant `mapstructure:"tenants"   validate:"dive"`
		Matrix    [][]int           `mapstructure:"matrix"    validate:"dive,dive,min=1"`
	}

	cfg := config{
		Hosts:     []string{"a.example", "b.example", "not a host"},
		Databases: map[string]string{"main": "mysql://db.example", "reporting": "not a url"},
		Tenants:   map[string]Tenant{"acme": {URL: "https://acme.example"}, "acme.corp]eu": {}},
		Matrix:    [][]int{{1, 0}},
	}

	assert.ElementsMatch(t, []string{
		"hosts[2]: fails hostname",
		"databases.reporting: fails url",
		"tenants.acme.corp]eu.url: fails required",
		"matrix[0][1]: fails min=1",
	}, messages(t, configvalidator.Validate(&cfg)))
}

func TestTagViolationKeysUnderUnnamedRoots(t *testing.T) {
	anonymous := struct {
		Port int `mapstructure:"port" validate:"min=1024"`
	}{Port: 80}

	generic := Box[HTTP]{V: validHTTP()}
	generic.V.Port = 80

	pointer := &Box[HTTP]{V: generic.V}

	assert.Equal(t, []string{"port: fails min=1024"}, messages(t, configvalidator.Validate(&anonymous)))
	assert.Equal(t, []string{"v.port: fails min=1024"}, messages(t, configvalidator.Validate(&generic)))
	assert.Equal(t, []string{"v.port: fails min=1024"}, messages(t, configvalidator.Validate(&pointer)))
}

func TestTagViolationOmitsTheValue(t *testing.T) {
	type config struct {
		Password string `mapstructure:"password" validate:"min=12"`
		Mode     string `mapstructure:"mode"     validate:"oneof=read write"`
	}

	err := configvalidator.Validate(&config{Password: "hunter2", Mode: "hunter2"})

	assert.Equal(t, []string{"password: fails min=12", "mode: fails oneof=read write"}, messages(t, err))
	assert.NotContains(t, err.Error(), "hunter2")
}
