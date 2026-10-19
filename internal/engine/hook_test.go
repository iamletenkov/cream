package engine

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/config"
	"github.com/iamletenkov/cream/internal/manifest"
	"github.com/iamletenkov/cream/internal/runner"
)

func TestRunHookEnvironment(t *testing.T) {
	t.Parallel()
	te := newTestEngine(t)
	in := seedInstalled(t, te, fixtureDir(t, "with-config"))
	writeHook(t, te, "with-config", "install", `env | sort > "$CREAM_STATE_DIR/env"; pwd > "$CREAM_STATE_DIR/pwd"`)
	if _, err := config.Open(te.L, "with-config", in.Manifest.Config).Set(
		[]config.Change{{Key: "COUNT", Value: "7"}, {Key: "TOKEN", Value: "s3cr3t-value"}}, config.SetOpts{}); err != nil {
		t.Fatal(err)
	}
	out, err := te.RunHook(context.Background(), in, manifest.HookInstall, runner.EnvInput{Reason: ReasonInstall})
	if err != nil || out != runner.OK {
		t.Fatalf("RunHook = %v, %v", out, err)
	}
	env := readState(t, te, "with-config", "env")
	for _, want := range []string{
		"CREAM_PHASE=live", "CREAM_EXT=with-config", "CREAM_EXT_VERSION=1.0.0",
		"CREAM_EXT_DIR=" + te.L.ExtTree("with-config"), "CREAM_ROOT=" + te.Root,
		"CREAM_REASON=install", "COUNT=7", "MODE=fast", "LANG=C.UTF-8",
	} {
		if !strings.Contains(env, want+"\n") {
			t.Errorf("в окружении нет %q:\n%s", want, env)
		}
	}
	if strings.Contains(env, "s3cr3t-value") || strings.Contains(env, "TOKEN=") {
		t.Errorf("секрет попал в окружение хука:\n%s", env)
	}
	if strings.Contains(env, "CREAM_LOCK_FD") {
		t.Errorf("CREAM_LOCK_FD без WithLock:\n%s", env)
	}
	if got := strings.TrimSpace(readState(t, te, "with-config", "pwd")); got != te.L.ExtTree("with-config") {
		t.Errorf("cwd = %q", got)
	}
}

func TestRunHookOutcomes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		body     string
		want     runner.Outcome
		wantExit int
		wantCode string
		reboot   bool
	}{
		{name: "ok", body: "exit 0", want: runner.OK},
		{name: "reboot", body: "exit 10", want: runner.Reboot, reboot: true},
		{name: "not-applicable", body: "exit 20", want: runner.NotApplicable, wantExit: cerr.ExitNotApplicable, wantCode: "hook.not_applicable"},
		{name: "failed", body: "exit 3", want: runner.Failed, wantExit: cerr.ExitHook, wantCode: "hook.failed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			te := newTestEngine(t)
			in := seedInstalled(t, te, fixtureDir(t, "noop"))
			writeHook(t, te, "noop", "install", tc.body)
			out, err := te.RunHook(context.Background(), in, manifest.HookInstall, runner.EnvInput{})
			if out != tc.want {
				t.Errorf("outcome = %v, want %v", out, tc.want)
			}
			if tc.wantExit == 0 && err != nil {
				t.Fatalf("err = %v", err)
			}
			if tc.wantExit != 0 {
				wantCode(t, err, tc.wantExit, tc.wantCode)
			}
			if tc.name == "failed" {
				if got := cerr.DetailsOf(err)["hook_exit"]; got != 3 {
					t.Errorf("details.hook_exit = %v, want 3", got)
				}
			}
			_, statErr := os.Stat(te.L.RebootRequired())
			if (statErr == nil) != tc.reboot {
				t.Errorf("/run/reboot-required есть = %v, want %v", statErr == nil, tc.reboot)
			}
			rec, _, _ := te.Store.Get("noop")
			if rec.RebootRequired != tc.reboot {
				t.Errorf("реестр reboot_required = %v, want %v", rec.RebootRequired, tc.reboot)
			}
		})
	}
}

func TestRunHookRebootInBuildPhaseNoRunFile(t *testing.T) {
	t.Parallel()
	te := newTestEngine(t)
	in := seedInstalled(t, te, fixtureDir(t, "noop"))
	writeHook(t, te, "noop", "install", "exit 10")
	out, err := te.RunHook(context.Background(), in, manifest.HookInstall, runner.EnvInput{Phase: PhaseBuild})
	if err != nil || out != runner.Reboot {
		t.Fatalf("RunHook = %v, %v", out, err)
	}
	if _, err := os.Stat(te.L.RebootRequired()); err == nil {
		t.Error("/run/reboot-required создан в фазе build")
	}
}

func TestRunHookFiltersSecrets(t *testing.T) {
	t.Parallel()
	te := newTestEngine(t)
	in := seedInstalled(t, te, fixtureDir(t, "with-config"))
	if _, err := config.Open(te.L, "with-config", in.Manifest.Config).Set(
		[]config.Change{{Key: "TOKEN", Value: "tok-SECRET-42"}}, config.SetOpts{}); err != nil {
		t.Fatal(err)
	}
	writeHook(t, te, "with-config", "install",
		`cat "$CREAM_SECRETS_FILE"; echo "err: $(cat "$CREAM_SECRETS_FILE")" >&2`)
	if _, err := te.RunHook(context.Background(), in, manifest.HookInstall, runner.EnvInput{}); err != nil {
		t.Fatal(err)
	}
	got := te.Err.String()
	if strings.Contains(got, "tok-SECRET-42") {
		t.Errorf("секрет в выводе хука:\n%s", got)
	}
	if strings.Count(got, "***") < 2 {
		t.Errorf("нет маски *** в выводе:\n%s", got)
	}
	if te.Out.String() != "" {
		t.Errorf("вывод хука попал в stdout: %q", te.Out.String())
	}
}

func TestRunHookTimeout(t *testing.T) {
	t.Parallel()
	te := newTestEngine(t)
	in := seedInstalled(t, te, fixtureDir(t, "noop"))
	writeHook(t, te, "noop", "install", "sleep 5")
	h := in.Manifest.Hooks[manifest.HookInstall]
	h.Timeout = 100 * time.Millisecond
	in.Manifest.Hooks[manifest.HookInstall] = h
	out, err := te.RunHook(context.Background(), in, manifest.HookInstall, runner.EnvInput{})
	if out != runner.Failed {
		t.Errorf("outcome = %v", out)
	}
	wantCode(t, err, cerr.ExitHook, "hook.failed")
	if cerr.DetailsOf(err)["timed_out"] != true {
		t.Errorf("details = %v", cerr.DetailsOf(err))
	}
}

func TestRunHookUndeclared(t *testing.T) {
	t.Parallel()
	te := newTestEngine(t)
	in := seedInstalled(t, te, fixtureDir(t, "noop"))
	_, err := te.RunHook(context.Background(), in, manifest.HookApply, runner.EnvInput{})
	wantCode(t, err, cerr.ExitInternal, cerr.CodeInternal)
}
