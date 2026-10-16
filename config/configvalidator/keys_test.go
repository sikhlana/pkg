package configvalidator_test

import (
	"log/slog"
	"net"
	"net/netip"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/sikhlana/pkg/config/configvalidator"
)

type Base struct {
	Name string
}

// pointerText decodes itself from text through a pointer receiver only, as time.Time does.
type pointerText struct {
	value string
}

func (p *pointerText) UnmarshalText(text []byte) error {
	p.value = string(text)

	return nil
}

func TestKeySegment(t *testing.T) {
	tests := []struct {
		name  string
		field reflect.StructField
		want  string
		ok    bool
	}{
		{
			name:  "tagged",
			field: reflect.StructField{Name: "ReadTimeout", Tag: `mapstructure:"read_timeout"`},
			want:  "read_timeout",
			ok:    true,
		},
		{name: "untagged", field: reflect.StructField{Name: "Port"}, want: "Port", ok: true},
		{
			name:  "options ignored",
			field: reflect.StructField{Name: "Port", Tag: `mapstructure:"port,omitempty" validate:"required"`},
			want:  "port",
			ok:    true,
		},
		{
			name:  "squashed",
			field: reflect.StructField{Name: "Base", Anonymous: true, Tag: `mapstructure:",squash"`},
			want:  "",
			ok:    true,
		},
		{
			name:  "squashed with a name",
			field: reflect.StructField{Name: "Base", Tag: `mapstructure:"base,omitempty,squash"`},
			want:  "",
			ok:    true,
		},
		{
			name:  "embedded without squash",
			field: reflect.StructField{Name: "Base", Anonymous: true},
			want:  "Base",
			ok:    true,
		},
		{name: "skipped", field: reflect.StructField{Name: "Secret", Tag: `mapstructure:"-"`}, want: "", ok: false},
		{
			name:  "unexported",
			field: reflect.StructField{Name: "secret", PkgPath: "example.com/app", Tag: `mapstructure:"secret"`},
			want:  "",
			ok:    false,
		},
		{
			name: "unexported embedded with squash",
			field: reflect.StructField{
				Name: "base", PkgPath: "example.com/app", Anonymous: true, Tag: `mapstructure:",squash"`,
			},
			want: "",
			ok:   true,
		},
		{
			name:  "unexported embedded without squash",
			field: reflect.StructField{Name: "base", PkgPath: "example.com/app", Anonymous: true},
			want:  "",
			ok:    false,
		},
		{
			name:  "unexported squash without embedding",
			field: reflect.StructField{Name: "base", PkgPath: "example.com/app", Tag: `mapstructure:",squash"`},
			want:  "",
			ok:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := configvalidator.KeySegment(tt.field)

			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDecodesText(t *testing.T) {
	tests := []struct {
		name string
		typ  reflect.Type
		want bool
	}{
		{name: "time.Time", typ: reflect.TypeFor[time.Time](), want: true},
		{name: "netip.Prefix", typ: reflect.TypeFor[netip.Prefix](), want: true},
		{name: "slog.Level", typ: reflect.TypeFor[slog.Level](), want: true},
		{name: "net.IP", typ: reflect.TypeFor[net.IP](), want: true},
		{name: "pointer receiver", typ: reflect.TypeFor[pointerText](), want: true},
		{name: "pointer to a text type", typ: reflect.TypeFor[*time.Time](), want: true},
		{name: "plain struct", typ: reflect.TypeFor[Base](), want: false},
		{name: "string", typ: reflect.TypeFor[string](), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, configvalidator.DecodesText(tt.typ))
		})
	}
}
