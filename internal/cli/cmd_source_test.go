package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iamletenkov/cream/internal/fsx"
	"github.com/iamletenkov/cream/internal/manifest"
	"github.com/iamletenkov/cream/internal/paths"
	"github.com/iamletenkov/cream/internal/source"
)

// sourceTestDeps — сборка cream 1.0.0 (как релиз, без platform.dev_build) и
// настоящий загрузчик: тесты ходят только по file://, без сети.
func sourceTestDeps() catalogDeps {
	return catalogDeps{cream: manifest.ParseCreamBuild("1.0.0"), fetch: source.NewHTTPFetcher(nil), arch: "amd64"}
}

// sourceRunCLI — как runCLI, но с командами источников и каталога на deps
// (сквозь тот же диспетчер run).
func sourceRunCLI(t *testing.T, d catalogDeps, in cliIn, args ...string) cliOut {
	t.Helper()
	var stdout, stderr bytes.Buffer
	app := App{Stdin: strings.NewReader(in.Stdin), Stdout: &stdout, Stderr: &stderr,
		Env:  func(k string) string { return in.Env[k] },
		Euid: func() int { return in.Euid },
		Now:  func() time.Time { return cliNow }}
	if in.Root != "" {
		args = append([]string{"--root", in.Root}, args...)
	}
	cmds := append(sourceCommandsWith(d), catalogCommandsWith(d)...)
	code := run(args, app, cmds)
	return cliOut{Code: code, Stdout: stdout.String(), Stderr: stderr.String()}
}

// sourceCopyFixture копирует каталог testdata/<rel> под root/src/<name> и
// возвращает путь копии (пути в выводе тогда заменяет normRoot).
func sourceCopyFixture(t *testing.T, root, name string, rel ...string) string {
	t.Helper()
	dst := filepath.Join(root, "src", name)
	if err := fsx.CopyTree(cliTestdataDir(t, rel...), dst); err != nil {
		t.Fatal(err)
	}
	return dst
}

// sourcePackFixture упаковывает каталог dir в root/<name>.tar.gz и
// возвращает file://-URL и sha256 архива.
func sourcePackFixture(t *testing.T, root, name, dir string) (string, string) {
	t.Helper()
	var buf bytes.Buffer
	if err := source.Pack(dir, &buf, time.Unix(0, 0)); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, name+".tar.gz")
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(buf.Bytes())
	return "file://" + p, hex.EncodeToString(sum[:])
}

// sourceEnvelope разбирает конверт --json.
type sourceEnvelope struct {
	OK       bool            `json:"ok"`
	Result   json.RawMessage `json:"result"`
	Warnings []struct {
		Code    string         `json:"code"`
		Details map[string]any `json:"details"`
	} `json:"warnings"`
	Error *struct {
		Code    string         `json:"code"`
		Details map[string]any `json:"details"`
	} `json:"error"`
}

func sourceParse(t *testing.T, out cliOut) sourceEnvelope {
	t.Helper()
	var env sourceEnvelope
	if err := json.Unmarshal([]byte(out.Stdout), &env); err != nil {
		t.Fatalf("конверт: %v\n%s\nstderr:\n%s", err, out.Stdout, out.Stderr)
	}
	return env
}

func sourceErrCode(t *testing.T, out cliOut) string {
	t.Helper()
	env := sourceParse(t, out)
	if env.Error == nil {
		return ""
	}
	return env.Error.Code
}

func sourceNames(t *testing.T, l paths.Layout) []string {
	t.Helper()
	f, err := source.LoadFile(l.Sources(), false)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, s := range f.Sources {
		out = append(out, s.Name)
	}
	return out
}

// TestGoldenSource — формы result `cream source …` (§9.1, §11.4).
func TestGoldenSource(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	d := sourceTestDeps()
	in := cliIn{Root: root}
	cat := sourceCopyFixture(t, root, "catalog", "catalog")
	next := sourceCopyFixture(t, root, "catalog-next", "catalog-next")
	url, sum := sourcePackFixture(t, root, "next", next)

	steps := []struct {
		name     string
		args     []string
		wantExit int
	}{
		{name: "source-add-sync", args: []string{"source", "add", "fx", "--path", cat, "--sync", "--json"}},
		{name: "source-add-unchanged", args: []string{"--json", "source", "add", "fx", "--path", cat}},
		{name: "source-add-archive", args: []string{"source", "add", "--json", "next", "--archive", url, "--sha256", sum}},
		{name: "source-list", args: []string{"source", "list", "--json"}},
		{name: "source-show", args: []string{"source", "show", "fx", "--json"}},
		{name: "source-sync", args: []string{"source", "sync", "--json"}},
		{name: "source-remove", args: []string{"source", "remove", "next", "--json"}},
		{name: "source-show-missing", args: []string{"source", "show", "next", "--json"}, wantExit: 66},
	}
	digests := map[string]string{sum: "<NEXT_SHA256>"}
	for _, st := range steps {
		out := sourceRunCLI(t, d, in, st.args...)
		if out.Code != st.wantExit {
			t.Errorf("%s: exit = %d, want %d; stdout:\n%s\nstderr:\n%s", st.name, out.Code, st.wantExit, out.Stdout, out.Stderr)
		}
		if st.name == "source-add-sync" {
			digests[sourceResolvedHex(t, paths.Layout{Root: root}, "fx")] = "<FX_TREE>"
		}
		golden(t, st.name, sourceNormDigests(normRoot(out.Stdout, root), digests))
	}
}

// sourceResolvedHex — hex ревизии источника name из sources.lock.
func sourceResolvedHex(t *testing.T, l paths.Layout, name string) string {
	t.Helper()
	lock, err := source.ReadLock(l.SourcesLock())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range lock {
		if e.Name == name {
			return strings.TrimPrefix(e.Resolved, "sha256:")
		}
	}
	t.Fatalf("%s нет в sources.lock", name)
	return ""
}

// sourceNormDigests заменяет ревизии источников (полный hex и rev12 токена
// §12.4) на имена: они зависят от всего дерева фикстур и версии gzip, а
// golden закрепляет форму, а не содержимое testdata.
func sourceNormDigests(out []byte, digests map[string]string) []byte {
	s := string(out)
	for hex, name := range digests {
		s = strings.ReplaceAll(s, hex, name)
	}
	for hex, name := range digests {
		s = strings.ReplaceAll(s, "#"+hex[:12], "#"+name)
	}
	return []byte(s)
}

func TestSourceAdd(t *testing.T) {
	t.Parallel()
	d := sourceTestDeps()
	const zeros = "0000000000000000000000000000000000000000000000000000000000000000"

	t.Run("sync пишет sources.lock и кеш", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		l := paths.Layout{Root: root}
		cat := sourceCopyFixture(t, root, "catalog", "catalog")
		out := sourceRunCLI(t, d, cliIn{Root: root}, "source", "add", "fx", "--path", cat, "--sync")
		if out.Code != 0 {
			t.Fatalf("add --sync: %+v", out)
		}
		lock, err := source.ReadLock(l.SourcesLock())
		if err != nil || len(lock) != 1 || lock[0].Name != "fx" || !strings.HasPrefix(lock[0].Resolved, "sha256:") {
			t.Errorf("sources.lock: %+v, %v", lock, err)
		}
		if _, err := os.Stat(filepath.Join(l.SourceCache("fx"), "cream-catalog.yaml")); err != nil {
			t.Errorf("кеш: %v", err)
		}
		if !strings.Contains(out.Stdout, "fx") {
			t.Errorf("текст: %q", out.Stdout)
		}
	})

	t.Run("идемпотентность и --replace", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		l := paths.Layout{Root: root}
		cat := sourceCopyFixture(t, root, "catalog", "catalog")
		other := sourceCopyFixture(t, root, "other", "catalog-next")
		if out := sourceRunCLI(t, d, cliIn{Root: root}, "source", "add", "fx", "--path", cat); out.Code != 0 {
			t.Fatalf("add: %+v", out)
		}
		before, err := os.ReadFile(l.Sources())
		if err != nil {
			t.Fatal(err)
		}
		out := sourceRunCLI(t, d, cliIn{Root: root}, "source", "add", "fx", "--path", cat, "--json")
		if out.Code != 0 || !strings.Contains(out.Stdout, `"changed": false`) {
			t.Errorf("повтор: %+v", out)
		}
		after, _ := os.ReadFile(l.Sources())
		if !bytes.Equal(before, after) {
			t.Errorf("повтор add изменил sources.yaml")
		}
		out = sourceRunCLI(t, d, cliIn{Root: root}, "source", "add", "fx", "--path", other, "--json")
		if out.Code != 65 || sourceErrCode(t, out) != "source.conflict" {
			t.Errorf("другие поля: %+v", out)
		}
		out = sourceRunCLI(t, d, cliIn{Root: root}, "source", "add", "fx", "--path", other, "--replace", "--json")
		if out.Code != 0 || !strings.Contains(out.Stdout, `"changed": true`) {
			t.Errorf("--replace: %+v", out)
		}
		f, err := source.LoadFile(l.Sources(), false)
		if err != nil || len(f.Sources) != 1 || f.Sources[0].Path != other {
			t.Errorf("после --replace: %+v, %v", f, err)
		}
		// Кеш без sync не появился (§9.1: add не синхронизирует).
		if _, err := os.Stat(l.SourceCache("fx")); !os.IsNotExist(err) {
			t.Errorf("add без --sync создал кеш: %v", err)
		}
	})

	t.Run("archive: sha256 не совпал — записи нет", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		l := paths.Layout{Root: root}
		url, sum := sourcePackFixture(t, root, "cat", cliTestdataDir(t, "catalog-next"))
		out := sourceRunCLI(t, d, cliIn{Root: root}, "source", "add", "ar", "--archive", url, "--sha256", zeros,
			"--sync", "--json")
		if out.Code != 69 || sourceErrCode(t, out) != "source.sha256_mismatch" {
			t.Errorf("неверный sha256: %+v", out)
		}
		if names := sourceNames(t, l); len(names) != 0 {
			t.Errorf("запись сохранена при сбое sync: %v", names)
		}
		if _, err := os.Stat(l.SourceCache("ar")); !os.IsNotExist(err) {
			t.Errorf("кеш при сбое: %v", err)
		}
		out = sourceRunCLI(t, d, cliIn{Root: root}, "source", "add", "ar", "--archive", url, "--sha256",
			strings.ToUpper(sum), "--sync", "--json")
		if out.Code != 0 || !strings.Contains(out.Stdout, `"resolved": "sha256:`+sum+`"`) {
			t.Errorf("верный sha256: %+v", out)
		}
	})

	t.Run("отказы вызова", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		cat := cliTestdataDir(t, "catalog")
		tests := []struct {
			name     string
			args     []string
			wantExit int
			wantCode string
		}{
			{"голый путь в --archive", []string{"add", "a", "--archive", cat + ".tar.gz", "--sha256", zeros}, 64, "usage.source"},
			{"--git", []string{"add", "g", "--git", "https://example.org/x.git"}, 64, "usage.reserved"},
			{"без типа", []string{"add", "a"}, 64, "usage.args"},
			{"два типа", []string{"add", "a", "--path", cat, "--archive", "file:///x.tar.gz", "--sha256", zeros}, 64, "usage.args"},
			{"--sha256 без --archive", []string{"add", "a", "--path", cat, "--sha256", zeros}, 64, "usage.args"},
			{"относительный --path", []string{"add", "a", "--path", "testdata/catalog"}, 64, "usage.source"},
			{"без имени", []string{"add", "--path", cat}, 64, "usage.args"},
			{"без подкоманды", nil, 64, "usage.args"},
			{"неизвестная подкоманда", []string{"frob"}, 64, "usage.args"},
			{"pin", []string{"pin", "a"}, 64, "usage.reserved"},
			{"remove без имени", []string{"remove"}, 64, "usage.args"},
			{"remove отсутствующего", []string{"remove", "nope"}, 66, "not_found.source"},
			{"sync отсутствующего", []string{"sync", "nope"}, 66, "not_found.source"},
			{"--replace у list", []string{"list", "--replace"}, 64, "usage.flag"},
		}
		for _, tc := range tests {
			args := append([]string{"source", "--json"}, tc.args...)
			out := sourceRunCLI(t, d, cliIn{Root: root}, args...)
			if out.Code != tc.wantExit || sourceErrCode(t, out) != tc.wantCode {
				t.Errorf("%s: exit %d, code %q; want %d %q\n%s", tc.name, out.Code, sourceErrCode(t, out),
					tc.wantExit, tc.wantCode, out.Stdout)
			}
		}
		if names := sourceNames(t, paths.Layout{Root: root}); len(names) != 0 {
			t.Errorf("отказы оставили записи: %v", names)
		}
	})
}

func TestSourceRemoveForgets(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	d := sourceTestDeps()
	l := paths.Layout{Root: root}
	cat := sourceCopyFixture(t, root, "catalog", "catalog")
	for _, args := range [][]string{
		{"source", "add", "fx", "--path", cat, "--sync"},
		{"source", "add", "fy", "--path", cat, "--sync"},
		{"source", "remove", "fx"},
	} {
		if out := sourceRunCLI(t, d, cliIn{Root: root}, args...); out.Code != 0 {
			t.Fatalf("%v: %+v", args, out)
		}
	}
	if names := sourceNames(t, l); len(names) != 1 || names[0] != "fy" {
		t.Errorf("sources.yaml: %v", names)
	}
	if _, err := os.Stat(l.SourceCache("fx")); !os.IsNotExist(err) {
		t.Errorf("кеш fx остался: %v", err)
	}
	lock, err := source.ReadLock(l.SourcesLock())
	if err != nil || len(lock) != 1 || lock[0].Name != "fy" {
		t.Errorf("sources.lock: %+v, %v", lock, err)
	}
}

// Синхронизация каталога, где расширению нужен cream новее, — успех с
// catalog.cream_newer (Р22); поставить его из кеша нельзя — 68.
func TestSourceSyncCreamNewer(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	d := sourceTestDeps()
	cat := sourceCopyFixture(t, root, "newer", "invalid", "too-new-cream")
	out := sourceRunCLI(t, d, cliIn{Root: root}, "source", "add", "nw", "--path", cat, "--sync", "--json")
	if out.Code != 0 {
		t.Fatalf("add --sync: %+v", out)
	}
	env := sourceParse(t, out)
	found := false
	for _, w := range env.Warnings {
		if w.Code == "catalog.cream_newer" && w.Details["ext"] == "too-new-cream" {
			found = true
		}
	}
	if !found {
		t.Errorf("нет catalog.cream_newer: %s", out.Stdout)
	}
	if out := sourceRunCLI(t, d, cliIn{Root: root}, "source", "sync", "--json"); out.Code != 0 {
		t.Errorf("sync: %+v", out)
	}
	out = sourceRunCLI(t, d, cliIn{Root: root}, "resolve", "--json", "--phase", "build", "--family", "deb",
		"too-new-cream")
	if out.Code != 68 || sourceErrCode(t, out) != "platform.cream_version" {
		t.Errorf("resolve: %+v", out)
	}
}

// Синхронизация продолжается после сбоя одного источника; код — первого сбоя.
func TestSourceSyncPartialFailure(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	d := sourceTestDeps()
	l := paths.Layout{Root: root}
	cat := sourceCopyFixture(t, root, "catalog", "catalog")
	gone := sourceCopyFixture(t, root, "gone", "catalog-next")
	for _, args := range [][]string{
		{"source", "add", "a", "--path", gone},
		{"source", "add", "b", "--path", cat},
	} {
		if out := sourceRunCLI(t, d, cliIn{Root: root}, args...); out.Code != 0 {
			t.Fatalf("%v: %+v", args, out)
		}
	}
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	out := sourceRunCLI(t, d, cliIn{Root: root}, "source", "sync", "--json")
	if out.Code != 69 || sourceErrCode(t, out) != "source.path" {
		t.Errorf("sync: %+v", out)
	}
	if _, err := os.Stat(l.SourceCache("b")); err != nil {
		t.Errorf("b не синхронизирован после сбоя a: %v", err)
	}
}

// На настоящем корне изменение источников машины требует root (77).
func TestSourceNeedsRoot(t *testing.T) {
	t.Parallel()
	d := sourceTestDeps()
	for _, args := range [][]string{
		{"source", "add", "x", "--path", "/nonexistent"},
		{"source", "remove", "x"},
		{"source", "sync"},
	} {
		out := sourceRunCLI(t, d, cliIn{Euid: 1000}, append(args, "--json")...)
		if out.Code != 77 || sourceErrCode(t, out) != "perm.root" {
			t.Errorf("%v: %+v", args, out)
		}
	}
}

// Свои sources.yaml и кеш (--sources, --cache, как у bisquite) — без root и
// без lock/global машины.
func TestSourcePrivateNoRoot(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	d := sourceTestDeps()
	cat := filepath.Join(dir, "catalog")
	if err := fsx.CopyTree(cliTestdataDir(t, "catalog"), cat); err != nil {
		t.Fatal(err)
	}
	sources := filepath.Join(dir, "sources.yaml")
	cache := filepath.Join(dir, "cache")
	out := sourceRunCLI(t, d, cliIn{Euid: 1000}, "--sources", sources, "--cache", cache,
		"source", "add", "fx", "--path", cat, "--sync", "--json")
	if out.Code != 0 {
		t.Fatalf("add --sync: %+v", out)
	}
	if _, err := os.Stat(filepath.Join(cache, "sources.lock")); err != nil {
		t.Errorf("sources.lock в --cache (Р12): %v", err)
	}
}
