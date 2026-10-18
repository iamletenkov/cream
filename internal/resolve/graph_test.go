package resolve

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/manifest"
	"github.com/iamletenkov/cream/internal/state"
)

const fixturesCatalog = "../../testdata/catalog/extensions"

func creamBuild() manifest.CreamBuild { return manifest.ParseCreamBuild("1.0.0") }

// fixture is the extension testdata/catalog/extensions/<name> as a requested node.
func fixture(t *testing.T, name string) Node {
	t.Helper()
	m, err := manifest.Load(filepath.Join(fixturesCatalog, name), creamBuild(), nil)
	if err != nil {
		t.Fatalf("фикстура %s: %v", name, err)
	}
	return Node{Manifest: m, Source: state.SourceRef{Name: "fx", Type: "path", Resolved: "sha256:" + strings.Repeat("ab", 32),
		CatalogVersion: "1.0.0"}, Digest: "sha256:" + strings.Repeat("cd", 32)}
}

// synth is a requested node from an inline manifest; extra holds additional
// top-level fields (a default platform is added unless extra has one).
func synth(t *testing.T, name, version, extra string) Node {
	t.Helper()
	y := "schema: cream/v1\nname: " + name + "\nversion: " + version + "\nsummary: s\ncream: \">=1.0.0\"\n" + extra + "\n"
	if !strings.Contains(extra, "kind: library") {
		y += "hooks:\n  install: {}\n"
	}
	if !strings.Contains(extra, "platform:") {
		y += "platform: {family: deb, arch: [amd64, arm64]}\n"
	}
	m, err := manifest.Parse([]byte(y), creamBuild(), nil)
	if err != nil {
		t.Fatalf("манифест %s: %v\n%s", name, err, y)
	}
	return Node{Manifest: m}
}

func installed(n Node) Node {
	n.Installed = true
	if n.Files == nil {
		for _, f := range n.Manifest.Files {
			n.Files = append(n.Files, f.Dst)
		}
	}
	return n
}

func liveHost() Host {
	return Host{Phase: PhaseLive, TargetArch: "amd64", HostArch: "amd64", Family: "deb", OSID: "debian", OSVersion: "12",
		HasCommand: func(string) bool { return true }}
}

func layerNames(ls []Layer) []string {
	out := make([]string, 0, len(ls))
	for _, l := range ls {
		out = append(out, l.Name)
	}
	return out
}

func checkResult(t *testing.T, ls []Layer, err error, wantOrder []string, wantExit int, wantCode string) {
	t.Helper()
	if wantCode != "" {
		if cerr.ExitCode(err) != wantExit || cerr.CodeOf(err) != wantCode {
			t.Fatalf("Resolve = %v (%d %s), ждали %d %s", err, cerr.ExitCode(err), cerr.CodeOf(err), wantExit, wantCode)
		}
		return
	}
	if err != nil {
		t.Fatalf("Resolve: %v (%s)", err, cerr.CodeOf(err))
	}
	if got := layerNames(ls); !slices.Equal(got, wantOrder) {
		t.Fatalf("порядок = %v, ждали %v", got, wantOrder)
	}
}

func TestResolveGraph(t *testing.T) {
	t.Parallel()
	fx := func(name string) Node { return fixture(t, name) }
	sy := func(name, version, extra string) Node { return synth(t, name, version, extra) }
	tests := []struct {
		name      string
		installed []Node
		requested []Node
		wantOrder []string
		wantExit  int
		wantCode  string
	}{
		{name: "одно расширение", requested: []Node{fx("noop")}, wantOrder: []string{"noop"}},
		{name: "depends: нет библиотеки", requested: []Node{fx("depends-lib")},
			wantExit: cerr.ExitGraph, wantCode: "deps.missing"},
		{name: "depends: библиотека установлена", installed: []Node{installed(fx("lib-fixture"))},
			requested: []Node{fx("depends-lib")}, wantOrder: []string{"depends-lib"}},
		{name: "depends: обе запрошены — библиотека раньше", requested: []Node{fx("depends-lib"), fx("lib-fixture")},
			wantOrder: []string{"lib-fixture", "depends-lib"}},
		{name: "depends: установленная версия вне диапазона",
			installed: []Node{installed(sy("lib-fixture", "2.0.0", "kind: library"))},
			requested: []Node{fx("depends-lib")}, wantExit: cerr.ExitGraph, wantCode: "deps.version"},
		{name: "depends: запрошенная замена ломает установленного зависимого",
			installed: []Node{installed(fx("lib-fixture")), installed(fx("depends-lib"))},
			requested: []Node{sy("lib-fixture", "2.0.0", "kind: library")}, wantExit: cerr.ExitGraph, wantCode: "deps.version"},
		{name: "depends: замена в диапазоне", installed: []Node{installed(fx("lib-fixture")), installed(fx("depends-lib"))},
			requested: []Node{sy("lib-fixture", "1.5.0", "kind: library")}, wantOrder: []string{"lib-fixture"}},
		{name: "depends без диапазона — любая версия", installed: []Node{installed(sy("base", "9.1.0", ""))},
			requested: []Node{sy("top", "1.0.0", "depends: [{name: base}]")}, wantOrder: []string{"top"}},
		{name: "requires: нет поставщика", requested: []Node{fx("requires-cap")},
			wantExit: cerr.ExitGraph, wantCode: "deps.requires"},
		{name: "requires: поставщик установлен", installed: []Node{installed(fx("apply-hook"))},
			requested: []Node{fx("requires-cap")}, wantOrder: []string{"requires-cap"}},
		{name: "requires: поставщик запрошен — раньше потребителя", requested: []Node{fx("requires-cap"), fx("conflict-cap")},
			wantOrder: []string{"conflict-cap", "requires-cap"}},
		{name: "requires: замена поставщика без способности ломает установленного",
			installed: []Node{installed(fx("apply-hook")), installed(fx("requires-cap"))},
			requested: []Node{sy("apply-hook", "2.0.0", "")}, wantExit: cerr.ExitGraph, wantCode: "deps.requires"},
		{name: "requires: другой поставщик остаётся",
			installed: []Node{installed(fx("apply-hook")), installed(sy("other-rt", "1.0.0", "provides: [fixture-runtime]")),
				installed(fx("requires-cap"))},
			requested: []Node{sy("apply-hook", "2.0.0", "")}, wantOrder: []string{"apply-hook"}},
		{name: "conflicts.extensions: запрошенный против установленного", installed: []Node{installed(fx("noop"))},
			requested: []Node{fx("conflict-name")}, wantExit: cerr.ExitGraph, wantCode: "conflict.extension"},
		{name: "conflicts.extensions: обратная сторона", installed: []Node{installed(fx("conflict-name"))},
			requested: []Node{fx("noop")}, wantExit: cerr.ExitGraph, wantCode: "conflict.extension"},
		{name: "conflicts.extensions: оба запрошены", requested: []Node{fx("noop"), fx("conflict-name")},
			wantExit: cerr.ExitGraph, wantCode: "conflict.extension"},
		{name: "conflicts.capabilities: другой поставщик", installed: []Node{installed(fx("apply-hook"))},
			requested: []Node{fx("conflict-cap")}, wantExit: cerr.ExitGraph, wantCode: "conflict.capability"},
		{name: "conflicts.capabilities: обратная сторона", installed: []Node{installed(fx("conflict-cap"))},
			requested: []Node{fx("apply-hook")}, wantExit: cerr.ExitGraph, wantCode: "conflict.capability"},
		{name: "conflicts.capabilities: сам себе не конфликт", requested: []Node{fx("conflict-cap")},
			wantOrder: []string{"conflict-cap"}},
		{name: "conflicts.capabilities: переустановка себя", installed: []Node{installed(fx("conflict-cap"))},
			requested: []Node{fx("conflict-cap")}, wantOrder: []string{"conflict-cap"}},
		{name: "files: dst занят установленным", installed: []Node{installed(fx("files-mode"))},
			requested: []Node{sy("thief", "1.0.0", "files: [{src: x, dst: /etc/files-mode/plain.conf}]")},
			wantExit:  cerr.ExitGraph, wantCode: "conflict.file"},
		{name: "files: dst у двух запрошенных",
			requested: []Node{fx("files-mode"), sy("thief", "1.0.0", "files: [{src: x, dst: /usr/local/bin/files-mode-tool}]")},
			wantExit:  cerr.ExitGraph, wantCode: "conflict.file"},
		{name: "files: переустановка не конфликтует сама с собой", installed: []Node{installed(fx("files-mode"))},
			requested: []Node{fx("files-mode")}, wantOrder: []string{"files-mode"}},
		{name: "files: по реестру, а не по манифесту установленного",
			installed: []Node{func() Node { n := installed(fx("noop")); n.Files = []string{"/etc/shared.conf"}; return n }()},
			requested: []Node{sy("thief", "1.0.0", "files: [{src: x, dst: /etc/shared.conf}]")},
			wantExit:  cerr.ExitGraph, wantCode: "conflict.file"},
		{name: "цикл по depends",
			requested: []Node{sy("cyc-a", "1.0.0", "depends: [{name: cyc-b}]"), sy("cyc-b", "1.0.0", "depends: [{name: cyc-a}]")},
			wantExit:  cerr.ExitGraph, wantCode: "deps.cycle"},
		{name: "цикл через requires",
			requested: []Node{sy("cyc-a", "1.0.0", "requires: [cap-b]\nprovides: [cap-a]"),
				sy("cyc-b", "1.0.0", "requires: [cap-a]\nprovides: [cap-b]")},
			wantExit: cerr.ExitGraph, wantCode: "deps.cycle"},
		{name: "равные — по имени", requested: []Node{fx("with-config"), fx("noop"), fx("apply-hook")},
			wantOrder: []string{"apply-hook", "noop", "with-config"}},
		{name: "цепочка и независимый",
			requested: []Node{sy("c-top", "1.0.0", "depends: [{name: b-mid}]"), sy("b-mid", "1.0.0", "depends: [{name: z-base}]"),
				sy("z-base", "1.0.0", ""), sy("a-free", "1.0.0", "")},
			wantOrder: []string{"a-free", "z-base", "b-mid", "c-top"}},
		{name: "сломанное установленное не мешает несвязанному",
			installed: []Node{installed(fx("depends-lib"))}, requested: []Node{fx("noop")}, wantOrder: []string{"noop"}},
		{name: "одно имя дважды", requested: []Node{fx("noop"), fx("noop")},
			wantExit: cerr.ExitUsage, wantCode: "usage.duplicate"},
		{name: "пустой запрос", wantOrder: []string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var w cerr.Warnings
			ls, err := Resolve(tc.installed, tc.requested, nil, liveHost(), &w)
			checkResult(t, ls, err, tc.wantOrder, tc.wantExit, tc.wantCode)
		})
	}
}

func TestResolveErrorDetails(t *testing.T) {
	t.Parallel()
	_, err := Resolve(nil, []Node{fixture(t, "depends-lib")}, nil, liveHost(), nil)
	d := cerr.DetailsOf(err)
	if d["ext"] != "depends-lib" {
		t.Fatalf("details = %v", d)
	}
	missing, ok := d["missing"].([]Missing)
	if !ok || len(missing) != 1 || missing[0].Name != "lib-fixture" || missing[0].Version != ">=1.0.0 <2" {
		t.Fatalf("details.missing = %#v", d["missing"])
	}
	if !strings.Contains(err.Error(), "lib-fixture") {
		t.Fatalf("сообщение без имени зависимости: %v", err)
	}

	_, err = Resolve(nil, []Node{synth(t, "cyc-a", "1.0.0", "depends: [{name: cyc-b}]"),
		synth(t, "cyc-b", "1.0.0", "depends: [{name: cyc-a}]")}, nil, liveHost(), nil)
	cyc, ok := cerr.DetailsOf(err)["cycle"].([]string)
	if !ok || !slices.Equal(cyc, []string{"cyc-a", "cyc-b", "cyc-a"}) {
		t.Fatalf("details.cycle = %#v", cerr.DetailsOf(err)["cycle"])
	}
}

func TestResolvePlatform(t *testing.T) {
	t.Parallel()
	sy := func(name, extra string) Node { return synth(t, name, "1.0.0", extra) }
	host := func(mod func(*Host)) Host { h := liveHost(); mod(&h); return h }
	tests := []struct {
		name     string
		node     Node
		host     Host
		wantMode string
		wantWarn string
		wantExit int
		wantCode string
	}{
		{name: "всё совпало", node: fixture(t, "noop"), host: liveHost(), wantMode: ModeLive},
		{name: "чужое семейство", node: fixture(t, "noop"), host: host(func(h *Host) { h.Family = "rpm" }),
			wantExit: cerr.ExitNotApplicable, wantCode: "platform.family"},
		{name: "arch не из списка", node: sy("amd-only", "platform: {family: deb, arch: [amd64]}"),
			host:     host(func(h *Host) { h.TargetArch, h.HostArch = "arm64", "arm64" }),
			wantExit: cerr.ExitNotApplicable, wantCode: "platform.arch"},
		{name: "arch aarch64 канонизируется", node: sy("arm-only", "platform: {family: deb, arch: [arm64]}"),
			host: host(func(h *Host) { h.TargetArch, h.HostArch = "aarch64", "aarch64" }), wantMode: ModeLive},
		{name: "os ubuntu 22.04 против >=22.04",
			node: sy("u", "platform: {family: deb, arch: [amd64], os: [{id: debian, version: \">=12\"}, {id: ubuntu, version: \">=22.04\"}]}"),
			host: host(func(h *Host) { h.OSID, h.OSVersion = "ubuntu", "22.04" }), wantMode: ModeLive},
		{name: "os ubuntu 20.04 против >=22.04",
			node:     sy("u", "platform: {family: deb, arch: [amd64], os: [{id: ubuntu, version: \">=22.04\"}]}"),
			host:     host(func(h *Host) { h.OSID, h.OSVersion = "ubuntu", "20.04" }),
			wantExit: cerr.ExitNotApplicable, wantCode: "platform.os"},
		{name: "os другой ID", node: sy("u", "platform: {family: deb, arch: [amd64], os: [{id: ubuntu}]}"),
			wantExit: cerr.ExitNotApplicable, wantCode: "platform.os"},
		{name: "os без версии — любая", node: sy("d", "platform: {family: deb, arch: [amd64], os: [{id: debian}]}"),
			host: host(func(h *Host) { h.OSVersion = "" }), wantMode: ModeLive},
		{name: "os: VERSION_ID нет, а диапазон есть",
			node: sy("d", "platform: {family: deb, arch: [amd64], os: [{id: debian, version: \">=12\"}]}"),
			host: host(func(h *Host) { h.OSVersion = "" }), wantExit: cerr.ExitNotApplicable, wantCode: "platform.os"},
		{name: "os неизвестна — предупреждение",
			node: sy("d", "platform: {family: deb, arch: [amd64], os: [{id: debian}]}"),
			host: host(func(h *Host) { h.OSID, h.OSVersion = "", "" }), wantMode: ModeLive, wantWarn: "platform.os_unknown"},
		{name: "commands: нет команды",
			node:     sy("c", "platform: {family: deb, arch: [amd64], commands: [curl, jq]}"),
			host:     host(func(h *Host) { h.HasCommand = func(c string) bool { return c == "curl" } }),
			wantExit: cerr.ExitNotApplicable, wantCode: "platform.commands"},
		{name: "commands: все есть", node: sy("c", "platform: {family: deb, arch: [amd64], commands: [curl]}"),
			wantMode: ModeLive},
		{name: "commands: проверка выключена", node: sy("c", "platform: {family: deb, arch: [amd64], commands: [curl]}"),
			host: host(func(h *Host) { h.HasCommand = nil }), wantMode: ModeLive},
		{name: "live: no", node: sy("ln", "targets: {live: no, live_note: n}"),
			wantExit: cerr.ExitNotApplicable, wantCode: "target.live"},
		{name: "live: confirm без --accept-risk", node: fixture(t, "live-confirm"),
			wantExit: cerr.ExitNotApplicable, wantCode: "target.confirm"},
		{name: "live: confirm с --accept-risk", node: fixture(t, "live-confirm"),
			host: host(func(h *Host) { h.AcceptRisk = true }), wantMode: ModeLive},
		{name: "build: exec", node: fixture(t, "noop"), host: host(func(h *Host) { h.Phase = PhaseBuild }), wantMode: ModeExec},
		{name: "build: live: no не мешает", node: sy("ln", "targets: {live: no, live_note: n}"),
			host: host(func(h *Host) { h.Phase = PhaseBuild }), wantMode: ModeExec},
		{name: "build: none → слой none", node: sy("bn", "targets: {build: none}"),
			host: host(func(h *Host) { h.Phase = PhaseBuild }), wantMode: ModeNone},
		{name: "build: барьер архитектуры", node: fixture(t, "noop"),
			host:     host(func(h *Host) { h.Phase, h.TargetArch, h.HostArch = PhaseBuild, "arm64", "amd64" }),
			wantExit: cerr.ExitNotApplicable, wantCode: "platform.arch_barrier"},
		{name: "build: барьер не касается none", node: sy("bn", "targets: {build: none}"),
			host: host(func(h *Host) { h.Phase, h.TargetArch, h.HostArch = PhaseBuild, "arm64", "amd64" }), wantMode: ModeNone},
		{name: "build: x86_64 = amd64 — барьера нет", node: fixture(t, "noop"),
			host: host(func(h *Host) { h.Phase, h.TargetArch, h.HostArch = PhaseBuild, "x86_64", "amd64" }), wantMode: ModeExec},
		{name: "неизвестная фаза", node: fixture(t, "noop"), host: host(func(h *Host) { h.Phase = "stage" }),
			wantExit: cerr.ExitUsage, wantCode: "usage.phase"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.host.Phase == "" {
				tc.host = liveHost()
			}
			var w cerr.Warnings
			ls, err := Resolve(nil, []Node{tc.node}, nil, tc.host, &w)
			if tc.wantCode != "" {
				if cerr.ExitCode(err) != tc.wantExit || cerr.CodeOf(err) != tc.wantCode {
					t.Fatalf("Resolve = %v (%d %s), ждали %d %s", err, cerr.ExitCode(err), cerr.CodeOf(err), tc.wantExit, tc.wantCode)
				}
				if d := cerr.DetailsOf(err); tc.wantExit != cerr.ExitUsage && d["ext"] != tc.node.Manifest.Name {
					t.Fatalf("details.ext = %v", d["ext"])
				}
				return
			}
			if err != nil {
				t.Fatalf("Resolve: %v (%s)", err, cerr.CodeOf(err))
			}
			if len(ls) != 1 || ls[0].Mode != tc.wantMode {
				t.Fatalf("слои = %+v, ждали mode %s", ls, tc.wantMode)
			}
			var codes []string
			for _, x := range w.List() {
				codes = append(codes, x.Code)
			}
			if tc.wantWarn != "" && !slices.Contains(codes, tc.wantWarn) || tc.wantWarn == "" && len(codes) > 0 {
				t.Fatalf("предупреждения = %v, ждали %q", codes, tc.wantWarn)
			}
		})
	}
}

func TestResolveBuildNoneOutsideGraph(t *testing.T) {
	t.Parallel()
	h := liveHost()
	h.Phase = PhaseBuild
	// Слой none при сборке не ставится: он не поставщик для depends и requires.
	lib := synth(t, "lib-fixture", "1.0.0", "kind: library\ntargets: {build: none}")
	_, err := Resolve(nil, []Node{fixture(t, "depends-lib"), lib}, nil, h, nil)
	if cerr.CodeOf(err) != "deps.missing" {
		t.Fatalf("Resolve = %v, ждали deps.missing", err)
	}
	// А его собственные зависимости не проверяются.
	top := synth(t, "top", "1.0.0", "depends: [{name: nowhere}]\ntargets: {build: none}")
	ls, err := Resolve(nil, []Node{top, fixture(t, "noop")}, nil, h, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := layerNames(ls); !slices.Equal(got, []string{"noop", "top"}) || ls[1].Mode != ModeNone {
		t.Fatalf("слои = %+v", ls)
	}
}

func TestDependents(t *testing.T) {
	t.Parallel()
	fx := func(name string) Node { return installed(fixture(t, name)) }
	tests := []struct {
		name      string
		installed []Node
		target    string
		want      []string
	}{
		{name: "по depends", installed: []Node{fx("lib-fixture"), fx("depends-lib"), fx("noop")}, target: "lib-fixture",
			want: []string{"depends-lib"}},
		{name: "по requires, единственный поставщик", installed: []Node{fx("apply-hook"), fx("requires-cap")},
			target: "apply-hook", want: []string{"requires-cap"}},
		{name: "по requires, есть другой поставщик",
			installed: []Node{fx("apply-hook"), installed(synth(t, "other-rt", "1.0.0", "provides: [fixture-runtime]")),
				fx("requires-cap")},
			target: "apply-hook", want: []string{}},
		{name: "никто не зависит", installed: []Node{fx("noop"), fx("lib-fixture")}, target: "noop", want: []string{}},
		{name: "по имени — отсортировано",
			installed: []Node{fx("lib-fixture"), installed(synth(t, "zz", "1.0.0", "depends: [{name: lib-fixture}]")),
				fx("depends-lib")},
			target: "lib-fixture", want: []string{"depends-lib", "zz"}},
		{name: "нет в установленных", installed: []Node{fx("depends-lib")}, target: "lib-fixture", want: []string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Dependents(tc.installed, tc.target)
			if got == nil || !slices.Equal(got, tc.want) {
				t.Fatalf("Dependents = %#v, ждали %v", got, tc.want)
			}
		})
	}
}
