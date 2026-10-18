package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/fsx"
)

// ownedOpener is the trusted opener for tests: the file must belong to the
// test user and be 0600 at most (the temp dir's ancestors are not root's).
func ownedOpener(path string) (*os.File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	if err := fsx.CheckOwnerPermFile(f, os.Getuid(), 0o600); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

func TestParseAssignments(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		args       []string
		stdin      *string
		params     string // содержимое --params-file ("" — нет файла)
		paramsPerm os.FileMode
		checkPerm  bool
		want       []Change
		wantExit   int
		wantCode   string
	}{
		{name: "K=V", args: []string{"PORT=8443", "HOST="}, want: []Change{{"PORT", "8443"}, {"HOST", ""}}},
		{name: "значение с =", args: []string{"LABEL_x=a=b"}, want: []Change{{"LABEL_x", "a=b"}}},
		{name: "без =", args: []string{"PORT"}, wantExit: cerr.ExitUsage, wantCode: "usage.assignment"},
		{name: "пустое имя", args: []string{"=1"}, wantExit: cerr.ExitUsage, wantCode: "usage.assignment"},
		{name: "неизвестный ключ", args: []string{"NOPE=1"}, wantExit: cerr.ExitData, wantCode: "config.unknown_key"},
		{name: "дубль", args: []string{"PORT=1", "PORT=2"}, wantExit: cerr.ExitUsage, wantCode: "usage.duplicate_key"},
		{name: "K=- со stdin", args: []string{"TOKEN=-"}, stdin: ptr("hunter2\n"), want: []Change{{"TOKEN", "hunter2"}}},
		{name: "K=- без перевода строки", args: []string{"PORT=-"}, stdin: ptr("8443"), want: []Change{{"PORT", "8443"}}},
		{name: "K=- многострочное", args: []string{"LABEL_x=-"}, stdin: ptr("a\nb\n"), want: []Change{{"LABEL_x", "a\nb"}}},
		{name: "два K=-", args: []string{"TOKEN=-", "PORT=-"}, stdin: ptr("x\n"), wantExit: cerr.ExitUsage, wantCode: "usage.stdin_twice"},
		{name: "пустой stdin", args: []string{"TOKEN=-"}, stdin: ptr(""), wantExit: cerr.ExitUsage, wantCode: "usage.stdin_empty"},
		{name: "stdin — пустая строка", args: []string{"TOKEN=-"}, stdin: ptr("\n"), wantExit: cerr.ExitUsage, wantCode: "usage.stdin_empty"},
		{name: "stdin нет", args: []string{"TOKEN=-"}, wantExit: cerr.ExitUsage, wantCode: "usage.stdin_empty"},
		{name: "секрет в argv", args: []string{"TOKEN=hunter2"}, wantExit: cerr.ExitUsage, wantCode: "secret.in_argv"},
		{name: "очистка секрета в argv", args: []string{"TOKEN="}, want: []Change{{"TOKEN", ""}}},
		{
			name:       "params-file в формате env",
			args:       []string{"PORT=1"},
			params:     "# параметры\nTOKEN='hun ter2'\nexport IGNORED=1\nLABEL_x=\"a\\\"b\"\n",
			paramsPerm: 0o600,
			want:       []Change{{"PORT", "1"}, {"TOKEN", "hun ter2"}, {"LABEL_x", "a\"b"}},
		},
		{
			name: "params-file 0600 с проверкой", params: "TOKEN=x\n", paramsPerm: 0o600, checkPerm: true,
			want: []Change{{"TOKEN", "x"}},
		},
		{
			name: "params-file 0644 с проверкой", params: "TOKEN=x\n", paramsPerm: 0o644, checkPerm: true,
			wantExit: cerr.ExitPerm, wantCode: "perm.params_file",
		},
		{
			name: "params-file 0644 без проверки", params: "TOKEN=x\n", paramsPerm: 0o644,
			want: []Change{{"TOKEN", "x"}},
		},
		{
			name: "params-file с неизвестным ключом", params: "NOPE=x\n", paramsPerm: 0o600,
			wantExit: cerr.ExitData, wantCode: "config.unknown_key",
		},
		{
			name: "params-file дублирует argv", args: []string{"PORT=1"}, params: "PORT=2\n", paramsPerm: 0o600,
			wantExit: cerr.ExitUsage, wantCode: "usage.duplicate_key",
		},
		{
			name: "битый params-file (не UTF-8)", params: "TOKEN=\xff\n", paramsPerm: 0o600,
			wantExit: cerr.ExitData, wantCode: "config.params_file",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := AssignInput{Args: tc.args, CheckPerm: tc.checkPerm}
			if tc.stdin != nil {
				in.Stdin = strings.NewReader(*tc.stdin)
			}
			if tc.params != "" {
				in.ParamsFile = filepath.Join(t.TempDir(), "params.env")
				writeFile(t, in.ParamsFile, tc.params, tc.paramsPerm)
			}
			got, err := parseAssignments(in, testSchema(t), ownedOpener)
			wantCerr(t, err, tc.wantExit, tc.wantCode)
			if err == nil && !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %q, ожидали %q", got, tc.want)
			}
			if err != nil && strings.Contains(err.Error(), "hunter2") {
				t.Errorf("значение секрета в тексте ошибки: %v", err)
			}
		})
	}
}

// The exported entry point checks the params file from / with root as owner:
// under a temporary directory that check always fails, which is what 77 means.
func TestParseAssignmentsTrustedFromRoot(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "params.env")
	writeFile(t, p, "TOKEN=x\n", 0o644)
	_, err := ParseAssignments(AssignInput{ParamsFile: p, CheckPerm: true}, testSchema(t))
	wantCerr(t, err, cerr.ExitPerm, "perm.params_file")
}

func TestParseAssignmentsMissingParamsFile(t *testing.T) {
	t.Parallel()
	_, err := ParseAssignments(AssignInput{ParamsFile: filepath.Join(t.TempDir(), "nope")}, testSchema(t))
	wantCerr(t, err, cerr.ExitNotFound, "not_found.params_file")
}

func ptr(s string) *string { return &s }
