package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/iamletenkov/cream/internal/cerr"
)

type textResult struct {
	Value int `json:"value"`
}

func (r textResult) WriteText(w io.Writer) error {
	_, err := fmt.Fprintf(w, "value %d\n", r.Value)
	return err
}

type exitResult struct {
	Code int `json:"exit_code"`
}

func (r exitResult) ExitCode() int { return r.Code }

// fakeCommands — команды для проверки диспетчера без настоящих групп.
func fakeCommands() []Command {
	return []Command{
		{Name: "ok", Usage: "ok [--flag] ARG…", Flags: func(fs *flag.FlagSet) { fs.Bool("flag", false, "") },
			Run: func(ctx *Context, args []string) (any, error) {
				_, _ = fmt.Fprintf(ctx.Out, "progress %s\n", strings.Join(args, ","))
				return textResult{Value: len(args)}, nil
			}},
		{Name: "fail", Usage: "fail", Run: func(*Context, []string) (any, error) {
			return nil, cerr.New(cerr.ExitGraph, "deps.missing", "нет зависимости %s", "docker")
		}},
		{Name: "plain", Usage: "plain", Run: func(*Context, []string) (any, error) {
			return nil, errors.New("boom")
		}},
		{Name: "panic", Usage: "panic", Run: func(*Context, []string) (any, error) {
			panic("invariant")
		}},
		{Name: "exit3", Usage: "exit3", Run: func(*Context, []string) (any, error) {
			return exitResult{Code: 3}, nil
		}},
		{Name: "warn", Usage: "warn", Run: func(ctx *Context, _ []string) (any, error) {
			ctx.Warn.Add("source.shadowed", "источник %s перекрыт", "a")
			return map[string]bool{"changed": true}, nil
		}},
		{Name: "layout", Usage: "layout", Run: func(ctx *Context, _ []string) (any, error) {
			return map[string]any{"root": ctx.Layout.Root, "sources": ctx.Layout.Sources(),
				"lock_timeout": ctx.Globals.LockTimeout.String(), "quiet": ctx.Globals.Quiet}, nil
		}},
		{Name: "res", Usage: "res [--with-deps]", Flags: func(fs *flag.FlagSet) { reserveFlags(fs, "with-deps") },
			Run: func(*Context, []string) (any, error) { return nil, nil }},
		{Name: "leak", Usage: "leak", Run: func(ctx *Context, _ []string) (any, error) {
			_, _ = io.WriteString(ctx.App.Stdout, "stray text\n")
			return nil, nil
		}},
		{Name: "act", Usage: "act <ext> <action> [args…]", PermuteUntil: 2,
			Run: func(_ *Context, args []string) (any, error) { return args, nil }},
	}
}

type runOut struct {
	code           int
	stdout, stderr string
}

func runWith(t *testing.T, env map[string]string, args ...string) runOut {
	t.Helper()
	var stdout, stderr bytes.Buffer
	app := App{
		Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr,
		Env:  func(k string) string { return env[k] },
		Euid: func() int { return 0 },
		Now:  func() time.Time { return time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC) },
	}
	code := run(args, app, fakeCommands())
	return runOut{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

// decodeOne требует, чтобы stdout был ровно одним JSON-объектом.
func decodeOne(t *testing.T, s string) map[string]any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(s))
	var obj map[string]any
	if err := dec.Decode(&obj); err != nil {
		t.Fatalf("stdout is not a JSON object: %v\n%s", err, s)
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		t.Fatalf("stdout has more than one JSON value: %s", s)
	}
	return obj
}

func errCode(t *testing.T, obj map[string]any) string {
	t.Helper()
	e, ok := obj["error"].(map[string]any)
	if !ok {
		t.Fatalf("error is not an object: %v", obj["error"])
	}
	s, _ := e["code"].(string)
	return s
}

func TestRunExitCodesAndErrorCodes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		args     []string
		wantExit int
		wantCode string // error.code в JSON; "" — ok=true
	}{
		{name: "success", args: []string{"ok", "a"}, wantExit: 0},
		{name: "unknown command", args: []string{"frobnicate"}, wantExit: 64, wantCode: "usage.unknown_command"},
		{name: "no command", args: nil, wantExit: 64, wantCode: "usage.no_command"},
		{name: "deferred command search", args: []string{"search"}, wantExit: 64, wantCode: "usage.reserved"},
		{name: "deferred command lineage", args: []string{"lineage"}, wantExit: 64, wantCode: "usage.reserved"},
		{name: "deferred command path", args: []string{"path", "x"}, wantExit: 64, wantCode: "usage.reserved"},
		{name: "deferred command stage", args: []string{"stage", "x"}, wantExit: 64, wantCode: "usage.reserved"},
		{name: "cerr keeps its exit", args: []string{"fail"}, wantExit: 67, wantCode: "deps.missing"},
		{name: "plain error is internal", args: []string{"plain"}, wantExit: 70, wantCode: "internal"},
		{name: "panic is internal", args: []string{"panic"}, wantExit: 70, wantCode: "internal"},
		{name: "reserved flag", args: []string{"res", "--with-deps"}, wantExit: 64, wantCode: "usage.reserved"},
		{name: "reserved command flag unset is fine", args: []string{"res"}, wantExit: 0},
		{name: "unknown flag", args: []string{"ok", "--nope"}, wantExit: 64, wantCode: "usage.flag"},
		{name: "unknown global flag", args: []string{"--nope", "ok"}, wantExit: 64, wantCode: "usage.flag"},
		{name: "flag without value", args: []string{"ok", "--root"}, wantExit: 64, wantCode: "usage.flag"},
		{name: "relative root", args: []string{"--root", "rel", "layout"}, wantExit: 64, wantCode: "usage.root"},
		{name: "negative lock timeout", args: []string{"layout", "--lock-timeout", "-1s"}, wantExit: 64, wantCode: "usage.flag"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Текстовый режим: код выхода тот же, ошибка — в stderr, stdout пуст.
			text := runWith(t, nil, tc.args...)
			if text.code != tc.wantExit {
				t.Errorf("text: exit = %d, want %d (stderr %q)", text.code, tc.wantExit, text.stderr)
			}
			if tc.wantCode != "" && text.stdout != "" {
				t.Errorf("text: stdout = %q, want empty on error", text.stdout)
			}
			if tc.wantCode != "" && !strings.Contains(text.stderr, "cream: ") {
				t.Errorf("text: stderr = %q, want an error line", text.stderr)
			}
			// Режим --json: ровно один объект, код в error.code.
			js := runWith(t, nil, append([]string{"--json"}, tc.args...)...)
			if js.code != tc.wantExit {
				t.Errorf("json: exit = %d, want %d", js.code, tc.wantExit)
			}
			obj := decodeOne(t, js.stdout)
			if tc.wantCode == "" {
				if obj["ok"] != true || obj["error"] != nil {
					t.Errorf("json: ok=%v error=%v, want ok=true error=null", obj["ok"], obj["error"])
				}
				return
			}
			if obj["ok"] != false {
				t.Errorf("json: ok = %v, want false", obj["ok"])
			}
			if obj["result"] != nil {
				t.Errorf("json: result = %v, want null on error", obj["result"])
			}
			if got := errCode(t, obj); got != tc.wantCode {
				t.Errorf("json: error.code = %q, want %q", got, tc.wantCode)
			}
		})
	}
}

func TestRunJSONEnvelope(t *testing.T) {
	t.Parallel()
	// --json после позиционных тоже глобальный флаг (перестановка §11.1).
	out := runWith(t, nil, "ok", "a", "b", "--json")
	if out.code != 0 {
		t.Fatalf("exit = %d, stderr %q", out.code, out.stderr)
	}
	obj := decodeOne(t, out.stdout)
	if obj["cream"] != "dev" || obj["command"] != "ok" || obj["ok"] != true {
		t.Errorf("envelope header = cream:%v command:%v ok:%v", obj["cream"], obj["command"], obj["ok"])
	}
	if r, _ := obj["result"].(map[string]any); r["value"] != float64(2) {
		t.Errorf("result = %v, want {value:2}", obj["result"])
	}
	if w, ok := obj["warnings"].([]any); !ok || len(w) != 0 {
		t.Errorf("warnings = %v, want []", obj["warnings"])
	}
	// Человеческий вывод команды при --json уходит в stderr.
	if !strings.Contains(out.stderr, "progress a,b") {
		t.Errorf("stderr = %q, want progress text", out.stderr)
	}
	if strings.Contains(out.stdout, "progress") || strings.Contains(out.stdout, "value 2") {
		t.Errorf("stdout has human text: %q", out.stdout)
	}
}

func TestRunJSONStdoutIsEnvelopeOnly(t *testing.T) {
	t.Parallel()
	js := runWith(t, nil, "--json", "leak")
	decodeOne(t, js.stdout)
	if !strings.Contains(js.stderr, "stray text") {
		t.Errorf("stderr = %q, want the stray text", js.stderr)
	}
	// Ошибка разбора до команды с --json=1 — всё равно конверт.
	bad := runWith(t, nil, "--json=1", "--nope", "ok")
	if bad.code != 64 || errCode(t, decodeOne(t, bad.stdout)) != "usage.flag" {
		t.Errorf("--json=1 --nope: code=%d stdout=%q", bad.code, bad.stdout)
	}
}

func TestRunTextMode(t *testing.T) {
	t.Parallel()
	out := runWith(t, nil, "ok", "a")
	if out.code != 0 || out.stdout != "progress a\nvalue 1\n" || out.stderr != "" {
		t.Errorf("got code=%d stdout=%q stderr=%q", out.code, out.stdout, out.stderr)
	}
}

func TestRunWarnings(t *testing.T) {
	t.Parallel()
	js := runWith(t, nil, "--json", "warn")
	obj := decodeOne(t, js.stdout)
	w, _ := obj["warnings"].([]any)
	if len(w) != 1 || w[0].(map[string]any)["code"] != "source.shadowed" {
		t.Errorf("warnings = %v", obj["warnings"])
	}
	text := runWith(t, nil, "warn")
	if !strings.Contains(text.stderr, "источник a перекрыт") {
		t.Errorf("text stderr = %q, want warning", text.stderr)
	}
	quiet := runWith(t, nil, "-q", "warn")
	if quiet.stderr != "" {
		t.Errorf("-q stderr = %q, want empty", quiet.stderr)
	}
}

func TestRunExitCoder(t *testing.T) {
	t.Parallel()
	js := runWith(t, nil, "--json", "exit3")
	if js.code != 3 {
		t.Errorf("exit = %d, want 3", js.code)
	}
	obj := decodeOne(t, js.stdout)
	if obj["ok"] != false || obj["error"] != nil {
		t.Errorf("ok=%v error=%v, want ok=false error=null", obj["ok"], obj["error"])
	}
}

func TestRunPermuteUntil(t *testing.T) {
	t.Parallel()
	js := runWith(t, nil, "act", "--json", "ext", "do", "--json", "-x")
	if js.code != 0 {
		t.Fatalf("exit = %d, stderr %q", js.code, js.stderr)
	}
	obj := decodeOne(t, js.stdout)
	got, _ := json.Marshal(obj["result"])
	if string(got) != `["ext","do","--json","-x"]` {
		t.Errorf("result = %s", got)
	}
}

func TestRunLayoutAndGlobals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		env         map[string]string
		args        []string
		wantRoot    string
		wantSources string
		wantTimeout string
	}{
		{name: "defaults", args: []string{"layout"}, wantRoot: "/", wantSources: "/etc/cream/sources.yaml", wantTimeout: "2m0s"},
		{name: "--root before command", args: []string{"--root", "/tmp/r", "layout"},
			wantRoot: "/tmp/r", wantSources: "/tmp/r/etc/cream/sources.yaml", wantTimeout: "2m0s"},
		{name: "--root after command", args: []string{"layout", "--root=/tmp/r", "--lock-timeout", "5s"},
			wantRoot: "/tmp/r", wantSources: "/tmp/r/etc/cream/sources.yaml", wantTimeout: "5s"},
		{name: "CREAM_ROOT and CREAM_SOURCES", env: map[string]string{"CREAM_ROOT": "/tmp/e", "CREAM_SOURCES": "/s.yaml"},
			args: []string{"layout"}, wantRoot: "/tmp/e", wantSources: "/s.yaml", wantTimeout: "2m0s"},
		{name: "--sources beats env", env: map[string]string{"CREAM_SOURCES": "/s.yaml"},
			args: []string{"layout", "--sources", "/f.yaml"}, wantRoot: "/", wantSources: "/f.yaml", wantTimeout: "2m0s"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := runWith(t, tc.env, append([]string{"--json"}, tc.args...)...)
			if out.code != 0 {
				t.Fatalf("exit = %d, stderr %q", out.code, out.stderr)
			}
			r, _ := decodeOne(t, out.stdout)["result"].(map[string]any)
			if r["root"] != tc.wantRoot || r["sources"] != tc.wantSources || r["lock_timeout"] != tc.wantTimeout {
				t.Errorf("result = %v", r)
			}
		})
	}
}

func TestRunHelp(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"help"}, {"--help"}, {"-h"}} {
		out := runWith(t, nil, args...)
		if out.code != 0 || !strings.Contains(out.stdout, "cream <команда>") || !strings.Contains(out.stdout, "ok [--flag]") {
			t.Errorf("%q: code=%d stdout=%q", args, out.code, out.stdout)
		}
	}
	for _, args := range [][]string{{"help", "ok"}, {"ok", "-h"}, {"ok", "--help"}} {
		out := runWith(t, nil, args...)
		if out.code != 0 || !strings.Contains(out.stdout, "ok [--flag] ARG…") || strings.Contains(out.stdout, "fail") {
			t.Errorf("%q: code=%d stdout=%q", args, out.code, out.stdout)
		}
	}
	if out := runWith(t, nil, "help", "nope"); out.code != 64 {
		t.Errorf("help nope: code=%d", out.code)
	}
	if out := runWith(t, nil); out.code != 64 || !strings.Contains(out.stderr, "cream <команда>") {
		t.Errorf("no args: code=%d stderr=%q", out.code, out.stderr)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("closed pipe") }

func TestRunStdoutWriteError(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"ok"}, {"--json", "ok"}} {
		var stderr bytes.Buffer
		app := App{Stdin: strings.NewReader(""), Stdout: failingWriter{}, Stderr: &stderr,
			Env: func(string) string { return "" }, Euid: func() int { return 0 }, Now: time.Now}
		if code := run(args, app, fakeCommands()); code != cerr.ExitState {
			t.Errorf("%q: exit = %d, want %d", args, code, cerr.ExitState)
		}
	}
}

func TestRealCommands(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for _, c := range (App{}).commands() {
		if c.Name == "" || c.Run == nil {
			t.Errorf("command %+v: empty name or Run", c)
		}
		if seen[c.Name] {
			t.Errorf("duplicate command %q", c.Name)
		}
		if isReservedCommand(c.Name) {
			t.Errorf("command %q is also reserved", c.Name)
		}
		seen[c.Name] = true
	}
	if !seen["version"] {
		t.Errorf("version command is missing")
	}
	var stdout, stderr bytes.Buffer
	app := App{Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr,
		Env: func(string) string { return "" }, Euid: func() int { return 0 }, Now: time.Now}
	if code := Run([]string{"version"}, app); code != 0 || stdout.String() != "cream dev\n" {
		t.Errorf("Run(version) = %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	if code := Run([]string{"--version"}, app); code != 0 || stdout.String() != "cream dev\n" {
		t.Errorf("Run(--version) = %d, stdout %q", code, stdout.String())
	}
	if code := Run([]string{"version", "extra"}, app); code != cerr.ExitUsage {
		t.Errorf("Run(version extra) = %d, want 64", code)
	}
}

func TestErrorLineOneLineWithCode(t *testing.T) {
	t.Parallel()
	err := cerr.New(cerr.ExitData, "hook.failed", "хук упал:\nстрока 1\r\nстрока 2")
	if got, want := errorLine(err), "хук упал: строка 1 строка 2 (hook.failed)"; got != want {
		t.Errorf("errorLine = %q, ждали %q", got, want)
	}
}
