package log_test

import (
	"encoding"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/log"
)

func TestFormatDecodesItsName(t *testing.T) {
	for _, want := range []log.Format{log.FormatJSON, log.FormatText} {
		t.Run(want.String(), func(t *testing.T) {
			text, err := want.MarshalText()
			require.NoError(t, err)

			got := log.Format(-1)
			require.NoError(t, got.UnmarshalText(text))
			assert.Equal(t, want, got)
		})
	}
}

func TestFormatDecodesText(t *testing.T) {
	var f log.Format

	require.NoError(t, f.UnmarshalText([]byte("text")))
	assert.Equal(t, log.FormatText, f)

	text, err := f.MarshalText()
	require.NoError(t, err)
	assert.Equal(t, "text", string(text))
}

func TestFormatRejectsAnUnknownName(t *testing.T) {
	f := log.FormatText

	err := f.UnmarshalText([]byte("yaml"))

	require.ErrorIs(t, err, log.ErrUnknownFormat)
	require.ErrorContains(t, err, `"yaml"`)
	assert.Equal(t, log.FormatText, f, "a failed decode leaves the format as it was")
}

func TestFormatOutsideTheConstants(t *testing.T) {
	f := log.Format(7)

	assert.Equal(t, "Format(7)", f.String())

	_, err := f.MarshalText()
	require.ErrorIs(t, err, log.ErrUnknownFormat)

	_, err = log.New(log.Params{Format: f})
	require.ErrorIs(t, err, log.ErrUnknownFormat)
}

func TestFormatConstantsMarshalAsValues(t *testing.T) {
	var m encoding.TextMarshaler = log.FormatText

	text, err := m.MarshalText()
	require.NoError(t, err)
	assert.Equal(t, "text", string(text))
}
