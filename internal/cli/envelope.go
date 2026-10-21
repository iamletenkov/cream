package cli

import (
	"encoding/json"
	"io"
	"strings"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/version"
)

// Contract is the version of the external contract (manifest, CLI, JSON forms).
const Contract = "cream/v1"

// envelope is the single object printed to stdout with --json (spec §11.4).
// phase and reason are present only where the command decided its phase
// (install, upgrade, remove; §10.1).
type envelope struct {
	Cream    string         `json:"cream"`
	Command  string         `json:"command"`
	Phase    string         `json:"phase,omitempty"`
	Reason   string         `json:"reason,omitempty"`
	OK       bool           `json:"ok"`
	Result   any            `json:"result"`
	Warnings []cerr.Warning `json:"warnings"`
	Error    *envelopeError `json:"error"`
}

type envelopeError struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
}

// creamVersion is the running version without the leading "v" ("dev" unset).
func creamVersion() string {
	return strings.TrimPrefix(version.Line(version.Version), "cream ")
}

func newEnvelope(command string, ok bool, result any, warnings []cerr.Warning, err error) envelope {
	env := envelope{Cream: creamVersion(), Command: command, OK: ok, Result: result, Warnings: warnings}
	if env.Warnings == nil {
		env.Warnings = []cerr.Warning{}
	}
	if err != nil {
		env.Result = nil
		env.Error = &envelopeError{Code: cerr.CodeOf(err), Message: err.Error(), Details: cerr.DetailsOf(err)}
	}
	return env
}

// writeJSON writes v as indented JSON without HTML escaping, ending in "\n".
func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
