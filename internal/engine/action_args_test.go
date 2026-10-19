package engine

import (
	"errors"
	"io"
	"maps"
	"slices"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/manifest"
	"github.com/iamletenkov/cream/internal/semver"
)

// actionFixture — объявление действия name фикстуры ext.
func actionFixture(t *testing.T, ext, name string) manifest.Action {
	t.Helper()
	m, err := manifest.Load(fixtureDir(t, ext), manifest.CreamBuild{Version: semver.Version{Major: 1}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	a, ok := m.Actions[name]
	if !ok {
		t.Fatalf("%s: нет действия %s", ext, name)
	}
	return a
}

func TestActionParseArgs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		action     string // действие фикстуры action-json
		args       []string
		stdin      string
		wantArgv   []string
		wantValues map[string]string
		wantSecret string
		wantExit   int
		wantCode   string
	}{
		{name: "none без аргументов", action: "status", wantArgv: []string{}},
		{name: "none с аргументом", action: "status", args: []string{"x"}, wantExit: 64, wantCode: "usage.args"},
		{name: "free как есть", action: "echo-args", args: []string{"--json", "a b", "K=V", "-"},
			wantArgv: []string{"--json", "a b", "K=V", "-"}},
		{name: "free без stdin не читает stdin", action: "echo-args", args: []string{"X=-"}, stdin: "s",
			wantArgv: []string{"X=-"}},
		{name: "pairs с секретом со stdin", action: "join",
			args: []string{"FIXTURE_PROXY=proxy.example:443", "FIXTURE_SECRET=-"}, stdin: "s3cr3t\n",
			wantArgv:   []string{"FIXTURE_PROXY=proxy.example:443", "FIXTURE_SECRET=-"},
			wantValues: map[string]string{"FIXTURE_PROXY": "proxy.example:443"}, wantSecret: "s3cr3t"},
		{name: "секрет в argv", action: "join", args: []string{"FIXTURE_PROXY=p.example", "FIXTURE_SECRET=x"},
			stdin: "s", wantExit: 64, wantCode: "secret.in_argv"},
		{name: "пустой секрет в argv", action: "join", args: []string{"FIXTURE_PROXY=p.example", "FIXTURE_SECRET="},
			stdin: "s", wantExit: 64, wantCode: "secret.in_argv"},
		{name: "пустой stdin", action: "join", args: []string{"FIXTURE_PROXY=p.example", "FIXTURE_SECRET=-"},
			wantExit: 64, wantCode: "usage.stdin_empty"},
		{name: "stdin из одного перевода строки", action: "join",
			args: []string{"FIXTURE_PROXY=p.example", "FIXTURE_SECRET=-"}, stdin: "\n",
			wantExit: 64, wantCode: "usage.stdin_empty"},
		{name: "обязательный пропущен", action: "join", args: []string{"FIXTURE_SECRET=-"}, stdin: "s",
			wantExit: 64, wantCode: "usage.param_required"},
		{name: "обязательный пустой", action: "join", args: []string{"FIXTURE_PROXY=", "FIXTURE_SECRET=-"},
			stdin: "s", wantExit: 64, wantCode: "usage.param_required"},
		{name: "неизвестный параметр", action: "join",
			args: []string{"FIXTURE_PROXY=p.example", "FIXTURE_SECRET=-", "OTHER=1"}, stdin: "s",
			wantExit: 64, wantCode: "usage.unknown_param"},
		{name: "не пара", action: "join", args: []string{"FIXTURE_PROXY"}, wantExit: 64, wantCode: "usage.assignment"},
		{name: "пустое имя", action: "join", args: []string{"=x"}, wantExit: 64, wantCode: "usage.assignment"},
		{name: "дважды", action: "join", args: []string{"FIXTURE_PROXY=a.example", "FIXTURE_PROXY=b.example"},
			wantExit: 64, wantCode: "usage.duplicate_key"},
		{name: "несекретный со stdin", action: "join", args: []string{"FIXTURE_PROXY=-", "FIXTURE_SECRET=-"},
			stdin: "s", wantExit: 64, wantCode: "usage.stdin_param"},
		{name: "неверный тип", action: "join", args: []string{"FIXTURE_PROXY=bad host", "FIXTURE_SECRET=-"},
			stdin: "s", wantExit: 65, wantCode: "config.invalid"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := actionFixture(t, "action-json", tc.action)
			stdin := &countingReader{r: strings.NewReader(tc.stdin)}
			in, err := actionParseArgs(a, tc.args, stdin)
			if tc.wantExit != 0 {
				wantCode(t, err, tc.wantExit, tc.wantCode)
				if tc.wantCode != "usage.stdin_empty" && stdin.n > 0 {
					t.Errorf("stdin прочитан (%d байт) при отказе в разборе", stdin.n)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(in.argv, tc.wantArgv) {
				t.Errorf("argv = %q, ждали %q", in.argv, tc.wantArgv)
			}
			if len(in.values) != len(tc.wantValues) {
				t.Errorf("values = %v, ждали %v", in.values, tc.wantValues)
			}
			for k, v := range tc.wantValues {
				if in.values[k] != v {
					t.Errorf("values[%s] = %q, ждали %q", k, in.values[k], v)
				}
			}
			if in.secret != tc.wantSecret || in.hasSecret != (tc.wantSecret != "") {
				t.Errorf("secret = %q/%v, ждали %q", in.secret, in.hasSecret, tc.wantSecret)
			}
			if tc.wantSecret == "" && stdin.n > 0 {
				t.Errorf("stdin прочитан без KEY=-")
			}
		})
	}
}

// countingReader считает прочитанные байты.
type countingReader struct {
	r io.Reader
	n int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

// Второй рубеж A10: даже если манифест пропустил параметр с занятым именем
// (валидация манифеста — первый), в окружение действия он не попадает.
func TestActionEnvValuesDenied(t *testing.T) {
	t.Parallel()
	pairs := map[string]string{"PROXY": "p:1"}
	for _, k := range []string{"PATH", "LANG", "HOME", "PWD", "LD_PRELOAD", "LD_LIBRARY_PATH", "BASH_ENV", "ENV", "IFS", "CREAM_EXT"} {
		pairs[k] = "evil"
	}
	got := actionEnvValues(map[string]string{"PROXY": "old", "LEVEL": "low"}, pairs)
	want := map[string]string{"PROXY": "p:1", "LEVEL": "low"}
	if !maps.Equal(got, want) {
		t.Errorf("окружение действия: %v, ждали %v", got, want)
	}
}

// TestActionReadSecretError — сбой чтения stdin — 74 io.stdin (Р29).
func TestActionReadSecretError(t *testing.T) {
	t.Parallel()
	_, err := actionReadSecret(iotest.ErrReader(errors.New("EIO")), "FIXTURE_SECRET")
	if cerr.ExitCode(err) != cerr.ExitState || cerr.CodeOf(err) != "io.stdin" {
		t.Fatalf("actionReadSecret = %v (%d %s), ждали 74 io.stdin", err, cerr.ExitCode(err), cerr.CodeOf(err))
	}
}
