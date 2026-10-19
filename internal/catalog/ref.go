// Package catalog validates extension catalogs (§4.1) and finds an
// extension by reference in the source caches (§9.3). It reads only local
// trees: the network belongs to source.Syncer.
package catalog

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/manifest"
	"github.com/iamletenkov/cream/internal/semver"
)

// reSource — имя источника (§9.1); то же правило, что в source.
var reSource = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// Ref is a parsed reference [source/]name[@version]. Version is an
// assertion, not a choice: Lookup refuses a different version (§9.3).
type Ref struct {
	Source, Name string
	Version      *semver.Version
}

// ParseRef parses [source/]name[@version]; a malformed reference is
// 64 usage.ref.
func ParseRef(s string) (Ref, error) {
	bad := func(format string, a ...any) (Ref, error) {
		e := cerr.New(cerr.ExitUsage, "usage.ref", "ссылка %q: %s", s, fmt.Sprintf(format, a...))
		e.Details = map[string]any{"ref": s}
		return Ref{}, e
	}
	var r Ref
	rest := s
	if before, after, ok := strings.Cut(rest, "@"); ok {
		v, err := semver.Parse(after)
		if err != nil {
			return bad("%v (ожидается [источник/]имя[@X.Y.Z])", err)
		}
		r.Version = &v
		rest = before
	}
	if before, after, ok := strings.Cut(rest, "/"); ok {
		if !reSource.MatchString(before) {
			return bad("имя источника %q: ожидается [a-z0-9][a-z0-9._-]{0,63}", before)
		}
		r.Source = before
		rest = after
	}
	if !manifest.ValidName(rest) {
		return bad("имя расширения %q: ожидается [a-z0-9][a-z0-9._-]{0,63}, кроме зарезервированных и префикса cream-", rest)
	}
	r.Name = rest
	return r, nil
}

// String formats the reference back as [source/]name[@version].
func (r Ref) String() string {
	s := r.Name
	if r.Source != "" {
		s = r.Source + "/" + s
	}
	if r.Version != nil {
		s += "@" + r.Version.String()
	}
	return s
}
