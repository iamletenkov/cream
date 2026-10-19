package systemd

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/iamletenkov/cream/internal/paths"
)

type call struct {
	name string
	args []string
}

// recorder is a fake ExecFunc: it records argv and answers with out/code/err.
type recorder struct {
	calls []call
	out   string
	code  int
	err   error
}

func (r *recorder) run(_ context.Context, name string, args ...string) ([]byte, int, error) {
	r.calls = append(r.calls, call{name, args})
	return []byte(r.out), r.code, r.err
}

func bootedLayout(t *testing.T) paths.Layout {
	t.Helper()
	l := paths.Layout{Root: t.TempDir()}
	if err := os.MkdirAll(l.SystemdRunning(), 0o755); err != nil {
		t.Fatal(err)
	}
	return l
}

func TestRunning(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		out       string
		code      int
		err       error
		wantState string
		wantOK    bool
	}{
		{"running", "running\n", 0, nil, "running", true},
		{"degraded", "degraded\n", 1, nil, "degraded", true},
		{"starting", "starting\n", 1, nil, "starting", true},
		{"initializing", "initializing\n", 1, nil, "initializing", true},
		{"maintenance", "maintenance\n", 1, nil, "maintenance", false},
		{"stopping", "stopping\n", 1, nil, "stopping", false},
		{"offline", "offline\n", 1, nil, "offline", false},
		{"garbage", "what\never\n", 1, nil, "", false},
		{"exec failed", "", -1, errors.New("no systemctl"), "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := &recorder{out: tc.out, code: tc.code, err: tc.err}
			state, ok := bootedRealRoot(r).Running(context.Background())
			if state != tc.wantState || ok != tc.wantOK {
				t.Errorf("Running = %q, %v; want %q, %v", state, ok, tc.wantState, tc.wantOK)
			}
			want := []call{{"systemctl", []string{"is-system-running"}}}
			if !reflect.DeepEqual(r.calls, want) {
				t.Errorf("calls = %v", r.calls)
			}
		})
	}
}

// bootedRealRoot is a Systemctl on Root "/" that sees /run/systemd/system.
func bootedRealRoot(r *recorder) Systemctl {
	return &systemctl{l: paths.Layout{Root: "/"}, run: r.run, isDir: func(p string) bool { return p == "/run/systemd/system" }}
}

// Р9: under another --root the live systemd of the host is not the target's:
// Running is offline without asking systemctl, even if the tree has
// run/systemd/system.
func TestRunningOtherRoot(t *testing.T) {
	t.Parallel()
	r := &recorder{out: "running\n"}
	state, ok := NewSystemctl(bootedLayout(t), r.run).Running(context.Background())
	if ok || state != StateOffline || len(r.calls) != 0 {
		t.Errorf("Running = %q, %v, calls %v", state, ok, r.calls)
	}
}

// Without /run/systemd/system under the root (build phase, container without
// systemd) systemctl is not called at all.
func TestRunningNoSystemd(t *testing.T) {
	t.Parallel()
	r := &recorder{out: "running\n"}
	state, ok := NewSystemctl(paths.Layout{Root: t.TempDir()}, r.run).Running(context.Background())
	if ok || state != "offline" || len(r.calls) != 0 {
		t.Errorf("Running = %q, %v, calls %v", state, ok, r.calls)
	}
}

func TestDaemonReloadAndRestart(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		do      func(Systemctl) error
		code    int
		err     error
		want    []string
		wantErr bool
	}{
		{"reload", func(s Systemctl) error { return s.DaemonReload(context.Background()) }, 0, nil,
			[]string{"daemon-reload"}, false},
		{"restart no-block", func(s Systemctl) error {
			return s.Restart(context.Background(), "cream-configure@docker.service", true)
		}, 0, nil, []string{"--no-block", "restart", "cream-configure@docker.service"}, false},
		{"restart wait", func(s Systemctl) error {
			return s.Restart(context.Background(), "cream-configure@docker.service", false)
		}, 0, nil, []string{"restart", "cream-configure@docker.service"}, false},
		{"restart fails", func(s Systemctl) error {
			return s.Restart(context.Background(), "cream-configure@docker.service", false)
		}, 1, nil, []string{"restart", "cream-configure@docker.service"}, true},
		{"stop", func(s Systemctl) error {
			return s.Stop(context.Background(), "cream-configure@docker.service")
		}, 0, nil, []string{"stop", "cream-configure@docker.service"}, false},
		{"stop fails", func(s Systemctl) error {
			return s.Stop(context.Background(), "cream-configure@docker.service")
		}, 5, nil, []string{"stop", "cream-configure@docker.service"}, true},
		{"exec fails", func(s Systemctl) error { return s.DaemonReload(context.Background()) }, -1,
			errors.New("boom"), []string{"daemon-reload"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := &recorder{code: tc.code, err: tc.err}
			err := tc.do(NewSystemctl(bootedLayout(t), r.run))
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v", err)
			}
			want := []call{{"systemctl", tc.want}}
			if !reflect.DeepEqual(r.calls, want) {
				t.Errorf("calls = %v, want %v", r.calls, want)
			}
		})
	}
}

func TestFake(t *testing.T) {
	t.Parallel()
	f := &Fake{State: "starting", OK: true}
	ctx := context.Background()
	if s, ok := f.Running(ctx); s != "starting" || !ok {
		t.Errorf("Running = %q %v", s, ok)
	}
	_ = f.DaemonReload(ctx)
	_ = f.Restart(ctx, "cream-configure@a.service", true)
	_ = f.Restart(ctx, "cream-configure@b.service", false)
	_ = f.Stop(ctx, "cream-configure@c.service")
	want := "is-system-running|daemon-reload|restart --no-block cream-configure@a.service|restart cream-configure@b.service|stop cream-configure@c.service"
	if got := strings.Join(f.Calls(), "|"); got != want {
		t.Errorf("calls = %q", got)
	}
	f.RestartErr = errors.New("x")
	if err := f.Restart(ctx, "u", true); err == nil {
		t.Error("RestartErr ignored")
	}
	f.StopErr = errors.New("y")
	if err := f.Stop(ctx, "u"); err == nil {
		t.Error("StopErr ignored")
	}
}
