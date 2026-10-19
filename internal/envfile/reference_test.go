package envfile

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Reference fixtures: every testdata/cases/<case>.env is fed to a real
// systemd as EnvironmentFile= and the resulting environment is stored as
// testdata/systemd-<ver>/<case>.json (captured once in containers, Task 19):
//
//	{"env": ["KEY=VALUE", ...]}   // variables systemd passed to the unit
//	{"error": true}               // systemd refused the file (unit failed)
//
// Only keys assigned by the case file are listed; order is not significant.
// Every version is checked against the production Parse: it either reads the
// same environment as systemd or refuses the file because systemd < 254 and
// v254+ read it differently (a comment ending in `\`); refusing is safe,
// reading something else is not.

type reference struct {
	Env   []string `json:"env"`
	Error bool     `json:"error"`
}

func TestReferenceSystemd(t *testing.T) {
	t.Parallel()
	dirs, err := filepath.Glob(filepath.Join("testdata", "systemd-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) == 0 {
		t.Fatal("нет эталонов testdata/systemd-<ver>/ (снимает test/integration/capture-envfile.sh)")
	}
	cases := caseFiles(t)
	for _, dir := range dirs {
		ver := strings.TrimPrefix(filepath.Base(dir), "systemd-")
		for _, c := range cases {
			name := strings.TrimSuffix(filepath.Base(c), ".env")
			t.Run(ver+"/"+name, func(t *testing.T) {
				t.Parallel()
				raw, err := os.ReadFile(filepath.Join(dir, name+".json"))
				if err != nil {
					t.Fatalf("нет эталона для случая %s: %v", name, err)
				}
				var ref reference
				if err := json.Unmarshal(raw, &ref); err != nil {
					t.Fatalf("эталон %s: %v", name, err)
				}
				data, err := os.ReadFile(c)
				if err != nil {
					t.Fatal(err)
				}
				f, err := Parse(data)
				if errors.Is(err, errDialect) {
					return
				}
				if ref.Error {
					if err == nil {
						t.Fatalf("systemd %s отверг файл, разборщик принял: %v", ver, f.Keys())
					}
					return
				}
				if err != nil {
					t.Fatalf("systemd %s принял файл, разборщик отверг: %v", ver, err)
				}
				want := map[string]string{}
				for _, kv := range ref.Env {
					k, v, ok := strings.Cut(kv, "=")
					if !ok {
						t.Fatalf("эталон %s: строка без '=': %q", name, kv)
					}
					want[k] = v
				}
				got := map[string]string{}
				for _, k := range f.Keys() {
					got[k], _ = f.Get(k)
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("systemd %s: %s\nразборщик: %q\nэталон:    %q", ver, name, got, want)
				}
			})
		}
	}
}

// The versions §14 names are all captured, and the divergence on a comment
// ending in `\` is pinned: systemd 249 (and 252) continue the comment onto
// the next line and lose A, 255+ (and 257) end the comment at the newline.
// Parse refuses that file (errDialect) precisely because of this row.
func TestReferenceCommentBackslashDiverges(t *testing.T) {
	t.Parallel()
	load := func(ver string) reference {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join("testdata", "systemd-"+ver, "comment-backslash.json"))
		if err != nil {
			t.Fatalf("эталон systemd %s: %v", ver, err)
		}
		var ref reference
		if err := json.Unmarshal(raw, &ref); err != nil {
			t.Fatalf("эталон systemd %s: %v", ver, err)
		}
		return ref
	}
	tests := []struct {
		ver  string
		want []string
	}{
		{"249", []string{"B=2"}},
		{"252", []string{"B=2"}},
		{"255", []string{"A=1", "B=2"}},
		{"257", []string{"A=1", "B=2"}},
	}
	for _, tc := range tests {
		t.Run(tc.ver, func(t *testing.T) {
			t.Parallel()
			if got := load(tc.ver); got.Error || !reflect.DeepEqual(got.Env, tc.want) {
				t.Errorf("systemd %s: comment-backslash = %+v, want env %q", tc.ver, got, tc.want)
			}
		})
	}
}

// Every case file must at least be handled without panicking; refused are
// the invalid UTF-8 case and the comment-backslash case, which systemd < 254
// and v254+ read differently.
func TestCaseFilesParse(t *testing.T) {
	t.Parallel()
	for _, c := range caseFiles(t) {
		data, err := os.ReadFile(c)
		if err != nil {
			t.Fatal(err)
		}
		_, err = Parse(data)
		if filepath.Base(c) == "comment-backslash.env" {
			if !errors.Is(err, errDialect) {
				t.Errorf("%s: err = %v, want the dialect error", c, err)
			}
			continue
		}
		wantErr := strings.HasPrefix(filepath.Base(c), "invalid-")
		if (err != nil) != wantErr {
			t.Errorf("%s: err = %v, want error: %v", c, err, wantErr)
		}
	}
}

func caseFiles(t *testing.T) []string {
	t.Helper()
	cases, err := filepath.Glob(filepath.Join("testdata", "cases", "*.env"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("testdata/cases/*.env пуст")
	}
	return cases
}
