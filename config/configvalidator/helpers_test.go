package configvalidator_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/config/configvalidator"
)

var (
	errUnknownMode = errors.New("unknown mode")
	errNoCert      = errors.New("missing cert")
	errHTTPPort    = errors.New("http")
	errGRPCPort    = errors.New("grpc")
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

// NoViolations returns an empty Errors, which reports nothing.
type NoViolations struct{}

func (NoViolations) Validate() error {
	return configvalidator.Errors{}
}

// Site is a Validator with a tag of its own, failing with its Reason.
type Site struct {
	URL    string `mapstructure:"url"    validate:"required"`
	Reason string `mapstructure:"reason"`
}

func (s Site) Validate() error {
	return errors.New(s.Reason)
}

// common is unexported and squashed into its parents, which still gain its exported fields.
type common struct {
	TLS   AMQP  `mapstructure:"tls"   validate:"-"`
	Audit Check `mapstructure:"audit"`
}

// UsesCommon delegates to a field promoted from common.
type UsesCommon struct {
	common `mapstructure:",squash"`
}

func (u UsesCommon) Validate() error {
	return configvalidator.ValidateField(&u, &u.TLS)
}

// TypedNil returns a nil *Error, which isn't a violation, or, with Bad set, an *Error that has no cause.
type TypedNil struct {
	Bad bool `mapstructure:"bad"`
}

func (n TypedNil) Validate() error {
	var violation *configvalidator.Error

	if n.Bad {
		violation = &configvalidator.Error{Key: "cert_path"}
	}

	return violation
}

// Secured gains SSL's Validate method through an embedded pointer, which is nil when the ssl section is missing.
type Secured struct {
	*SSL `mapstructure:"ssl"`
}

// Shadowing declares its own Validate method beside an embedded SSL pointer.
type Shadowing struct {
	*SSL `mapstructure:"ssl"`
}

func (Shadowing) Validate() error {
	return errors.New("shadowing")
}

// Databases delegates to its active database, held in a map of pointers its tags skip.
type Databases struct {
	Active string           `mapstructure:"active"`
	DBs    map[string]*AMQP `mapstructure:"dbs"    validate:"-"`
}

func (d Databases) Validate() error {
	return configvalidator.ValidateField(&d, d.DBs[d.Active])
}

// OptionalAMQP dispatches to an optional AMQP section, nil when it's missing.
type OptionalAMQP struct {
	Mode string `mapstructure:"mode" validate:"oneof=file amqp"`
	AMQP *AMQP  `mapstructure:"amqp" validate:"-"`
}

func (o OptionalAMQP) Validate() error {
	if o.Mode == "amqp" {
		return configvalidator.ValidateField(&o, o.AMQP)
	}

	return nil
}

// Hosts delegates to its hosts, a map its tags skip.
type Hosts struct {
	Hosts map[string]PointerCheck `mapstructure:"hosts" validate:"-"`
}

func (h Hosts) Validate() error {
	return configvalidator.ValidateField(&h, &h.Hosts)
}

// Brokers delegates to its brokers, a slice its tags skip.
type Brokers struct {
	Brokers []AMQP `mapstructure:"brokers" validate:"-"`
}

func (b Brokers) Validate() error {
	return configvalidator.ValidateField(&b, &b.Brokers)
}

// Overlap fails with a message that wraps two errors.
type Overlap struct{}

func (Overlap) Validate() error {
	return fmt.Errorf("ports overlap between %w and %w", errHTTPPort, errGRPCPort)
}

// violations returns the violations in err, which Validate returns as an Errors.
func violations(t *testing.T, err error) []*configvalidator.Error {
	t.Helper()

	if err == nil {
		return nil
	}

	errs, ok := err.(configvalidator.Errors) //nolint:errorlint // The result must be an Errors, not wrap one.
	require.True(t, ok, "%T isn't a configvalidator.Errors", err)
	require.NotEmpty(t, errs)

	result := make([]*configvalidator.Error, 0, len(errs))

	for _, part := range errs {
		violation, isError := part.(*configvalidator.Error) //nolint:errorlint // Each part must be an *Error, not wrap one.
		require.True(t, isError, "%T isn't a *configvalidator.Error", part)

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

func validHTTP() HTTP {
	return HTTP{Host: "localhost", Port: 8080}
}
