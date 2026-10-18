// Package cerr holds cream's typed errors and exit codes (spec §11.3).
//
// Every layer returns errors; only cli.Run turns them into an exit code and
// the error object of the --json envelope. An *Error carries both the exit
// code and the stable machine-readable code ("deps.missing", "lock.timeout");
// any other error is reported as 70 "internal".
package cerr

import (
	"errors"
	"fmt"
	"sync"
)

// Exit codes, spec §11.3. They are an external contract: values never change.
const (
	ExitOK            = 0
	ExitCheck         = 1
	ExitUsage         = 64
	ExitData          = 65
	ExitNotFound      = 66
	ExitGraph         = 67
	ExitNotApplicable = 68
	ExitSource        = 69
	ExitInternal      = 70
	ExitState         = 74
	ExitLock          = 75
	ExitProtocol      = 76
	ExitPerm          = 77
	ExitHook          = 79
)

// CodeInternal is the error.code of any error that is not an *Error.
const CodeInternal = "internal"

// Error is a cream error with an exit code and a machine-readable code.
type Error struct {
	Exit    int
	Code    string
	Message string
	Details map[string]any
	Err     error
}

// New returns an *Error with a formatted human message.
func New(exit int, code, format string, a ...any) *Error {
	return &Error{Exit: exit, Code: code, Message: fmt.Sprintf(format, a...)}
}

// Wrap returns an *Error that wraps err; errors.Is/As see through it.
func Wrap(err error, exit int, code, format string, a ...any) *Error {
	return &Error{Exit: exit, Code: code, Message: fmt.Sprintf(format, a...), Err: err}
}

// Error returns the message, followed by the wrapped error if there is one.
func (e *Error) Error() string {
	if e.Err == nil {
		return e.Message
	}
	if e.Message == "" {
		return e.Err.Error()
	}
	return e.Message + ": " + e.Err.Error()
}

// Unwrap returns the wrapped error.
func (e *Error) Unwrap() error { return e.Err }

// ExitCode maps err to the process exit code: nil → 0, the outermost *Error
// found by errors.As → its Exit, anything else → 70.
func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}
	var e *Error
	if errors.As(err, &e) {
		return e.Exit
	}
	return ExitInternal
}

// CodeOf returns the machine-readable code of err: "" for nil, the code of the
// outermost *Error, "internal" otherwise.
func CodeOf(err error) string {
	if err == nil {
		return ""
	}
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return CodeInternal
}

// DetailsOf returns the details of the outermost *Error, or nil.
func DetailsOf(err error) map[string]any {
	var e *Error
	if errors.As(err, &e) {
		return e.Details
	}
	return nil
}

// Warning is a non-fatal message for the warnings list of the envelope (§11.4).
type Warning struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// Warnings collects warnings of one command run. It is passed explicitly
// (never global); the zero value is ready to use and safe for concurrent Add.
type Warnings struct {
	mu   sync.Mutex
	list []Warning
}

// Add appends a warning with a formatted message.
func (w *Warnings) Add(code, format string, a ...any) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.list = append(w.list, Warning{Code: code, Message: fmt.Sprintf(format, a...)})
}

// AddDetails appends a warning with a formatted message and machine-readable
// details (ruling Р20: the extension of a corrupt registry record).
func (w *Warnings) AddDetails(code string, details map[string]any, format string, a ...any) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.list = append(w.list, Warning{Code: code, Message: fmt.Sprintf(format, a...), Details: details})
}

// List returns a copy of the warnings in the order they were added; never nil.
func (w *Warnings) List() []Warning {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]Warning, len(w.list))
	copy(out, w.list)
	return out
}
