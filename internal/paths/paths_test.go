package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestDetect(t *testing.T) {
	t.Parallel()
	home := map[string]string{"HOME": "/home/u"}
	tests := []struct {
		name                      string
		env                       map[string]string
		euid                      int
		rootFlag, srcFlag, cacheF string
		wantRoot                  string
		wantUser                  bool
		wantSources               string
		wantLock                  string
		wantCache                 string
	}{
		{
			name: "root on live machine", env: home, euid: 0,
			wantRoot: "/", wantSources: "/etc/cream/sources.yaml",
			wantLock: "/var/lib/cream/sources.lock", wantCache: "/var/cache/cream",
		},
		{
			name: "non-root without XDG uses HOME defaults", env: home, euid: 1000,
			wantRoot: "/", wantUser: true, wantSources: "/home/u/.config/cream/sources.yaml",
			wantLock: "/home/u/.local/state/cream/sources.lock", wantCache: "/home/u/.cache/cream",
		},
		{
			name: "non-root with XDG",
			env: map[string]string{"HOME": "/home/u", "XDG_CONFIG_HOME": "/x/cfg",
				"XDG_CACHE_HOME": "/x/cache", "XDG_STATE_HOME": "/x/state"},
			euid: 1000, wantRoot: "/", wantUser: true, wantSources: "/x/cfg/cream/sources.yaml",
			wantLock: "/x/state/cream/sources.lock", wantCache: "/x/cache/cream",
		},
		{
			name: "relative XDG is ignored", env: map[string]string{"HOME": "/home/u", "XDG_CONFIG_HOME": "rel"},
			euid: 1000, wantRoot: "/", wantUser: true, wantSources: "/home/u/.config/cream/sources.yaml",
			wantLock: "/home/u/.local/state/cream/sources.lock", wantCache: "/home/u/.cache/cream",
		},
		{
			name: "non-root without HOME falls back to system layout", env: nil, euid: 1000,
			wantRoot: "/", wantSources: "/etc/cream/sources.yaml",
			wantLock: "/var/lib/cream/sources.lock", wantCache: "/var/cache/cream",
		},
		{
			name: "--root moves everything and disables user mode", env: home, euid: 1000, rootFlag: "/tmp/x",
			wantRoot: "/tmp/x", wantSources: "/tmp/x/etc/cream/sources.yaml",
			wantLock: "/tmp/x/var/lib/cream/sources.lock", wantCache: "/tmp/x/var/cache/cream",
		},
		{
			name: "CREAM_ROOT", env: map[string]string{"CREAM_ROOT": "/tmp/env/"}, euid: 0,
			wantRoot: "/tmp/env", wantSources: "/tmp/env/etc/cream/sources.yaml",
			wantLock: "/tmp/env/var/lib/cream/sources.lock", wantCache: "/tmp/env/var/cache/cream",
		},
		{
			name: "--root beats CREAM_ROOT", env: map[string]string{"CREAM_ROOT": "/tmp/env"}, euid: 0, rootFlag: "/tmp/flag",
			wantRoot: "/tmp/flag", wantSources: "/tmp/flag/etc/cream/sources.yaml",
			wantLock: "/tmp/flag/var/lib/cream/sources.lock", wantCache: "/tmp/flag/var/cache/cream",
		},
		{
			name: "CREAM_SOURCES and CREAM_CACHE are used as is",
			env:  map[string]string{"CREAM_SOURCES": "/data/s.yaml", "CREAM_CACHE": "/data/cream"}, euid: 0, rootFlag: "/tmp/x",
			wantRoot: "/tmp/x", wantSources: "/data/s.yaml",
			wantLock: "/data/cream/sources.lock", wantCache: "/data/cream",
		},
		{
			name: "flags beat env for sources and cache",
			env:  map[string]string{"HOME": "/home/u", "CREAM_SOURCES": "/env/s.yaml", "CREAM_CACHE": "/env/c"},
			euid: 1000, srcFlag: "/flag/s.yaml", cacheF: "/flag/c",
			wantRoot: "/", wantUser: true, wantSources: "/flag/s.yaml",
			wantLock: "/flag/c/sources.lock", wantCache: "/flag/c",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			l := Detect(envOf(tc.env), tc.euid, tc.rootFlag, tc.srcFlag, tc.cacheF)
			if l.Root != tc.wantRoot {
				t.Errorf("Root = %q, want %q", l.Root, tc.wantRoot)
			}
			if l.User != tc.wantUser {
				t.Errorf("User = %v, want %v", l.User, tc.wantUser)
			}
			if got := l.Sources(); got != tc.wantSources {
				t.Errorf("Sources() = %q, want %q", got, tc.wantSources)
			}
			if got := l.SourcesLock(); got != tc.wantLock {
				t.Errorf("SourcesLock() = %q, want %q", got, tc.wantLock)
			}
			if got := l.CacheRoot(); got != tc.wantCache {
				t.Errorf("CacheRoot() = %q, want %q", got, tc.wantCache)
			}
			if got := l.SourceCache("stvor"); got != tc.wantCache+"/sources/stvor" {
				t.Errorf("SourceCache() = %q, want %q", got, tc.wantCache+"/sources/stvor")
			}
		})
	}
}

func TestLayoutMethodsUnderRoot(t *testing.T) {
	t.Parallel()
	l := Layout{Root: "/tmp/x"}
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"Bin", l.Bin(), "/tmp/x/usr/bin/cream"},
		{"ExtTree", l.ExtTree("teleport"), "/tmp/x/opt/cream/ext/teleport"},
		{"ExtNew", l.ExtNew("teleport"), "/tmp/x/opt/cream/ext/.teleport.new"},
		{"ExtPrev", l.ExtPrev("teleport"), "/tmp/x/opt/cream/ext/.teleport.prev"},
		{"EtcDir", l.EtcDir("teleport"), "/tmp/x/etc/cream/teleport"},
		{"ConfigFile", l.ConfigFile("teleport"), "/tmp/x/etc/cream/teleport/config"},
		{"SecretsFile", l.SecretsFile("teleport"), "/tmp/x/etc/cream/teleport/secrets"},
		{"StateDir", l.StateDir("teleport"), "/tmp/x/var/lib/cream/ext/teleport"},
		{"Sources", l.Sources(), "/tmp/x/etc/cream/sources.yaml"},
		{"SourcesLock", l.SourcesLock(), "/tmp/x/var/lib/cream/sources.lock"},
		{"CacheRoot", l.CacheRoot(), "/tmp/x/var/cache/cream"},
		{"SourceCache", l.SourceCache("stvor"), "/tmp/x/var/cache/cream/sources/stvor"},
		{"InstalledDir", l.InstalledDir(), "/tmp/x/var/lib/cream/installed"},
		{"InstalledFile", l.InstalledFile("teleport"), "/tmp/x/var/lib/cream/installed/teleport.json"},
		{"History", l.History(), "/tmp/x/var/lib/cream/history.jsonl"},
		{"LockDir", l.LockDir(), "/tmp/x/var/lib/cream/lock"},
		{"GlobalLock", l.GlobalLock(), "/tmp/x/var/lib/cream/lock/global"},
		{"ExtLock", l.ExtLock("teleport"), "/tmp/x/var/lib/cream/lock/teleport"},
		{"Incoming", l.Incoming("abc"), "/tmp/x/var/lib/cream/incoming/abc"},
		{"UnitTemplate", l.UnitTemplate(), "/tmp/x/usr/lib/systemd/system/cream-configure@.service"},
		{"DropIn", l.DropIn("teleport"), "/tmp/x/etc/systemd/system/cream-configure@teleport.service.d/10-stage.conf"},
		{"WantsDir", l.WantsDir("multi-user.target"), "/tmp/x/etc/systemd/system/multi-user.target.wants"},
		{"RunDir", l.RunDir(), "/tmp/x/run/cream"},
		{"RunReason", l.RunReason("teleport"), "/tmp/x/run/cream/teleport.reason"},
		{"RunResult", l.RunResult("teleport"), "/tmp/x/run/cream/configure/teleport.result"},
		{"RebootRequired", l.RebootRequired(), "/tmp/x/run/reboot-required"},
		{"PolicyRcD", l.PolicyRcD(), "/tmp/x/usr/sbin/policy-rc.d"},
		{"OSRelease", l.OSRelease(), "/tmp/x/etc/os-release"},
		{"SystemdRunning", l.SystemdRunning(), "/tmp/x/run/systemd/system"},
		{"PID1Comm", l.PID1Comm(), "/tmp/x/proc/1/comm"},
		{"CloudUserUnit", l.CloudUserUnit(), "/tmp/x/usr/lib/systemd/system/cream-configure-cloud-user.service"},
		{"ImageLabels", l.ImageLabels(), "/tmp/x/etc/image-labels.d/cream.json"},
		{"ConfiguredFile", l.ConfiguredFile("teleport"), "/tmp/x/var/lib/cream/configured/teleport.json"},
		{"ConfiguredDir", l.ConfiguredDir(), "/tmp/x/var/lib/cream/configured"},
		{"MachineID", l.MachineID(), "/tmp/x/etc/machine-id"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.got != tc.want {
				t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
			}
		})
	}
}

func TestLayoutSystemRoot(t *testing.T) {
	t.Parallel()
	l := Layout{Root: "/"}
	if got := l.ConfigFile("docker"); got != "/etc/cream/docker/config" {
		t.Errorf("ConfigFile = %q", got)
	}
	if !l.NeedsRoot() {
		t.Errorf("NeedsRoot() = false for Root=/, want true")
	}
	if (Layout{Root: "/tmp/x"}).NeedsRoot() {
		t.Errorf("NeedsRoot() = true for Root=/tmp/x, want false")
	}
	// User без XDG-каталогов не даёт относительных путей.
	if got := (Layout{User: true}).Sources(); got != "/etc/cream/sources.yaml" {
		t.Errorf("Layout{User:true}.Sources() = %q", got)
	}
	if got := (Layout{User: true}).SourcesLock(); got != "/var/lib/cream/sources.lock" {
		t.Errorf("Layout{User:true}.SourcesLock() = %q", got)
	}
	// Р12: с --cache sources.lock лежит в каталоге кеша, а не в /var/lib/cream.
	if got := (Layout{Root: "/tmp/x", CacheDir: "/c/"}).SourcesLock(); got != "/c/sources.lock" {
		t.Errorf("Layout{CacheDir:/c/}.SourcesLock() = %q", got)
	}
	if got := (Layout{User: true}).CacheRoot(); got != "/var/cache/cream" {
		t.Errorf("Layout{User:true}.CacheRoot() = %q", got)
	}
	// Нулевой Layout ведёт себя как Root=/.
	if got := (Layout{}).History(); got != "/var/lib/cream/history.jsonl" {
		t.Errorf("zero Layout History() = %q", got)
	}
	if !(Layout{}).NeedsRoot() {
		t.Errorf("zero Layout NeedsRoot() = false, want true")
	}
}

func TestLayoutOpenWrt(t *testing.T) {
	t.Parallel()
	l := Layout{Root: "/tmp/x", Family: FamilyOpenWrt}
	if !l.OpenWrt() || (Layout{}).OpenWrt() {
		t.Fatalf("OpenWrt(): openwrt = %v, zero = %v", l.OpenWrt(), (Layout{}).OpenWrt())
	}
	tests := []struct {
		name string
		got  string
		want string
	}{
		// Состояние — вне tmpfs (/var на OpenWrt — ссылка на /tmp), спека OpenWrt §4.1 п.4.
		{"StateDir", l.StateDir("demo"), "/tmp/x/opt/cream/var/ext/demo"},
		{"SourcesLock", l.SourcesLock(), "/tmp/x/opt/cream/var/sources.lock"},
		{"CacheRoot", l.CacheRoot(), "/tmp/x/opt/cream/var/cache"},
		{"InstalledFile", l.InstalledFile("demo"), "/tmp/x/opt/cream/var/installed/demo.json"},
		{"ConfiguredFile", l.ConfiguredFile("demo"), "/tmp/x/opt/cream/var/configured/demo.json"},
		{"History", l.History(), "/tmp/x/opt/cream/var/history.jsonl"},
		{"GlobalLock", l.GlobalLock(), "/tmp/x/opt/cream/var/lock/global"},
		{"Incoming", l.Incoming("abc"), "/tmp/x/opt/cream/var/incoming/abc"},
		{"HookTmp", l.HookTmp(), "/tmp/x/opt/cream/var/tmp"},
		// Метки одного запуска — в tmpfs: после перезагрузки их быть не должно.
		{"RunReason", l.RunReason("demo"), "/tmp/x/var/run/cream/demo.reason"},
		{"RunResult", l.RunResult("demo"), "/tmp/x/var/run/cream/configure/demo.result"},
		{"RebootRequired", l.RebootRequired(), "/tmp/x/var/run/reboot-required"},
		// procd: init-скрипты, ссылки rc.d и списки стадий.
		{"InitScript", l.InitScript("cream-configure"), "/tmp/x/etc/init.d/cream-configure"},
		{"RcLink", l.RcLink("S95cream-configure"), "/tmp/x/etc/rc.d/S95cream-configure"},
		{"StageMark", l.StageMark("system", "demo"), "/tmp/x/opt/cream/var/stages/system/demo"},
		// Не меняются.
		{"ExtTree", l.ExtTree("demo"), "/tmp/x/opt/cream/ext/demo"},
		{"ConfigFile", l.ConfigFile("demo"), "/tmp/x/etc/cream/demo/config"},
		{"Sources", l.Sources(), "/tmp/x/etc/cream/sources.yaml"},
		{"ImageLabels", l.ImageLabels(), "/tmp/x/etc/image-labels.d/cream.json"},
	}
	for _, tc := range tests {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
	// Debian: HookTmp не задаётся (хуки получают TMPDIR только на openwrt), стадий procd нет.
	if got := (Layout{Root: "/tmp/x"}).StateDir("demo"); got != "/tmp/x/var/lib/cream/ext/demo" {
		t.Errorf("deb StateDir = %q", got)
	}
}

func TestDetectFamily(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, file, body, want string
	}{
		{"openwrt by ID", "etc/os-release", "NAME=\"OpenWrt\"\nID=\"openwrt\"\nID_LIKE=\"lede openwrt\"\nVERSION_ID=\"25.12.6\"\n", FamilyOpenWrt},
		{"lede in ID_LIKE", "etc/os-release", "ID=mywrt\nID_LIKE=\"lede\"\n", FamilyOpenWrt},
		{"usr/lib fallback", "usr/lib/os-release", "ID=openwrt\n", FamilyOpenWrt},
		{"debian", "etc/os-release", "ID=debian\nVERSION_ID=\"12\"\n", ""},
		{"ubuntu like debian", "etc/os-release", "ID=ubuntu\nID_LIKE=debian\n", ""},
		{"no os-release", "", "", ""},
		{"garbage", "etc/os-release", "this is = not \"closed\n", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			if tc.file != "" {
				writeFile(t, root, tc.file, tc.body)
			}
			if got := DetectFamily(root); got != tc.want {
				t.Errorf("DetectFamily = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDetectSetsFamily(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, root, "etc/os-release", "ID=openwrt\n")
	l := Detect(envOf(nil), 0, root, "", "")
	if l.Family != FamilyOpenWrt {
		t.Fatalf("Family = %q, want openwrt", l.Family)
	}
	if got := l.InstalledDir(); got != root+"/opt/cream/var/installed" {
		t.Errorf("InstalledDir = %q", got)
	}
}

func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
