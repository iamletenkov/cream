package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/iamletenkov/cream/internal/lock"
	"github.com/iamletenkov/cream/internal/paths"
)

// TestGoldenAction — формы result `cream run` и `cream actions` (§7.2, §11.4).
// Это контракт stvor-agent: после слияния — только добавление полей.
func TestGoldenAction(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		args     []string
		stdin    string
		wantExit int
	}{
		{name: "run-json", args: []string{"run", "--json", "action-json", "status"}},
		{name: "run-join", args: []string{"--json", "run", "action-json", "join", "FIXTURE_PROXY=proxy.example:443",
			"FIXTURE_SECRET=-"}, stdin: "s3cr3t\n"},
		{name: "run-text", args: []string{"run", "--json", "action-json", "echo-args", "--json", "a"}},
		{name: "run-exit-code", args: []string{"run", "--json", "action-json", "exit-code", "3"}, wantExit: 3},
		{name: "run-protocol", args: []string{"run", "--json", "action-bad-protocol", "two-objects"}, wantExit: 76},
		{name: "run-exit-high", args: []string{"run", "--json", "action-bad-protocol", "exit-high"}, wantExit: 76},
		{name: "run-secret-in-argv", args: []string{"run", "--json", "action-json", "join",
			"FIXTURE_PROXY=proxy.example", "FIXTURE_SECRET=x"}, wantExit: 64},
		{name: "run-not-found", args: []string{"run", "--json", "action-json", "nope"}, wantExit: 66},
		{name: "run-phase", args: []string{"run", "--json", "lock-restart", "set-managed"}, wantExit: 68},
		{name: "actions", args: []string{"actions", "action-json", "--json"}},
		{name: "actions-lock-restart", args: []string{"actions", "--json", "lock-restart"}},
		{name: "actions-not-installed", args: []string{"actions", "--json", "noop"}, wantExit: 66},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			for _, ext := range []string{"action-json", "action-bad-protocol", "lock-restart"} {
				seedRoot(t, root, ext)
			}
			in := cliIn{Root: root, Stdin: tc.stdin}
			if tc.name == "run-phase" {
				in.Env = map[string]string{"CREAM_PHASE": "build"}
			}
			out := runCLI(t, in, tc.args...)
			if out.Code != tc.wantExit {
				t.Errorf("exit = %d, want %d; stderr:\n%s", out.Code, tc.wantExit, out.Stderr)
			}
			if strings.Contains(out.Stdout+out.Stderr, "s3cr3t") {
				t.Errorf("секрет в выводе:\n%s\n%s", out.Stdout, out.Stderr)
			}
			golden(t, tc.name, normRoot(out.Stdout, root))
		})
	}
}

func TestRunActionCLIText(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	seedRoot(t, root, "action-json")
	in := cliIn{Root: root}

	// Без --json stdout — ровно объект действия.
	if out := runCLI(t, in, "run", "action-json", "status"); out.Code != 0 ||
		out.Stdout != "{\"ok\":true,\"ext\":\"action-json\",\"phase\":\"live\"}\n" {
		t.Errorf("status: %+v", out)
	}
	// Флаги cream только до <ext>: --json и --lock-timeout после имени действия — его аргументы.
	if out := runCLI(t, in, "run", "action-json", "echo-args", "--json", "--lock-timeout", "1s"); out.Code != 0 ||
		out.Stdout != "--json\n--lock-timeout\n1s\n" {
		t.Errorf("echo-args: %+v", out)
	}
	// Флаг cream между <ext> и <action> — уже не флаг.
	if out := runCLI(t, in, "run", "action-json", "--json", "status"); out.Code != 66 {
		t.Errorf("флаг после <ext>: %+v", out)
	}
	// --lock-timeout до <ext> принимается.
	if out := runCLI(t, in, "run", "--lock-timeout", "1s", "action-json", "echo-args", "x"); out.Code != 0 || out.Stdout != "x\n" {
		t.Errorf("--lock-timeout: %+v", out)
	}
	// Код действия насквозь.
	if out := runCLI(t, in, "run", "action-json", "exit-code", "5"); out.Code != 5 || out.Stdout != "" {
		t.Errorf("exit-code: %+v", out)
	}
	// Не хватает аргументов.
	if out := runCLI(t, in, "run", "action-json"); out.Code != 64 {
		t.Errorf("без действия: %+v", out)
	}
	// Список действий текстом.
	out := runCLI(t, in, "actions", "action-json")
	if out.Code != 0 || !strings.Contains(out.Stdout, "join") || !strings.Contains(out.Stdout, "регистрация") {
		t.Errorf("actions: %+v", out)
	}
	// Действие с root: true на настоящем корне без root — 77 (Р9) проверить нельзя без
	// --root /, поэтому здесь только временный корень: не-root допускается.
	if out := runCLI(t, cliIn{Root: root, Euid: 1000}, "run", "action-json", "status"); out.Code != 0 {
		t.Errorf("не-root на временном корне: %+v", out)
	}
}

// pidDead сообщает, что процесса pid нет или он уже зомби (в контейнере
// осиротевших потомков может никто не пожинать).
func pidDead(pid int) bool {
	if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
		return true
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return true
	}
	_, rest, _ := strings.Cut(string(data), ") ")
	return strings.HasPrefix(rest, "Z")
}

// TestRunActionCanceled — сигнал (отмена App.Context) во время действия:
// группа процессов действия убита, cream завершается, lock/<ext> свободна.
func TestRunActionCanceled(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	seedRoot(t, root, "action-json")
	l := paths.Layout{Root: root}
	pidFile := filepath.Join(root, "child.pid")
	writeRootFile(t, root, "/opt/cream/ext/action-json/actions/echo-args",
		"#!/bin/sh\nsleep 60 &\necho $! > "+pidFile+"\nwait\n", 0o755)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout, stderr bytes.Buffer
	app := App{Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr,
		Env: func(string) string { return "" }, Euid: func() int { return 0 },
		Now: func() time.Time { return cliNow }, Context: ctx}
	done := make(chan int, 1)
	go func() { done <- Run([]string{"--root", root, "run", "action-json", "echo-args"}, app) }()

	var pid int
	for deadline := time.Now().Add(10 * time.Second); pid == 0; {
		if data, err := os.ReadFile(pidFile); err == nil && strings.HasSuffix(string(data), "\n") {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
		}
		if time.Now().After(deadline) {
			t.Fatal("действие не запустилось")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if h, err := lock.NewLocker(l, 0, nil).Ext(context.Background(), "action-json", lock.Exclusive); err == nil {
		h.Release()
		t.Error("lock/action-json свободна во время действия")
	}
	cancel()
	select {
	case code := <-done:
		if code == 0 {
			t.Errorf("отменённое действие — код 0; stderr:\n%s", stderr.String())
		}
	case <-time.After(15 * time.Second):
		t.Fatal("cream не завершился после отмены")
	}
	for deadline := time.Now().Add(5 * time.Second); !pidDead(pid); {
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("потомок действия %d жив после отмены", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
	h, err := lock.NewLocker(l, 0, nil).Ext(context.Background(), "action-json", lock.Exclusive)
	if err != nil {
		t.Fatalf("lock/action-json не освобождена: %v", err)
	}
	h.Release()
}
