package repository_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/tools/release/repository"
)

func TestParse(t *testing.T) {
	owner, name, err := repository.Parse("sikhlana/pkg")
	require.NoError(t, err)

	assert.Equal(t, "sikhlana", owner)
	assert.Equal(t, "pkg", name)
}

func TestParseRejectsInvalidNames(t *testing.T) {
	for _, s := range []string{"", "sikhlana", "/pkg", "sikhlana/", "a/b/c"} {
		_, _, err := repository.Parse(s)
		require.ErrorIs(t, err, repository.ErrInvalid, s)
	}
}
