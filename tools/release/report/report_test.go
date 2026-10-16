package report_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/tools/release/report"
)

var problems = []report.Problem{
	{
		File:    "metrics/VERSION",
		Module:  "metrics",
		Message: "metrics is new, and a new module starts at 0.1.0, not 0.2.0",
	},
	{
		File:    "logger/go.mod",
		Module:  "logger",
		Message: "the API comparison against v0.3.0 failed",
		Detail:  "boom\nbang",
	},
}

func TestWriterOutsideActions(t *testing.T) {
	var out bytes.Buffer

	w := report.New(&out, func(string) string { return "" })
	w.Summary("## Summary")
	w.Errors(problems)
	w.Warning("proxy unavailable")
	w.Output("empty", "true")

	assert.Equal(t, "## Summary\nwarning: proxy unavailable\nempty=true\n", out.String())
}

func TestWriterInActions(t *testing.T) {
	dir := t.TempDir()
	env := map[string]string{
		"GITHUB_ACTIONS":      "true",
		"GITHUB_STEP_SUMMARY": filepath.Join(dir, "summary"),
		"GITHUB_OUTPUT":       filepath.Join(dir, "output"),
	}

	var out bytes.Buffer

	w := report.New(&out, func(k string) string { return env[k] })
	w.Summary("## Summary")
	w.Errors(problems)
	w.Warning("proxy unavailable")
	w.Output("modules", `["logger"]`)

	assert.Equal(t, "## Summary\n"+
		"::error file=metrics/VERSION::metrics is new, and a new module starts at 0.1.0, not 0.2.0\n"+
		"::error file=logger/go.mod::the API comparison against v0.3.0 failed%0A%0Aboom%0Abang\n"+
		"::warning::proxy unavailable\n"+
		"modules=[\"logger\"]\n", out.String())

	summary, err := os.ReadFile(env["GITHUB_STEP_SUMMARY"])
	require.NoError(t, err)
	assert.Contains(t, string(summary), "## Summary")

	output, err := os.ReadFile(env["GITHUB_OUTPUT"])
	require.NoError(t, err)
	assert.Contains(t, string(output), `["logger"]`)
}

func TestCell(t *testing.T) {
	assert.Equal(t, `a \| b`, report.Cell("a | b"))
	assert.Equal(t, "first<br>second<br>third", report.Cell("first\nsecond\r\nthird"))
	assert.Equal(t, "plain", report.Cell("plain"))
}
