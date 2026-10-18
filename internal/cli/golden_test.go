package cli

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// update переписывает golden-файлы по текущему выводу: make test-golden.
var update = flag.Bool("update", false, "переписать testdata/golden/*.json")

// golden сверяет got с testdata/golden/<name>.json; с -update — записывает файл.
// Им пользуются golden-тесты всех команд (T12–T17): формы result — контракт §11.4.
func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name+".json")
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden %s: %v (make test-golden создаст файл)", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("golden %s mismatch (make test-golden — переписать)\n--- got\n%s\n--- want\n%s", path, got, want)
	}
}

// runReal запускает настоящий набор команд и возвращает stdout.
func runReal(t *testing.T, args ...string) (int, []byte) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	app := App{Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr,
		Env: func(string) string { return "" }, Euid: func() int { return 0 },
		Now: func() time.Time { return time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC) }}
	return Run(args, app), stdout.Bytes()
}

func TestGoldenVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		args     []string
		wantExit int
	}{
		{name: "version", args: []string{"version", "--json"}, wantExit: 0},
		{name: "version-usage", args: []string{"--json", "version", "extra"}, wantExit: 64},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			code, out := runReal(t, tc.args...)
			if code != tc.wantExit {
				t.Errorf("exit = %d, want %d", code, tc.wantExit)
			}
			golden(t, tc.name, out)
		})
	}
}
