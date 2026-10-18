package cli

// Общие помощники CLI-тестов (Task 12). После волны 4 файл заморожен: T13–T17
// ими пользуются, но не правят (нужна правка — доклад контроллеру).

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iamletenkov/cream/internal/fsx"
	"github.com/iamletenkov/cream/internal/manifest"
	"github.com/iamletenkov/cream/internal/paths"
	"github.com/iamletenkov/cream/internal/state"
)

// cliTestdataRoot — фикстуры §14 в корне репозитория.
const cliTestdataRoot = "../../testdata"

// cliNow — часы CLI-тестов.
var cliNow = time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)

// cliIn — вход одного вызова runCLI.
type cliIn struct {
	// Root — --root; "" — не передавать (тогда корень — "/").
	Root string
	// Stdin — stdin cream.
	Stdin string
	// Euid — эффективный uid (0 — root).
	Euid int
	// Env — окружение cream.
	Env map[string]string
}

// cliOut — итог вызова: код выхода и потоки.
type cliOut struct {
	Code           int
	Stdout, Stderr string
}

// runCLI вызывает настоящий набор команд, как main: с --root in.Root перед
// args, systemctl и хуками через реальную сборку движка (wire.go). На
// временном корне systemd «не запущен» (/run/systemd/system нет), поэтому
// systemctl не вызывается.
func runCLI(t *testing.T, in cliIn, args ...string) cliOut {
	t.Helper()
	var stdout, stderr bytes.Buffer
	app := App{Stdin: strings.NewReader(in.Stdin), Stdout: &stdout, Stderr: &stderr,
		Env:  func(k string) string { return in.Env[k] },
		Euid: func() int { return in.Euid },
		Now:  func() time.Time { return cliNow }}
	if in.Root != "" {
		args = append([]string{"--root", in.Root}, args...)
	}
	code := Run(args, app)
	return cliOut{Code: code, Stdout: stdout.String(), Stderr: stderr.String()}
}

// normRoot заменяет корень машины в выводе на <ROOT> (для golden).
func normRoot(out, root string) []byte {
	return []byte(strings.ReplaceAll(out, root, "<ROOT>"))
}

// cliTestdataDir — абсолютный путь testdata/<rel…> корня репозитория.
func cliTestdataDir(t *testing.T, rel ...string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join(append([]string{cliTestdataRoot}, rel...)...))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("фикстура %s: %v", p, err)
	}
	return p
}

// fixtureDir — каталог расширения-фикстуры testdata/catalog/extensions/<name>.
func fixtureDir(t *testing.T, name string) string {
	t.Helper()
	return cliTestdataDir(t, "catalog", "extensions", name)
}

// seedRoot кладёт дерево фикстуры name в /opt/cream/ext/<name> под root и
// пишет запись реестра «installed», как после live-установки (как
// seedInstalled движка). mutate правит запись до записи.
func seedRoot(t *testing.T, root, name string, mutate ...func(*state.Record)) {
	t.Helper()
	l := paths.Layout{Root: root}
	src := fixtureDir(t, name)
	m, err := manifest.Load(src, manifest.ParseCreamBuild("1.0.0"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := fsx.CopyTree(src, l.ExtTree(m.Name)); err != nil {
		t.Fatal(err)
	}
	digest, err := fsx.TreeDigest(l.ExtTree(m.Name))
	if err != nil {
		t.Fatal(err)
	}
	rec := state.Record{Name: m.Name, Version: m.Version.String(), Kind: string(m.Kind),
		Status: state.StatusInstalled, ExtDigest: digest, Phase: "live", CreamVersion: "1.0.0",
		Source: state.SourceRef{Name: "test", Type: "path", Origin: "/fixtures"}, InstalledAt: cliNow}
	if h, ok := m.Hooks[manifest.HookConfigure]; ok {
		rec.Configure = &state.ConfigureRec{Stage: string(h.Stage), Target: "multi-user.target"}
	}
	for _, f := range mutate {
		f(&rec)
	}
	if err := state.NewStore(l, func() time.Time { return cliNow }).Put(rec); err != nil {
		t.Fatal(err)
	}
}

// writeRootFile пишет файл под root (rel — системный путь, «/etc/…»).
func writeRootFile(t *testing.T, root, rel, body string, perm os.FileMode) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), perm); err != nil {
		t.Fatal(err)
	}
}
