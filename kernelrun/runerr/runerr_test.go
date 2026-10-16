package runerr_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/sikhlana/pkg/kernelrun/runerr"
)

func TestFromReadsWhatWithAttached(t *testing.T) {
	err := errors.New("crashed")

	assert.Equal(t, err, runerr.From(runerr.With(t.Context(), err)))
}

func TestFromABareContext(t *testing.T) {
	assert.NoError(t, runerr.From(t.Context()))
}

func TestFromANilOutcome(t *testing.T) {
	assert.NoError(t, runerr.From(runerr.With(t.Context(), nil)))
}
