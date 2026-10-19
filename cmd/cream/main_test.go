package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

// main — только umask и cli.Run; здесь проверяется, что run собирает App и
// отдаёт код выхода диспетчера. Сам диспетчер покрыт internal/cli.
func TestRun(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{name: "version", args: []string{"version"}, wantCode: 0, wantStdout: "cream dev\n"},
		{name: "version json", args: []string{"version", "--json"}, wantCode: 0, wantStdout: `"contract": "cream/v1"`},
		{name: "no args", args: nil, wantCode: 64, wantStderr: "cream <команда>"},
		{name: "unknown command", args: []string{"frobnicate"}, wantCode: 64, wantStderr: `"frobnicate"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			env := func(string) string { return "" }
			code := run(context.Background(), tc.args, strings.NewReader(""), &stdout, &stderr, env)
			if code != tc.wantCode {
				t.Errorf("exit code = %d, want %d", code, tc.wantCode)
			}
			if !strings.Contains(stdout.String(), tc.wantStdout) {
				t.Errorf("stdout = %q, want it to contain %q", stdout.String(), tc.wantStdout)
			}
			if !strings.Contains(stderr.String(), tc.wantStderr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tc.wantStderr)
			}
		})
	}
}

// signalHelperEnv включает в дочернем процессе теста режим помощника
// TestSignalContextSecondInterruptKills.
const signalHelperEnv = "CREAM_TEST_SIGNAL_HELPER"

// TestSignalHelper — дочерний процесс: ждёт первого сигнала через
// signalContext, сообщает о нём и «зависает» (как cream, ждущий хук).
func TestSignalHelper(t *testing.T) {
	if os.Getenv(signalHelperEnv) != "1" {
		t.Skip("только как дочерний процесс")
	}
	ctx, stop := signalContext(context.Background())
	defer stop()
	fmt.Println("ready")
	<-ctx.Done()
	fmt.Println("canceled")
	time.Sleep(30 * time.Second)
}

// TestSignalContextSecondInterruptKills — первый SIGINT отменяет контекст, после
// чего перехват снят: второй SIGINT убивает процесс сразу.
func TestSignalContextSecondInterruptKills(t *testing.T) {
	t.Parallel()
	cmd := exec.Command(os.Args[0], "-test.run=^TestSignalHelper$", "-test.count=1")
	cmd.Env = append(os.Environ(), signalHelperEnv+"=1")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	lines := bufio.NewScanner(out)
	expect := func(want string) {
		t.Helper()
		for lines.Scan() {
			if lines.Text() == want {
				return
			}
		}
		t.Fatalf("помощник не напечатал %q: %v", want, lines.Err())
	}
	expect("ready")
	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	expect("canceled")
	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatalf("помощник завершился без ошибки: %v", err)
		}
		ws, ok := ee.Sys().(syscall.WaitStatus)
		if !ok || !ws.Signaled() || ws.Signal() != syscall.SIGINT {
			t.Fatalf("помощник не убит SIGINT: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("второй SIGINT не убил процесс")
	}
}
