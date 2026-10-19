package main

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/sikhlana/pkg/tools/release/parallel"
)

func TestPrintPanicStack(t *testing.T) {
	var out bytes.Buffer

	err := fmt.Errorf("checking logger: %w", &parallel.PanicError{Value: "boom", Stack: []byte("goroutine 7\n")})
	printPanicStack(&out, errors.Join(errors.New("other"), err))

	assert.Equal(t, "goroutine 7\n", out.String())
}

func TestPrintPanicStackPrintsEveryPanic(t *testing.T) {
	var out bytes.Buffer

	first := fmt.Errorf("checking logger: %w", &parallel.PanicError{Value: "boom", Stack: []byte("goroutine 7\n")})
	second := fmt.Errorf("checking kernel: %w", &parallel.PanicError{Value: "bang", Stack: []byte("goroutine 9\n")})
	printPanicStack(&out, errors.Join(first, errors.New("other"), second))

	assert.Equal(t, "goroutine 7\ngoroutine 9\n", out.String())
}

func TestPrintPanicStackWithoutAPanic(t *testing.T) {
	var out bytes.Buffer

	printPanicStack(&out, errors.New("other"))

	assert.Empty(t, out.String())
}
