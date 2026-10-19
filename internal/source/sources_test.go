package source

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iamletenkov/cream/internal/cerr"
)

const hexA = "9f2c000000000000000000000000000000000000000000000000000000000001"

func archiveSrc(name string) Source {
	return Source{Name: name, Type: TypeArchive, URL: "https://portal.example/c.tar.gz", SHA256: hexA}
}

func TestAdd(t *testing.T) {
	t.Parallel()
	base := func() *File {
		return &File{Sources: []Source{archiveSrc("stvor"), {Name: "dev", Type: TypePath, Path: "/home/me/ext"}}}
	}
	tests := []struct {
		name        string
		src         Source
		replace     bool
		wantChanged bool
		wantExit    int
		wantCode    string
		wantNames   []string
	}{
		{name: "same fields is idempotent", src: archiveSrc("stvor"), wantNames: []string{"stvor", "dev"}},
		{name: "upper-case sha256 is the same pin", src: Source{Name: "stvor", Type: TypeArchive,
			URL: "https://portal.example/c.tar.gz", SHA256: strings.ToUpper(hexA)}, wantNames: []string{"stvor", "dev"}},
		{name: "other fields without replace", src: Source{Name: "stvor", Type: TypeArchive,
			URL: "https://portal.example/d.tar.gz", SHA256: hexA}, wantExit: cerr.ExitData, wantCode: "source.conflict"},
		{name: "other fields with replace keeps position", src: Source{Name: "stvor", Type: TypePath, Path: "/srv/c"},
			replace: true, wantChanged: true, wantNames: []string{"stvor", "dev"}},
		{name: "new source goes last (lowest priority)", src: archiveSrc("extra"), wantChanged: true,
			wantNames: []string{"stvor", "dev", "extra"}},
		{name: "file:// archive", src: Source{Name: "off", Type: TypeArchive, URL: "file:///srv/c.tar.gz", SHA256: hexA},
			wantChanged: true, wantNames: []string{"stvor", "dev", "off"}},
		{name: "bare path instead of file://", src: Source{Name: "off", Type: TypeArchive, URL: "/srv/c.tar.gz", SHA256: hexA},
			wantExit: cerr.ExitUsage, wantCode: "usage.source"},
		{name: "http is refused", src: Source{Name: "off", Type: TypeArchive, URL: "http://x/c.tar.gz", SHA256: hexA},
			wantExit: cerr.ExitUsage, wantCode: "usage.source"},
		{name: "file:// with host", src: Source{Name: "off", Type: TypeArchive, URL: "file://host/c.tar.gz", SHA256: hexA},
			wantExit: cerr.ExitUsage, wantCode: "usage.source"},
		{name: "archive without sha256", src: Source{Name: "off", Type: TypeArchive, URL: "https://x/c.tar.gz"},
			wantExit: cerr.ExitUsage, wantCode: "usage.source"},
		{name: "short sha256", src: Source{Name: "off", Type: TypeArchive, URL: "https://x/c.tar.gz", SHA256: "abc"},
			wantExit: cerr.ExitUsage, wantCode: "usage.source"},
		{name: "archive with path", src: Source{Name: "off", Type: TypeArchive, URL: "https://x/c.tar.gz", SHA256: hexA,
			Path: "/x"}, wantExit: cerr.ExitUsage, wantCode: "usage.source"},
		{name: "relative path", src: Source{Name: "p", Type: TypePath, Path: "ext"},
			wantExit: cerr.ExitUsage, wantCode: "usage.source"},
		{name: "path with url", src: Source{Name: "p", Type: TypePath, Path: "/x", URL: "https://x"},
			wantExit: cerr.ExitUsage, wantCode: "usage.source"},
		{name: "git is reserved", src: Source{Name: "core", Type: "git", URL: "https://github.com/x/y.git"},
			wantExit: cerr.ExitData, wantCode: "source.reserved"},
		{name: "unknown type", src: Source{Name: "o", Type: "oci", URL: "https://x"},
			wantExit: cerr.ExitUsage, wantCode: "usage.source"},
		{name: "bad name", src: Source{Name: "Bad/Name", Type: TypePath, Path: "/x"},
			wantExit: cerr.ExitUsage, wantCode: "usage.source"},
		{name: "name starting with dot", src: Source{Name: ".x", Type: TypePath, Path: "/x"},
			wantExit: cerr.ExitUsage, wantCode: "usage.source"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := base()
			changed, err := f.Add(tc.src, tc.replace)
			if tc.wantExit != 0 {
				wantCerr(t, err, tc.wantExit, tc.wantCode)
				if len(f.Sources) != 2 || f.Sources[0] != archiveSrc("stvor") {
					t.Errorf("файл изменён при отказе: %+v", f.Sources)
				}
				return
			}
			if err != nil {
				t.Fatalf("Add: %v", err)
			}
			if changed != tc.wantChanged {
				t.Errorf("changed = %v, ждали %v", changed, tc.wantChanged)
			}
			var names []string
			for _, s := range f.Sources {
				names = append(names, s.Name)
			}
			if strings.Join(names, ",") != strings.Join(tc.wantNames, ",") {
				t.Errorf("порядок %v, ждали %v", names, tc.wantNames)
			}
			if tc.replace {
				if got := f.Sources[0]; got != tc.src {
					t.Errorf("после replace %+v, ждали %+v", got, tc.src)
				}
			}
		})
	}
}

func TestRemoveAndGet(t *testing.T) {
	t.Parallel()
	f := &File{Sources: []Source{archiveSrc("a"), archiveSrc("b")}}
	if s, ok := f.Get("b"); !ok || s.Name != "b" {
		t.Errorf("Get(b) = %+v, %v", s, ok)
	}
	if err := f.Remove("a"); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.Get("a"); ok || len(f.Sources) != 1 {
		t.Errorf("после Remove: %+v", f.Sources)
	}
	wantCerr(t, f.Remove("a"), cerr.ExitNotFound, "not_found.source")
}

func TestLoadSaveRoundTrip(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "etc", "cream", "sources.yaml")
	f := &File{Sources: []Source{archiveSrc("stvor"), {Name: "dev", Type: TypePath, Path: "/home/me/ext"}}}
	if err := f.Save(p); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o644 {
		t.Errorf("режим %v, ждали 0644", st.Mode().Perm())
	}
	got, err := LoadFile(p, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Sources) != 2 || got.Sources[0] != f.Sources[0] || got.Sources[1] != f.Sources[1] {
		t.Errorf("прочитано %+v", got.Sources)
	}
	if want := "sources:\n  - name: stvor\n    type: archive\n"; !strings.HasPrefix(readFile(t, p), want) {
		t.Errorf("формат файла:\n%s", readFile(t, p))
	}
}

func TestLoadFile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		body     string
		wantExit int
		wantCode string
		wantN    int
	}{
		{name: "empty file", body: "", wantN: 0},
		{name: "spec example without git", body: "sources:\n  - name: stvor\n    type: archive\n" +
			"    url: https://portal.example/c.tar.gz\n    sha256: " + hexA + "\n" +
			"  - name: dev\n    type: path\n    path: /home/me/cream-extensions\n", wantN: 2},
		{name: "x- extension field is accepted", body: "sources:\n  - name: dev\n    type: path\n    path: /x\n" +
			"    x-signers: [a]\n", wantN: 1},
		{name: "git is reserved", body: "sources:\n  - name: core\n    type: git\n" +
			"    url: https://github.com/x/y.git\n    ref: v4.0.0\n", wantExit: cerr.ExitData, wantCode: "source.reserved"},
		{name: "unknown field", body: "sources:\n  - name: dev\n    type: path\n    path: /x\n    ref: v1\n",
			wantExit: cerr.ExitData, wantCode: "source.invalid"},
		{name: "unknown top-level key", body: "mirrors: []\n", wantExit: cerr.ExitData, wantCode: "source.invalid"},
		{name: "duplicate name", body: "sources:\n  - name: dev\n    type: path\n    path: /x\n" +
			"  - name: dev\n    type: path\n    path: /y\n", wantExit: cerr.ExitData, wantCode: "source.invalid"},
		{name: "bare path url", body: "sources:\n  - name: s\n    type: archive\n    url: /c.tar.gz\n" +
			"    sha256: " + hexA + "\n", wantExit: cerr.ExitData, wantCode: "source.invalid"},
		{name: "not yaml", body: "sources: [\n", wantExit: cerr.ExitData, wantCode: "source.invalid"},
		{name: "non-string value", body: "sources:\n  - name: dev\n    type: path\n    path: [a]\n",
			wantExit: cerr.ExitData, wantCode: "source.invalid"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := filepath.Join(t.TempDir(), "sources.yaml")
			if err := os.WriteFile(p, []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			f, err := LoadFile(p, false)
			if tc.wantExit != 0 {
				wantCerr(t, err, tc.wantExit, tc.wantCode)
				return
			}
			if err != nil {
				t.Fatalf("LoadFile: %v", err)
			}
			if len(f.Sources) != tc.wantN {
				t.Errorf("источников %d, ждали %d", len(f.Sources), tc.wantN)
			}
		})
	}
}

func TestLoadFileMissingIsEmpty(t *testing.T) {
	t.Parallel()
	f, err := LoadFile(filepath.Join(t.TempDir(), "none.yaml"), false)
	if err != nil || f == nil || len(f.Sources) != 0 {
		t.Fatalf("LoadFile(нет файла) = %+v, %v", f, err)
	}
}

func TestLoadFileCheckPerm(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("под root файл во временном каталоге доверенный")
	}
	p := filepath.Join(t.TempDir(), "sources.yaml")
	if err := os.WriteFile(p, []byte("sources: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Файл и каталоги принадлежат не root: при проверке прав — отказ 77.
	_, err := LoadFile(p, true)
	wantCerr(t, err, cerr.ExitPerm, "perm.sources")
}
