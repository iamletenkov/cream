package engine

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/state"
)

func TestConfigureFromUnitReason(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		marker string // "" — маркера нет
		want   string
	}{
		{name: "boot", want: ReasonBoot},
		{name: "apply", marker: "apply\n", want: ReasonApply},
		{name: "install", marker: "install", want: ReasonInstall},
		{name: "garbage", marker: "rm -rf /", want: ReasonBoot},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			te := newTestEngine(t)
			te.systemdRunning("running")
			seedInstalled(t, te, fixtureDir(t, "configure-system"))
			if tc.marker != "" {
				writeFile(t, te, "/run/cream/configure-system.reason", tc.marker, 0o644)
			}
			res, err := te.ConfigureReport(context.Background(), "configure-system", "", ConfigureOptions{FromUnit: true})
			if err != nil {
				t.Fatal(err)
			}
			if res.Mode != ApplyHook || res.Reason != tc.want {
				t.Errorf("res = %+v", res)
			}
			if log := readState(t, te, "configure-system", "configure.log"); !strings.Contains(log, "reason="+tc.want+" ") {
				t.Errorf("CREAM_REASON: %q", log)
			}
			if _, err := os.Stat(te.L.RunReason("configure-system")); err == nil {
				t.Error("маркер не удалён")
			}
			if strings.TrimSpace(readState(t, te, "configure-system", "message")) != "hello" {
				t.Error("configure не получил MESSAGE")
			}
			if calls := te.Sys.Calls(); slices.Contains(calls, "restart --no-block cream-configure@configure-system.service") {
				t.Errorf("--from-unit перезапускает юнит: %v", calls)
			}
		})
	}
}

func TestConfigureFromUnitFailureGoesToHistory(t *testing.T) {
	t.Parallel()
	te := newTestEngine(t)
	seedInstalled(t, te, fixtureDir(t, "configure-system"))
	writeHook(t, te, "configure-system", "configure", "exit 4")
	err := te.Configure(context.Background(), "configure-system", "", ConfigureOptions{FromUnit: true})
	wantCode(t, err, cerr.ExitHook, "hook.failed")
	data, rerr := os.ReadFile(te.L.History())
	if rerr != nil || !strings.Contains(string(data), `"op":"configure"`) || !strings.Contains(string(data), `"status":"failed"`) {
		t.Errorf("history: %q, %v", data, rerr)
	}
	rec, _, _ := te.Store.Get("configure-system")
	if rec.Status != state.StatusInstalled {
		t.Errorf("реестр изменён: %s", rec.Status)
	}
}

func TestConfigureLive(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		state     string
		wait      bool
		wantMode  string
		wantCalls []string
		wantHook  int
		wantWarn  bool
	}{
		{name: "no-systemd-runs-hook", wantMode: ApplyHook, wantCalls: []string{"is-system-running"}, wantHook: 1},
		{name: "running-wait", state: "running", wait: true, wantMode: ApplyRestart,
			wantCalls: []string{"is-system-running", "restart cream-configure@configure-system.service"}},
		{name: "running", state: "running", wantMode: ApplyQueued,
			wantCalls: []string{"is-system-running", "restart --no-block cream-configure@configure-system.service"}},
		{name: "initializing-wait", state: "initializing", wait: true, wantMode: ApplyQueued, wantWarn: true,
			wantCalls: []string{"is-system-running", "restart --no-block cream-configure@configure-system.service"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			te := newTestEngine(t)
			if tc.state != "" {
				te.systemdRunning(tc.state)
			}
			seedInstalled(t, te, fixtureDir(t, "configure-system"))
			res, err := te.ConfigureReport(context.Background(), "configure-system", ReasonInstall, ConfigureOptions{Wait: tc.wait})
			if err != nil {
				t.Fatal(err)
			}
			if res.Mode != tc.wantMode || res.Reason != ReasonInstall {
				t.Errorf("res = %+v", res)
			}
			if got := te.Sys.Calls(); !slices.Equal(got, tc.wantCalls) {
				t.Errorf("systemctl = %v, want %v", got, tc.wantCalls)
			}
			if n := hookCount(t, te, "configure-system", "configure"); n != tc.wantHook {
				t.Errorf("configure вызван %d раз", n)
			}
			if tc.wantHook > 0 && !strings.Contains(readState(t, te, "configure-system", "configure.log"), "reason=install ") {
				t.Error("CREAM_REASON не install")
			}
			if tc.state != "" {
				marker, err := os.ReadFile(te.L.RunReason("configure-system"))
				if err != nil || strings.TrimSpace(string(marker)) != ReasonInstall {
					t.Errorf("маркер = %q, %v", marker, err)
				}
			}
			if gotWarn := slices.Contains(warningCodes(te), "configure.no_block"); gotWarn != tc.wantWarn {
				t.Errorf("предупреждения %v", warningCodes(te))
			}
		})
	}
}

func TestConfigureWithoutHook(t *testing.T) {
	t.Parallel()
	te := newTestEngine(t)
	te.systemdRunning("running")
	seedInstalled(t, te, fixtureDir(t, "noop"))
	res, err := te.ConfigureReport(context.Background(), "noop", ReasonApply, ConfigureOptions{Wait: true})
	if err != nil || res.Mode != ApplyNone {
		t.Errorf("%+v, %v", res, err)
	}
	if len(te.Sys.Calls()) != 0 {
		t.Errorf("systemctl: %v", te.Sys.Calls())
	}
}

func TestConfigureRefusals(t *testing.T) {
	t.Parallel()
	te := newTestEngine(t)
	seedInstalled(t, te, fixtureDir(t, "configure-system"))
	wantCode(t, te.Configure(context.Background(), "nope", ReasonApply, ConfigureOptions{}), 66, "not_found.extension")
	wantCode(t, te.Configure(context.Background(), "configure-system", "later", ConfigureOptions{}), 70, cerr.CodeInternal)
	wantCode(t, te.Configure(context.Background(), "configure-system", ReasonApply,
		ConfigureOptions{FromUnit: true, Wait: true}), 64, "usage.flag")
}

func TestConfigureRebootRequired(t *testing.T) {
	t.Parallel()
	te := newTestEngine(t)
	seedInstalled(t, te, fixtureDir(t, "configure-system"))
	writeHook(t, te, "configure-system", "configure", "exit 10")
	res, err := te.ConfigureReport(context.Background(), "configure-system", "", ConfigureOptions{FromUnit: true})
	if err != nil || !res.RebootRequired {
		t.Fatalf("%+v, %v", res, err)
	}
	if rec, _, _ := te.Store.Get("configure-system"); !rec.RebootRequired {
		t.Error("reboot_required не в реестре")
	}
}
