package runner

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fixture returns the absolute path of a script in testdata.
func fixture(t *testing.T, name string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// lines returns the lines of out that start with prefix, without it.
func lines(out, prefix string) []string {
	var res []string
	for _, l := range strings.Split(out, "\n") {
		if v, ok := strings.CutPrefix(l, prefix); ok {
			res = append(res, v)
		}
	}
	return res
}

func TestExecCleanEnvCwdStdin(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	env := []string{"PATH=" + fixedPath, "LANG=C.UTF-8", "CREAM_EXT=demo", "KEY=value with spaces"}
	var out, errb bytes.Buffer
	res, err := Exec{}.Run(context.Background(), Spec{
		Path: fixture(t, "echo-env"), Dir: dir, Env: env, Stdout: &out, Stderr: &errb,
	})
	if err != nil {
		t.Fatalf("Run: %v (stderr %q)", err, errb.String())
	}
	if res.ExitCode != 0 || res.TimedOut {
		t.Fatalf("результат %+v, stderr %q", res, errb.String())
	}
	if got := lines(out.String(), "env:"); !slices.Equal(got, env) {
		t.Errorf("окружение процесса %q, ожидали ровно %q", got, env)
	}
	realDir, _ := filepath.EvalSymlinks(dir)
	if got := lines(out.String(), "cwd:"); !slices.Equal(got, []string{realDir}) {
		t.Errorf("cwd %q, ожидали %q", got, realDir)
	}
	if got := lines(out.String(), "stdin:"); !slices.Equal(got, []string{os.DevNull}) {
		t.Errorf("stdin %q, ожидали /dev/null", got)
	}
	if got := lines(out.String(), "fd3:"); got != nil {
		t.Errorf("fd 3 открыт без ExtraFiles: %q", got)
	}
}

func TestExecNilEnvIsEmpty(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	if _, err := (Exec{}).Run(context.Background(), Spec{Path: fixture(t, "echo-env"), Stdout: &out}); err != nil {
		t.Fatal(err)
	}
	if got := lines(out.String(), "env:"); len(got) != 0 {
		t.Fatalf("Env == nil должно давать пустое окружение, а не окружение cream: %q", got)
	}
}

func TestExecExtraFilesGiveFD3(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "lock")
	if err := os.WriteFile(p, []byte("lockfile"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var out bytes.Buffer
	_, err = Exec{}.Run(context.Background(), Spec{
		Path: fixture(t, "echo-env"), Env: BuildEnv(EnvInput{Ext: "x", LockFD: FirstExtraFD}),
		ExtraFiles: []*os.File{f}, Stdout: &out,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := lines(out.String(), "fd3:"); !slices.Equal(got, []string{"lockfile"}) {
		t.Errorf("fd 3: %q", got)
	}
	if !slices.Contains(lines(out.String(), "env:"), "CREAM_LOCK_FD=3") {
		t.Errorf("нет CREAM_LOCK_FD=3: %s", out.String())
	}
}

func TestExecExitCodes(t *testing.T) {
	t.Parallel()
	for _, code := range []int{0, 1, 10, 20, 63, 64} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			t.Parallel()
			res, err := Exec{}.Run(context.Background(), Spec{Path: fixture(t, "exit-code"), Args: []string{strconv.Itoa(code)}})
			if err != nil {
				t.Fatal(err)
			}
			if res.ExitCode != code || res.TimedOut {
				t.Fatalf("результат %+v, ожидали код %d", res, code)
			}
		})
	}
}

func TestExecStdinAndSecretFilter(t *testing.T) {
	t.Parallel()
	const secret = "hunter2-very-secret"
	var out, errb bytes.Buffer
	res, err := Exec{}.Run(context.Background(), Spec{
		Path: fixture(t, "print-secret"), Stdin: strings.NewReader(secret),
		Stdout: &out, Stderr: &errb, Secrets: []string{secret, ""},
	})
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("Run: %+v, %v", res, err)
	}
	if out.String() != "out:***\n" {
		t.Errorf("stdout %q", out.String())
	}
	if errb.String() != "err:***\n" {
		t.Errorf("stderr %q", errb.String())
	}
}

// Один и тот же приёмник для stdout и stderr: вывод не теряется и не
// перемешивается внутри строк, гонок нет (тест идёт с -race).
func TestExecSameWriterForBothStreams(t *testing.T) {
	t.Parallel()
	const secret = "s3cr3t"
	var both bytes.Buffer
	_, err := Exec{}.Run(context.Background(), Spec{
		Path: fixture(t, "print-secret"), Stdin: strings.NewReader(secret),
		Stdout: &both, Stderr: &both, Secrets: []string{secret},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := both.String()
	if strings.Contains(got, secret) || !strings.Contains(got, "out:***\n") || !strings.Contains(got, "err:***\n") {
		t.Fatalf("вывод %q", got)
	}
}

func TestExecWithoutSecretsPassesThrough(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	_, err := Exec{}.Run(context.Background(), Spec{
		Path: fixture(t, "print-secret"), Stdin: strings.NewReader("plain"), Stdout: &out,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != "out:plain\n" {
		t.Fatalf("stdout %q", out.String())
	}
}

func TestExecTimeoutKillsGroup(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		mode string
	}{
		{"SIGTERM", ""},
		{"SIGKILL после grace", "ignore-term"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pidFile := filepath.Join(t.TempDir(), "pid")
			start := time.Now()
			res, err := Exec{Grace: 200 * time.Millisecond}.Run(context.Background(), Spec{
				Path: fixture(t, "sleep"), Args: []string{pidFile, tc.mode}, Timeout: 300 * time.Millisecond,
			})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if !res.TimedOut {
				t.Fatalf("ожидали TimedOut: %+v", res)
			}
			if Classify(res.ExitCode) != Failed {
				t.Errorf("код по таймауту %d должен быть сбоем", res.ExitCode)
			}
			if elapsed := time.Since(start); elapsed > 10*time.Second {
				t.Errorf("Run ждал %v", elapsed)
			}
			data, err := os.ReadFile(pidFile)
			if err != nil {
				t.Fatalf("pid внука: %v", err)
			}
			pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil {
				t.Fatal(err)
			}
			waitGone(t, pid)
		})
	}
}

// waitGone ждёт, пока процесс исчезнет или станет зомби: в контейнере
// PID 1 — не init и сирот не пожинает.
func waitGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
		if errors.Is(err, fs.ErrNotExist) {
			return
		}
		if err == nil {
			if i := strings.LastIndexByte(string(stat), ')'); i > 0 && strings.HasPrefix(string(stat[i+1:]), " Z") {
				return
			}
		}
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("внук %d жив после таймаута", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestExecParentContextCanceled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	pidFile := filepath.Join(t.TempDir(), "pid")
	go func() {
		for {
			if _, err := os.Stat(pidFile); err == nil {
				cancel()
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	res, err := Exec{Grace: 200 * time.Millisecond}.Run(ctx, Spec{Path: fixture(t, "sleep"), Args: []string{pidFile}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ошибка %v, ожидали context.Canceled", err)
	}
	if res.TimedOut {
		t.Error("отмена родителем — не таймаут")
	}
	data, _ := os.ReadFile(pidFile)
	if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
		waitGone(t, pid)
	}
}

func TestExecStartErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	notExec := filepath.Join(dir, "plain")
	if err := os.WriteFile(notExec, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		path string
		is   error
	}{
		{"нет файла", filepath.Join(dir, "missing"), fs.ErrNotExist},
		{"нет бита исполнения", notExec, fs.ErrPermission},
		{"относительный путь", "testdata/exit-code", ErrRelativePath},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := Exec{}.Run(context.Background(), Spec{Path: tc.path, Args: []string{"0"}})
			if !errors.Is(err, tc.is) {
				t.Fatalf("ошибка %v, ожидали %v", err, tc.is)
			}
		})
	}
}

func TestExecIsRunner(t *testing.T) {
	t.Parallel()
	var _ Runner = Exec{}
	var _ Runner = Func(func(context.Context, Spec) (Result, error) { return Result{}, nil })
}
