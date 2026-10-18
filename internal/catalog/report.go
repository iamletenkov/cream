package catalog

import (
	"fmt"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/manifest"
)

// Problem is one validation error: the cream exit code and error code it
// maps to, and where it was found. Path is relative to the validated root
// (catalog or extension directory), slash-separated; "." is the root.
type Problem struct {
	Ext     string `json:"ext,omitempty"`
	Path    string `json:"path"`
	Exit    int    `json:"exit"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ExtensionInfo is a valid extension found by the validator.
type ExtensionInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Path    string `json:"path"`
}

// Report is the result of Validate/ValidateDir: every error found (the
// validator does not stop at the first one) and the warnings of §6.3, §7.3,
// §8.4. NotApplicable lists the extensions of a catalog that need a newer
// cream (68 platform.cream_version): they do not make the catalog invalid
// (Р22), are not in Extensions and are warned as catalog.cream_newer.
// Catalog and CatalogVersion are empty for ValidateDir.
type Report struct {
	Catalog        string          `json:"catalog,omitempty"`
	CatalogVersion string          `json:"catalog_version,omitempty"`
	Extensions     []ExtensionInfo `json:"extensions"`
	NotApplicable  []Problem       `json:"not_applicable"`
	Errors         []Problem       `json:"errors"`
	Warnings       []cerr.Warning  `json:"warnings"`
}

func newReport() Report {
	return Report{Extensions: []ExtensionInfo{}, NotApplicable: []Problem{}, Errors: []Problem{}, Warnings: []cerr.Warning{}}
}

// OK reports whether no errors were found (warnings do not count).
func (r Report) OK() bool { return len(r.Errors) == 0 }

// Err is nil for a valid report; otherwise an *cerr.Error with the exit and
// code of the first problem, its message prefixed by the error count, and
// every problem in Details["errors"].
func (r Report) Err() error {
	if r.OK() {
		return nil
	}
	p := r.Errors[0]
	msg := p.Message
	if len(r.Errors) > 1 {
		msg = fmt.Sprintf("%s (и ещё ошибок: %d)", msg, len(r.Errors)-1)
	}
	e := cerr.New(p.Exit, p.Code, "%s", msg)
	e.Details = map[string]any{"errors": r.Errors}
	return e
}

func (r *Report) fail(ext, path, code, format string, a ...any) {
	r.Errors = append(r.Errors, Problem{Ext: ext, Path: path, Exit: cerr.ExitData, Code: code, Message: fmt.Sprintf(format, a...)})
}

// failErr records err (an *cerr.Error keeps its exit and code; anything
// else is 74 state.io: the tree could not be read).
func (r *Report) failErr(ext, path string, err error) {
	exit, code := cerr.ExitCode(err), cerr.CodeOf(err)
	if code == cerr.CodeInternal {
		exit, code = cerr.ExitState, "state.io"
	}
	r.Errors = append(r.Errors, Problem{Ext: ext, Path: path, Exit: exit, Code: code, Message: err.Error()})
}

func (r *Report) warn(code, format string, a ...any) {
	r.Warnings = append(r.Warnings, cerr.Warning{Code: code, Message: fmt.Sprintf(format, a...)})
}

// creamNewer records an extension that needs a newer cream (Р22): not an
// error, but NotApplicable and a catalog.cream_newer warning.
func (r *Report) creamNewer(ext, path string, err error, cream manifest.CreamBuild) {
	r.NotApplicable = append(r.NotApplicable, Problem{Ext: ext, Path: path, Exit: cerr.ExitCode(err),
		Code: cerr.CodeOf(err), Message: err.Error()})
	required := cerr.DetailsOf(err)["required"]
	r.Warnings = append(r.Warnings, cerr.Warning{Code: warnCreamNewer,
		Message: fmt.Sprintf("%s: требует cream %v, а это cream %s — расширение неприменимо", ext, required, cream.String()),
		Details: map[string]any{"ext": ext, "required": required, "cream": cream.String()}})
}
