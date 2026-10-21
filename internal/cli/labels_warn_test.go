package cli

// Доставка предупреждения labels.write_failed до конверта --json (спека
// bisquite-bundled-cream §8.2; замечание ревью Task 8): сбой записи
// /etc/image-labels.d/cream.json — предупреждение, операция успешна.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type labelsEnvelope struct {
	OK       bool `json:"ok"`
	Warnings []struct {
		Code string `json:"code"`
	} `json:"warnings"`
	Error any `json:"error"`
}

func labelsWarnCodes(t *testing.T, stdout string) (labelsEnvelope, map[string]int) {
	t.Helper()
	var env labelsEnvelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("конверт --json: %v\n%s", err, stdout)
	}
	codes := map[string]int{}
	for _, w := range env.Warnings {
		codes[w.Code]++
	}
	return env, codes
}

// TestInstallLabelsWriteFailedWarning — /etc/image-labels.d занят обычным
// файлом: install успешен (0, ok: true, error: null), в warnings ровно одно
// labels.write_failed, расширение в реестре.
func TestInstallLabelsWriteFailedWarning(t *testing.T) {
	t.Parallel()
	root := installCLIRoot(t)
	writeRootFile(t, root, "/etc/image-labels.d", "не каталог\n", 0o644)

	out := runCLI(t, cliIn{Root: root}, "install", "--json", "noop")
	if out.Code != 0 {
		t.Fatalf("exit = %d, want 0; stderr:\n%s", out.Code, out.Stderr)
	}
	env, codes := labelsWarnCodes(t, out.Stdout)
	if !env.OK || env.Error != nil {
		t.Errorf("ok = %v, error = %v; want ok: true, error: null", env.OK, env.Error)
	}
	if codes["labels.write_failed"] != 1 {
		t.Errorf("warnings = %v, want одно labels.write_failed\n%s", codes, out.Stdout)
	}
	if _, err := os.Stat(filepath.Join(root, "var/lib/cream/installed/noop.json")); err != nil {
		t.Errorf("запись реестра noop: %v", err)
	}
}

// TestInstallLabelsWritten — контроль: тот же install без помехи пишет
// cream.json и предупреждения labels.write_failed не даёт.
func TestInstallLabelsWritten(t *testing.T) {
	t.Parallel()
	root := installCLIRoot(t)

	out := runCLI(t, cliIn{Root: root}, "install", "--json", "noop")
	if out.Code != 0 {
		t.Fatalf("exit = %d, want 0; stderr:\n%s", out.Code, out.Stderr)
	}
	if _, codes := labelsWarnCodes(t, out.Stdout); codes["labels.write_failed"] != 0 {
		t.Errorf("лишнее labels.write_failed: %s", out.Stdout)
	}
	b, err := os.ReadFile(filepath.Join(root, "etc/image-labels.d/cream.json"))
	if err != nil {
		t.Fatalf("cream.json: %v", err)
	}
	var labels map[string]string
	if err := json.Unmarshal(b, &labels); err != nil {
		t.Fatalf("cream.json: %v\n%s", err, b)
	}
	var exts []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(labels["cream.extensions"]), &exts); err != nil || len(exts) != 1 || exts[0].Name != "noop" {
		t.Errorf("cream.extensions = %q (%v), want [noop]", labels["cream.extensions"], err)
	}
}
