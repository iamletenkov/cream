package resolve

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/paths"
)

// rootWith returns a root directory whose os-release (at rel) is the fixture
// testdata/os-release/<fixture>; fixture "" leaves the root without one.
func rootWith(t *testing.T, fixture, rel string) string {
	t.Helper()
	root := t.TempDir()
	if fixture == "" {
		return root
	}
	data, err := os.ReadFile(filepath.Join("testdata", "os-release", fixture))
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func fakeLookPath(have ...string) func(string) (string, error) {
	return func(name string) (string, error) {
		for _, h := range have {
			if h == name {
				return "/usr/bin/" + name, nil
			}
		}
		return "", errors.New("not found")
	}
}

func TestProbeDetect(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                        string
		fixture, rel, arch, phase   string
		wantFamily, wantID, wantVer string
		wantArch                    string
		wantExit                    int
		wantCode                    string
	}{
		{name: "debian 12 amd64", fixture: "debian-12", rel: "etc/os-release", arch: "amd64", phase: PhaseLive,
			wantFamily: "deb", wantID: "debian", wantVer: "12", wantArch: "amd64"},
		{name: "ubuntu через ID_LIKE, aarch64 канонизируется", fixture: "ubuntu-22.04", rel: "etc/os-release",
			arch: "aarch64", phase: PhaseBuild, wantFamily: "deb", wantID: "ubuntu", wantVer: "22.04", wantArch: "arm64"},
		{name: "запасной /usr/lib/os-release", fixture: "debian-12", rel: "usr/lib/os-release", arch: "x86_64",
			phase: PhaseLive, wantFamily: "deb", wantID: "debian", wantVer: "12", wantArch: "amd64"},
		{name: "debian testing без VERSION_ID", fixture: "debian-testing", rel: "etc/os-release", arch: "amd64",
			phase: PhaseLive, wantFamily: "deb", wantID: "debian", wantArch: "amd64"},
		{name: "чужое семейство", fixture: "fedora-40", rel: "etc/os-release", arch: "amd64", phase: PhaseLive,
			wantID: "fedora", wantVer: "40", wantArch: "amd64"},
		{name: "os-release нет — всё неизвестно", arch: "amd64", phase: PhaseLive, wantArch: "amd64"},
		{name: "неизвестная фаза", fixture: "debian-12", rel: "etc/os-release", arch: "amd64", phase: "stage",
			wantExit: cerr.ExitUsage, wantCode: "usage.phase"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := rootWith(t, tc.fixture, tc.rel)
			p := Probe{Layout: paths.Layout{Root: root}, LookPath: fakeLookPath("curl"), Arch: tc.arch}
			h, err := p.Detect(tc.phase)
			if tc.wantCode != "" {
				if cerr.ExitCode(err) != tc.wantExit || cerr.CodeOf(err) != tc.wantCode {
					t.Fatalf("Detect = %v (%d %s), ждали %d %s", err, cerr.ExitCode(err), cerr.CodeOf(err), tc.wantExit, tc.wantCode)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if h.Phase != tc.phase || h.Family != tc.wantFamily || h.OSID != tc.wantID || h.OSVersion != tc.wantVer ||
				h.TargetArch != tc.wantArch || h.HostArch != tc.wantArch {
				t.Fatalf("Host = %+v", h)
			}
			if h.HasCommand == nil || !h.HasCommand("curl") || h.HasCommand("jq") {
				t.Fatal("HasCommand не через LookPath")
			}
			if h.AcceptRisk {
				t.Fatal("AcceptRisk по умолчанию ложь")
			}
		})
	}
}

func TestProbeDetectUnreadable(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// Каталог на месте файла: чтение падает не ENOENT — это 74, а не «неизвестно».
	if err := os.MkdirAll(filepath.Join(root, "etc", "os-release"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Probe{Layout: paths.Layout{Root: root}, Arch: "amd64"}.Detect(PhaseLive)
	if cerr.ExitCode(err) != cerr.ExitState || cerr.CodeOf(err) != "state.io" {
		t.Fatalf("Detect = %v (%d %s), ждали 74 state.io", err, cerr.ExitCode(err), cerr.CodeOf(err))
	}
}

func TestDetectHostUsesRuntimeArch(t *testing.T) {
	t.Parallel()
	root := rootWith(t, "debian-12", "etc/os-release")
	h, err := DetectHost(paths.Layout{Root: root}, PhaseLive, fakeLookPath())
	if err != nil {
		t.Fatal(err)
	}
	if h.TargetArch == "" || h.TargetArch != h.HostArch || h.OSID != "debian" {
		t.Fatalf("Host = %+v", h)
	}
	if h.HasCommand("curl") {
		t.Fatal("HasCommand не через переданный lookPath")
	}
}

func TestDetectHostNilLookPath(t *testing.T) {
	t.Parallel()
	h, err := DetectHost(paths.Layout{Root: t.TempDir()}, PhaseLive, nil)
	if err != nil {
		t.Fatal(err)
	}
	if h.HasCommand != nil {
		t.Fatal("без lookPath команды не проверяются (HasCommand == nil)")
	}
}
