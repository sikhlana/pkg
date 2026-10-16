// Package report sends the release tool's results to the terminal and, when
// running in GitHub Actions, to the job summary, annotations and step
// outputs.
package report

import (
	"fmt"
	"io"
	"strings"

	"github.com/sethvargo/go-githubactions"
)

var cellEscaper = strings.NewReplacer("|", `\|`, "\r\n", "<br>", "\n", "<br>")

// Cell escapes s for a markdown table cell: pipes would end the cell and
// newlines the row.
func Cell(s string) string {
	return cellEscaper.Replace(s)
}

// Problem is one broken rule, reported against the file to fix.
type Problem struct {
	// File is the slash-separated path, relative to the repository root, of
	// the file to fix.
	File string
	// Module is the directory of the library module the problem belongs to,
	// or empty for a repository-wide problem.
	Module string
	// Message says what's wrong, on one line.
	Message string
	// Detail is optional longer output, such as a tool's report, that
	// annotations show after the message.
	Detail string
}

// Writer writes results as plain text and, inside GitHub Actions, also as
// workflow commands and environment files.
type Writer struct {
	out     io.Writer
	action  *githubactions.Action
	actions bool
}

// New returns a Writer that prints to out and reads the environment through
// getenv. GitHub Actions output is enabled when GITHUB_ACTIONS is "true".
func New(out io.Writer, getenv func(string) string) Writer {
	return Writer{
		out:     out,
		action:  githubactions.New(githubactions.WithWriter(out), githubactions.WithGetenv(getenv)),
		actions: getenv("GITHUB_ACTIONS") == "true",
	}
}

// Summary prints markdown and, in GitHub Actions, appends it to the job
// summary.
func (w Writer) Summary(markdown string) {
	_, _ = fmt.Fprintln(w.out, markdown)

	if w.actions {
		w.action.AddStepSummary(markdown)
	}
}

// Errors attaches an error annotation for each problem to its file. Outside
// GitHub Actions it does nothing: the summary already lists every problem.
func (w Writer) Errors(problems []Problem) {
	if !w.actions {
		return
	}

	for _, p := range problems {
		message := p.Message

		if p.Detail != "" {
			message += "\n\n" + p.Detail
		}

		w.action.WithFieldsMap(map[string]string{"file": p.File}).Errorf("%s", message)
	}
}

// Warning reports a problem that doesn't fail the run.
func (w Writer) Warning(message string) {
	if w.actions {
		w.action.Warningf("%s", message)
		return
	}

	_, _ = fmt.Fprintln(w.out, "warning: "+message)
}

// Output prints key=value and, in GitHub Actions, sets it as a step output.
func (w Writer) Output(key, value string) {
	_, _ = fmt.Fprintf(w.out, "%s=%s\n", key, value)

	if w.actions {
		w.action.SetOutput(key, value)
	}
}
