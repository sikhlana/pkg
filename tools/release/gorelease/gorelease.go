// Package gorelease builds and runs gorelease, which compares a module's
// exported API with a released version and suggests the next version.
package gorelease

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Package is the gorelease command the tool's go.mod pins.
const Package = "golang.org/x/exp/cmd/gorelease"

// BaseNone compares against nothing, for a module with no release yet.
const BaseNone = "none"

// DefaultTimeout bounds one gorelease run, including fetching the base
// version, so a stalled download can't hold CI for hours.
const DefaultTimeout = 10 * time.Minute

// ErrTimeout reports a gorelease run that didn't finish within its timeout.
var ErrTimeout = errors.New("gorelease timed out")

var suggestedVersion = regexp.MustCompile(`(?m)^Suggested version: (v\S+)`)

// Runner runs a gorelease binary.
type Runner struct {
	// Path is the gorelease executable.
	Path string
	// Timeout bounds one run. Zero means DefaultTimeout.
	Timeout time.Duration
}

// Result is gorelease's verdict on one module.
type Result struct {
	// Output is gorelease's combined standard output and error.
	Output string
	// Suggested is the lowest version gorelease allows, such as v0.4.0, or
	// empty when it suggests none.
	Suggested string
	// Failed reports a non-zero exit status.
	Failed bool
	// Incompatible reports that gorelease found incompatible API changes it
	// can't suggest a version for, as for a module past v1.
	Incompatible bool
}

// Build compiles the gorelease version pinned by toolDir's go.mod into
// outDir, with GOWORK=off.
func Build(ctx context.Context, toolDir, outDir string) (Runner, error) {
	bin := filepath.Join(outDir, "gorelease")

	var output bytes.Buffer

	cmd := exec.CommandContext(ctx, "go", "build", "-o", bin, Package)
	cmd.Dir = toolDir

	cmd.Env = append(os.Environ(), "GOWORK=off")
	cmd.Stdout = &output
	cmd.Stderr = &output

	if err := cmd.Run(); err != nil {
		return Runner{}, fmt.Errorf("building gorelease: %w: %s", err, strings.TrimSpace(output.String()))
	}

	return Runner{Path: bin}, nil
}

// Run compares the module in dir against base, a version such as v0.3.0 or
// BaseNone, with GOWORK=off so sibling modules resolve as a consumer sees
// them. gorelease exiting non-zero sets Result.Failed rather than returning
// an error; an error means gorelease couldn't run at all or timed out, which
// wraps ErrTimeout.
func (r Runner) Run(ctx context.Context, dir, base string) (Result, error) {
	timeout := r.Timeout

	if timeout == 0 {
		timeout = DefaultTimeout
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)

	defer cancel()

	var output bytes.Buffer

	// Path is the binary Build produced or the one --gorelease named.
	cmd := exec.CommandContext(ctx, r.Path, "-base="+base) //nolint:gosec // The tool's own gorelease, not user input.
	cmd.Dir = dir

	cmd.Env = append(os.Environ(), "GOWORK=off")
	cmd.Stdout = &output
	cmd.Stderr = &output
	// gorelease starts go commands of its own; don't wait on their pipes
	// after it's killed.
	cmd.WaitDelay = 10 * time.Second

	err := cmd.Run()

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return Result{}, fmt.Errorf(
			"%w after %s in %s: %s",
			ErrTimeout,
			timeout,
			dir,
			strings.TrimSpace(output.String()),
		)
	}

	if _, exited := errors.AsType[*exec.ExitError](err); err != nil && !exited {
		return Result{}, fmt.Errorf("running gorelease in %s: %w", dir, err)
	}

	result := Result{
		Output:       strings.TrimSpace(output.String()),
		Failed:       err != nil,
		Incompatible: strings.Contains(output.String(), "Incompatible changes were detected."),
	}

	if match := suggestedVersion.FindStringSubmatch(result.Output); match != nil {
		result.Suggested = match[1]
	}

	return result, nil
}
