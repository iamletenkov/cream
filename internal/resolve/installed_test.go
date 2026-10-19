package resolve

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/iamletenkov/cream/internal/cerr"
)

func writeInstalled(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "installed.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadInstalled(t *testing.T) {
	t.Parallel()
	p := writeInstalled(t, `[
	  {"name": "teleport", "version": "4.0.0", "provides": ["teleport-node"]},
	  {"token": "core/cloud-user@1.2.0#35a60f234b90", "params_digest": "sha256:x", "deps": []},
	  {"token": "docker@27.1.0"},
	  {"name": "teleport", "version": "4.1.0", "provides": ["teleport-node"]}
	]`)
	nodes, err := ReadInstalled(p)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, n := range nodes {
		if !n.Installed {
			t.Errorf("%s: не Installed", n.Manifest.Name)
		}
		got = append(got, n.Manifest.Name+"@"+n.Manifest.Version.String())
	}
	// Позже в lineage — новее: повтор имени заменяет прежний.
	if want := []string{"teleport@4.1.0", "cloud-user@1.2.0", "docker@27.1.0"}; !slices.Equal(got, want) {
		t.Errorf("узлы %v, ждали %v", got, want)
	}
	if !slices.Equal(nodes[0].Manifest.Provides, []string{"teleport-node"}) {
		t.Errorf("provides %v", nodes[0].Manifest.Provides)
	}
}

func TestReadInstalledRefusals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, body string
	}{
		{"не массив", `{"name": "a", "version": "1.0.0"}`},
		{"null", `null`},
		{"пусто", "  \n"},
		{"хвост после массива", `[] []`},
		{"нет версии", `[{"name": "a"}]`},
		{"версия не semver", `[{"name": "a", "version": "1.0"}]`},
		{"имя неверное", `[{"name": "A", "version": "1.0.0"}]`},
		{"token неверный", `[{"token": "a"}]`},
		{"token против name", `[{"token": "a@1.0.0", "name": "b"}]`},
		{"token против version", `[{"token": "a@1.0.0", "version": "2.0.0"}]`},
		{"provides неверный", `[{"name": "a", "version": "1.0.0", "provides": ["X"]}]`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := ReadInstalled(writeInstalled(t, tc.body))
			if cerr.ExitCode(err) != cerr.ExitData || cerr.CodeOf(err) != CodeInstalledInvalid {
				t.Errorf("err = %v (%d %s)", err, cerr.ExitCode(err), cerr.CodeOf(err))
			}
		})
	}
	_, err := ReadInstalled(filepath.Join(t.TempDir(), "nope.json"))
	if cerr.ExitCode(err) != cerr.ExitNotFound || cerr.CodeOf(err) != "not_found.installed" {
		t.Errorf("нет файла: %v", err)
	}
}

// Предел --installed — 1 МиБ: файл ровно в предел читается, на байт больше —
// 65 installed.invalid (а не чтение без границы).
func TestReadInstalledSizeLimit(t *testing.T) {
	t.Parallel()
	pad := func(n int) string {
		body := `[{"name": "a", "version": "1.0.0"}]`
		return body + strings.Repeat(" ", n-len(body))
	}
	if _, err := ReadInstalled(writeInstalled(t, pad(maxInstalledSize))); err != nil {
		t.Errorf("ровно %d байт: %v", maxInstalledSize, err)
	}
	_, err := ReadInstalled(writeInstalled(t, pad(maxInstalledSize+1)))
	if cerr.ExitCode(err) != cerr.ExitData || cerr.CodeOf(err) != CodeInstalledInvalid {
		t.Errorf("больше предела: %v", err)
	}
	if maxInstalledSize != 1<<20 {
		t.Errorf("предел %d, по docs/cli.md — 1 МиБ", maxInstalledSize)
	}
}

// Узлы --installed закрывают depends и requires слоя фазы build и попадают в
// deps слоя; сами слоями не становятся.
func TestResolveBuildWithInstalled(t *testing.T) {
	t.Parallel()
	inst, err := ReadInstalled(writeInstalled(t,
		`[{"name": "libx", "version": "1.2.0"}, {"name": "rt", "version": "2.0.0", "provides": ["runtime"]}]`))
	if err != nil {
		t.Fatal(err)
	}
	app := synth(t, "app", "1.0.0", "depends: [{name: libx, version: \">=1.0.0 <2\"}]\nrequires: [runtime]")
	h := Host{Phase: PhaseBuild, TargetArch: "amd64", HostArch: "amd64"}
	layers, err := Resolve(inst, []Node{app}, nil, h, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(layers) != 1 || layers[0].Name != "app" || !slices.Equal(layers[0].Deps, []string{"libx@1.2.0", "rt@2.0.0"}) {
		t.Errorf("слои %+v", layers)
	}
	// Без --installed — прежний отказ 67 deps.missing.
	if _, err := Resolve(nil, []Node{app}, nil, h, nil); cerr.CodeOf(err) != "deps.missing" {
		t.Errorf("без установленного: %v", err)
	}
	// Версия из базы вне диапазона — 67 deps.version.
	old, err := ReadInstalled(writeInstalled(t,
		`[{"name": "libx", "version": "2.0.0"}, {"name": "rt", "version": "2.0.0", "provides": ["runtime"]}]`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(old, []Node{app}, nil, h, nil); cerr.CodeOf(err) != "deps.version" {
		t.Errorf("версия базы вне диапазона: %v", err)
	}
}
