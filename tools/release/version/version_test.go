package version_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/tools/release/version"
)

func TestParse(t *testing.T) {
	valid := []struct{ in, want string }{
		{"0.1.0", "v0.1.0"},
		{"1.20.3", "v1.20.3"},
		{"0.2.0\n", "v0.2.0"},
		{" 2.0.0\t", "v2.0.0"},
	}

	for _, tc := range valid {
		got, err := version.Parse(tc.in)
		require.NoError(t, err, tc.in)
		assert.Equal(t, tc.want, got, tc.in)
	}

	for _, in := range []string{"", "v0.2.0", "0.2", "1", "0.2.0-rc.1", "0.2.0+build", "01.2.0", "0.2.0.1", "latest"} {
		_, err := version.Parse(in)
		require.ErrorIs(t, err, version.ErrInvalid, in)
	}
}

func TestParseNamesTheValue(t *testing.T) {
	_, err := version.Parse("v0.2.0")
	require.ErrorContains(t, err, `"v0.2.0"`)
}

func TestIsRelease(t *testing.T) {
	assert.True(t, version.IsRelease("v0.1.0"))
	assert.False(t, version.IsRelease("0.1.0"))
	assert.False(t, version.IsRelease("v0.1"))
	assert.False(t, version.IsRelease("v0.1.0-rc.1"))
	assert.False(t, version.IsRelease("v0.1.0+meta"))
}

func TestPlain(t *testing.T) {
	assert.Equal(t, "0.4.0", version.Plain("v0.4.0"))
	assert.Equal(t, "0.4.0", version.Plain("0.4.0"))
}

func TestBelow(t *testing.T) {
	sorted := []string{"v0.2.0", "v0.9.1", "v0.10.0", "v1.0.0"}

	assert.Equal(t, "v1.0.0", version.Below(sorted, "v1.1.0"))
	assert.Equal(t, "v0.10.0", version.Below(sorted, "v1.0.0"))
	assert.Equal(t, "v0.9.1", version.Below(sorted, "v0.10.0"))
	assert.Empty(t, version.Below(sorted, "v0.2.0"))
	assert.Empty(t, version.Below(nil, "v0.1.0"))
}
